package db

import (
	"encoding/json"
	"testing"
	"time"
)

// Sets up a database at v8 (apply schema, force version back to 8, then seed
// legacy task_attribution rows). Calling d.Migrate() should run the v8→v9
// data rewrite.
func newV8DBWithLegacyAttribution(t *testing.T) *LocalDB {
	t.Helper()
	d := newEmptyLocalDB(t)
	if _, err := d.Exec(SchemaSQL); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	if _, err := d.Exec(`DELETE FROM schema_version`); err != nil {
		t.Fatalf("reset version: %v", err)
	}
	if _, err := d.Exec(`INSERT INTO schema_version (version) VALUES (8)`); err != nil {
		t.Fatalf("set version 8: %v", err)
	}
	return d
}

func seedLegacyAttribution(t *testing.T, d *LocalDB, key, outcomesJSON string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := d.Exec(`
		INSERT INTO task_attribution (jira_issue_key, session_count, total_lines_final,
			lines_attributed_agent, lines_attributed_human, session_outcomes, jira_done_at)
		VALUES (?, 1, 50, 50, 0, ?, ?)
	`, key, outcomesJSON, now); err != nil {
		t.Fatalf("seed %s: %v", key, err)
	}
}

func TestMigrationV9_RewritesMapShape(t *testing.T) {
	d := newV8DBWithLegacyAttribution(t)
	// Production wrapper output: map[wrapperReason]int.
	seedLegacyAttribution(t, d, "MAPS-1", `{"agent_finished":2,"error":1}`)

	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var raw string
	if err := d.QueryRow(`SELECT session_outcomes FROM task_attribution WHERE jira_issue_key = 'MAPS-1'`).Scan(&raw); err != nil {
		t.Fatalf("read back: %v", err)
	}
	var got map[string]int
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("unmarshal: %v (raw=%s)", err, raw)
	}
	if got[string(ReasonAgentFinished)] != 2 || got[string(ReasonError)] != 1 {
		t.Errorf("post-migration shape wrong: %+v", got)
	}
}

func TestMigrationV9_RewritesArrayShape(t *testing.T) {
	d := newV8DBWithLegacyAttribution(t)
	// Phase 04 ad-hoc seed: []map[string]any with free-text reason.
	seedLegacyAttribution(t, d, "ARRS-1", `[{"reason":"test failure"},{"reason":"lint violation"},{"reason":"timeout"}]`)

	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var raw string
	if err := d.QueryRow(`SELECT session_outcomes FROM task_attribution WHERE jira_issue_key = 'ARRS-1'`).Scan(&raw); err != nil {
		t.Fatalf("read back: %v", err)
	}
	var got map[string]int
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("unmarshal: %v (raw=%s)", err, raw)
	}
	if got[string(ReasonTestFail)] != 1 || got[string(ReasonLintFail)] != 1 || got[string(ReasonTimeout)] != 1 {
		t.Errorf("post-migration shape wrong: %+v", got)
	}
}

func TestMigrationV9_MalformedJSONLeftAlone(t *testing.T) {
	d := newV8DBWithLegacyAttribution(t)
	seedLegacyAttribution(t, d, "BAD-1", `not valid json {{{`)

	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate must not fail on bad row: %v", err)
	}

	var raw string
	_ = d.QueryRow(`SELECT session_outcomes FROM task_attribution WHERE jira_issue_key = 'BAD-1'`).Scan(&raw)
	if raw != `not valid json {{{` {
		t.Errorf("malformed row should be preserved verbatim, got %q", raw)
	}
}

func TestMigrationV9_Idempotent(t *testing.T) {
	d := newV8DBWithLegacyAttribution(t)
	seedLegacyAttribution(t, d, "IDEM-1", `{"agent_finished":1}`)

	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate first: %v", err)
	}
	// Second migrate is a no-op because schema_version >= 9 now.
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate second: %v", err)
	}

	var raw string
	if err := d.QueryRow(`SELECT session_outcomes FROM task_attribution WHERE jira_issue_key = 'IDEM-1'`).Scan(&raw); err != nil {
		t.Fatalf("read back: %v", err)
	}
	var got map[string]int
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got[string(ReasonAgentFinished)] != 1 {
		t.Errorf("idempotent migrate corrupted data: %+v", got)
	}
}
