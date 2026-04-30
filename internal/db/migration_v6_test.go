package db

import (
	"testing"
)

// TestMigration_V6_BackfillsJiraDoneAtToUTC covers CLITEST2-14:
// rows stored before v6 carry timezone offsets like +07:00. The window-scan
// in AggregateAttribution binds bounds as Z and string-compares, dropping
// non-Z rows. v6 backfills those to UTC Z form.
func TestMigration_V6_BackfillsJiraDoneAtToUTC(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Seed pre-v6 style rows: mixed offsets, including one already-Z control.
	rows := []struct {
		key  string
		done string
	}{
		{"TASK-A", "2026-04-30T10:33:32+07:00"},
		{"TASK-B", "2026-04-30T03:33:32-05:00"},
		{"TASK-C", "2026-04-30T03:33:32Z"},
	}
	for _, r := range rows {
		if _, err := d.Exec(
			`INSERT INTO task_attribution (jira_issue_key, session_count, total_lines_final,
			 lines_attributed_agent, lines_attributed_human, jira_done_at)
			 VALUES (?, 1, 0, 0, 0, ?)`,
			r.key, r.done,
		); err != nil {
			t.Fatalf("seed %s: %v", r.key, err)
		}
	}

	// Force re-run the backfill statement (Migrate already advanced past it
	// during newEmptyLocalDB's first Migrate call, so call the SQL directly).
	if _, err := d.Exec(MigrationV5ToV6); err != nil {
		t.Fatalf("apply v5->v6 backfill: %v", err)
	}

	want := map[string]string{
		"TASK-A": "2026-04-30T03:33:32Z",
		"TASK-B": "2026-04-30T08:33:32Z",
		"TASK-C": "2026-04-30T03:33:32Z",
	}
	for key, expected := range want {
		var got string
		if err := d.QueryRow(`SELECT jira_done_at FROM task_attribution WHERE jira_issue_key=?`, key).Scan(&got); err != nil {
			t.Fatalf("scan %s: %v", key, err)
		}
		if got != expected {
			t.Errorf("%s: jira_done_at = %q, want %q", key, got, expected)
		}
	}
}
