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

func newRcaMux(t *testing.T) (*http.ServeMux, *db.LocalDB) {
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
	server.RegisterRcaRoutes(mux, store)
	return mux, store
}

func insertRcaAttribution(t *testing.T, store *db.LocalDB, key, outcomesJSON string, doneAt time.Time) {
	t.Helper()
	_, err := store.Exec(`
		INSERT INTO task_attribution
			(jira_issue_key, session_count, total_lines_final,
			 lines_attributed_agent, lines_attributed_human,
			 session_outcomes, jira_done_at, computed_at)
		VALUES (?, 1, 0, 0, 0, ?, ?, datetime('now'))
	`, key, outcomesJSON, doneAt.UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("insert attribution %s: %v", key, err)
	}
}

func TestRcaEndpoint_EmptyDB(t *testing.T) {
	mux, _ := newRcaMux(t)
	req := httptest.NewRequest(http.MethodGet, "/api/rca/breakdown?since=28d", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
	}
	var rows []db.RcaRow
	if err := json.Unmarshal(w.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("expected empty array on empty DB, got %d rows", len(rows))
	}
}

func TestRcaEndpoint_ReturnsCauses(t *testing.T) {
	mux, store := newRcaMux(t)

	now := time.Now().UTC()
	insertRcaAttribution(t, store, "BUG-1", `{"lint_fail":2,"test_fail":1}`, now.AddDate(0, 0, -3))

	req := httptest.NewRequest(http.MethodGet, "/api/rca/breakdown?since=28d", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
	}

	var rows []db.RcaRow
	if err := json.Unmarshal(w.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("expected at least one RCA row")
	}

	idx := map[string]db.RcaRow{}
	for _, r := range rows {
		idx[r.Cause] = r
	}

	if r, ok := idx["lint_fail"]; !ok {
		t.Error("missing lint_fail")
	} else if r.Count != 2 {
		t.Errorf("lint_fail count = %d, want 2", r.Count)
	}

	if r, ok := idx["test_fail"]; !ok {
		t.Error("missing test_fail")
	} else if r.Count != 1 {
		t.Errorf("test_fail count = %d, want 1", r.Count)
	}
}

func TestRcaEndpoint_ContentTypeJSON(t *testing.T) {
	mux, _ := newRcaMux(t)
	req := httptest.NewRequest(http.MethodGet, "/api/rca/breakdown", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

func TestRcaEndpoint_JsonShape(t *testing.T) {
	mux, store := newRcaMux(t)
	now := time.Now().UTC()
	insertRcaAttribution(t, store, "X-1", `{"timeout":3}`, now.AddDate(0, 0, -1))

	req := httptest.NewRequest(http.MethodGet, "/api/rca/breakdown?since=28", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// Verify the JSON has the expected field names by decoding into a generic map.
	var raw []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode generic: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("expected at least one row")
	}
	row := raw[0]
	for _, field := range []string{"cause", "count", "pct", "top_agent", "top_task_type", "wow_delta"} {
		if _, ok := row[field]; !ok {
			t.Errorf("missing JSON field %q", field)
		}
	}
}
