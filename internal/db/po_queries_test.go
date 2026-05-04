package db

import (
	"testing"
	"time"
)

func seedPORun(t *testing.T, d *LocalDB, id, sprint, dept, project, eng string, started time.Time, cost, dur float64) {
	t.Helper()
	issueKey := ""
	if project != "" {
		issueKey = project + "-1"
	}
	_, err := d.Exec(`
		INSERT INTO runs (id, jira_issue_key, jira_sprint_id, agent_name, agent_type,
			user, workstation_id, started_at, duration_sec, status,
			cost_usd, engineer_name, department)
		VALUES (?, ?, ?, 'alpha', 'claude_code', 'u', 'ws', ?, ?, 'done', ?, ?, ?)
	`, id, issueKey, sprint, started.Format(time.RFC3339), dur, cost, eng, dept)
	if err != nil {
		t.Fatalf("seed run %s: %v", id, err)
	}
}

func TestListSprints_GroupsByID(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now().UTC()
	seedPORun(t, d, "r1", "CLITEST1-S1", "Platform", "CLITEST1", "alice", now.AddDate(0, 0, -3), 1.0, 600)
	seedPORun(t, d, "r2", "CLITEST1-S1", "Platform", "CLITEST1", "alice", now.AddDate(0, 0, -2), 2.0, 1200)
	seedPORun(t, d, "r3", "CLITEST2-S1", "Growth", "CLITEST2", "bob", now.AddDate(0, 0, -1), 3.0, 800)

	got, err := d.ListSprints()
	if err != nil {
		t.Fatalf("ListSprints: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 sprints, got %d", len(got))
	}
	for _, s := range got {
		if s.ID == "CLITEST1-S1" {
			if s.RunCount != 2 {
				t.Errorf("CLITEST1-S1 run_count = %d, want 2", s.RunCount)
			}
			if s.ProjectKey != "CLITEST1" {
				t.Errorf("CLITEST1-S1 project_key = %q, want CLITEST1", s.ProjectKey)
			}
			if s.TotalCost != 3.0 {
				t.Errorf("CLITEST1-S1 total_cost = %v, want 3.0", s.TotalCost)
			}
		}
	}
}

func TestSprintBurndown_GroupsByDay(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	base := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	seedPORun(t, d, "r1", "S-1", "Platform", "P", "a", base, 1.0, 600)
	seedPORun(t, d, "r2", "S-1", "Platform", "P", "a", base.Add(2*time.Hour), 2.0, 1200)
	seedPORun(t, d, "r3", "S-1", "Platform", "P", "a", base.AddDate(0, 0, 1), 0.5, 300)

	got, err := d.SprintBurndown("S-1")
	if err != nil {
		t.Fatalf("SprintBurndown: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 days, got %d (%+v)", len(got), got)
	}
	if got[0].Runs != 2 || got[0].Cost != 3.0 {
		t.Errorf("day 0 = %+v, want runs=2 cost=3.0", got[0])
	}
}

func TestSprintRuns_ScopesToSprint(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	base := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	seedPORun(t, d, "r1", "S-1", "Platform", "PROJ-1", "alpha", base, 1.0, 600)
	seedPORun(t, d, "r2", "S-1", "Platform", "PROJ-2", "alpha", base.Add(time.Hour), 2.0, 800)
	seedPORun(t, d, "r3", "S-2", "Platform", "PROJ-3", "beta", base.Add(2*time.Hour), 0.5, 400)

	got, err := d.SprintRuns("S-1")
	if err != nil {
		t.Fatalf("SprintRuns: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 runs in S-1, got %d", len(got))
	}
	for _, r := range got {
		if r.JiraSprintID != "S-1" {
			t.Errorf("got run from sprint %q, want S-1", r.JiraSprintID)
		}
	}
}

func TestCostByDepartment_FilterByDept(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	base := time.Now().UTC().Add(-2 * 24 * time.Hour)
	seedPORun(t, d, "r1", "S-1", "Platform", "P1", "a", base, 1.0, 600)
	seedPORun(t, d, "r2", "S-1", "Growth", "P2", "b", base.Add(time.Hour), 2.0, 800)

	all, err := d.CostByDepartment(POFilter{})
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("want 2 dept rows unfiltered, got %d", len(all))
	}

	platform, err := d.CostByDepartment(POFilter{Dept: "Platform"})
	if err != nil {
		t.Fatalf("platform: %v", err)
	}
	if len(platform) != 1 || platform[0].Department != "Platform" || platform[0].Cost != 1.0 {
		t.Errorf("dept=Platform = %+v, want 1 row Platform cost=1.0", platform)
	}
}

func TestDailyCostSeries_PadsZeroes(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	got, err := d.DailyCostSeries(14)
	if err != nil {
		t.Fatalf("DailyCostSeries: %v", err)
	}
	if len(got) != 14 {
		t.Errorf("len = %d, want 14", len(got))
	}
	for _, p := range got {
		if p.Cost != 0 || p.Day == "" {
			t.Errorf("expected zero-cost padded row, got %+v", p)
		}
	}
}

func TestTaskLifecycle_OrdersByStarted(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	// Insert with custom issue key + multiple runs.
	for i, h := range []int{2, 0, 1} {
		_, err := d.Exec(`INSERT INTO runs (id, jira_issue_key, agent_name, agent_type, user, workstation_id, started_at, duration_sec, status, cost_usd)
			VALUES (?, 'CLITEST-1', 'alpha', 'claude_code', 'u', 'ws', ?, 60, 'done', 0.5)`,
			"lr-"+string(rune('a'+i)), base.Add(time.Duration(h)*time.Hour).Format(time.RFC3339))
		if err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	got, err := d.TaskLifecycle("CLITEST-1")
	if err != nil {
		t.Fatalf("TaskLifecycle: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 runs, got %d", len(got))
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].StartedAt > got[i].StartedAt {
			t.Errorf("not sorted: %s before %s", got[i-1].StartedAt, got[i].StartedAt)
		}
	}
}

func TestLeadTimeDistribution_BucketsCorrectly(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	base := time.Now().UTC().Add(-24 * time.Hour)
	// 0-1h: 1 run (1800s); 1-4h: 2 runs (5400s, 10800s); 4-12h: 1 run (20000s); 12h+: 1 run (50000s).
	durations := []float64{1800, 5400, 10800, 20000, 50000}
	for i, dur := range durations {
		seedPORun(t, d, "ld-"+string(rune('a'+i)), "S-X", "Platform", "P", "a",
			base.Add(time.Duration(i)*time.Minute), 0.1, dur)
	}
	got, err := d.LeadTimeDistribution(POFilter{})
	if err != nil {
		t.Fatalf("LeadTimeDistribution: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("want 4 buckets, got %d", len(got))
	}
	want := map[string]int{"0-1h": 1, "1-4h": 2, "4-12h": 1, "12h+": 1}
	for _, b := range got {
		if b.Count != want[b.Label] {
			t.Errorf("bucket %s count = %d, want %d", b.Label, b.Count, want[b.Label])
		}
	}
}
