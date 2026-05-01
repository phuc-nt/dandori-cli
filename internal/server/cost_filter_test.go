package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/phuc-nt/dandori-cli/internal/db"
	"github.com/phuc-nt/dandori-cli/internal/server"
)

// setupCostFilterDB creates a test DB with runs across different projects/dates.
func setupCostFilterDB(t *testing.T) *db.LocalDB {
	t.Helper()
	tmpDir := t.TempDir()
	store, err := db.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := store.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return store
}

// seedCostRun inserts a run with specific issue key and started_at.
func seedCostRun(t *testing.T, store *db.LocalDB, runID, issueKey string, costUSD float64, startedAt time.Time) {
	t.Helper()
	_, err := store.Exec(`
		INSERT INTO runs (id, jira_issue_key, agent_name, agent_type, user, workstation_id,
		                  started_at, cost_usd, status, input_tokens, output_tokens)
		VALUES (?, ?, 'claude', 'claude', 'tester', 'ws-1', ?, ?, 'done', 1000, 300)
	`, runID, issueKey, startedAt.UTC().Format(time.RFC3339), costUSD)
	if err != nil {
		t.Fatalf("seedCostRun %s: %v", runID, err)
	}
}

// newCostMux builds a mux with /api/cost/task and /api/cost/day handlers from cmd layer.
// We test the server-layer plumbing by calling the registered handlers directly via
// a test HTTP server. To avoid importing cmd (circular), we expose handler factories
// from the server package.
func newCostMux(t *testing.T, store *db.LocalDB) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	server.RegisterCostRoutes(mux, store)
	return mux
}

func costGet(t *testing.T, mux *http.ServeMux, path string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w.Code, w.Body.Bytes()
}

// TestCostTaskEndpoint_PeriodFilter seeds an old run and a recent run,
// then verifies that ?period=7d returns only the recent one.
func TestCostTaskEndpoint_PeriodFilter(t *testing.T) {
	store := setupCostFilterDB(t)
	defer store.Close()

	now := time.Now().UTC()
	seedCostRun(t, store, "recent", "PROJ-1", 1.00, now.Add(-1*24*time.Hour))
	seedCostRun(t, store, "old", "PROJ-2", 2.00, now.Add(-60*24*time.Hour))

	mux := newCostMux(t, store)
	status, body := costGet(t, mux, "/api/cost/task?period=7d")
	if status != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", status, body)
	}
	var groups []map[string]any
	if err := json.Unmarshal(body, &groups); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, body)
	}
	if len(groups) != 1 {
		t.Errorf("period=7d: got %d groups, want 1 (only recent run)", len(groups))
	}
	if len(groups) > 0 {
		if groups[0]["Group"] != "PROJ-1" && groups[0]["group"] != "PROJ-1" {
			// Check both capitalization conventions.
			g, _ := groups[0]["Group"].(string)
			g2, _ := groups[0]["group"].(string)
			if g != "PROJ-1" && g2 != "PROJ-1" {
				t.Errorf("expected PROJ-1, got group=%v", groups[0])
			}
		}
	}
}

// TestCostDayEndpoint_ProjectFilter seeds runs for two projects and verifies
// that ?project=CLITEST returns only CLITEST-* day buckets.
func TestCostDayEndpoint_ProjectFilter(t *testing.T) {
	store := setupCostFilterDB(t)
	defer store.Close()

	now := time.Now().UTC()
	seedCostRun(t, store, "cli1", "CLITEST-1", 1.00, now.Add(-1*24*time.Hour))
	seedCostRun(t, store, "cli2", "CLITEST-2", 1.00, now.Add(-2*24*time.Hour))
	seedCostRun(t, store, "oth1", "OTHER-1", 5.00, now.Add(-1*24*time.Hour))

	mux := newCostMux(t, store)
	status, body := costGet(t, mux, "/api/cost/day?project=CLITEST")
	if status != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", status, body)
	}
	var groups []map[string]any
	if err := json.Unmarshal(body, &groups); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, body)
	}
	// CLITEST-1 and CLITEST-2 are on different days → 2 day buckets.
	if len(groups) != 2 {
		t.Errorf("project=CLITEST: got %d day groups, want 2", len(groups))
	}
	// Total cost should be 2.00 (not 7.00 which would include OTHER-1).
	total := 0.0
	for _, g := range groups {
		c, _ := g["Cost"].(float64)
		total += c
	}
	if total < 1.99 || total > 2.01 {
		t.Errorf("total cost for CLITEST = %.2f, want ~2.00", total)
	}
}

// TestCostTaskEndpoint_BadDate verifies that ?period=custom&from=BAD returns HTTP 400.
func TestCostTaskEndpoint_BadDate(t *testing.T) {
	store := setupCostFilterDB(t)
	defer store.Close()

	mux := newCostMux(t, store)
	status, _ := costGet(t, mux, "/api/cost/task?period=custom&from=BAD&to=2026-01-01")
	if status != http.StatusBadRequest {
		t.Errorf("bad date: status=%d want 400", status)
	}
}
