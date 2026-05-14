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

func newPRCycleMux(t *testing.T) (*http.ServeMux, *db.LocalDB) {
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
	server.RegisterPRCycleRoutes(mux, store)
	return mux, store
}

func TestPRCycleEndpoint_EmptyDB_NoData(t *testing.T) {
	mux, _ := newPRCycleMux(t)
	req := httptest.NewRequest(http.MethodGet, "/api/metrics/pr-cycle-time?days=28", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
	}
	var res db.PRCycleResult
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.HasData {
		t.Error("expected HasData=false on empty pr_events")
	}
	if res.WindowDays != 28 {
		t.Errorf("window_days = %d, want 28", res.WindowDays)
	}
}

func TestPRCycleEndpoint_DefaultDays(t *testing.T) {
	mux, _ := newPRCycleMux(t)
	req := httptest.NewRequest(http.MethodGet, "/api/metrics/pr-cycle-time", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var res db.PRCycleResult
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if res.WindowDays != 28 {
		t.Errorf("window_days = %d, want 28 default", res.WindowDays)
	}
}

func TestPRCycleEndpoint_DaysClamped(t *testing.T) {
	mux, _ := newPRCycleMux(t)
	req := httptest.NewRequest(http.MethodGet, "/api/metrics/pr-cycle-time?days=9999", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var res db.PRCycleResult
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if res.WindowDays != 365 {
		t.Errorf("window_days = %d, want 365 (clamped)", res.WindowDays)
	}
}

func TestPRCycleEndpoint_FullResponseShape(t *testing.T) {
	mux, store := newPRCycleMux(t)
	now := time.Now().UTC()

	submitted := now.Add(-24 * time.Hour)
	approval := submitted.Add(5 * time.Hour)
	subStr := submitted.Format(time.RFC3339)
	mergedStr := now.Format(time.RFC3339)
	apprStr := approval.Format(time.RFC3339)

	if err := store.UpsertPR(db.PREvent{
		Repo: "o/r", PRNumber: 1, Title: "feat: x", State: "merged",
		CreatedAt: subStr, SubmittedAt: subStr,
		MergedAt: &mergedStr, ClosedAt: &mergedStr,
		FirstApprovalAt: &apprStr,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/metrics/pr-cycle-time?days=28", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
	}
	var raw map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, field := range []string{"median_hours", "p75_hours", "merged_total", "with_approval", "window_days", "has_data"} {
		if _, ok := raw[field]; !ok {
			t.Errorf("missing JSON field %q", field)
		}
	}
	if raw["has_data"] != true {
		t.Errorf("has_data = %v, want true", raw["has_data"])
	}
}
