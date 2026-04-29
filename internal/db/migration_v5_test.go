package db

import (
	"testing"
)

// TestMigration_V5_AddsSessionEndAndAttribution covers the G7 schema bump.
// Five new columns on runs (session_end_reason + four message counters) and
// a brand-new task_attribution table indexed by jira_done_at.
func TestMigration_V5_AddsSessionEndAndAttribution(t *testing.T) {
	d := newEmptyLocalDB(t)

	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	for _, col := range []string{
		"session_end_reason",
		"human_message_count",
		"agent_message_count",
		"human_intervention_count",
		"human_approval_count",
	} {
		if !columnExists(t, d, "runs", col) {
			t.Errorf("runs.%s column missing", col)
		}
	}

	for _, col := range []string{
		"jira_issue_key",
		"session_count",
		"total_lines_final",
		"lines_attributed_agent",
		"lines_attributed_human",
		"intervention_rate",
		"jira_done_at",
	} {
		if !columnExists(t, d, "task_attribution", col) {
			t.Errorf("task_attribution.%s column missing", col)
		}
	}

	var n int
	if err := d.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_attribution_done_at'`,
	).Scan(&n); err != nil {
		t.Fatalf("query idx: %v", err)
	}
	if n != 1 {
		t.Error("idx_attribution_done_at missing")
	}

	var version int
	if err := d.QueryRow(`SELECT MAX(version) FROM schema_version`).Scan(&version); err != nil {
		t.Fatalf("read schema_version: %v", err)
	}
	if version != SchemaVersion {
		t.Fatalf("schema_version=%d want %d", version, SchemaVersion)
	}
	if SchemaVersion != 5 {
		t.Fatalf("SchemaVersion const=%d want 5 (G7)", SchemaVersion)
	}
}

// TestMigration_V5_Idempotent ensures Migrate() is safe to call twice — needed
// for upgrade flows where the binary restarts with an already-migrated DB.
func TestMigration_V5_Idempotent(t *testing.T) {
	d := newEmptyLocalDB(t)

	if err := d.Migrate(); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if err := d.Migrate(); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
}

// TestMigration_V5_PreservesRunRows covers the upgrade path: a v4 DB with
// production-shaped run rows must keep them intact after the v4→v5 ALTERs.
func TestMigration_V5_PreservesRunRows(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	_, err := d.Exec(`INSERT INTO runs
		(id, agent_name, agent_type, user, workstation_id, started_at, status, cost_usd)
		VALUES ('r-keep', 'alpha', 'claude_code', 'u', 'w', '2026-04-01T00:00:00Z', 'done', 2.25)`)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	if err := d.Migrate(); err != nil {
		t.Fatalf("re-migrate: %v", err)
	}

	var cost float64
	var endReason string
	row := d.QueryRow(`SELECT cost_usd, COALESCE(session_end_reason,'') FROM runs WHERE id='r-keep'`)
	if err := row.Scan(&cost, &endReason); err != nil {
		t.Fatalf("select: %v", err)
	}
	if cost != 2.25 {
		t.Errorf("cost lost: got %v want 2.25", cost)
	}
	if endReason != "" {
		t.Errorf("session_end_reason should be NULL/empty for legacy rows, got %q", endReason)
	}
}

// TestTaskAttribution_RoundTrip exercises the new table end to end via raw SQL.
// Higher-level helpers (Phase 03) will wrap this; here we only confirm storage.
func TestTaskAttribution_RoundTrip(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	_, err := d.Exec(`INSERT INTO task_attribution
		(jira_issue_key, session_count, total_lines_final, lines_attributed_agent, lines_attributed_human,
		 total_agent_tokens, total_agent_cost_usd, total_iterations, total_human_messages,
		 total_intervention_count, intervention_rate, session_outcomes, git_head_at_jira_done, jira_done_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"PROJ-1", 2, 100, 80, 20, 1500, 0.075, 1, 5, 1, 0.2,
		`{"agent_finished":1,"user_interrupted":1}`, "abc123", "2026-04-29T10:00:00Z",
	)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	var (
		sessionCount  int
		linesAgent    int
		linesHuman    int
		interventionR float64
		outcomes      string
	)
	row := d.QueryRow(`SELECT session_count, lines_attributed_agent, lines_attributed_human,
		intervention_rate, session_outcomes
		FROM task_attribution WHERE jira_issue_key=?`, "PROJ-1")
	if err := row.Scan(&sessionCount, &linesAgent, &linesHuman, &interventionR, &outcomes); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if sessionCount != 2 || linesAgent != 80 || linesHuman != 20 {
		t.Errorf("counts mismatch: sessions=%d agent=%d human=%d", sessionCount, linesAgent, linesHuman)
	}
	if interventionR != 0.2 {
		t.Errorf("intervention_rate=%v want 0.2", interventionR)
	}
	if outcomes == "" {
		t.Error("session_outcomes empty")
	}
}
