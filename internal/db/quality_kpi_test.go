package db

import (
	"testing"
	"time"
)

// seedRunFull seeds a run with agent, engineer, sprint, issue, cost, and time.
func seedRunFull(t *testing.T, d *LocalDB, runID, agent, engineer, sprint, issueKey string, costUSD float64, startedAt time.Time) {
	t.Helper()
	_, err := d.Exec(`
		INSERT INTO runs (id, jira_issue_key, jira_sprint_id, agent_name, engineer_name, agent_type, user, workstation_id, started_at, cost_usd, status)
		VALUES (?, ?, ?, ?, ?, 'claude_code', 'tester', 'ws', ?, ?, 'done')
	`, runID, issueKey, sprint, agent, engineer, startedAt.Format(time.RFC3339), costUSD)
	if err != nil {
		t.Fatalf("seedRunFull: %v", err)
	}
}

// seedIterationStart seeds a task.iteration.start event for a run+issue.
func seedIterationStart(t *testing.T, d *LocalDB, runID, issueKey string) {
	t.Helper()
	_, err := d.Exec(`
		INSERT INTO events (run_id, layer, event_type, data, ts)
		VALUES (?, 3, 'task.iteration.start', json_object('issue_key', ?), ?)
	`, runID, issueKey, time.Now().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("seedIterationStart: %v", err)
	}
}

// now is a fixed reference for since-filter tests.
var now = time.Now()

// ---- RegressionRate ----

func TestRegressionRate_ByAgent(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()

	seedRunFull(t, d, "r1", "alpha", "Alice", "SP-1", "TASK-1", 0.10, now)
	seedRunFull(t, d, "r2", "alpha", "Alice", "SP-1", "TASK-2", 0.10, now)
	seedRunFull(t, d, "r3", "beta", "Bob", "SP-1", "TASK-3", 0.10, now)
	// TASK-1 gets one iteration → regressed
	seedIterationStart(t, d, "r1", "TASK-1")

	rows, err := d.RegressionRate("agent", 0)
	if err != nil {
		t.Fatalf("RegressionRate: %v", err)
	}
	got := map[string]RegressionRow{}
	for _, r := range rows {
		got[r.GroupKey] = r
	}
	if got["alpha"].TotalTasks != 2 {
		t.Errorf("alpha total_tasks=%d want 2", got["alpha"].TotalTasks)
	}
	if got["alpha"].RegressedTasks != 1 {
		t.Errorf("alpha regressed=%d want 1", got["alpha"].RegressedTasks)
	}
	if got["beta"].RegressedTasks != 0 {
		t.Errorf("beta regressed=%d want 0", got["beta"].RegressedTasks)
	}
}

func TestRegressionRate_ByEngineer(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()

	seedRunFull(t, d, "r1", "alpha", "Alice", "SP-1", "TASK-1", 0.10, now)
	seedRunFull(t, d, "r2", "alpha", "Alice", "SP-1", "TASK-2", 0.10, now)
	seedIterationStart(t, d, "r1", "TASK-1")

	rows, err := d.RegressionRate("engineer", 0)
	if err != nil {
		t.Fatalf("RegressionRate engineer: %v", err)
	}
	got := map[string]RegressionRow{}
	for _, r := range rows {
		got[r.GroupKey] = r
	}
	if got["Alice"].TotalTasks != 2 {
		t.Errorf("Alice total=%d want 2", got["Alice"].TotalTasks)
	}
	if got["Alice"].RegressedTasks != 1 {
		t.Errorf("Alice regressed=%d want 1", got["Alice"].RegressedTasks)
	}
}

func TestRegressionRate_BySprint(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()

	seedRunFull(t, d, "r1", "alpha", "Alice", "SP-10", "TASK-1", 0.10, now)
	seedRunFull(t, d, "r2", "alpha", "Alice", "SP-10", "TASK-2", 0.10, now)
	seedRunFull(t, d, "r3", "beta", "Bob", "SP-11", "TASK-3", 0.10, now)
	seedIterationStart(t, d, "r1", "TASK-1")

	rows, err := d.RegressionRate("sprint", 0)
	if err != nil {
		t.Fatalf("RegressionRate sprint: %v", err)
	}
	got := map[string]RegressionRow{}
	for _, r := range rows {
		got[r.GroupKey] = r
	}
	if got["SP-10"].TotalTasks != 2 {
		t.Errorf("SP-10 total=%d want 2", got["SP-10"].TotalTasks)
	}
	if got["SP-10"].RegressedTasks != 1 {
		t.Errorf("SP-10 regressed=%d want 1", got["SP-10"].RegressedTasks)
	}
	if got["SP-11"].RegressedTasks != 0 {
		t.Errorf("SP-11 regressed=%d want 0", got["SP-11"].RegressedTasks)
	}
}

// ---- BugRate ----

func TestBugRate_ByAgent(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()

	seedRunFull(t, d, "r1", "alpha", "Alice", "SP-1", "TASK-1", 0.10, now)
	seedRunFull(t, d, "r2", "alpha", "Alice", "SP-1", "TASK-2", 0.10, now)
	seedRunFull(t, d, "r3", "beta", "Bob", "SP-1", "TASK-3", 0.10, now)
	seedBugFiled(t, d, "r1", "BUG-A")
	seedBugFiled(t, d, "r2", "BUG-B")

	rows, err := d.BugRate("agent", 0)
	if err != nil {
		t.Fatalf("BugRate: %v", err)
	}
	got := map[string]BugRateRow{}
	for _, r := range rows {
		got[r.GroupKey] = r
	}
	if got["alpha"].Runs != 2 {
		t.Errorf("alpha runs=%d want 2", got["alpha"].Runs)
	}
	if got["alpha"].Bugs != 2 {
		t.Errorf("alpha bugs=%d want 2", got["alpha"].Bugs)
	}
	// BugsPerRun = 2/2 = 1.0
	if got["alpha"].BugsPerRun != 1.0 {
		t.Errorf("alpha bugs_per_run=%.2f want 1.0", got["alpha"].BugsPerRun)
	}
	if got["beta"].Bugs != 0 {
		t.Errorf("beta bugs=%d want 0", got["beta"].Bugs)
	}
}

func TestBugRate_ByEngineer(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()

	seedRunFull(t, d, "r1", "alpha", "Alice", "SP-1", "TASK-1", 0.10, now)
	seedRunFull(t, d, "r2", "alpha", "Bob", "SP-1", "TASK-2", 0.10, now)
	seedBugFiled(t, d, "r1", "BUG-A")

	rows, err := d.BugRate("engineer", 0)
	if err != nil {
		t.Fatalf("BugRate engineer: %v", err)
	}
	got := map[string]BugRateRow{}
	for _, r := range rows {
		got[r.GroupKey] = r
	}
	if got["Alice"].Bugs != 1 {
		t.Errorf("Alice bugs=%d want 1", got["Alice"].Bugs)
	}
	if got["Bob"].Bugs != 0 {
		t.Errorf("Bob bugs=%d want 0", got["Bob"].Bugs)
	}
}

func TestBugRate_BySprint(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()

	seedRunFull(t, d, "r1", "alpha", "Alice", "SP-10", "TASK-1", 0.10, now)
	seedRunFull(t, d, "r2", "beta", "Bob", "SP-11", "TASK-2", 0.10, now)
	seedBugFiled(t, d, "r1", "BUG-A")

	rows, err := d.BugRate("sprint", 0)
	if err != nil {
		t.Fatalf("BugRate sprint: %v", err)
	}
	got := map[string]BugRateRow{}
	for _, r := range rows {
		got[r.GroupKey] = r
	}
	if got["SP-10"].Bugs != 1 {
		t.Errorf("SP-10 bugs=%d want 1", got["SP-10"].Bugs)
	}
	if got["SP-11"].Bugs != 0 {
		t.Errorf("SP-11 bugs=%d want 0", got["SP-11"].Bugs)
	}
}

// ---- QualityAdjustedCost ----

func TestQualityAdjustedCost_ByAgent(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()

	seedRunFull(t, d, "r1", "alpha", "Alice", "SP-1", "TASK-1", 0.50, now)
	seedRunFull(t, d, "r2", "alpha", "Alice", "SP-1", "TASK-1", 0.30, now) // second run same task
	seedRunFull(t, d, "r3", "beta", "Bob", "SP-1", "TASK-2", 0.20, now)
	seedIterationStart(t, d, "r1", "TASK-1")
	seedBugFiled(t, d, "r3", "BUG-A")

	rows, err := d.QualityAdjustedCost("agent", 0, 50)
	if err != nil {
		t.Fatalf("QualityAdjustedCost: %v", err)
	}
	got := map[string]TaskCostRow{}
	for _, r := range rows {
		got[r.IssueKey] = r
	}
	if got["TASK-1"].TotalCostUSD != 0.80 {
		t.Errorf("TASK-1 cost=%.2f want 0.80", got["TASK-1"].TotalCostUSD)
	}
	if got["TASK-1"].RunCount != 2 {
		t.Errorf("TASK-1 runs=%d want 2", got["TASK-1"].RunCount)
	}
	if got["TASK-1"].IterationCount != 1 {
		t.Errorf("TASK-1 iterations=%d want 1", got["TASK-1"].IterationCount)
	}
	if got["TASK-1"].IsClean {
		t.Error("TASK-1 should NOT be clean (has iteration)")
	}
	if got["TASK-2"].IsClean {
		t.Error("TASK-2 should NOT be clean (has bug)")
	}
	if got["TASK-1"].GroupKey != "alpha" {
		t.Errorf("TASK-1 group=%q want alpha", got["TASK-1"].GroupKey)
	}
}

func TestQualityAdjustedCost_ByEngineer(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()

	seedRunFull(t, d, "r1", "alpha", "Alice", "SP-1", "TASK-1", 0.50, now)
	seedRunFull(t, d, "r2", "beta", "Bob", "SP-1", "TASK-2", 0.20, now)

	rows, err := d.QualityAdjustedCost("engineer", 0, 50)
	if err != nil {
		t.Fatalf("QualityAdjustedCost engineer: %v", err)
	}
	got := map[string]TaskCostRow{}
	for _, r := range rows {
		got[r.IssueKey] = r
	}
	if got["TASK-1"].GroupKey != "Alice" {
		t.Errorf("TASK-1 group=%q want Alice", got["TASK-1"].GroupKey)
	}
	if got["TASK-2"].GroupKey != "Bob" {
		t.Errorf("TASK-2 group=%q want Bob", got["TASK-2"].GroupKey)
	}
}

func TestQualityAdjustedCost_BySprint(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()

	seedRunFull(t, d, "r1", "alpha", "Alice", "SP-10", "TASK-1", 0.50, now)
	seedRunFull(t, d, "r2", "beta", "Bob", "SP-11", "TASK-2", 0.20, now)

	rows, err := d.QualityAdjustedCost("sprint", 0, 50)
	if err != nil {
		t.Fatalf("QualityAdjustedCost sprint: %v", err)
	}
	got := map[string]TaskCostRow{}
	for _, r := range rows {
		got[r.IssueKey] = r
	}
	if got["TASK-1"].GroupKey != "SP-10" {
		t.Errorf("TASK-1 group=%q want SP-10", got["TASK-1"].GroupKey)
	}
	if got["TASK-2"].GroupKey != "SP-11" {
		t.Errorf("TASK-2 group=%q want SP-11", got["TASK-2"].GroupKey)
	}
}

// ---- Edge cases ----

func TestQualityKPI_Empty(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()

	rows1, err := d.RegressionRate("agent", 0)
	if err != nil {
		t.Fatalf("RegressionRate empty: %v", err)
	}
	if len(rows1) != 0 {
		t.Errorf("RegressionRate empty: got %d rows", len(rows1))
	}

	rows2, err := d.BugRate("agent", 0)
	if err != nil {
		t.Fatalf("BugRate empty: %v", err)
	}
	if len(rows2) != 0 {
		t.Errorf("BugRate empty: got %d rows", len(rows2))
	}

	rows3, err := d.QualityAdjustedCost("agent", 0, 50)
	if err != nil {
		t.Fatalf("QualityAdjustedCost empty: %v", err)
	}
	if len(rows3) != 0 {
		t.Errorf("QualityAdjustedCost empty: got %d rows", len(rows3))
	}
}

func TestQualityKPI_InvalidGroupBy(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()

	if _, err := d.RegressionRate("bogus", 0); err == nil {
		t.Error("RegressionRate: expected error for invalid groupBy")
	}
	if _, err := d.BugRate("bogus", 0); err == nil {
		t.Error("BugRate: expected error for invalid groupBy")
	}
	if _, err := d.QualityAdjustedCost("bogus", 0, 50); err == nil {
		t.Error("QualityAdjustedCost: expected error for invalid groupBy")
	}
}

func TestQualityKPI_NoSprintData(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()

	// Run with NULL jira_sprint_id — should not crash, group shows as ""
	seedRunFull(t, d, "r1", "alpha", "Alice", "", "TASK-1", 0.10, now)

	rows, err := d.RegressionRate("sprint", 0)
	if err != nil {
		t.Fatalf("RegressionRate no-sprint: %v", err)
	}
	// TASK-1 has no sprint — should appear under empty key ""
	got := map[string]RegressionRow{}
	for _, r := range rows {
		got[r.GroupKey] = r
	}
	if _, ok := got[""]; !ok {
		t.Errorf("expected empty-key group for NULL sprint; got %+v", got)
	}
}

func TestQualityKPI_SinceFilter(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()

	old := now.AddDate(0, 0, -60) // 60 days ago
	seedRunFull(t, d, "r1", "alpha", "Alice", "SP-1", "TASK-OLD", 0.10, old)
	seedRunFull(t, d, "r2", "alpha", "Alice", "SP-1", "TASK-NEW", 0.10, now)
	seedIterationStart(t, d, "r1", "TASK-OLD")

	// sinceDays=30: only r2 (TASK-NEW) should be in window
	rows, err := d.RegressionRate("agent", 30)
	if err != nil {
		t.Fatalf("RegressionRate since: %v", err)
	}
	got := map[string]RegressionRow{}
	for _, r := range rows {
		got[r.GroupKey] = r
	}
	if got["alpha"].TotalTasks != 1 {
		t.Errorf("alpha total_tasks=%d want 1 (only TASK-NEW in window)", got["alpha"].TotalTasks)
	}
	if got["alpha"].RegressedTasks != 0 {
		t.Errorf("alpha regressed=%d want 0 (TASK-OLD excluded)", got["alpha"].RegressedTasks)
	}
}
