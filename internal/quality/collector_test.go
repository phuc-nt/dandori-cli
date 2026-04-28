package quality

import (
	"os"
	"testing"
	"time"
)

func TestComputeMetrics(t *testing.T) {
	before := &Snapshot{
		LintErrors:   5,
		LintWarnings: 10,
		TestsTotal:   100,
		TestsPassed:  90,
		TestsFailed:  10,
	}

	after := &Snapshot{
		LintErrors:   2,
		LintWarnings: 8,
		TestsTotal:   105,
		TestsPassed:  100,
		TestsFailed:  5,
	}

	metrics := ComputeMetrics("run-123", before, after)

	if metrics.RunID != "run-123" {
		t.Errorf("RunID = %v, want run-123", metrics.RunID)
	}

	// Before values
	if metrics.LintErrorsBefore != 5 {
		t.Errorf("LintErrorsBefore = %v, want 5", metrics.LintErrorsBefore)
	}
	if metrics.TestsPassedBefore != 90 {
		t.Errorf("TestsPassedBefore = %v, want 90", metrics.TestsPassedBefore)
	}

	// After values
	if metrics.LintErrorsAfter != 2 {
		t.Errorf("LintErrorsAfter = %v, want 2", metrics.LintErrorsAfter)
	}
	if metrics.TestsPassedAfter != 100 {
		t.Errorf("TestsPassedAfter = %v, want 100", metrics.TestsPassedAfter)
	}

	// Deltas
	if metrics.LintDelta != -3 {
		t.Errorf("LintDelta = %v, want -3 (improvement)", metrics.LintDelta)
	}
	if metrics.TestsDelta != 10 {
		t.Errorf("TestsDelta = %v, want 10 (improvement)", metrics.TestsDelta)
	}

	// IsImproved
	if !metrics.IsImproved() {
		t.Error("IsImproved() = false, want true")
	}
}

func TestComputeMetrics_NilSnapshots(t *testing.T) {
	// Both nil
	metrics := ComputeMetrics("run-nil", nil, nil)
	if metrics.LintDelta != 0 || metrics.TestsDelta != 0 {
		t.Error("Expected zero deltas for nil snapshots")
	}

	// Only before nil
	after := &Snapshot{LintErrors: 5, TestsPassed: 10}
	metrics = ComputeMetrics("run-after-only", nil, after)
	if metrics.LintDelta != 5 {
		t.Errorf("LintDelta = %v, want 5", metrics.LintDelta)
	}
}

func TestCollector_NewCollector(t *testing.T) {
	cfg := Config{
		Enabled:     true,
		LintCommand: "echo test",
		Timeout:     "10s",
	}

	collector := NewCollector(cfg)
	if collector.timeout != 10*time.Second {
		t.Errorf("timeout = %v, want 10s", collector.timeout)
	}

	// Invalid timeout falls back to 30s
	cfg.Timeout = "invalid"
	collector = NewCollector(cfg)
	if collector.timeout != 30*time.Second {
		t.Errorf("timeout = %v, want 30s (fallback)", collector.timeout)
	}
}

func TestCollector_Snapshot_RealProject(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if os.Getenv("DANDORI_QUALITY_RUNNING") != "" {
		t.Skip("skipping recursive quality snapshot (called from within quality collector)")
	}

	cfg := DefaultConfig()
	collector := NewCollector(cfg)

	// Snapshot current project (dandori-cli)
	snap := collector.Snapshot(".")

	t.Logf("Snapshot: lint=%d errors, %d warnings; tests=%d total, %d passed",
		snap.LintErrors, snap.LintWarnings, snap.TestsTotal, snap.TestsPassed)

	// We expect some tests to exist in this project
	if snap.TestsTotal == 0 && snap.Error == "" {
		t.Log("Warning: no tests found, but no error either")
	}
}

func TestIsImproved(t *testing.T) {
	tests := []struct {
		name      string
		lintDelta int
		testDelta int
		want      bool
	}{
		{"fewer errors", -1, 0, true},
		{"more passing", 0, 1, true},
		{"both improved", -1, 1, true},
		{"more errors", 1, 0, false},
		{"fewer passing", 0, -1, false},
		{"no change", 0, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Metrics{
				LintDelta:  tt.lintDelta,
				TestsDelta: tt.testDelta,
			}
			if got := m.IsImproved(); got != tt.want {
				t.Errorf("IsImproved() = %v, want %v", got, tt.want)
			}
		})
	}
}
