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

func newTrendMux(t *testing.T) (*http.ServeMux, *db.LocalDB) {
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
	server.RegisterTrendRoutes(mux, store)
	return mux, store
}

func insertTrendRunServer(t *testing.T, store *db.LocalDB, id string, exitCode int, cost float64, when time.Time) {
	t.Helper()
	_, err := store.Exec(`
		INSERT INTO runs (id, agent_name, exit_code, cost_usd, user, workstation_id, started_at, status)
		VALUES (?, 'alpha', ?, ?, 'tester', 'ws1', ?, 'done')
	`, id, exitCode, cost, when.Format(time.RFC3339))
	if err != nil {
		t.Fatalf("insert run %s: %v", id, err)
	}
}

// TestTrendEndpoint_EmptyDB verifies all 3 trend endpoints return 200 + [] on empty DB.
func TestTrendEndpoint_EmptyDB(t *testing.T) {
	paths := []string{
		"/api/trends/success-rate?days=90",
		"/api/trends/cost?days=90",
		"/api/trends/rework-rate?days=90",
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			mux, _ := newTrendMux(t)
			req := httptest.NewRequest(http.MethodGet, path, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
			}
			var pts []db.TrendPoint
			if err := json.Unmarshal(w.Body.Bytes(), &pts); err != nil {
				t.Fatalf("decode: %v", err)
			}
			// All points must be gap points (HasData=false).
			for _, p := range pts {
				if p.HasData {
					t.Errorf("empty DB: HasData=true for week %s", p.WeekStart)
				}
			}
		})
	}
}

func TestTrendEndpoint_SuccessRateShape(t *testing.T) {
	mux, store := newTrendMux(t)

	now := time.Now().UTC()
	insertTrendRunServer(t, store, "r1", 0, 1.0, now)
	insertTrendRunServer(t, store, "r2", 1, 1.0, now)

	req := httptest.NewRequest(http.MethodGet, "/api/trends/success-rate?days=14", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
	}

	var pts []db.TrendPoint
	if err := json.Unmarshal(w.Body.Bytes(), &pts); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(pts) == 0 {
		t.Fatal("expected at least 1 point")
	}

	// Find the HasData point and verify shape.
	var found *db.TrendPoint
	for i := range pts {
		if pts[i].HasData {
			found = &pts[i]
			break
		}
	}
	if found == nil {
		t.Fatal("no HasData=true point found")
	}
	if found.RunCount != 2 {
		t.Errorf("run_count = %d, want 2", found.RunCount)
	}
	if found.Value != 50.0 {
		t.Errorf("value = %.1f, want 50.0 (1 success / 2 runs)", found.Value)
	}
	if found.WeekStart == "" {
		t.Error("WeekStart must not be empty")
	}
}

func TestTrendEndpoint_ContentTypeJSON(t *testing.T) {
	mux, _ := newTrendMux(t)
	req := httptest.NewRequest(http.MethodGet, "/api/trends/cost", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

func TestTrendEndpoint_JsonFieldsPresent(t *testing.T) {
	mux, store := newTrendMux(t)
	now := time.Now().UTC()
	insertTrendRunServer(t, store, "r1", 0, 2.5, now)

	req := httptest.NewRequest(http.MethodGet, "/api/trends/cost?days=14", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var raw []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("expected at least 1 point")
	}
	for _, field := range []string{"week_start", "value", "run_count", "has_data"} {
		if _, ok := raw[0][field]; !ok {
			t.Errorf("missing JSON field %q", field)
		}
	}
}
