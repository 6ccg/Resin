package probe

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Resinat/Resin/internal/config"
	"github.com/Resinat/Resin/internal/node"
	"github.com/Resinat/Resin/internal/topology"
)

func TestCancelledProbeWaitDoesNotFailNode(t *testing.T) {
	pool := topology.NewGlobalNodePool(topology.PoolConfig{MaxConsecutiveFailures: func() int { return 3 }})
	hash := node.HashFromRawOptions([]byte(`{"type":"cancelled-wait"}`))
	pool.AddNodeFromSub(hash, nil, "sub")
	entry, _ := pool.GetEntry(hash)
	storeOutbound(entry)
	started, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	manager := NewProbeManager(ProbeConfig{Pool: pool, Fetcher: func(node.Hash, string) ([]byte, time.Duration, error) {
		close(started)
		<-release
		return nil, time.Millisecond, nil
	}})
	defer manager.Stop()
	go func() { _, err := manager.ProbeLatencySync(hash); done <- err }()
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := manager.probeLatencySync(ctx, true, hash)
	if !errors.Is(err, errProbeCancelled) {
		close(release)
		t.Fatalf("unexpected wait error: %v", err)
	}
	if entry.FailureCount.Load() != 0 || len(entry.Diagnostics().Checks) != 0 {
		close(release)
		t.Fatal("cancelled queue wait was recorded as a node failure")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestEntryResolutionRunsInsideProbeBudget(t *testing.T) {
	pool := topology.NewGlobalNodePool(topology.PoolConfig{MaxConsecutiveFailures: func() int { return 3 }})
	var hashes []node.Hash
	for _, raw := range []string{`{"type":"http","server":"entry.example","server_port":8080}`, `{"type":"http","server":"other.example","server_port":8080}`} {
		h := node.HashFromRawOptions([]byte(raw))
		pool.AddNodeFromSub(h, []byte(raw), "sub")
		e, _ := pool.GetEntry(h)
		storeOutbound(e)
		hashes = append(hashes, h)
	}
	cfg := config.NewDefaultRuntimeConfig()
	cfg.ProbeConcurrency = 1
	started, release, fetched := make(chan struct{}), make(chan struct{}), make(chan struct{}, 2)
	var once sync.Once
	defer once.Do(func() { close(release) })
	manager := NewProbeManager(ProbeConfig{Pool: pool, RuntimeConfig: func() *config.RuntimeConfig { return cfg }, ResolveEntry: func(string) ([]string, error) { close(started); <-release; return []string{"203.0.113.10"}, nil }, Fetcher: func(node.Hash, string) ([]byte, time.Duration, error) {
		fetched <- struct{}{}
		return []byte("ip=203.0.113.20"), time.Millisecond, nil
	}})
	defer manager.Stop()
	done := make(chan error, 2)
	go func() { _, err := manager.ProbeEgressSync(hashes[0]); done <- err }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("entry resolution did not start")
	}
	go func() { _, err := manager.ProbeLatencySync(hashes[1]); done <- err }()
	select {
	case <-fetched:
		t.Fatal("entry DNS resolution bypassed the global probe budget")
	case <-time.After(50 * time.Millisecond):
	}
	once.Do(func() { close(release) })
	for i := 0; i < 2; i++ {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("probe did not finish")
		}
	}
}

func TestManualProbesRespectGlobalAndPerNodeLimits(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		limit                int
		sameNode, concurrent bool
	}{
		{"global_limit", 1, false, false},
		{"one_request_per_node", 2, true, false},
		{"different_nodes_can_overlap", 2, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool := topology.NewGlobalNodePool(topology.PoolConfig{MaxConsecutiveFailures: func() int { return 3 }})
			a := node.HashFromRawOptions([]byte(`{"type":"a"}`))
			b := node.HashFromRawOptions([]byte(`{"type":"b"}`))
			for _, h := range []node.Hash{a, b} {
				pool.AddNodeFromSub(h, nil, "sub")
				entry, _ := pool.GetEntry(h)
				storeOutbound(entry)
			}
			if tc.sameNode {
				b = a
			}
			cfg := config.NewDefaultRuntimeConfig()
			cfg.ProbeConcurrency = tc.limit
			firstStarted, secondStarted, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			defer releaseOnce.Do(func() { close(release) })
			var calls atomic.Int32
			manager := NewProbeManager(ProbeConfig{Pool: pool, RuntimeConfig: func() *config.RuntimeConfig { return cfg }, Fetcher: func(node.Hash, string) ([]byte, time.Duration, error) {
				if calls.Add(1) == 1 {
					close(firstStarted)
					<-release
				} else {
					close(secondStarted)
				}
				return nil, time.Millisecond, nil
			}})
			defer manager.Stop()
			done := make(chan error, 2)
			go func() { _, err := manager.ProbeLatencySync(a); done <- err }()
			<-firstStarted
			go func() { _, err := manager.ProbeLatencySync(b); done <- err }()
			if tc.concurrent {
				select {
				case <-secondStarted:
				case <-time.After(time.Second):
					t.Fatal("different nodes were serialized")
				}
			} else {
				select {
				case <-secondStarted:
					t.Fatal("probe bypassed its concurrency limit")
				case <-time.After(50 * time.Millisecond):
				}
			}
			releaseOnce.Do(func() { close(release) })
			for i := 0; i < 2; i++ {
				select {
				case err := <-done:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(time.Second):
					t.Fatal("probe did not finish")
				}
			}
		})
	}
}

func TestDisabledAutomaticProbesSkipAlreadyQueuedTasks(t *testing.T) {
	pool := topology.NewGlobalNodePool(topology.PoolConfig{MaxConsecutiveFailures: func() int { return 3 }})
	hash := node.HashFromRawOptions([]byte(`{"type":"queued"}`))
	pool.AddNodeFromSub(hash, nil, "sub")
	entry, _ := pool.GetEntry(hash)
	storeOutbound(entry)
	cfg := config.NewDefaultRuntimeConfig()
	var calls int
	manager := NewProbeManager(ProbeConfig{Pool: pool, RuntimeConfig: func() *config.RuntimeConfig { return cfg }, Fetcher: func(node.Hash, string) ([]byte, time.Duration, error) { calls++; return nil, time.Millisecond, nil }})
	defer manager.Stop()
	cfg.ProbeEnabled = false
	manager.executeTask(probeTask{key: probeTaskKey{hash: hash, kind: probeTaskKindLatency}})
	if calls != 0 {
		t.Fatal("disabling automatic probes still executes queued work")
	}
}

func TestEgressMetadataFailureDoesNotChangeConnectivityHealth(t *testing.T) {
	for _, broken := range []bool{false, true} {
		pool := topology.NewGlobalNodePool(topology.PoolConfig{MaxConsecutiveFailures: func() int { return 3 }})
		hash := node.HashFromRawOptions([]byte(`{"type":"metadata"}`))
		pool.AddNodeFromSub(hash, nil, "sub")
		entry, _ := pool.GetEntry(hash)
		storeOutbound(entry)
		entry.RecordProbe(node.ProbeObservation{Kind: "latency", Success: true, CheckedAt: time.Now()})
		entry.FailureCount.Store(2)
		entry.CircuitOpenSince.Store(0)
		if broken {
			entry.CircuitOpenSince.Store(time.Now().UnixNano())
		}
		beforeValidity := entry.Validity(true, time.Hour)
		manager := NewProbeManager(ProbeConfig{Pool: pool, Fetcher: func(node.Hash, string) ([]byte, time.Duration, error) {
			return []byte("<html>target unavailable</html>"), time.Millisecond, nil
		}})
		_, _, err := manager.performEgressProbe(hash)
		manager.Stop()
		if err == nil {
			t.Fatal("missing trace IP was accepted")
		}
		if entry.FailureCount.Load() != 2 || entry.IsCircuitOpen() != broken {
			t.Fatal("metadata response changed connectivity health")
		}
		if got := entry.Validity(true, time.Hour); got != beforeValidity {
			t.Fatalf("metadata failure changed validity: %s", got)
		}
		if entry.Diagnostics().Stability.Samples != 1 {
			t.Fatal("metadata failure polluted connectivity statistics")
		}
	}
}
