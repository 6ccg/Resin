package probe

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Resinat/Resin/internal/config"
	"github.com/Resinat/Resin/internal/node"
	"github.com/Resinat/Resin/internal/topology"
)

func TestAttributeFailuresBackOffAndResetOnSuccess(t *testing.T) {
	for _, tc := range []struct {
		name     string
		failures int
		age      time.Duration
		due      bool
	}{
		{"first_failure", 1, 90 * time.Second, true},
		{"repeated_failure", 3, 90 * time.Second, false},
		{"capped_before_due", 20, 59 * time.Minute, false},
		{"capped_due", 20, 61 * time.Minute, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool := topology.NewGlobalNodePool(topology.PoolConfig{MaxConsecutiveFailures: func() int { return 3 }})
			hash := node.HashFromRawOptions([]byte(`{"type":"attribute-backoff"}`))
			pool.AddNodeFromSub(hash, nil, "sub")
			entry, _ := pool.GetEntry(hash)
			storeOutbound(entry)
			entry.CircuitOpenSince.Store(0)
			attempt := time.Now().Add(-tc.age)
			data, _ := json.Marshal(node.Inspection{AttributesAttemptAt: &attempt, AttributesError: "provider timeout", AttributesFailures: tc.failures})
			if err := entry.RestoreInspection(string(data)); err != nil {
				t.Fatal(err)
			}
			cfg := config.NewDefaultRuntimeConfig()
			manager := NewProbeManager(ProbeConfig{Pool: pool, RuntimeConfig: func() *config.RuntimeConfig { return cfg }})
			defer manager.Stop()
			manager.scanAttributes()
			if got := manager.taskQueue.normal.len() > 0; got != tc.due {
				t.Fatalf("scheduled=%v, want %v", got, tc.due)
			}
			entry.SetAttributes(&node.EgressAttributes{}, nil)
			if entry.Inspection().AttributesFailures != 0 {
				t.Fatal("successful source response did not reset backoff")
			}
		})
	}
}

func TestProbeScheduling_KeepaliveAndRecovery(t *testing.T) {
	for _, test := range []struct {
		name                       string
		enabled, keepalive, broken bool
		failures                   int
		want                       bool
	}{
		{"disabled", false, true, false, 0, false},
		{"regular_not_due", true, false, false, 0, false},
		{"keepalive_due", true, true, false, 0, true},
		{"failed_backoff", true, false, true, 5, false},
		{"failed_retry_due", true, false, true, 3, true},
		{"transient_failure_confirmation_due", true, false, false, 1, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			pool := topology.NewGlobalNodePool(topology.PoolConfig{MaxConsecutiveFailures: func() int { return 3 }})
			hash := node.HashFromRawOptions([]byte(`{"type":"policy"}`))
			pool.AddNodeFromSub(hash, nil, "sub")
			entry, _ := pool.GetEntry(hash)
			storeOutbound(entry)
			entry.CircuitOpenSince.Store(0)
			if test.broken {
				entry.CircuitOpenSince.Store(time.Now().UnixNano())
			}
			entry.FailureCount.Store(int32(test.failures))
			entry.LastLatencyProbeAttempt.Store(time.Now().Add(-90 * time.Second).UnixNano())
			entry.RecordProbe(node.ProbeObservation{Kind: "latency", Success: !test.broken && test.failures == 0, CheckedAt: time.Now().Add(-90 * time.Second)})
			cfg := config.NewDefaultRuntimeConfig()
			cfg.ProbeEnabled = test.enabled
			cfg.KeepaliveEnabled = test.keepalive
			cfg.KeepaliveInterval = config.Duration(time.Minute)
			manager := NewProbeManager(ProbeConfig{Pool: pool, RuntimeConfig: func() *config.RuntimeConfig { return cfg }})
			manager.scanLatency()
			if got := manager.taskQueue.normal.len()+manager.taskQueue.high.len() > 0; got != test.want {
				t.Fatalf("scheduled=%v, want %v", got, test.want)
			}
		})
	}
}

func TestConfirmationIsNotPostponedByPassiveTraffic(t *testing.T) {
	pool := topology.NewGlobalNodePool(topology.PoolConfig{MaxConsecutiveFailures: func() int { return 3 }})
	hash := node.HashFromRawOptions([]byte(`{"type":"active-confirmation"}`))
	pool.AddNodeFromSub(hash, nil, "sub")
	entry, _ := pool.GetEntry(hash)
	storeOutbound(entry)
	entry.CircuitOpenSince.Store(0)
	entry.RecordProbe(node.ProbeObservation{Kind: "latency", Success: false, CheckedAt: time.Now().Add(-90 * time.Second)})
	entry.LastLatencyProbeAttempt.Store(time.Now().UnixNano())
	manager := NewProbeManager(ProbeConfig{Pool: pool})
	defer manager.Stop()
	manager.scanLatency()
	if manager.taskQueue.high.len() != 1 {
		t.Fatal("recent passive latency postponed a failed active-check confirmation")
	}
}

func TestPeriodicScanDoesNotRepeatQueuedOrRunningProbe(t *testing.T) {
	for _, kind := range []probeTaskKind{probeTaskKindLatency, probeTaskKindEgress, probeTaskKindAttributes} {
		t.Run(map[probeTaskKind]string{probeTaskKindLatency: "latency", probeTaskKindEgress: "egress", probeTaskKindAttributes: "attributes"}[kind], func(t *testing.T) {
			pool := topology.NewGlobalNodePool(topology.PoolConfig{MaxConsecutiveFailures: func() int { return 3 }})
			hash := node.HashFromRawOptions([]byte(`{"type":"periodic"}`))
			pool.AddNodeFromSub(hash, nil, "sub")
			entry, _ := pool.GetEntry(hash)
			storeOutbound(entry)
			entry.CircuitOpenSince.Store(0)
			cfg := config.NewDefaultRuntimeConfig()
			manager := NewProbeManager(ProbeConfig{Pool: pool, RuntimeConfig: func() *config.RuntimeConfig { return cfg }})
			defer manager.Stop()
			scan := manager.scanLatency
			if kind == probeTaskKindEgress {
				scan = manager.scanEgress
			}
			if kind == probeTaskKindAttributes {
				scan = manager.scanAttributes
			}
			scan()
			scan()
			key := probeTaskKey{hash: hash, kind: kind}
			state, ok := manager.taskStates.Load(key)
			if !ok {
				t.Fatal("periodic probe not scheduled")
			}
			if state.flags.Load()&taskFlagDirty != 0 {
				t.Fatal("repeated scan schedules an unnecessary extra probe")
			}
			manager.taskQueue.Dequeue()
			if _, ok := manager.markTaskRunning(key); !ok {
				t.Fatal("cannot start scheduled probe")
			}
			scan()
			manager.finishTask(key, state)
			if manager.taskQueue.high.len()+manager.taskQueue.normal.len() != 0 {
				t.Fatal("scan during an in-flight probe schedules an unnecessary repeat")
			}
		})
	}
}

func TestScanLatencyPrioritizesHealthyAndUntestedNodes(t *testing.T) {
	pool := topology.NewGlobalNodePool(topology.PoolConfig{MaxConsecutiveFailures: func() int { return 3 }})
	for _, kind := range []string{"healthy", "untested", "recovery"} {
		hash := node.HashFromRawOptions([]byte(`{"type":"` + kind + `"}`))
		pool.AddNodeFromSub(hash, nil, "sub")
		entry, _ := pool.GetEntry(hash)
		storeOutbound(entry)
		entry.CircuitOpenSince.Store(0)
		if kind != "healthy" {
			entry.CircuitOpenSince.Store(time.Now().UnixNano())
		}
		if kind == "recovery" {
			entry.FailureCount.Store(3)
		}
	}
	manager := NewProbeManager(ProbeConfig{Pool: pool})
	defer manager.Stop()
	manager.scanLatency()
	if manager.taskQueue.high.len() != 2 || manager.taskQueue.normal.len() != 1 {
		t.Fatalf("healthy/untested nodes must have priority over failed-node recovery: high=%d normal=%d", manager.taskQueue.high.len(), manager.taskQueue.normal.len())
	}
}
