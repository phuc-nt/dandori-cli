package db

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func seedRunForIntent(t *testing.T, d *LocalDB, runID string) {
	t.Helper()
	_, err := d.Exec(`
		INSERT INTO runs (id, jira_issue_key, agent_name, agent_type, user, workstation_id, started_at, status)
		VALUES (?, 'CLITEST-1', 'claude-code', 'claude_code', 'tester', 'ws-1', ?, 'done')
	`, runID, time.Now().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("seed run: %v", err)
	}
}

func TestGetIntentEvents_NoEvents(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()
	seedRunForIntent(t, d, "r1")

	result, err := d.GetIntentEvents("r1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Intent != nil {
		t.Errorf("expected nil Intent, got %+v", result.Intent)
	}
	if len(result.Decisions) != 0 {
		t.Errorf("expected 0 decisions, got %d", len(result.Decisions))
	}
}

func TestGetIntentEvents_IntentOnly(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()
	seedRunForIntent(t, d, "r2")

	insertEvent(t, d, "r2", "intent.extracted", map[string]any{
		"first_user_msg": "Fix the bug",
		"summary":        "Patched it",
		"spec_links": map[string]any{
			"jira_key":        "CLITEST-1",
			"confluence_urls": []any{"https://acme.atlassian.net/wiki/abc"},
			"source_paths":    []any{},
		},
	})

	result, err := d.GetIntentEvents("r2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Intent == nil {
		t.Fatal("expected Intent, got nil")
	}
	if result.Intent.FirstUserMsg != "Fix the bug" {
		t.Errorf("first_user_msg=%q", result.Intent.FirstUserMsg)
	}
	if result.Intent.Summary != "Patched it" {
		t.Errorf("summary=%q", result.Intent.Summary)
	}
	if result.Intent.SpecLinks.JiraKey != "CLITEST-1" {
		t.Errorf("jira_key=%q", result.Intent.SpecLinks.JiraKey)
	}
	if len(result.Intent.SpecLinks.ConfluenceURLs) != 1 {
		t.Errorf("confluence_urls len=%d, want 1", len(result.Intent.SpecLinks.ConfluenceURLs))
	}
	if len(result.Decisions) != 0 {
		t.Errorf("expected 0 decisions, got %d", len(result.Decisions))
	}
}

func TestGetIntentEvents_IntentWithDecisions(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()
	seedRunForIntent(t, d, "r3")

	insertEvent(t, d, "r3", "intent.extracted", map[string]any{
		"first_user_msg": "Add hashing",
		"summary":        "Used bcrypt",
		"spec_links":     map[string]any{"jira_key": "", "confluence_urls": []any{}, "source_paths": []any{}},
	})
	insertEvent(t, d, "r3", "decision.point", map[string]any{
		"chosen":    "bcrypt",
		"rejected":  []any{"argon2"},
		"rationale": "already in deps",
	})
	insertEvent(t, d, "r3", "decision.point", map[string]any{
		"chosen":    "SHA-256 for tokens",
		"rejected":  []any{},
		"rationale": "",
	})

	result, err := d.GetIntentEvents("r3")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Intent == nil {
		t.Fatal("expected Intent")
	}
	if len(result.Decisions) != 2 {
		t.Errorf("expected 2 decisions, got %d", len(result.Decisions))
	}
	if result.Decisions[0].Chosen != "bcrypt" {
		t.Errorf("decision[0].chosen=%q, want bcrypt", result.Decisions[0].Chosen)
	}
	if len(result.Decisions[0].Rejected) != 1 || result.Decisions[0].Rejected[0] != "argon2" {
		t.Errorf("decision[0].rejected=%v, want [argon2]", result.Decisions[0].Rejected)
	}
}

func TestGetIntentEvents_DecisionsWithoutIntent(t *testing.T) {
	// Decision.point rows without intent.extracted → Decisions present, Intent nil.
	d := setupTestDB(t)
	defer d.Close()
	seedRunForIntent(t, d, "r4")

	insertEvent(t, d, "r4", "decision.point", map[string]any{
		"chosen":    "option A",
		"rejected":  []any{"option B"},
		"rationale": "faster",
	})

	result, err := d.GetIntentEvents("r4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Intent != nil {
		t.Errorf("expected nil Intent (no intent.extracted row)")
	}
	if len(result.Decisions) != 1 {
		t.Errorf("expected 1 decision, got %d", len(result.Decisions))
	}
}

func TestGetIntentEvents_MalformedEventSkipped(t *testing.T) {
	// A malformed JSON row should be silently skipped; other rows still parse.
	d := setupTestDB(t)
	defer d.Close()
	seedRunForIntent(t, d, "r5")

	// Insert a malformed intent.extracted row.
	_, err := d.Exec(`
		INSERT INTO events (run_id, layer, event_type, data, ts)
		VALUES (?, 4, 'intent.extracted', 'not-json', ?)
	`, "r5", "2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("insert malformed: %v", err)
	}

	// Insert a valid decision.point after the malformed row.
	insertEvent(t, d, "r5", "decision.point", map[string]any{
		"chosen":    "good choice",
		"rejected":  []any{},
		"rationale": "",
	})

	result, err := d.GetIntentEvents("r5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Malformed intent row skipped → nil Intent.
	if result.Intent != nil {
		t.Errorf("malformed intent.extracted should be skipped")
	}
	// Valid decision still parsed.
	if len(result.Decisions) != 1 {
		t.Errorf("expected 1 decision, got %d", len(result.Decisions))
	}
}

func TestGetIntentEvents_WrongRunID(t *testing.T) {
	// Events for run "r-other" must not appear for run "r-mine".
	d := setupTestDB(t)
	defer d.Close()
	seedRunForIntent(t, d, "r-mine")
	seedRunForIntent(t, d, "r-other")

	insertEvent(t, d, "r-other", "intent.extracted", map[string]any{
		"first_user_msg": "other run msg",
		"summary":        "other",
		"spec_links":     map[string]any{"jira_key": "", "confluence_urls": []any{}, "source_paths": []any{}},
	})

	result, err := d.GetIntentEvents("r-mine")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Intent != nil {
		t.Errorf("should not see events from another run")
	}
}

// ---- GetRecentIntentEvents tests ----

// seedRunWithEngineer inserts a run row with a specific engineer name.
func seedRunWithEngineer(t *testing.T, d *LocalDB, runID, engineer string, ts time.Time) {
	t.Helper()
	_, err := d.Exec(`
		INSERT INTO runs (id, jira_issue_key, agent_name, agent_type, user, workstation_id,
		                  engineer_name, started_at, status)
		VALUES (?, 'K-1', 'claude-code', 'claude_code', 'tester', 'ws-1', ?, ?, 'done')
	`, runID, engineer, ts.UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("seedRunWithEngineer %s: %v", runID, err)
	}
}

// insertIntentEventAtTime inserts a layer-4 event with an explicit timestamp.
func insertIntentEventAtTime(t *testing.T, d *LocalDB, runID, eventType string, data map[string]any, ts time.Time) {
	t.Helper()
	raw, _ := json.Marshal(data)
	_, err := d.Exec(`
		INSERT INTO events (run_id, layer, event_type, data, ts)
		VALUES (?, 4, ?, ?, ?)
	`, runID, eventType, string(raw), ts.UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("insertIntentEventAtTime %s: %v", runID, err)
	}
}

func TestGetRecentIntentEvents_LimitAndOrder(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()

	base := time.Now().Add(-60 * time.Minute)
	// Insert 30 runs + events
	for i := 0; i < 30; i++ {
		runID := fmt.Sprintf("r-limit-%02d", i)
		ts := base.Add(time.Duration(i) * time.Minute)
		seedRunWithEngineer(t, d, runID, "alice", ts)
		insertIntentEventAtTime(t, d, runID, "decision.point", map[string]any{
			"chosen": fmt.Sprintf("opt-%d", i),
		}, ts)
	}

	events, err := d.GetRecentIntentEvents(20, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 20 {
		t.Errorf("expected 20 events, got %d", len(events))
	}
	// Verify descending order (newest first)
	for i := 1; i < len(events); i++ {
		if events[i-1].TS.Before(events[i].TS) {
			t.Errorf("events not descending: [%d] %v < [%d] %v",
				i-1, events[i-1].TS, i, events[i].TS)
		}
	}
}

func TestGetRecentIntentEvents_EngineerFilter(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()

	base := time.Now().Add(-10 * time.Minute)

	// alice: 5 events
	for i := 0; i < 5; i++ {
		runID := fmt.Sprintf("r-alice-%d", i)
		ts := base.Add(time.Duration(i) * time.Minute)
		seedRunWithEngineer(t, d, runID, "alice", ts)
		insertIntentEventAtTime(t, d, runID, "decision.point", map[string]any{
			"chosen": "alice-opt",
		}, ts)
	}
	// bob: 3 events
	for i := 0; i < 3; i++ {
		runID := fmt.Sprintf("r-bob-%d", i)
		ts := base.Add(time.Duration(i) * time.Minute)
		seedRunWithEngineer(t, d, runID, "bob", ts)
		insertIntentEventAtTime(t, d, runID, "decision.point", map[string]any{
			"chosen": "bob-opt",
		}, ts)
	}

	// Filter empty → all 8
	all, err := d.GetRecentIntentEvents(50, "", "")
	if err != nil {
		t.Fatalf("all filter: %v", err)
	}
	if len(all) != 8 {
		t.Errorf("empty filter: expected 8 events, got %d", len(all))
	}

	// Filter "alice" → 5
	aliceEvents, err := d.GetRecentIntentEvents(50, "alice", "")
	if err != nil {
		t.Fatalf("alice filter: %v", err)
	}
	if len(aliceEvents) != 5 {
		t.Errorf("alice filter: expected 5 events, got %d", len(aliceEvents))
	}
	for _, ev := range aliceEvents {
		if ev.EngineerName != "alice" {
			t.Errorf("non-alice event returned: engineer=%q", ev.EngineerName)
		}
	}
}

// seedRunWithKey inserts a run with both an engineer and a specific jira_issue_key
// so project-filter tests can target the prefix match.
func seedRunWithKey(t *testing.T, d *LocalDB, runID, engineer, jiraKey string, ts time.Time) {
	t.Helper()
	_, err := d.Exec(`
		INSERT INTO runs (id, jira_issue_key, agent_name, agent_type, user, workstation_id,
		                  engineer_name, started_at, status)
		VALUES (?, ?, 'claude-code', 'claude_code', 'tester', 'ws-1', ?, ?, 'done')
	`, runID, jiraKey, engineer, ts.UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("seedRunWithKey %s: %v", runID, err)
	}
}

// TestGetRecentIntentEvents_ProjectFilter verifies project="" returns all events
// and project="<KEY>" returns only events whose run.jira_issue_key starts with
// "<KEY>-".
func TestGetRecentIntentEvents_ProjectFilter(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()

	base := time.Now().Add(-30 * time.Minute)

	// 4 CLITEST events.
	for i := 0; i < 4; i++ {
		runID := fmt.Sprintf("r-cli-%d", i)
		ts := base.Add(time.Duration(i) * time.Minute)
		seedRunWithKey(t, d, runID, "alice", fmt.Sprintf("CLITEST-%d", i+1), ts)
		insertIntentEventAtTime(t, d, runID, "decision.point", map[string]any{"k": i}, ts)
	}
	// 2 DEMO events.
	for i := 0; i < 2; i++ {
		runID := fmt.Sprintf("r-demo-%d", i)
		ts := base.Add(time.Duration(10+i) * time.Minute)
		seedRunWithKey(t, d, runID, "bob", fmt.Sprintf("DEMO-%d", i+1), ts)
		insertIntentEventAtTime(t, d, runID, "decision.point", map[string]any{"k": i}, ts)
	}

	all, err := d.GetRecentIntentEvents(50, "", "")
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	if len(all) != 6 {
		t.Errorf("all: expected 6, got %d", len(all))
	}

	cli, err := d.GetRecentIntentEvents(50, "", "CLITEST")
	if err != nil {
		t.Fatalf("project=CLITEST: %v", err)
	}
	if len(cli) != 4 {
		t.Errorf("project=CLITEST: expected 4, got %d", len(cli))
	}
	for _, ev := range cli {
		if !strings.HasPrefix(ev.JiraIssueKey, "CLITEST-") {
			t.Errorf("CLITEST filter leaked: jira_issue_key=%q", ev.JiraIssueKey)
		}
	}

	// Engineer + project compose: alice + CLITEST → 4 (alice owns all CLITESTs).
	composed, err := d.GetRecentIntentEvents(50, "alice", "CLITEST")
	if err != nil {
		t.Fatalf("composed: %v", err)
	}
	if len(composed) != 4 {
		t.Errorf("alice+CLITEST: expected 4, got %d", len(composed))
	}

	// alice + DEMO → 0 (DEMO is bob's).
	mismatched, err := d.GetRecentIntentEvents(50, "alice", "DEMO")
	if err != nil {
		t.Fatalf("mismatched: %v", err)
	}
	if len(mismatched) != 0 {
		t.Errorf("alice+DEMO: expected 0, got %d", len(mismatched))
	}
}
