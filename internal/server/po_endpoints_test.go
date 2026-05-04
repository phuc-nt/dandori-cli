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

func newPOMux(t *testing.T) (*http.ServeMux, *db.LocalDB) {
	t.Helper()
	tmpDir := t.TempDir()
	store, err := db.Open(filepath.Join(tmpDir, "po.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := store.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	mux := http.NewServeMux()
	server.RegisterPORoutes(mux, store)
	return mux, store
}

func seedPORun(t *testing.T, d *db.LocalDB, id, sprint, dept, project string, started time.Time, cost, dur float64) {
	t.Helper()
	issue := ""
	if project != "" {
		issue = project + "-1"
	}
	_, err := d.Exec(`
		INSERT INTO runs (id, jira_issue_key, jira_sprint_id, agent_name, agent_type,
			user, workstation_id, started_at, duration_sec, status,
			cost_usd, engineer_name, department)
		VALUES (?, ?, ?, 'alpha', 'claude_code', 'u', 'ws', ?, ?, 'done', ?, 'a', ?)
	`, id, issue, sprint, started.Format(time.RFC3339), dur, cost, dept)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func doGET(t *testing.T, mux *http.ServeMux, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

func TestPO_SprintsList_Empty(t *testing.T) {
	mux, _ := newPOMux(t)
	w := doGET(t, mux, "/api/sprints")
	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if w.Body.String() == "null\n" || w.Body.String() == "null" {
		t.Errorf("empty result should be [] not null, got %q", w.Body.String())
	}
}

func TestPO_SprintsList_GroupsByID(t *testing.T) {
	mux, store := newPOMux(t)
	now := time.Now().UTC().Add(-24 * time.Hour)
	seedPORun(t, store, "r1", "CLITEST1-S1", "Platform", "CLITEST1", now, 1.5, 600)
	seedPORun(t, store, "r2", "CLITEST1-S1", "Platform", "CLITEST1", now.Add(2*time.Hour), 2.0, 800)
	seedPORun(t, store, "r3", "CLITEST2-S1", "Growth", "CLITEST2", now.Add(time.Hour), 3.0, 1200)

	w := doGET(t, mux, "/api/sprints")
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	var got []db.SprintInfo
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("want 2 sprints, got %d", len(got))
	}
}

func TestPO_SprintBurndown_RequiresID(t *testing.T) {
	mux, _ := newPOMux(t)
	w := doGET(t, mux, "/api/sprints/burndown")
	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestPO_SprintBurndown_ReturnsDays(t *testing.T) {
	mux, store := newPOMux(t)
	base := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	seedPORun(t, store, "r1", "S-1", "P", "P1", base, 1.0, 600)
	seedPORun(t, store, "r2", "S-1", "P", "P1", base.AddDate(0, 0, 1), 0.5, 300)

	w := doGET(t, mux, "/api/sprints/burndown?id=S-1")
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	var got []db.SprintBurndownDay
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("want 2 days, got %d", len(got))
	}
}

func TestPO_CostByDepartment_Filtered(t *testing.T) {
	mux, store := newPOMux(t)
	now := time.Now().UTC().Add(-24 * time.Hour)
	seedPORun(t, store, "r1", "S-1", "Platform", "P1", now, 1.0, 600)
	seedPORun(t, store, "r2", "S-1", "Growth", "P1", now.Add(time.Hour), 2.0, 800)

	w := doGET(t, mux, "/api/cost/department?dept=Platform")
	if w.Code != 200 {
		t.Fatalf("status = %d, body=%q", w.Code, w.Body.String())
	}
	var got []db.CostByDeptDay
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 || got[0].Department != "Platform" {
		t.Errorf("filtered = %+v, want 1 Platform row", got)
	}
}

func TestPO_CostProjection_InsufficientData(t *testing.T) {
	mux, _ := newPOMux(t)
	w := doGET(t, mux, "/api/cost/projection")
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	var got struct {
		History        []db.DailyCost `json:"history"`
		DataSufficient bool           `json:"data_sufficient"`
		Slope          float64        `json:"slope"`
	}
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.History) != 14 {
		t.Errorf("history len = %d, want 14", len(got.History))
	}
	if got.DataSufficient {
		t.Errorf("DataSufficient should be false on empty DB")
	}
}

func TestPO_CostProjection_FitsLinearSeries(t *testing.T) {
	mux, store := newPOMux(t)
	// Insert 1 run per day for last 14 days, increasing cost.
	now := time.Now().UTC()
	for i := 0; i < 14; i++ {
		ts := now.AddDate(0, 0, -i)
		seedPORun(t, store, "p"+string(rune('a'+i)), "S-X", "Platform", "P", ts, float64(i+1), 600)
	}
	w := doGET(t, mux, "/api/cost/projection")
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	var got struct {
		DataSufficient bool           `json:"data_sufficient"`
		Slope          float64        `json:"slope"`
		ProjectedEOM   float64        `json:"projected_eom"`
		ConfidenceLow  float64        `json:"confidence_low"`
		ConfidenceHigh float64        `json:"confidence_high"`
		History        []db.DailyCost `json:"history"`
	}
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.DataSufficient {
		t.Error("DataSufficient should be true with 14d non-zero data")
	}
	if got.ConfidenceHigh < got.ConfidenceLow {
		t.Errorf("confidence band invalid: low=%v high=%v", got.ConfidenceLow, got.ConfidenceHigh)
	}
}

func TestPO_TaskLifecycle_RequiresKey(t *testing.T) {
	mux, _ := newPOMux(t)
	w := doGET(t, mux, "/api/tasks/lifecycle")
	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestPO_LeadTime_BucketsCorrectly(t *testing.T) {
	mux, store := newPOMux(t)
	base := time.Now().UTC().Add(-24 * time.Hour)
	for i, dur := range []float64{1500, 7200, 18000, 60000} {
		seedPORun(t, store, "l"+string(rune('a'+i)), "S-1", "P", "P1",
			base.Add(time.Duration(i)*time.Minute), 0.1, dur)
	}
	w := doGET(t, mux, "/api/runs/lead-time")
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	var got []db.LeadTimeBucket
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 4 {
		t.Errorf("want 4 buckets, got %d", len(got))
	}
}

func TestPO_AttributionTimeline_EmptyArray(t *testing.T) {
	mux, _ := newPOMux(t)
	w := doGET(t, mux, "/api/attribution/timeline")
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	if w.Body.String() == "null\n" {
		t.Error("empty result should be [] not null")
	}
}

func TestPO_BadDateReturns400(t *testing.T) {
	mux, _ := newPOMux(t)
	w := doGET(t, mux, "/api/cost/department?from=not-a-date")
	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}
