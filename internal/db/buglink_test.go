package db

import (
	"testing"
	"time"
)

func seedRunForBug(t *testing.T, d *LocalDB, runID string) {
	t.Helper()
	_, err := d.Exec(`
		INSERT INTO runs (id, agent_type, user, workstation_id, started_at, status)
		VALUES (?, 'claude_code', 'tester', 'ws-1', ?, 'done')
	`, runID, time.Now().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func TestFindRunByPrefix_Match(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()
	seedRunForBug(t, d, "e1777abcdef9aa")

	got, err := d.FindRunByPrefix("e1777abcdef9")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != "e1777abcdef9aa" {
		t.Errorf("got %q, want e1777abcdef9aa", got)
	}
}

func TestFindRunByPrefix_NotFound(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()
	got, err := d.FindRunByPrefix("ffffffffffff")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestFindRunByPrefix_Ambiguous(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()
	seedRunForBug(t, d, "abc123def456aaa")
	seedRunForBug(t, d, "abc123def456bbb")

	_, err := d.FindRunByPrefix("abc123def456")
	if err == nil {
		t.Fatal("expected ambiguous error, got nil")
	}
}

func TestBugEventExists_True(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()
	seedRunForBug(t, d, "run-1")
	_, err := d.Exec(`
		INSERT INTO events (run_id, layer, event_type, data, ts)
		VALUES ('run-1', 4, 'bug.filed', '{"bug_key":"BUG-9"}', ?)
	`, time.Now().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("seed event: %v", err)
	}

	got, err := d.BugEventExists("BUG-9")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !got {
		t.Errorf("got false, want true")
	}
}

func TestBugEventExists_False(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()
	got, err := d.BugEventExists("BUG-NONE")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got {
		t.Errorf("got true, want false")
	}
}
