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

func newKPIMux(t *testing.T) (*http.ServeMux, *db.LocalDB) {
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
	server.RegisterKPIRoutes(mux, store)
	return mux, store
}

func TestKPIStrip_EmptyDB(t *testing.T) {
	mux, _ := newKPIMux(t)

	req := httptest.NewRequest(http.MethodGet, "/api/kpi/strip", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
	}

	var resp server.KPIStripResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Days) != 14 {
		t.Errorf("days length = %d, want 14", len(resp.Days))
	}
	if resp.Current.Runs != 0 || resp.Prior.Runs != 0 {
		t.Errorf("empty DB should yield 0 totals, got current=%+v prior=%+v", resp.Current, resp.Prior)
	}
}

func TestKPIStrip_WoWSplit(t *testing.T) {
	mux, store := newKPIMux(t)

	now := time.Now().UTC()
	insert := func(id string, when time.Time, cost float64) {
		_, err := store.Exec(`
			INSERT INTO runs (id, user, workstation_id, started_at, status, cost_usd, input_tokens, output_tokens)
			VALUES (?, 'phuc', 'ws1', ?, 'success', ?, 100, 50)
		`, id, when.Format(time.RFC3339), cost)
		if err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	// Current week (last 7 days incl today): 2 runs, $3 total.
	insert("c1", now, 1.0)
	insert("c2", now.AddDate(0, 0, -3), 2.0)
	// Prior week (8-14 days ago): 1 run, $0.5.
	insert("p1", now.AddDate(0, 0, -10), 0.5)

	req := httptest.NewRequest(http.MethodGet, "/api/kpi/strip", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var resp server.KPIStripResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Current.Runs != 2 || resp.Current.Cost != 3.0 {
		t.Errorf("current = %+v, want runs=2 cost=3", resp.Current)
	}
	if resp.Prior.Runs != 1 || resp.Prior.Cost != 0.5 {
		t.Errorf("prior = %+v, want runs=1 cost=0.5", resp.Prior)
	}
}
