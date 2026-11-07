package node

import (
	"testing"
	"time"

	"github.com/Resinat/Resin/internal/testutil"
)

func TestValiditySeparatesUnconfirmedFailuresFromCircuitFailure(t *testing.T) {
	for _, tc := range []struct {
		name     string
		sample   *ProbeObservation
		failures int
		broken   bool
		want     string
	}{
		{"successful_check", &ProbeObservation{Kind: "latency", Success: true, CheckedAt: time.Now()}, 0, false, "valid"},
		{"one_failed_check", &ProbeObservation{Kind: "latency", Success: false, CheckedAt: time.Now()}, 1, false, "unstable"},
		{"failed_passive_request", &ProbeObservation{Kind: "latency", Success: true, CheckedAt: time.Now()}, 1, false, "unstable"},
		{"confirmed_circuit_failure", &ProbeObservation{Kind: "latency", Success: false, CheckedAt: time.Now()}, 3, true, "invalid"},
		{"expired_success", &ProbeObservation{Kind: "egress", Success: true, CheckedAt: time.Now().Add(-3 * time.Hour)}, 0, false, "expired"},
		{"initial_pending", nil, 0, true, "untested"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entry := NewNodeEntry(Hash{}, nil, time.Now(), 0)
			ob := testutil.NewNoopOutbound()
			entry.Outbound.Store(&ob)
			entry.FailureCount.Store(int32(tc.failures))
			if tc.broken {
				entry.CircuitOpenSince.Store(time.Now().UnixNano())
			}
			if tc.sample != nil {
				entry.RecordProbe(*tc.sample)
			}
			if got := entry.Validity(true, 2*time.Hour); got != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}
