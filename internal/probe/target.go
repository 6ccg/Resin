package probe

import (
	"context"
	"fmt"
	"time"

	"github.com/Resinat/Resin/internal/node"
)

func (m *ProbeManager) TriggerTargetProbes(hash node.Hash) bool {
	return m.enqueueProbe(hash, probeTaskKindTarget, probePriorityNormal)
}

// ProbeTargetSync observes an application target independently of node health.
// A blocked target must not open the global node circuit breaker.
func (m *ProbeManager) ProbeTargetSync(hash node.Hash, target string, expectedStatuses []int) (*node.ProbeObservation, error) {
	return m.probeTargetSync(context.Background(), false, hash, target, expectedStatuses)
}

func (m *ProbeManager) probeTargetSync(ctx context.Context, batch bool, hash node.Hash, target string, expectedStatuses []int) (*node.ProbeObservation, error) {
	_, release, err := m.acquireNodeSlot(ctx, batch, hash)
	if err != nil {
		return nil, err
	}
	defer release()
	return m.probeTarget(hash, target, expectedStatuses)
}

func (m *ProbeManager) probeTarget(hash node.Hash, target string, expectedStatuses []int) (*node.ProbeObservation, error) {
	if m.targetFetcher == nil {
		return nil, fmt.Errorf("target probe fetcher not configured")
	}
	select {
	case <-m.stopCh:
		return nil, fmt.Errorf("probe manager stopped")
	default:
	}
	started := time.Now()
	status, err := m.targetFetcher(hash, target)
	observation := node.ProbeObservation{
		Kind: "target", Target: target, CheckedAt: time.Now(),
		DurationMs: float64(time.Since(started)) / float64(time.Millisecond), HTTPStatus: status,
	}
	if err != nil {
		observation.Error = err.Error()
	} else {
		observation.Success = status >= 200 && status < 400
		if len(expectedStatuses) > 0 {
			observation.Success = false
			for _, expected := range expectedStatuses {
				if status == expected {
					observation.Success = true
					break
				}
			}
		}
		if !observation.Success {
			observation.Error = fmt.Sprintf("unexpected HTTP status %d", status)
		}
	}
	m.pool.RecordProbeObservation(hash, observation)
	return &observation, nil
}
