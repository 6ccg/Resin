package probe

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Resinat/Resin/internal/node"
	"github.com/google/uuid"
)

type BatchStatus struct {
	ID         string     `json:"id"`
	Kind       string     `json:"kind"`
	Total      int        `json:"total"`
	Completed  int        `json:"completed"`
	Successes  int        `json:"successes"`
	Failures   int        `json:"failures"`
	Skipped    int        `json:"skipped"`
	Running    bool       `json:"running"`
	Cancelled  bool       `json:"cancelled"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

type probeJob struct {
	status BatchStatus
	cancel context.CancelFunc
}

func (m *ProbeManager) BatchStatus() *BatchStatus {
	m.jobMu.Lock()
	defer m.jobMu.Unlock()
	if m.job == nil {
		return nil
	}
	copy := m.job.status
	return &copy
}

func (m *ProbeManager) CancelBatch(id string) bool {
	m.jobMu.Lock()
	defer m.jobMu.Unlock()
	if m.job == nil || m.job.status.ID != id {
		return false
	}
	if m.job.status.Running {
		m.job.status.Cancelled = true
		m.job.cancel()
	}
	return true
}

func (m *ProbeManager) StartBatch(kind string, hashes []node.Hash) (*BatchStatus, error) {
	switch kind {
	case "latency", "egress", "attributes", "targets":
	default:
		return nil, fmt.Errorf("unsupported probe kind")
	}
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	if m.stopped {
		return nil, fmt.Errorf("probe manager stopped")
	}
	m.jobMu.Lock()
	defer m.jobMu.Unlock()
	if m.job != nil && m.job.status.Running {
		return nil, fmt.Errorf("a batch probe is already running")
	}
	ctx, cancel := context.WithCancel(context.Background())
	job := &probeJob{status: BatchStatus{ID: uuid.NewString(), Kind: kind, Total: len(hashes), Running: true, StartedAt: time.Now()}, cancel: cancel}
	m.job = job
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer cancel()
		var next atomic.Int64
		var workers sync.WaitGroup
		for i := 0; i < min(len(hashes), m.workerCount); i++ {
			workers.Add(1)
			go func() {
				defer workers.Done()
				for {
					index := int(next.Add(1) - 1)
					if index >= len(hashes) {
						return
					}
					if ctx.Err() != nil {
						return
					}
					hash := hashes[index]
					entry, ok := m.pool.GetEntry(hash)
					skipped := !ok || !entry.HasOutbound() || m.pool.IsNodeDisabled(hash)
					var err error
					if !skipped {
						switch kind {
						case "latency":
							_, err = m.probeLatencySync(ctx, true, hash)
						case "egress":
							m.refreshEntry(hash, entry)
							_, err = m.probeEgressSync(ctx, true, hash)
						case "attributes":
							_, err = m.probeAttributesSync(ctx, true, hash)
						case "targets":
							for _, target := range m.settings().ReachabilityTargets {
								result, checkErr := m.probeTargetSync(ctx, true, hash, target, nil)
								if checkErr != nil {
									err = checkErr
								} else if !result.Success {
									err = fmt.Errorf("target check failed")
								}
								if ctx.Err() != nil {
									break
								}
							}
						}
					}
					if errors.Is(err, errProbeCancelled) {
						return
					}
					m.jobMu.Lock()
					job.status.Completed++
					if skipped {
						job.status.Skipped++
					} else if err != nil {
						job.status.Failures++
					} else {
						job.status.Successes++
					}
					m.jobMu.Unlock()
				}
			}()
		}
		workers.Wait()
		m.jobMu.Lock()
		now := time.Now()
		job.status.Running = false
		if job.status.Completed < job.status.Total {
			job.status.Cancelled = true
		}
		job.status.FinishedAt = &now
		m.jobMu.Unlock()
	}()
	copy := job.status
	return &copy, nil
}
