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

func (e *NodeEntry) RecordProbe(observation ProbeObservation) {
	e.mu.Lock()
	defer e.mu.Unlock()
	general := isGeneralProbe(observation.Kind)
	count := 0
	for _, sample := range e.probeHistory {
		if isGeneralProbe(sample.Kind) == general {
			count++
		}
	}
	if count >= ProbeWindowSize {
		for i, sample := range e.probeHistory {
			if isGeneralProbe(sample.Kind) == general {
				e.probeHistory = append(e.probeHistory[:i], e.probeHistory[i+1:]...)
				break
			}
		}
	}
	e.probeHistory = append(e.probeHistory, observation)
}

func (e *NodeEntry) ProbeHistoryJSON() json.RawMessage {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if len(e.probeHistory) == 0 {
		return json.RawMessage("[]")
	}
	data, _ := json.Marshal(e.probeHistory)
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
	for _, sample := range samples {
		e.RecordProbe(sample)
	}
	return nil
}

func (e *NodeEntry) Diagnostics() ProbeDiagnostics {
	e.mu.RLock()
	checks := append([]ProbeObservation{}, e.probeHistory...)
	e.mu.RUnlock()
	stats := summarizeProbes(checks)
	for i, j := 0, len(checks)-1; i < j; i, j = i+1, j-1 {
		checks[i], checks[j] = checks[j], checks[i]
	}
	return ProbeDiagnostics{Stability: stats, Checks: checks}
}

func (e *NodeEntry) ProbeStability() ProbeStability {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return summarizeProbes(e.probeHistory)
}

// LastConnectivityCheck excludes passive traffic and auxiliary diagnostics.
func (e *NodeEntry) LastConnectivityCheck() time.Time {
	e.mu.RLock()
	defer e.mu.RUnlock()
	for i := len(e.probeHistory) - 1; i >= 0; i-- {
		if isGeneralProbe(e.probeHistory[i].Kind) {
			return e.probeHistory[i].CheckedAt
		}
	}
	return time.Time{}
}

func isGeneralProbe(kind string) bool { return kind == "latency" || kind == "egress" }

func summarizeProbes(checks []ProbeObservation) ProbeStability {
	stats := ProbeStability{}
	var first, last time.Time
	var squaredDeviations float64
	for _, sample := range checks {
		if !isGeneralProbe(sample.Kind) {
			continue
		}
		stats.Samples++
		if stats.Samples == 1 {
			first = sample.CheckedAt
		}
		last = sample.CheckedAt
		if !sample.Success {
			stats.ConsecutiveFailures++
			continue
		}
		stats.ConsecutiveFailures = 0
		stats.Successes++
		delta := sample.DurationMs - stats.AverageDurationMs
		stats.AverageDurationMs += delta / float64(stats.Successes)
		squaredDeviations += delta * (sample.DurationMs - stats.AverageDurationMs)
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
