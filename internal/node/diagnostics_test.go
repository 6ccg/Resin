package node

import (
	"math"
	"testing"
	"time"
)

func TestDiagnostics_StabilityExcludesTargetsAndRestores(t *testing.T) {
	entry := NewNodeEntry(Hash{}, nil, time.Now(), 0)
	for _, sample := range []ProbeObservation{
		{Kind: "latency", Success: true, DurationMs: 100},
		{Kind: "egress", Success: true, DurationMs: 300},
		{Kind: "latency", Success: false},
		{Kind: "target", Success: true},
	} {
		sample.CheckedAt = time.Now()
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
	if restored.Diagnostics().Checks[0].Kind != "target" {
		t.Fatal("history must list most recent checks first")
	}
}

func TestDiagnostics_BoundsGeneralAndTargetWindowsIndependently(t *testing.T) {
	entry := NewNodeEntry(Hash{}, nil, time.Now(), 0)
	entry.RecordProbe(ProbeObservation{Kind: "egress", Success: true})
	for i := 0; i < ProbeWindowSize+5; i++ {
		entry.RecordProbe(ProbeObservation{Kind: "latency", Success: false})
		entry.RecordProbe(ProbeObservation{Kind: "target", Success: true})
	}
	diagnostics := entry.Diagnostics()
	if len(diagnostics.Checks) != 2*ProbeWindowSize || diagnostics.Stability.Samples != ProbeWindowSize || diagnostics.Stability.Successes != 0 {
		t.Fatalf("unbounded or mixed windows: %+v", diagnostics.Stability)
	}
}
