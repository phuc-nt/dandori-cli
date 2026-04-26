package db

import (
	"testing"
	"time"
)

func seedBugFiled(t *testing.T, d *LocalDB, runID, bugKey string) {
	t.Helper()
	_, err := d.Exec(`
		INSERT INTO events (run_id, layer, event_type, data, ts)
		VALUES (?, 3, 'bug.filed', json_object('bug_key', ?), ?)
	`, runID, bugKey, time.Now().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("seed event: %v", err)
	}
}

func seedRunForAgent(t *testing.T, d *LocalDB, runID, agent, issueKey string) {
	t.Helper()
	_, err := d.Exec(`
		INSERT INTO runs (id, jira_issue_key, agent_name, agent_type, user, workstation_id, started_at, status)
		VALUES (?, ?, ?, 'claude_code', 'tester', 'ws', ?, 'done')
	`, runID, issueKey, agent, time.Now().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("seed run: %v", err)
	}
}

func TestBugStats_ByAgent(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()
	seedRunForAgent(t, d, "r1", "alpha", "TASK-1")
	seedRunForAgent(t, d, "r2", "alpha", "TASK-2")
	seedRunForAgent(t, d, "r3", "beta", "TASK-3")
	seedBugFiled(t, d, "r1", "BUG-A")
	seedBugFiled(t, d, "r2", "BUG-B")
	seedBugFiled(t, d, "r3", "BUG-C")
	// Re-emit BUG-A on a different run — should still count once via DISTINCT
	seedBugFiled(t, d, "r2", "BUG-A")

	rows, err := d.BugStats("agent", 0)
	if err != nil {
		t.Fatalf("BugStats: %v", err)
	}
	got := map[string]int{}
	for _, r := range rows {
		got[r.GroupKey] = r.BugCount
	}
	if got["alpha"] != 2 {
		t.Errorf("alpha: got %d, want 2 (BUG-A + BUG-B distinct)", got["alpha"])
	}
	if got["beta"] != 1 {
		t.Errorf("beta: got %d, want 1", got["beta"])
	}
}

func TestBugStats_ByTask(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()
	seedRunForAgent(t, d, "r1", "alpha", "TASK-1")
	seedRunForAgent(t, d, "r2", "alpha", "TASK-2")
	seedBugFiled(t, d, "r1", "BUG-A")
	seedBugFiled(t, d, "r1", "BUG-B")
	seedBugFiled(t, d, "r2", "BUG-C")

	rows, err := d.BugStats("task", 0)
	if err != nil {
		t.Fatalf("BugStats: %v", err)
	}
	got := map[string]int{}
	for _, r := range rows {
		got[r.GroupKey] = r.BugCount
	}
	if got["TASK-1"] != 2 || got["TASK-2"] != 1 {
		t.Errorf("got %v, want TASK-1=2 TASK-2=1", got)
	}
}

func TestBugStats_InvalidGroupBy(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()
	if _, err := d.BugStats("sprint", 0); err == nil {
		t.Error("expected error for unsupported groupBy, got nil")
	}
}

func TestBugStats_Empty(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()
	rows, err := d.BugStats("agent", 0)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("got %d rows, want 0", len(rows))
	}
}
