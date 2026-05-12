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

func newTrustMux(t *testing.T) (*http.ServeMux, *db.LocalDB) {
	t.Helper()
	tmpDir := t.TempDir()
	store, err := db.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := store.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	mux := http.NewServeMux()
	server.RegisterTrustRoutes(mux, store)
	return mux, store
}

func TestTrustEndpoint_EmptyDB_NoData(t *testing.T) {
	mux, _ := newTrustMux(t)
	req := httptest.NewRequest(http.MethodGet, "/api/metrics/trust-index?days=28", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
	}
	var res db.TrustResult
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.HasData {
		t.Error("expected HasData=false on empty DB")
	}
	if res.Band != "no-data" {
		t.Errorf("band = %q, want no-data", res.Band)
	}
	if res.WindowDays != 28 {
		t.Errorf("window_days = %d, want 28", res.WindowDays)
	}
}

func TestTrustEndpoint_DefaultDays(t *testing.T) {
	mux, _ := newTrustMux(t)
	req := httptest.NewRequest(http.MethodGet, "/api/metrics/trust-index", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var res db.TrustResult
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if res.WindowDays != 28 {
		t.Errorf("window_days = %d, want 28 default", res.WindowDays)
	}
}

func TestTrustEndpoint_FullResponseShape(t *testing.T) {
	mux, store := newTrustMux(t)
	now := time.Now().UTC()

	if _, err := store.Exec(`
		INSERT INTO task_attribution
			(jira_issue_key, session_count, total_lines_final,
			 lines_attributed_agent, lines_attributed_human,
			 jira_done_at, computed_at, total_iterations)
		VALUES ('FEAT-1', 1, 100, 80, 20, ?, ?, 1)
	`, now.Format(time.RFC3339), now.Format(time.RFC3339)); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	if _, err := store.Exec(`
		INSERT INTO runs (id, agent_name, jira_issue_key, exit_code, cost_usd, user, workstation_id, started_at, status, human_intervention_count)
		VALUES ('r1', 'alpha', 'FEAT-1', 0, 1.0, 'tester', 'ws1', ?, 'done', 0)
	`, now.Format(time.RFC3339)); err != nil {
		t.Fatalf("insert run: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/metrics/trust-index?days=28", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
	}
	var raw map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, field := range []string{"value", "band", "components", "window_days", "has_data"} {
		if _, ok := raw[field]; !ok {
			t.Errorf("missing JSON field %q", field)
		}
	}
	comps, ok := raw["components"].(map[string]any)
	if !ok {
		t.Fatal("components is not an object")
	}
	for _, field := range []string{"acceptance", "ai_cfr", "intervention_rate"} {
		if _, ok := comps[field]; !ok {
			t.Errorf("missing components.%s", field)
		}
	}
}

func TestTrustEndpoint_DaysClamped(t *testing.T) {
	mux, _ := newTrustMux(t)
	req := httptest.NewRequest(http.MethodGet, "/api/metrics/trust-index?days=9999", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var res db.TrustResult
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if res.WindowDays != 365 {
		t.Errorf("window_days = %d, want 365 (clamped)", res.WindowDays)
	}
}
