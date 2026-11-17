package node

import (
	"encoding/json"
	"math"
	"time"
)

// Keep independent windows for connectivity checks and auxiliary diagnostics.
// Target failures and exit-metadata errors do not grade connectivity health.
const ProbeWindowSize = 50

type ProbeObservation struct {
	Kind       string    `json:"kind"`
	Target     string    `json:"target"`
	CheckedAt  time.Time `json:"checked_at"`
	Success    bool      `json:"success"`
	DurationMs float64   `json:"duration_ms"`
	LatencyMs  float64   `json:"latency_ms,omitempty"`
	HTTPStatus int       `json:"http_status,omitempty"`
	Error      string    `json:"error,omitempty"`
}

type ProbeStability struct {
	Samples             int        `json:"samples"`
	Successes           int        `json:"successes"`
	SuccessRate         float64    `json:"success_rate"`
	ConsecutiveFailures int        `json:"consecutive_failures"`
	AverageDurationMs   float64    `json:"average_duration_ms"`
	DurationStddevMs    float64    `json:"duration_stddev_ms"`
	FirstCheckedAt      *time.Time `json:"first_checked_at,omitempty"`
	LastCheckedAt       *time.Time `json:"last_checked_at,omitempty"`
}

type ProbeDiagnostics struct {
	Stability ProbeStability     `json:"stability"`
	Checks    []ProbeObservation `json:"checks"`
}

// Keep the latest connectivity result with the node so periodic scans need
// not read the backing array of the full diagnostic history.
type connectivityCheck struct {
	checkedAt time.Time
	success   bool
	present   bool
}

func (e *NodeEntry) RecordProbe(observation ProbeObservation) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.recordProbeLocked(observation)
}

func (e *NodeEntry) recordProbeLocked(observation ProbeObservation) {
	general := isGeneralProbe(observation.Kind)
	if general {
		e.lastConnectivity = connectivityCheck{
			checkedAt: observation.CheckedAt,
			success:   observation.Success,
			present:   true,
		}
	}
	e.probeHistory.record(observation, general)
}

func (e *NodeEntry) ProbeHistoryJSON() json.RawMessage {
	e.mu.RLock()
	checks := e.probeHistory.snapshot()
	e.mu.RUnlock()
	data, _ := json.Marshal(checks)
	return data
}

// RestoreProbeHistory is called before the probe workers start.
func (e *NodeEntry) RestoreProbeHistory(data json.RawMessage) error {
	if len(data) == 0 {
		return nil
	}
	var samples []ProbeObservation
	if err := json.Unmarshal(data, &samples); err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.probeHistory.samples) == 0 && len(samples) > 0 {
		e.probeHistory.samples = make([]storedProbe, 0, min(len(samples), 2*ProbeWindowSize))
	}
	for _, sample := range samples {
		e.recordProbeLocked(sample)
	}
	return nil
}

func (e *NodeEntry) Diagnostics() ProbeDiagnostics {
	e.mu.RLock()
	checks := e.probeHistory.snapshot()
	stats := summarizeProbes(e.probeHistory.samples)
	e.mu.RUnlock()
	for i, j := 0, len(checks)-1; i < j; i, j = i+1, j-1 {
		checks[i], checks[j] = checks[j], checks[i]
	}
	return ProbeDiagnostics{Stability: stats, Checks: checks}
}

func (e *NodeEntry) ProbeStability() ProbeStability {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return summarizeProbes(e.probeHistory.samples)
}

// LastConnectivityCheck excludes passive traffic and auxiliary diagnostics.
func (e *NodeEntry) LastConnectivityCheck() time.Time {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.lastConnectivity.checkedAt
}

func isGeneralProbe(kind string) bool { return kind == "latency" || kind == "egress" }

func summarizeProbes(checks []storedProbe) ProbeStability {
	stats := ProbeStability{}
	var first, last time.Time
	var squaredDeviations float64
	for _, sample := range checks {
		if !sample.general {
			continue
		}
		stats.Samples++
		if stats.Samples == 1 {
			first = sample.checkedAt
		}
		last = sample.checkedAt
		if !sample.success {
			stats.ConsecutiveFailures++
			continue
		}
		stats.ConsecutiveFailures = 0
		stats.Successes++
		delta := sample.durationMs - stats.AverageDurationMs
		stats.AverageDurationMs += delta / float64(stats.Successes)
		squaredDeviations += delta * (sample.durationMs - stats.AverageDurationMs)
	}
	if stats.Samples > 0 {
		stats.SuccessRate = float64(stats.Successes) / float64(stats.Samples)
		stats.FirstCheckedAt, stats.LastCheckedAt = &first, &last
	}
	if stats.Successes > 0 {
		stats.DurationStddevMs = math.Sqrt(math.Max(0, squaredDeviations/float64(stats.Successes)))
	}
	return stats
}
