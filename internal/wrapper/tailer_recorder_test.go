package wrapper

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/phuc-nt/dandori-cli/internal/db"
	"github.com/phuc-nt/dandori-cli/internal/event"
)

// openTempDB opens a fresh dandori SQLite DB and runs migrations. Local helper
// so this test file is self-contained and the wrapper package keeps its
// existing unit-test setup intact.
func openTempDB(t *testing.T) *db.LocalDB {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	localDB, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { localDB.Close() })
	if err := localDB.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return localDB
}

func insertPendingRunForTailer(t *testing.T, localDB *db.LocalDB, runID string) {
	t.Helper()
	_, err := localDB.Exec(`
        INSERT INTO runs (id, agent_type, user, workstation_id, started_at, status)
        VALUES (?, 'claude_code', 'tester', 'ws-test', datetime('now'), 'pending')
    `, runID)
	if err != nil {
		t.Fatalf("insert pending run: %v", err)
	}
}

// TestTailSessionLog_EmitsEvents drops the fixture into a snapshot directory,
// runs the tailer with a recorder, and asserts that ≥4 distinct event types
// were persisted to the events table. This is the GREEN target for phase 01.
func TestTailSessionLog_EmitsEvents(t *testing.T) {
	localDB := openTempDB(t)
	rec := event.NewRecorder(localDB)
	runID := "tailer-test-1"
	insertPendingRunForTailer(t, localDB, runID)

	usage := parseAndRecordFixture(t, "testdata/session-with-tools.jsonl", rec, runID)
	if usage.Input == 0 {
		t.Errorf("token regression: input=%d", usage.Input)
	}

	rows, err := localDB.Query(`SELECT event_type, COUNT(*) FROM events WHERE run_id=? GROUP BY event_type`, runID)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var et string
		var n int
		if err := rows.Scan(&et, &n); err != nil {
			t.Fatalf("scan: %v", err)
		}
		counts[et] = n
	}
	if counts["tool.use"] < 4 {
		t.Errorf("tool.use=%d, want >=4 (got %v)", counts["tool.use"], counts)
	}
	if counts["tool.result"] < 4 {
		t.Errorf("tool.result=%d, want >=4 (got %v)", counts["tool.result"], counts)
	}
	if counts["skill.invoke"] < 1 {
		t.Errorf("skill.invoke=%d, want >=1 (got %v)", counts["skill.invoke"], counts)
	}
}

// TestTailSessionLog_NilRecorder_NoOp confirms the tailer still parses tokens
// when no recorder is attached (callers using TailSessionLog without phase-01
// wiring must not break).
func TestTailSessionLog_NilRecorder_NoOp(t *testing.T) {
	usage := parseAndRecordFixture(t, "testdata/session-with-tools.jsonl", nil, "")
	if usage.Input == 0 {
		t.Errorf("nil-recorder regression: input=%d", usage.Input)
	}
}

// parseAndRecordFixture exercises parseLogFromOffsetWithRecorder, which is the
// new variant we expect to add. Wraps the call in a context so future expansion
// (e.g. follow-the-tail mode) can plug in.
func parseAndRecordFixture(t *testing.T, path string, rec *event.Recorder, runID string) TokenUsage {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = ctx
	usage, _ := parseLogFromOffsetWithRecorder(path, 0, rec, runID)
	return usage
}
