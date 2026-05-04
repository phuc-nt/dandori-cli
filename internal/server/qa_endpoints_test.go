package server_test

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/phuc-nt/dandori-cli/internal/db"
	"github.com/phuc-nt/dandori-cli/internal/server"
)

func newQAMux(t *testing.T) (*http.ServeMux, *db.LocalDB) {
	t.Helper()
	tmp := t.TempDir()
	store, err := db.Open(filepath.Join(tmp, "qa.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := store.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	mux := http.NewServeMux()
	server.RegisterQARoutes(mux, store)
	server.RegisterAuditRoutes(mux, store)
	return mux, store
}

func seedQA(t *testing.T, d *db.LocalDB, id, jira string, lintDelta, testsDelta int, msgQ, qScore, cost float64) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := d.Exec(`
		INSERT INTO runs (id, jira_issue_key, agent_name, agent_type, user, workstation_id, cwd, git_remote,
			started_at, status, cost_usd, engineer_name, department, human_intervention_count)
		VALUES (?, ?, 'a', 'cc', 'u', 'ws-1', '/tmp', 'git@x:r.git', ?, 'done', ?, 'alice', 'eng', 1)
	`, id, jira, now, cost); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	if _, err := d.Exec(`
		INSERT INTO quality_metrics (run_id, lint_delta, tests_delta, commit_msg_quality, quality_score)
		VALUES (?, ?, ?, ?, ?)
	`, id, lintDelta, testsDelta, msgQ, qScore); err != nil {
		t.Fatalf("seed quality: %v", err)
	}
}

func TestQA_Timeline_ReturnsArrayNotNull(t *testing.T) {
	mux, _ := newQAMux(t)
	w := doGET(t, mux, "/api/quality/timeline")
	if w.Code != 200 {
		t.Fatalf("status=%d", w.Code)
	}
	if w.Body.String() == "null\n" || w.Body.String() == "null" {
		t.Errorf("want [], got %q", w.Body.String())
	}
}

func TestQA_Scatter_ReturnsPoints(t *testing.T) {
	mux, store := newQAMux(t)
	seedQA(t, store, "r1", "P-1", 0, 0, 70, 80, 1.0)
	seedQA(t, store, "r2", "P-2", 0, 0, 70, 60, 0.5)

	w := doGET(t, mux, "/api/quality/scatter?limit=10")
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var got []db.CostQualityPoint
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("want 2 points, got %d", len(got))
	}
}

func TestQA_CommitMsg_Returns4Buckets(t *testing.T) {
	mux, _ := newQAMux(t)
	w := doGET(t, mux, "/api/quality/commit-msg")
	if w.Code != 200 {
		t.Fatalf("status=%d", w.Code)
	}
	var got []db.CommitMsgBucket
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 4 {
		t.Errorf("want 4 buckets, got %d", len(got))
	}
}

func TestQA_BugHotspots_RegressionsOnly(t *testing.T) {
	mux, store := newQAMux(t)
	seedQA(t, store, "r1", "P-1", 5, 0, 70, 60, 1.0)  // lint regression
	seedQA(t, store, "r2", "P-1", 0, -2, 70, 60, 1.0) // tests regression
	seedQA(t, store, "r3", "P-1", 0, 0, 70, 90, 1.0)  // clean

	w := doGET(t, mux, "/api/bug-hotspots?weeks=8")
	if w.Code != 200 {
		t.Fatalf("status=%d", w.Code)
	}
	var got []db.BugHotspotCell
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	total := 0
	for _, c := range got {
		total += c.Count
	}
	if total != 2 {
		t.Errorf("regressions = %d, want 2", total)
	}
}

func TestQA_ReworkCauses_FixedFiveBuckets(t *testing.T) {
	mux, _ := newQAMux(t)
	w := doGET(t, mux, "/api/rework/causes")
	if w.Code != 200 {
		t.Fatalf("status=%d", w.Code)
	}
	var got []db.ReworkCause
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 5 {
		t.Errorf("want 5 buckets, got %d", len(got))
	}
}

func TestQA_InterventionHeatmap_Cells(t *testing.T) {
	mux, store := newQAMux(t)
	seedQA(t, store, "r1", "P-1", 0, 0, 70, 60, 1.0) // sets human_intervention_count=1

	w := doGET(t, mux, "/api/intervention/heatmap?days=28")
	if w.Code != 200 {
		t.Fatalf("status=%d", w.Code)
	}
	var got []db.InterventionCell
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("want 1 cell, got %d", len(got))
	}
}
