package server_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// TestG9Insights_ReturnsCards seeds runs that trigger a WoW cost spike and
// verifies the /api/g9/insights endpoint returns an array of cards with the
// expected fields.
func TestG9Insights_ReturnsCards(t *testing.T) {
	store := setupG9DB(t)
	defer store.Close()

	now := time.Now().UTC()

	// Last week: 10 runs at $1 each
	for i := 0; i < 10; i++ {
		_, err := store.Exec(`
			INSERT INTO runs (id, agent_name, agent_type, user, workstation_id,
			                  engineer_name, jira_issue_key, started_at, cost_usd, status)
			VALUES (?, 'claude-code', 'claude_code', 'tester', 'ws-1', 'alice', 'SPIKE-1', ?, 1.0, 'done')
		`, fmt.Sprintf("ins-lw-%d", i), now.AddDate(0, 0, -10).UTC().Format(time.RFC3339))
		if err != nil {
			t.Fatalf("seed last-week run: %v", err)
		}
	}
	// This week: 5 runs at $3 each = $15 (ratio 1.5 → high severity spike)
	for i := 0; i < 5; i++ {
		_, err := store.Exec(`
			INSERT INTO runs (id, agent_name, agent_type, user, workstation_id,
			                  engineer_name, jira_issue_key, started_at, cost_usd, status)
			VALUES (?, 'claude-code', 'claude_code', 'tester', 'ws-1', 'alice', 'SPIKE-1', ?, 3.0, 'done')
		`, fmt.Sprintf("ins-tw-%d", i), now.Add(-time.Duration(i)*24*time.Hour).UTC().Format(time.RFC3339))
		if err != nil {
			t.Fatalf("seed this-week run: %v", err)
		}
	}

	mux := newG9Mux(store)
	status, body := g9Get(t, mux, "/api/g9/insights")
	if status != 200 {
		t.Fatalf("status=%d want 200; body=%s", status, body)
	}

	var cards []map[string]any
	if err := json.Unmarshal(body, &cards); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, body)
	}
	if len(cards) == 0 {
		t.Fatal("expected at least 1 card, got 0")
	}
	// Verify each card has required fields.
	for _, c := range cards {
		if c["id"] == nil {
			t.Errorf("card missing 'id': %v", c)
		}
		if c["severity"] == nil {
			t.Errorf("card missing 'severity': %v", c)
		}
		if c["title"] == nil {
			t.Errorf("card missing 'title': %v", c)
		}
		if c["body"] == nil {
			t.Errorf("card missing 'body': %v", c)
		}
	}
}

// TestG9Insights_ProjectScope seeds runs in two projects, scopes to CLITEST,
// and asserts no cards from the OTHER project bleed through.
func TestG9Insights_ProjectScope(t *testing.T) {
	store := setupG9DB(t)
	defer store.Close()

	now := time.Now().UTC()

	// CLITEST project: no spike — flat costs
	for i := 0; i < 5; i++ {
		_, err := store.Exec(`
			INSERT INTO runs (id, agent_name, agent_type, user, workstation_id,
			                  engineer_name, jira_issue_key, started_at, cost_usd, status)
			VALUES (?, 'claude-code', 'claude_code', 'tester', 'ws-1', 'alice', 'CLITEST-1', ?, 1.0, 'done')
		`, fmt.Sprintf("scope-cli-%d", i), now.Add(-time.Duration(i+1)*24*time.Hour).UTC().Format(time.RFC3339))
		if err != nil {
			t.Fatalf("seed CLITEST run: %v", err)
		}
	}

	// OTHER project: massive WoW spike to produce cards
	for i := 0; i < 10; i++ {
		_, err := store.Exec(`
			INSERT INTO runs (id, agent_name, agent_type, user, workstation_id,
			                  engineer_name, jira_issue_key, started_at, cost_usd, status)
			VALUES (?, 'claude-code', 'claude_code', 'tester', 'ws-1', 'bob', 'OTHER-1', ?, 1.0, 'done')
		`, fmt.Sprintf("scope-oth-lw-%d", i), now.AddDate(0, 0, -10).UTC().Format(time.RFC3339))
		if err != nil {
			t.Fatalf("seed OTHER last-week: %v", err)
		}
	}
	for i := 0; i < 5; i++ {
		_, err := store.Exec(`
			INSERT INTO runs (id, agent_name, agent_type, user, workstation_id,
			                  engineer_name, jira_issue_key, started_at, cost_usd, status)
			VALUES (?, 'claude-code', 'claude_code', 'tester', 'ws-1', 'bob', 'OTHER-1', ?, 3.0, 'done')
		`, fmt.Sprintf("scope-oth-tw-%d", i), now.Add(-time.Duration(i)*24*time.Hour).UTC().Format(time.RFC3339))
		if err != nil {
			t.Fatalf("seed OTHER this-week: %v", err)
		}
	}

	mux := newG9Mux(store)
	status, body := g9Get(t, mux, "/api/g9/insights?role=project&id=CLITEST")
	if status != 200 {
		t.Fatalf("status=%d want 200; body=%s", status, body)
	}

	var cards []map[string]any
	if err := json.Unmarshal(body, &cards); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, body)
	}

	// No wow-spike card should appear — CLITEST has no spike.
	for _, c := range cards {
		if c["id"] == "wow-spike" {
			t.Errorf("wow-spike card should not appear for CLITEST scope; OTHER spike must not bleed through")
		}
	}
}

// TestG9Insights_EmptyDB_ReturnsEmptyArray verifies that an empty DB returns
// [] (JSON array), not null.
func TestG9Insights_EmptyDB_ReturnsEmptyArray(t *testing.T) {
	store := setupG9DB(t)
	defer store.Close()

	mux := newG9Mux(store)
	status, body := g9Get(t, mux, "/api/g9/insights")
	if status != 200 {
		t.Fatalf("status=%d want 200; body=%s", status, body)
	}

	// Must parse as array, not null.
	var cards []map[string]any
	if err := json.Unmarshal(body, &cards); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, body)
	}
	if cards == nil {
		t.Error("expected empty array [], got null")
	}
	if len(cards) != 0 {
		t.Errorf("expected 0 cards on empty DB, got %d: %v", len(cards), cards)
	}
}
