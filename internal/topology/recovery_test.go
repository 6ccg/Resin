package topology

import (
	"github.com/Resinat/Resin/internal/node"
	"testing"
)

func TestRecovery_RequiresConsecutiveSuccesses(t *testing.T) {
	pool := NewGlobalNodePool(PoolConfig{MaxConsecutiveFailures: func() int { return 3 }, RecoverySuccessThreshold: func() int { return 2 }})
	hash := node.HashFromRawOptions([]byte(`{"type":"recovery"}`))
	pool.AddNodeFromSub(hash, nil, "sub")
	entry, _ := pool.GetEntry(hash)
	pool.RecordResult(hash, true)
	if !entry.IsCircuitOpen() {
		t.Fatal("recovered before success threshold")
	}
	pool.RecordResult(hash, false)
	pool.RecordResult(hash, true)
	if !entry.IsCircuitOpen() {
		t.Fatal("non-consecutive successes recovered the node")
	}
	pool.RecordResult(hash, true)
	if entry.IsCircuitOpen() || entry.FailureCount.Load() != 0 || entry.RecoverySuccessCount.Load() != 0 {
		t.Fatal("node failed to recover after consecutive successes")
	}
}
