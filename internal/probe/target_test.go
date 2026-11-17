package probe

import (
	"errors"
	"testing"
	"time"

	"github.com/Resinat/Resin/internal/node"
	"github.com/Resinat/Resin/internal/topology"
)

func TestTargetProbe_ReportsStatusAndKeepsGlobalHealthIndependent(t *testing.T) {
	pool := topology.NewGlobalNodePool(topology.PoolConfig{MaxConsecutiveFailures: func() int { return 3 }})
	hash := node.HashFromRawOptions([]byte(`{"type":"target"}`))
	pool.AddNodeFromSub(hash, nil, "sub")
	entry, _ := pool.GetEntry(hash)
	storeOutbound(entry)
	entry.CircuitOpenSince.Store(0)
	entry.FailureCount.Store(2)
	var fetchErr error
	manager := NewProbeManager(ProbeConfig{
		Pool: pool,
		TargetFetcher: func(_ node.Hash, target string) (int, error) {
			if target != "https://example.com/health" {
				t.Fatalf("unexpected target: %s", target)
			}
			return 403, fetchErr
		},
	})
	result, err := manager.ProbeTargetSync(hash, "https://example.com/health", nil)
	if err != nil || result.Success || result.HTTPStatus != 403 {
		t.Fatalf("expected blocked target, got %+v, %v", result, err)
	}
	result, err = manager.ProbeTargetSync(hash, "https://example.com/health", []int{403})
	if err != nil || !result.Success {
		t.Fatalf("expected configured status to pass: %+v, %v", result, err)
	}
	fetchErr = errors.New("connection timeout")
	result, err = manager.ProbeTargetSync(hash, "https://example.com/health", nil)
	if err != nil || result.Success || result.Error != "connection timeout" {
		t.Fatalf("expected recorded network failure: %+v, %v", result, err)
	}
	if entry.FailureCount.Load() != 2 || entry.IsCircuitOpen() || entry.Diagnostics().Stability.Samples != 0 {
		t.Fatal("target results changed global health or general stability")
	}
}

func TestLatencyProbes_StoreResultsAndRunConfiguredTargets(t *testing.T) {
	pool := topology.NewGlobalNodePool(topology.PoolConfig{MaxConsecutiveFailures: func() int { return 3 }})
	hash := node.HashFromRawOptions([]byte(`{"type":"latency"}`))
	pool.AddNodeFromSub(hash, nil, "sub")
	entry, _ := pool.GetEntry(hash)
	storeOutbound(entry)
	checked := make(chan string, 1)
	manager := NewProbeManager(ProbeConfig{
		Pool: pool, Concurrency: 1,
		Fetcher:             func(node.Hash, string) ([]byte, time.Duration, error) { return nil, time.Millisecond, nil },
		ReachabilityTargets: func() []string { return []string{"https://example.com"} },
		TargetFetcher:       func(_ node.Hash, target string) (int, error) { checked <- target; return 204, nil },
	})
	manager.Start()
	defer manager.Stop()
	manager.TriggerImmediateLatencyProbe(hash)
	select {
	case target := <-checked:
		if target != "https://example.com" {
			t.Fatalf("wrong configured target: %s", target)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("configured target did not run after latency probe")
	}
	if stats := entry.Diagnostics().Stability; stats.Samples != 1 || stats.Successes != 1 {
		t.Fatalf("latency probe did not update stability: %+v", stats)
	}
}

func TestLatencyRetest_ReturnsSampleAlongsideSmoothedValue(t *testing.T) {
	pool := topology.NewGlobalNodePool(topology.PoolConfig{MaxLatencyTableEntries: 8, MaxConsecutiveFailures: func() int { return 3 }, LatencyDecayWindow: func() time.Duration { return time.Hour }})
	hash := node.HashFromRawOptions([]byte(`{"type":"latency-sample"}`))
	pool.AddNodeFromSub(hash, nil, "sub")
	entry, _ := pool.GetEntry(hash)
	storeOutbound(entry)
	sample := 3 * time.Millisecond
	manager := NewProbeManager(ProbeConfig{Pool: pool, Fetcher: func(node.Hash, string) ([]byte, time.Duration, error) { return nil, sample, nil }})
	if _, err := manager.ProbeLatencySync(hash); err != nil {
		t.Fatal(err)
	}
	sample = 100 * time.Millisecond
	result, err := manager.ProbeLatencySync(hash)
	if err != nil {
		t.Fatal(err)
	}
	if result.LatencyMs != 100 || result.LatencyEwmaMs >= 100 {
		t.Fatalf("retest hid the current measurement behind smoothing: %+v", result)
	}
}
