package probe

import (
	"testing"
	"time"

	"github.com/Resinat/Resin/internal/config"
	"github.com/Resinat/Resin/internal/node"
	"github.com/Resinat/Resin/internal/topology"
)

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
