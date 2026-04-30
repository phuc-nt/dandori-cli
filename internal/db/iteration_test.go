package db

import (
	"testing"
	"time"
)

func insertRunForIteration(t *testing.T, d *LocalDB, runID, issueKey, status string, startedAt time.Time) {
	t.Helper()
	_, err := d.Exec(`
		INSERT INTO runs (id, jira_issue_key, agent_type, user, workstation_id, started_at, status)
		VALUES (?, ?, 'claude_code', 'tester', 'ws-1', ?, ?)
	`, runID, issueKey, startedAt.Format(time.RFC3339), status)
	if err != nil {
		t.Fatalf("insert run: %v", err)
	}
}

func insertIterationEvent(t *testing.T, d *LocalDB, runID, dataJSON string) {
	t.Helper()
	_, err := d.Exec(`
		INSERT INTO events (run_id, layer, event_type, data, ts)
		VALUES (?, 4, 'task.iteration.start', ?, ?)
	`, runID, dataJSON, time.Now().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("insert event: %v", err)
	}
}

func insertRunWithDept(t *testing.T, d *LocalDB, runID, dept, status string, startedAt time.Time) {
	t.Helper()
	_, err := d.Exec(`
		INSERT INTO runs (id, agent_type, user, workstation_id, started_at, status, department)
		VALUES (?, 'claude_code', 'tester', 'ws-1', ?, ?, ?)
	`, runID, startedAt.Format(time.RFC3339), status, dept)
	if err != nil {
		t.Fatalf("insert run: %v", err)
	}
}

func TestTotalRunIDs_WindowAndDepartment(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()

	end := time.Date(2026, 4, 29, 0, 0, 0, 0, time.UTC)
	start := end.AddDate(0, 0, -28)

	insertRunWithDept(t, d, "r-old", "payments", "done", start.Add(-24*time.Hour)) // before window
	insertRunWithDept(t, d, "r-1", "payments", "done", start)                      // boundary inclusive
	insertRunWithDept(t, d, "r-2", "payments", "cancelled", start.Add(time.Hour))  // cancelled stays
	insertRunWithDept(t, d, "r-3", "platform", "done", start.Add(2*time.Hour))     // other team
	insertRunWithDept(t, d, "r-end", "payments", "done", end)                      // exclusive end excluded

	all, err := d.TotalRunIDs(start, end, "")
	if err != nil {
		t.Fatalf("total all: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("all count=%d want 3 (r-1, r-2, r-3)", len(all))
	}

	pay, err := d.TotalRunIDs(start, end, "payments")
	if err != nil {
		t.Fatalf("total payments: %v", err)
	}
	if len(pay) != 2 {
		t.Errorf("payments count=%d want 2 (r-1, r-2)", len(pay))
	}
}

func TestReworkRunIDs_RoundFilter(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()

	end := time.Date(2026, 4, 29, 0, 0, 0, 0, time.UTC)
	start := end.AddDate(0, 0, -28)

	insertRunWithDept(t, d, "r-1", "payments", "done", start.Add(time.Hour))
	insertIterationEvent(t, d, "r-1", `{"round":1,"issue_key":"KEY-1","transitioned_at":"2026-04-10T00:00:00Z"}`)

	insertRunWithDept(t, d, "r-2", "payments", "done", start.Add(2*time.Hour))
	insertIterationEvent(t, d, "r-2", `{"round":2,"issue_key":"KEY-2","transitioned_at":"2026-04-10T00:00:00Z"}`)
	insertIterationEvent(t, d, "r-2", `{"round":3,"issue_key":"KEY-2","transitioned_at":"2026-04-11T00:00:00Z"}`)

	insertRunWithDept(t, d, "r-3", "platform", "cancelled", start.Add(3*time.Hour))
	insertIterationEvent(t, d, "r-3", `{"round":2,"issue_key":"KEY-3","transitioned_at":"2026-04-12T00:00:00Z"}`)

	insertRunWithDept(t, d, "r-old", "payments", "done", start.Add(-72*time.Hour))
	insertIterationEvent(t, d, "r-old", `{"round":2,"issue_key":"KEY-OLD","transitioned_at":"2026-04-01T00:00:00Z"}`)

	all, err := d.ReworkRunIDs(start, end, "")
	if err != nil {
		t.Fatalf("rework all: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("all rework=%d want 2 (r-2, r-3 — r-1 round=1, r-old before window)", len(all))
	}

	pay, err := d.ReworkRunIDs(start, end, "payments")
	if err != nil {
		t.Fatalf("rework payments: %v", err)
	}
	if len(pay) != 1 || pay[0] != "r-2" {
		t.Errorf("payments rework=%v want [r-2]", pay)
	}
}

func TestLatestRunForIssue(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()

	t0 := time.Date(2026, 4, 20, 9, 0, 0, 0, time.UTC)
	insertRunForIteration(t, d, "r1", "KEY-1", "done", t0)
	insertRunForIteration(t, d, "r2", "KEY-1", "done", t0.Add(2*time.Hour))
	insertRunForIteration(t, d, "r3", "KEY-1", "done", t0.Add(4*time.Hour))
	insertRunForIteration(t, d, "rX", "OTHER-99", "done", t0.Add(time.Hour))

	got, err := d.LatestRunForIssue("KEY-1")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got == nil {
		t.Fatal("got nil, want r3")
	}
	if got.ID != "r3" {
		t.Errorf("id=%q, want r3", got.ID)
	}
	if got.Status != "done" {
		t.Errorf("status=%q", got.Status)
	}
}

func TestLatestRunForIssue_None(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()
	got, err := d.LatestRunForIssue("NOPE-1")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != nil {
		t.Errorf("got %+v, want nil", got)
	}
}

func TestIterationEventsForIssue(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()

	t0 := time.Date(2026, 4, 20, 9, 0, 0, 0, time.UTC)
	insertRunForIteration(t, d, "r1", "KEY-1", "done", t0)
	insertRunForIteration(t, d, "r2", "KEY-1", "done", t0.Add(2*time.Hour))
	insertIterationEvent(t, d, "r2", `{"round":2,"issue_key":"KEY-1","transitioned_at":"2026-04-21T08:00:00Z"}`)
	insertIterationEvent(t, d, "r2", `{"round":3,"issue_key":"KEY-1","transitioned_at":"2026-04-22T08:00:00Z"}`)

	events, err := d.IterationEventsForIssue("KEY-1")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}

	rounds := map[int]bool{events[0].Round: true, events[1].Round: true}
	if !rounds[2] || !rounds[3] {
		t.Errorf("rounds=%v, want {2,3}", rounds)
	}
}

func TestIterationEventsForIssue_Empty(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()
	events, err := d.IterationEventsForIssue("NOPE-1")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("got %d, want 0", len(events))
	}
}
