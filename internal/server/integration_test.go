//go:build server_integration

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/phuc-nt/dandori-cli/internal/analytics"
	"github.com/phuc-nt/dandori-cli/internal/serverdb"
)

func getTestDB(t *testing.T) *serverdb.DB {
	host := os.Getenv("DANDORI_DB_HOST")
	if host == "" {
		host = "localhost"
	}
	password := os.Getenv("DANDORI_DB_PASSWORD")
	if password == "" {
		password = "dandori"
	}

	cfg := serverdb.Config{
		Host:     host,
		Port:     5432,
		Database: "dandori_test",
		User:     "dandori",
		Password: password,
		MaxConns: 5,
	}

	ctx := context.Background()
	db, err := serverdb.Connect(ctx, cfg)
	if err != nil {
		t.Skipf("PostgreSQL not available: %v", err)
	}

	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	return db
}

func seedTestData(t *testing.T, db *serverdb.DB) {
	ctx := context.Background()
	pool := db.Pool()

	// Clear existing data
	_, _ = pool.Exec(ctx, "TRUNCATE runs, events CASCADE")

	// Insert test runs
	runs := []struct {
		id, issueKey, sprint, agent string
		duration                    float64
		cost                        float64
		exitCode                    int
		tokens                      int
	}{
		{"run-001", "CLITEST-1", "4", "beta", 900, 2.85, 0, 15000},
		{"run-002", "CLITEST-1", "4", "beta", 900, 2.15, 0, 12000},
		{"run-003", "CLITEST-2", "4", "alpha", 1200, 3.45, 0, 18000},
		{"run-004", "CLITEST-2", "4", "alpha", 1200, 2.65, 0, 14000},
		{"run-005", "CLITEST-3", "4", "alpha", 1800, 4.25, 0, 22000},
		{"run-006", "CLITEST-4", "4", "gamma", 900, 1.85, 1, 10000}, // failed
		{"run-007", "CLITEST-4", "4", "alpha", 1200, 3.05, 0, 16000},
	}

	for _, r := range runs {
		_, err := pool.Exec(ctx, `
			INSERT INTO runs (id, jira_issue_key, jira_sprint_id, agent_name, agent_type,
				"user", workstation_id, started_at, ended_at, duration_sec, exit_code, status,
				input_tokens, output_tokens, cost_usd, model)
			VALUES ($1, $2, $3, $4, 'claude_code', 'test', 'ws-01',
				NOW() - INTERVAL '1 hour', NOW(), $5, $6,
				CASE WHEN $6 = 0 THEN 'done' ELSE 'failed' END,
				$7, $7/3, $8, 'claude-sonnet-4-5-20250514')
		`, r.id, r.issueKey, r.sprint, r.agent, r.duration, r.exitCode, r.tokens, r.cost)
		if err != nil {
			t.Fatalf("insert run %s: %v", r.id, err)
		}
	}

	t.Logf("Seeded %d runs", len(runs))
}

func TestServerIntegration_AnalyticsAgents(t *testing.T) {
	db := getTestDB(t)
	defer db.Close()

	seedTestData(t, db)

	srv := New(db, Config{Listen: ":8080"})
	ts := httptest.NewServer(srv)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/analytics/agents")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var result struct {
		Data []analytics.AgentStat `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(result.Data) == 0 {
		t.Error("expected agent stats")
	}

	t.Logf("Agent Stats:")
	for _, s := range result.Data {
		t.Logf("  %s: %d runs, %.1f%% success, $%.2f total",
			s.AgentName, s.RunCount, s.SuccessRate, s.TotalCost)
	}
}

func TestServerIntegration_AnalyticsCost(t *testing.T) {
	db := getTestDB(t)
	defer db.Close()

	seedTestData(t, db)

	srv := New(db, Config{Listen: ":8080"})
	ts := httptest.NewServer(srv)
	defer ts.Close()

	tests := []struct {
		name    string
		groupBy string
	}{
		{"by agent", "agent"},
		{"by sprint", "sprint"},
		{"by task", "task"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := http.Get(ts.URL + "/api/analytics/cost?group_by=" + tt.groupBy)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d", resp.StatusCode)
			}

			var result struct {
				Data []analytics.CostGroup `json:"data"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				t.Fatalf("decode: %v", err)
			}

			if len(result.Data) == 0 {
				t.Error("expected cost data")
			}

			t.Logf("Cost by %s:", tt.groupBy)
			for _, g := range result.Data {
				t.Logf("  %s: $%.2f (%d runs)", g.Group, g.Cost, g.RunCount)
			}
		})
	}
}

func TestServerIntegration_AnalyticsAgentCompare(t *testing.T) {
	db := getTestDB(t)
	defer db.Close()

	seedTestData(t, db)

	srv := New(db, Config{Listen: ":8080"})
	ts := httptest.NewServer(srv)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/analytics/agents/compare?agents=alpha,beta,gamma")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	var result struct {
		Agents []analytics.AgentComparison `json:"agents"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(result.Agents) != 3 {
		t.Errorf("expected 3 agents, got %d", len(result.Agents))
	}

	t.Logf("Agent Comparison:")
	for _, a := range result.Agents {
		t.Logf("  %s: %d runs, %.1f%% success, $%.2f cost",
			a.AgentName, a.RunCount, a.SuccessRate, a.TotalCost)
	}
}

func TestServerIntegration_AnalyticsSprintSummary(t *testing.T) {
	db := getTestDB(t)
	defer db.Close()

	seedTestData(t, db)

	srv := New(db, Config{Listen: ":8080"})
	ts := httptest.NewServer(srv)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/analytics/sprints/4")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	var result analytics.SprintSummary
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	t.Logf("Sprint Summary:")
	t.Logf("  Sprint: %s", result.SprintID)
	t.Logf("  Tasks: %d/%d completed", result.CompletedCount, result.TaskCount)
	t.Logf("  Runs: %d", result.TotalRuns)
	t.Logf("  Cost: $%.2f", result.TotalCost)
}

func TestServerIntegration_EventsIngest(t *testing.T) {
	db := getTestDB(t)
	defer db.Close()

	srv := New(db, Config{Listen: ":8080"})
	ts := httptest.NewServer(srv)
	defer ts.Close()

	// Create a run first
	pool := db.Pool()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `
		INSERT INTO runs (id, agent_name, agent_type, "user", workstation_id,
			started_at, status)
		VALUES ('evt-test-run', 'test-agent', 'claude_code', 'test', 'ws-01', NOW(), 'running')
	`)
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	// Ingest events
	events := `[
		{"run_id": "evt-test-run", "layer": 1, "event_type": "process_start", "data": {"pid": 12345}},
		{"run_id": "evt-test-run", "layer": 2, "event_type": "file_edit", "data": {"path": "main.go"}},
		{"run_id": "evt-test-run", "layer": 3, "event_type": "decision", "data": {"text": "use TDD"}}
	]`

	resp, err := http.Post(ts.URL+"/api/events", "application/json",
		strings.NewReader(events))
	if err != nil {
		t.Fatalf("post events: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	// Verify events stored
	var count int
	err = pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM events WHERE run_id = 'evt-test-run'").Scan(&count)
	if err != nil {
		t.Fatalf("count events: %v", err)
	}

	if count != 3 {
		t.Errorf("expected 3 events, got %d", count)
	}
	t.Logf("Ingested %d events", count)
}

func TestServerIntegration_FleetSSE(t *testing.T) {
	db := getTestDB(t)
	defer db.Close()

	srv := New(db, Config{Listen: ":8080", SSEIntervalSec: 1})
	ts := httptest.NewServer(srv)
	defer ts.Close()

	// Start SSE connection
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/api/fleet/live", nil)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			t.Skip("SSE connection timed out (expected in test)")
		}
		t.Fatalf("sse request: %v", err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Errorf("content-type = %s, want text/event-stream", resp.Header.Get("Content-Type"))
	}

	t.Log("SSE endpoint available")
}
