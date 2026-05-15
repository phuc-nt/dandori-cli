package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/phuc-nt/dandori-cli/internal/db"
	"github.com/phuc-nt/dandori-cli/internal/server"
)

// newMultiMux mounts trust + pr-cycle + repos routes so we can exercise
// `?repo=` validation across them with a single store.
func newMultiMux(t *testing.T) (*http.ServeMux, *db.LocalDB) {
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
	server.RegisterPRCycleRoutes(mux, store)
	server.RegisterReposRoute(mux, store)
	return mux, store
}

func TestReposEndpoint_EmptyDB(t *testing.T) {
	mux, _ := newMultiMux(t)
	req := httptest.NewRequest(http.MethodGet, "/api/metrics/repos", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var out []db.RepoSummary
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v (body=%q)", err, w.Body.String())
	}
	if out == nil {
		t.Error("expected empty array, got nil — must not serialise as null")
	}
	if len(out) != 0 {
		t.Errorf("expected zero repos, got %d", len(out))
	}
}

func TestReposEndpoint_ListsAndOrders(t *testing.T) {
	mux, store := newMultiMux(t)
	now := time.Now().UTC()
	ts := func(d time.Duration) string {
		return now.Add(d).UTC().Format(time.RFC3339)
	}
	tsStr := ts(-24 * time.Hour)
	// repo-A x 2, repo-B x 1
	for i, repo := range []string{"o/a", "o/a", "o/b"} {
		if err := store.UpsertPR(db.PREvent{
			Repo: repo, PRNumber: i + 1, Title: "t", State: "merged",
			CreatedAt: tsStr, SubmittedAt: tsStr,
			MergedAt: &tsStr, ClosedAt: &tsStr,
		}); err != nil {
			t.Fatal(err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/metrics/repos?days=28", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var out []db.RepoSummary
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if len(out) != 2 {
		t.Fatalf("want 2 repos, got %d: %+v", len(out), out)
	}
	if out[0].Repo != "o/a" || out[0].MergedCount != 2 {
		t.Errorf("first = %+v", out[0])
	}
}

func TestTrustEndpoint_RejectsMalformedRepo(t *testing.T) {
	mux, _ := newMultiMux(t)
	bad := []string{
		"justname",       // missing slash
		"a/b/c",          // too many segments
		"owner/repo;",    // semicolon
		"owner/../repo",  // path traversal
		"owner/repo?x",   // querystring
		"owner repo",     // space
		"owner/repo'--",  // sql-flavoured chars
	}
	for _, b := range bad {
		u := "/api/metrics/trust-index?repo=" + url.QueryEscape(b)
		req := httptest.NewRequest(http.MethodGet, u, nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("repo=%q: status = %d, want 400 (body=%s)", b, w.Code, w.Body.String())
		}
	}
}

func TestPRCycleEndpoint_RejectsMalformedRepo(t *testing.T) {
	mux, _ := newMultiMux(t)
	req := httptest.NewRequest(http.MethodGet, "/api/metrics/pr-cycle-time?repo=bad", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestTrustEndpoint_AcceptsValidRepo(t *testing.T) {
	mux, _ := newMultiMux(t)
	req := httptest.NewRequest(http.MethodGet, "/api/metrics/trust-index?repo=phuc-nt/dandori-cli", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d (body=%s)", w.Code, w.Body.String())
	}
	var res db.TrustResult
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if res.Repo != "phuc-nt/dandori-cli" {
		t.Errorf("Repo = %q, want phuc-nt/dandori-cli", res.Repo)
	}
	if res.RepoScope != "cfr_only" {
		t.Errorf("RepoScope = %q, want cfr_only", res.RepoScope)
	}
}

func TestTrustEndpoint_UnfilteredHasScopeAll(t *testing.T) {
	mux, _ := newMultiMux(t)
	req := httptest.NewRequest(http.MethodGet, "/api/metrics/trust-index", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var res db.TrustResult
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if res.RepoScope != "all" {
		t.Errorf("unfiltered RepoScope = %q, want all", res.RepoScope)
	}
	if res.Repo != "" {
		t.Errorf("unfiltered Repo = %q, want empty", res.Repo)
	}
}
