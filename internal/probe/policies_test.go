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
			if got := manager.taskQueue.normal.len() > 0; got != test.want {
				t.Fatalf("scheduled=%v, want %v", got, test.want)
			}
		})
	}
}
