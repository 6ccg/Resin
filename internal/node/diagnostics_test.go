package node

import (
	"fmt"
	"math"
	"slices"
	"testing"
	"time"
)

func TestDiagnostics_StabilityExcludesTargetsAndRestores(t *testing.T) {
	entry := NewNodeEntry(Hash{}, nil, time.Now(), 0)
	start := time.Now()
	for i, sample := range []ProbeObservation{
		{Kind: "latency", Success: true, DurationMs: 100},
		{Kind: "egress", Success: true, DurationMs: 300},
		{Kind: "latency", Success: false},
		{Kind: "target", Success: true},
	} {
		sample.CheckedAt = start.Add(time.Duration(i) * time.Second)
		entry.RecordProbe(sample)
	}
	restored := NewNodeEntry(Hash{}, nil, time.Now(), 0)
	if err := restored.RestoreProbeHistory(entry.ProbeHistoryJSON()); err != nil {
		t.Fatal(err)
	}
	stats := restored.Diagnostics().Stability
	if stats.Samples != 3 || stats.Successes != 2 || stats.ConsecutiveFailures != 1 {
		t.Fatalf("unexpected stability: %+v", stats)
	}
	if math.Abs(stats.SuccessRate-2.0/3) > 1e-9 || stats.AverageDurationMs != 200 || stats.DurationStddevMs != 100 {
		t.Fatalf("incorrect rate or duration statistics: %+v", stats)
	}
	checks := restored.Diagnostics().Checks
	if checks[0].Kind != "target" {
		t.Fatal("history must list most recent checks first")
	}
	if got := restored.LastConnectivityCheck(); !got.Equal(checks[1].CheckedAt) {
		t.Fatalf("last connectivity check = %v, want %v", got, checks[1].CheckedAt)
	}
}

func TestDiagnostics_BoundsGeneralAndTargetWindowsIndependently(t *testing.T) {
	entry := NewNodeEntry(Hash{}, nil, time.Now(), 0)
	entry.RecordProbe(ProbeObservation{Kind: "egress", Success: true})
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var checks []ProbeObservation
	for i := 0; i < ProbeWindowSize+5; i++ {
		at := start.Add(time.Duration(i) * time.Second)
		latency := ProbeObservation{Kind: "latency", CheckedAt: at, Target: "https://example.com", Error: fmt.Sprintf("failure %d", i)}
		target := ProbeObservation{Kind: "target", CheckedAt: at, Target: "https://example.com", Success: true, DurationMs: float64(i) + 0.5, LatencyMs: 3.25, HTTPStatus: 204}
		entry.RecordProbe(latency)
		entry.RecordProbe(target)
		checks = append(checks, latency, target)
	}
	diagnostics := entry.Diagnostics()
	if len(diagnostics.Checks) != 2*ProbeWindowSize || diagnostics.Stability.Samples != ProbeWindowSize || diagnostics.Stability.Successes != 0 {
		t.Fatalf("unbounded or mixed windows: %+v", diagnostics.Stability)
	}
	want := checks[len(checks)-2*ProbeWindowSize:]
	slices.Reverse(want)
	restored := NewNodeEntry(Hash{}, nil, time.Now(), 0)
	if err := restored.RestoreProbeHistory(entry.ProbeHistoryJSON()); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(restored.Diagnostics().Checks, want) {
		t.Fatal("history changed after rollover and JSON replay")
	}
}
