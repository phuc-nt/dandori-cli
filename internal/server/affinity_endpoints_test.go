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

func newAffinityMux(t *testing.T) (*http.ServeMux, *db.LocalDB) {
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
	server.RegisterAffinityRoutes(mux, store)
	return mux, store
}

func insertAffinityRunServer(t *testing.T, store *db.LocalDB, id, agent, issueKey string, exitCode int) {
	t.Helper()
	_, err := store.Exec(`
		INSERT INTO runs (id, agent_name, jira_issue_key, exit_code, user, workstation_id, started_at, status)
		VALUES (?, ?, ?, ?, 'tester', 'ws1', ?, 'done')
	`, id, agent, issueKey, exitCode, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("insert run %s: %v", id, err)
	}
}

func TestAffinityEndpoint_EmptyDB(t *testing.T) {
	mux, _ := newAffinityMux(t)
	req := httptest.NewRequest(http.MethodGet, "/api/analytics/agent-task-affinity?since=28d", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
	}
	var cells []db.AffinityCell
	if err := json.Unmarshal(w.Body.Bytes(), &cells); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(cells) != 0 {
		t.Errorf("expected empty array, got %d cells", len(cells))
	}
}

func TestAffinityEndpoint_ReturnsMatrix(t *testing.T) {
	mux, store := newAffinityMux(t)

	insertAffinityRunServer(t, store, "r1", "alpha", "FEAT-1", 0)
	insertAffinityRunServer(t, store, "r2", "alpha", "FEAT-2", 1)
	insertAffinityRunServer(t, store, "r3", "beta", "BUG-1", 0)

	req := httptest.NewRequest(http.MethodGet, "/api/analytics/agent-task-affinity?since=28d", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
	}

	var cells []db.AffinityCell
	if err := json.Unmarshal(w.Body.Bytes(), &cells); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(cells) != 2 {
		t.Fatalf("expected 2 cells, got %d: %+v", len(cells), cells)
	}

	idx := map[string]db.AffinityCell{}
	for _, c := range cells {
		idx[c.Agent+"|"+c.TaskType] = c
	}

	if _, ok := idx["alpha|Feat"]; !ok {
		t.Error("missing (alpha, Feat)")
	}
	if _, ok := idx["beta|Bug"]; !ok {
		t.Error("missing (beta, Bug)")
	}
}

func TestAffinityEndpoint_ContentTypeJSON(t *testing.T) {
	mux, _ := newAffinityMux(t)
	req := httptest.NewRequest(http.MethodGet, "/api/analytics/agent-task-affinity", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

func TestParseSinceDays(t *testing.T) {
	// parseSinceDays is package-internal but tested via endpoint behaviour;
	// exercise boundary values via direct-exported path. Since the function
	// is unexported we verify it through integration tests on the handler.
	cases := []struct {
		query   string
		wantMin int // cells should reflect this many days back
	}{
		{"28d", 28},
		{"90d", 90},
		{"28", 28},
	}
	for _, tc := range cases {
		mux, store := newAffinityMux(t)
		// Insert one run exactly `wantMin` days ago (within window)
		ts := time.Now().UTC().AddDate(0, 0, -(tc.wantMin - 1))
		_, err := store.Exec(`
			INSERT INTO runs (id, agent_name, jira_issue_key, exit_code, user, workstation_id, started_at, status)
			VALUES (?, 'alpha', 'TEST-1', 0, 'u', 'ws', ?, 'done')
		`, "r-"+tc.query, ts.Format(time.RFC3339))
		if err != nil {
			t.Fatalf("insert: %v", err)
		}
		req := httptest.NewRequest(http.MethodGet, "/api/analytics/agent-task-affinity?since="+tc.query, nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("since=%s: status %d", tc.query, w.Code)
		}
		var cells []db.AffinityCell
		_ = json.Unmarshal(w.Body.Bytes(), &cells)
		if len(cells) == 0 {
			t.Errorf("since=%s: expected ≥1 cell (run within window), got 0", tc.query)
		}
	}
}
