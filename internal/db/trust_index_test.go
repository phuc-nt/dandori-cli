package db

import (
	"testing"
	"time"
)

// ---------- composeTrust (pure formula, no DB) ----------

func TestComposeTrust_NoData(t *testing.T) {
	got := composeTrust(TrustComponents{}, 28, false)
	if got.HasData {
		t.Fatal("HasData = true, want false")
	}
	if got.Band != "no-data" {
		t.Errorf("band = %q, want no-data", got.Band)
	}
	if got.Value != 0 {
		t.Errorf("value = %d, want 0", got.Value)
	}
}

func TestComposeTrust_AllPerfect(t *testing.T) {
	c := TrustComponents{Acceptance: 1.0, AICFR: 0.0, InterventionRate: 0.0}
	got := composeTrust(c, 28, true)
	if got.Value != 100 {
		t.Errorf("value = %d, want 100", got.Value)
	}
	if got.Band != "autonomous" {
		t.Errorf("band = %q, want autonomous", got.Band)
	}
}

func TestComposeTrust_AllBroken(t *testing.T) {
	c := TrustComponents{Acceptance: 0.0, AICFR: 1.0, InterventionRate: 1.0}
	got := composeTrust(c, 28, true)
	if got.Value != 0 {
		t.Errorf("value = %d, want 0", got.Value)
	}
	if got.Band != "copilot" {
		t.Errorf("band = %q, want copilot", got.Band)
	}
}

func TestComposeTrust_InterventionClamp(t *testing.T) {
	// Intervention rate > 1.0 is legitimate (avg > 1 per run). The (1-rate)
	// term must clamp to 0, not go negative.
	c := TrustComponents{Acceptance: 1.0, AICFR: 0.0, InterventionRate: 3.0}
	got := composeTrust(c, 28, true)
	// Expect: 0.40*1.0 + 0.35*1.0 + 0.25*0.0 = 0.75 → 75
	if got.Value != 75 {
		t.Errorf("value = %d, want 75 (intervention clamped)", got.Value)
	}
	if got.Band != "co-own" {
		t.Errorf("band = %q, want co-own", got.Band)
	}
}

func TestComposeTrust_CFRClamp(t *testing.T) {
	// CFR > 1 should clamp the stability term to 0 (defensive — shouldn't
	// happen in practice but the formula must not go negative).
	c := TrustComponents{Acceptance: 1.0, AICFR: 2.0, InterventionRate: 0.0}
	got := composeTrust(c, 28, true)
	// Expect: 0.40*1.0 + 0.35*0 + 0.25*1.0 = 0.65 → 65
	if got.Value != 65 {
		t.Errorf("value = %d, want 65 (cfr clamped)", got.Value)
	}
}

func TestComposeTrust_BandBoundaries(t *testing.T) {
	cases := []struct {
		name  string
		value int
		want  string
	}{
		{"below copilot/co-own", 59, "copilot"},
		{"at co-own lower", 60, "co-own"},
		{"co-own upper", 79, "co-own"},
		{"at autonomous", 80, "autonomous"},
		{"max", 100, "autonomous"},
		{"zero", 0, "copilot"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := bandFor(tc.value)
			if got != tc.want {
				t.Errorf("bandFor(%d) = %q, want %q", tc.value, got, tc.want)
			}
		})
	}
}

func TestComposeTrust_WorkedExample(t *testing.T) {
	// From docs/reference/04-metric-framework.md §3 worked example:
	//   acceptance=0.78, cfr=0.15, intervention=0.25
	//   Trust = 0.40*0.78 + 0.35*0.85 + 0.25*0.75 = 0.312 + 0.2975 + 0.1875
	//         = 0.797 → 80 (after round)
	c := TrustComponents{Acceptance: 0.78, AICFR: 0.15, InterventionRate: 0.25}
	got := composeTrust(c, 28, true)
	if got.Value != 80 {
		t.Errorf("worked-example value = %d, want 80", got.Value)
	}
	if got.Band != "autonomous" {
		t.Errorf("worked-example band = %q, want autonomous", got.Band)
	}
}

func TestComposeTrust_DemoDBCrossCheck(t *testing.T) {
	// Cross-check against demo.db state captured during v0.12 design:
	//   acceptance=0.80, cfr=0.00, intervention=0.00
	//   Trust = 0.40*0.80 + 0.35*1.00 + 0.25*1.00 = 0.32 + 0.35 + 0.25 = 0.92 → 92
	c := TrustComponents{Acceptance: 0.80, AICFR: 0.00, InterventionRate: 0.00}
	got := composeTrust(c, 28, true)
	if got.Value != 92 {
		t.Errorf("demo-db cross-check value = %d, want 92", got.Value)
	}
	if got.Band != "autonomous" {
		t.Errorf("demo-db band = %q, want autonomous", got.Band)
	}
}

// ---------- GetTrustIndex (full SQL path) ----------

func TestGetTrustIndex_EmptyDB(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	got, err := d.GetTrustIndex(28)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.HasData {
		t.Fatal("empty DB should report HasData=false")
	}
	if got.Band != "no-data" {
		t.Errorf("band = %q, want no-data", got.Band)
	}
	if got.WindowDays != 28 {
		t.Errorf("window_days = %d, want 28", got.WindowDays)
	}
}

func TestGetTrustIndex_DefaultsTo28Days(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	got, err := d.GetTrustIndex(0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.WindowDays != 28 {
		t.Errorf("window_days = %d, want 28 (default)", got.WindowDays)
	}
}

func TestGetTrustIndex_FullPath(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now().UTC()

	// 2 tasks, both with 80% agent / 20% human lines, neither reopened.
	insertTaskAttribution(t, d, "FEAT-1", 80, 20, now)
	insertTaskAttribution(t, d, "FEAT-2", 80, 20, now)
	// 2 runs, zero interventions.
	insertTrendRun(t, d, "r1", 0, 1.0, now, "FEAT-1")
	insertTrendRun(t, d, "r2", 0, 1.0, now, "FEAT-2")

	got, err := d.GetTrustIndex(28)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.HasData {
		t.Fatal("expected HasData=true")
	}
	// acceptance = 160/200 = 0.80, cfr = 0/2 = 0, intervention = 0/2 = 0
	// Trust = 0.40*0.80 + 0.35*1.0 + 0.25*1.0 = 0.92 → 92
	if got.Value != 92 {
		t.Errorf("value = %d, want 92", got.Value)
	}
	if got.Band != "autonomous" {
		t.Errorf("band = %q, want autonomous", got.Band)
	}
	if got.Components.Acceptance < 0.79 || got.Components.Acceptance > 0.81 {
		t.Errorf("acceptance = %.3f, want ~0.80", got.Components.Acceptance)
	}
	if got.Components.AICFR != 0 {
		t.Errorf("ai_cfr = %.3f, want 0", got.Components.AICFR)
	}
	if got.Components.InterventionRate != 0 {
		t.Errorf("intervention = %.3f, want 0", got.Components.InterventionRate)
	}
}

func TestGetTrustIndex_OnlyRunsNoTasks(t *testing.T) {
	// Runs exist but no task_attribution rows → HasData=false (need both sources).
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now().UTC()
	insertTrendRun(t, d, "r1", 0, 1.0, now, "FEAT-1")

	got, err := d.GetTrustIndex(28)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.HasData {
		t.Error("runs alone (no task_attribution) should give HasData=false")
	}
}

func TestGetTrustIndex_OnlyTasksNoRuns(t *testing.T) {
	// task_attribution exists but no runs → HasData=false.
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now().UTC()
	insertTaskAttribution(t, d, "FEAT-1", 50, 50, now)

	got, err := d.GetTrustIndex(28)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.HasData {
		t.Error("tasks alone (no runs) should give HasData=false")
	}
}

func TestGetTrustIndex_ReopenedTaskAffectsCFR(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now().UTC()

	// 4 tasks total; 1 reopened (total_iterations > 1) → cfr = 0.25.
	insertTaskAttribution(t, d, "FEAT-1", 100, 0, now)
	insertTaskAttribution(t, d, "FEAT-2", 100, 0, now)
	insertTaskAttribution(t, d, "FEAT-3", 100, 0, now)
	insertTaskAttribution(t, d, "FEAT-4", 100, 0, now)
	// Bump FEAT-4 to iterations=2 to mark it reopened.
	if _, err := d.Exec(`UPDATE task_attribution SET total_iterations = 2 WHERE jira_issue_key = ?`, "FEAT-4"); err != nil {
		t.Fatalf("bump iterations: %v", err)
	}
	insertTrendRun(t, d, "r1", 0, 1.0, now, "FEAT-1")

	got, err := d.GetTrustIndex(28)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.HasData {
		t.Fatal("expected HasData=true")
	}
	// acceptance=1.0, cfr=0.25, intervention=0
	// Trust = 0.40*1.0 + 0.35*0.75 + 0.25*1.0 = 0.40 + 0.2625 + 0.25 = 0.9125 → 91
	if got.Value != 91 {
		t.Errorf("value = %d, want 91", got.Value)
	}
	if got.Components.AICFR < 0.24 || got.Components.AICFR > 0.26 {
		t.Errorf("ai_cfr = %.3f, want ~0.25", got.Components.AICFR)
	}
}
