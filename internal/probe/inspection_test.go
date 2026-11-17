package probe

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Resinat/Resin/internal/config"
	"github.com/Resinat/Resin/internal/node"
	"github.com/Resinat/Resin/internal/topology"
)

func TestEgressProbeChecksDueAttributesInTheSameRound(t *testing.T) {
	for _, tc := range []struct {
		name    string
		body    string
		wantErr string
	}{
		{"success", `{"success":true,"ip":"192.0.2.1","countryCode":"JP"}`, ""},
		{"provider_html_error", "<html>502 Server Error</html>", "check-host response"},
		{"different_exit", `{"success":true,"ip":"192.0.2.2"}`, "attribute provider observed exit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool := topology.NewGlobalNodePool(topology.PoolConfig{MaxConsecutiveFailures: func() int { return 3 }})
			hash := node.HashFromRawOptions([]byte(`{"type":"egress-with-attributes"}`))
			pool.AddNodeFromSub(hash, nil, "sub")
			entry, _ := pool.GetEntry(hash)
			storeOutbound(entry)
			cfg := config.NewDefaultRuntimeConfig()
			cfg.ProbeConcurrency = 1
			var traceCalls, attributeCalls int
			manager := NewProbeManager(ProbeConfig{
				Pool:          pool,
				RuntimeConfig: func() *config.RuntimeConfig { return cfg },
				Fetcher: func(_ node.Hash, url string) ([]byte, time.Duration, error) {
					switch url {
					case egressTraceURL:
						traceCalls++
						return []byte("ip=192.0.2.1\nloc=JP"), time.Millisecond, nil
					case cfg.EgressAttributesURL:
						if got := entry.GetEgressIP().String(); got != "192.0.2.1" {
							t.Fatalf("lookup ran before egress was recorded: %s", got)
						}
						attributeCalls++
						return []byte(tc.body), time.Millisecond, nil
					default:
						t.Fatalf("unexpected probe URL: %s", url)
						return nil, 0, nil
					}
				},
			})
			defer manager.Stop()

			result, err := manager.probeEgressSync(context.Background(), false, hash)
			if err != nil || result.EgressIP != "192.0.2.1" || attributeCalls != 1 {
				t.Fatalf("egress=%+v error=%v attribute calls=%d", result, err, attributeCalls)
			}
			if entry.GetEgressIP().String() != result.EgressIP || entry.FailureCount.Load() != 0 || entry.IsCircuitOpen() {
				t.Fatalf("attribute lookup changed the egress or health: %s", entry.GetEgressIP())
			}

			inspection := entry.Inspection()
			if tc.wantErr == "" {
				if inspection.Attributes == nil || inspection.Attributes.IP != result.EgressIP {
					t.Fatalf("attributes missing for observed exit: %+v", inspection.Attributes)
				}
			} else if !strings.Contains(inspection.AttributesError, tc.wantErr) || inspection.Attributes != nil {
				t.Fatalf("attribute failure not recorded: %+v", inspection)
			}

			manager.executeTask(probeTask{key: probeTaskKey{hash: hash, kind: probeTaskKindAttributes}})
			manager.probeEgress(hash, entry)
			if traceCalls != 2 || attributeCalls != 1 {
				t.Fatalf("fresh attributes or active backoff must suppress repeats: trace=%d attributes=%d", traceCalls, attributeCalls)
			}
		})
	}
}

func TestAttributeProbe_SourceFailureDoesNotChangeHealth(t *testing.T) {
	pool := topology.NewGlobalNodePool(topology.PoolConfig{MaxConsecutiveFailures: func() int { return 3 }})
	hash := node.HashFromRawOptions([]byte(`{"type":"attributes"}`))
	pool.AddNodeFromSub(hash, nil, "sub")
	entry, _ := pool.GetEntry(hash)
	storeOutbound(entry)
	entry.CircuitOpenSince.Store(0)
	entry.FailureCount.Store(2)
	cfg := config.NewDefaultRuntimeConfig()
	manager := NewProbeManager(ProbeConfig{Pool: pool, RuntimeConfig: func() *config.RuntimeConfig { return cfg }, Fetcher: func(node.Hash, string) ([]byte, time.Duration, error) {
		return nil, 0, errors.New("provider rate limited")
	}})
	if _, err := manager.ProbeAttributesSync(hash); err == nil {
		t.Fatal("provider failure not reported")
	}
	if entry.FailureCount.Load() != 2 || entry.IsCircuitOpen() {
		t.Fatal("provider failure changed node health")
	}
	inspection := entry.Inspection()
	if inspection.AttributesAttemptAt == nil || inspection.AttributesError != "provider rate limited" {
		t.Fatalf("failure state not stored: %+v", inspection)
	}
}
