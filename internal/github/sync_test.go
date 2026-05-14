package github

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/phuc-nt/dandori-cli/internal/db"
)

// newTestDB applies the full v12 schema to a temp SQLite file and returns
// a LocalDB for sync tests.
func newTestDB(t *testing.T) *db.LocalDB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	d, err := db.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return d
}

// fakeGitHub returns an httptest.Server that mounts a single PRs list +
// per-PR reviews handler driven by the supplied JSON payloads.
type fakeGitHub struct {
	prsJSON     string
	reviewsJSON map[int]string // keyed by PR number
	hits        atomic.Int32
}

func (f *fakeGitHub) handler(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		switch {
		case strings.HasSuffix(r.URL.Path, "/pulls"):
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, f.prsJSON)
		case strings.Contains(r.URL.Path, "/reviews"):
			// /repos/o/r/pulls/<n>/reviews
			parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
			var prNum int
			for i, p := range parts {
				if p == "pulls" && i+1 < len(parts) {
					fmt.Sscanf(parts[i+1], "%d", &prNum)
					break
				}
			}
			body := f.reviewsJSON[prNum]
			if body == "" {
				body = "[]"
			}
			fmt.Fprint(w, body)
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	})
}

func TestPullPREvents_HappyPath_UpsertsAndWatermark(t *testing.T) {
	fg := &fakeGitHub{
		prsJSON: `[
			{"number":7,"title":"feat: add login","state":"closed","created_at":"2026-05-10T10:00:00Z","updated_at":"2026-05-11T10:00:00Z","merged_at":"2026-05-11T09:30:00Z","closed_at":"2026-05-11T09:30:00Z","merge_commit_sha":"abc","user":{"login":"phuc"},"base":{"ref":"main","sha":"a"},"head":{"ref":"feat","sha":"b"}}
		]`,
		reviewsJSON: map[int]string{
			7: `[
				{"id":1,"state":"COMMENTED","submitted_at":"2026-05-10T11:00:00Z","user":{"login":"x"}},
				{"id":2,"state":"APPROVED","submitted_at":"2026-05-11T08:00:00Z","user":{"login":"y"}},
				{"id":3,"state":"APPROVED","submitted_at":"2026-05-11T09:00:00Z","user":{"login":"z"}}
			]`,
		},
	}
	srv := httptest.NewServer(fg.handler(t))
	defer srv.Close()

	client := NewClient(ClientConfig{Repo: "o/r", Token: "t", BaseURL: srv.URL})
	store := newTestDB(t)

	s, err := PullPREvents(client, store, 90)
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	if s.PRsPulled != 1 {
		t.Errorf("PRsPulled = %d, want 1", s.PRsPulled)
	}
	if s.ReviewsFetched != 3 {
		t.Errorf("ReviewsFetched = %d, want 3", s.ReviewsFetched)
	}
	got, err := store.GetPRByNumber("o/r", 7)
	if err != nil || got == nil {
		t.Fatalf("row missing: %v", err)
	}
	if got.State != "merged" {
		t.Errorf("state = %s, want merged", got.State)
	}
	if got.FirstApprovalAt == nil || *got.FirstApprovalAt != "2026-05-11T08:00:00Z" {
		t.Errorf("first_approval_at = %v, want earliest APPROVED", got.FirstApprovalAt)
	}
	// Watermark stored.
	v, ok, _ := store.GetSyncState(SyncStateKey)
	if !ok || v == "" {
		t.Error("watermark not persisted")
	}
}

func TestPullPREvents_Idempotent_NoNewRows(t *testing.T) {
	fg := &fakeGitHub{
		prsJSON: `[
			{"number":1,"title":"feat: a","state":"closed","created_at":"2026-05-10T10:00:00Z","updated_at":"2026-05-10T10:00:00Z","merged_at":"2026-05-10T11:00:00Z","closed_at":"2026-05-10T11:00:00Z","merge_commit_sha":"a","user":{"login":"u"},"base":{"ref":"main","sha":"a"},"head":{"ref":"x","sha":"b"}},
			{"number":2,"title":"feat: b","state":"open","created_at":"2026-05-11T10:00:00Z","updated_at":"2026-05-11T10:00:00Z","merged_at":null,"closed_at":null,"merge_commit_sha":"","user":{"login":"u"},"base":{"ref":"main","sha":"a"},"head":{"ref":"y","sha":"c"}}
		]`,
	}
	srv := httptest.NewServer(fg.handler(t))
	defer srv.Close()
	client := NewClient(ClientConfig{Repo: "o/r", Token: "t", BaseURL: srv.URL})
	store := newTestDB(t)

	for i := 0; i < 3; i++ {
		if _, err := PullPREvents(client, store, 90); err != nil {
			t.Fatalf("pull #%d: %v", i, err)
		}
	}
	n, _ := store.CountPRs("o/r")
	if n != 2 {
		t.Errorf("want 2 rows after 3 pulls, got %d", n)
	}
}

func TestPullPREvents_RevertDetection(t *testing.T) {
	// Two-PR scenario: PR#5 "feat: add login" merged 3d ago, then PR#9
	// "Revert \"feat: add login\"" lands. Sync must flip PR#5.is_reverted=1.
	mergedAt := time.Now().UTC().Add(-3 * 24 * time.Hour).Format(time.RFC3339)
	revertedAt := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)
	fg := &fakeGitHub{
		prsJSON: fmt.Sprintf(`[
			{"number":9,"title":"Revert \"feat: add login\"","state":"closed","created_at":%q,"updated_at":%q,"merged_at":%q,"closed_at":%q,"merge_commit_sha":"rev","user":{"login":"u"},"base":{"ref":"main","sha":"a"},"head":{"ref":"r","sha":"b"}},
			{"number":5,"title":"feat: add login","state":"closed","created_at":"2026-05-08T00:00:00Z","updated_at":"2026-05-08T00:00:00Z","merged_at":%q,"closed_at":%q,"merge_commit_sha":"orig","user":{"login":"u"},"base":{"ref":"main","sha":"a"},"head":{"ref":"f","sha":"c"}}
		]`, revertedAt, revertedAt, revertedAt, revertedAt, mergedAt, mergedAt),
	}
	srv := httptest.NewServer(fg.handler(t))
	defer srv.Close()
	client := NewClient(ClientConfig{Repo: "o/r", Token: "t", BaseURL: srv.URL})
	store := newTestDB(t)

	s, err := PullPREvents(client, store, 90)
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	if s.RevertsDetected != 1 {
		t.Errorf("RevertsDetected = %d, want 1", s.RevertsDetected)
	}
	orig, _ := store.GetPRByNumber("o/r", 5)
	if orig == nil || !orig.IsReverted {
		t.Fatalf("PR#5 not flagged as reverted: %+v", orig)
	}
	if orig.RevertedByPR == nil || *orig.RevertedByPR != 9 {
		t.Errorf("reverted_by_pr = %v, want 9", orig.RevertedByPR)
	}
}

func TestPullPREvents_ReopenDetection(t *testing.T) {
	store := newTestDB(t)
	// Seed: PR was previously closed.
	closedAt := "2026-05-09T00:00:00Z"
	if err := store.UpsertPR(db.PREvent{
		Repo: "o/r", PRNumber: 4, Title: "feat: x", State: "closed",
		CreatedAt: "2026-05-08T00:00:00Z", ClosedAt: &closedAt,
	}); err != nil {
		t.Fatal(err)
	}

	// Now GitHub returns the same PR back as open.
	fg := &fakeGitHub{
		prsJSON: `[
			{"number":4,"title":"feat: x","state":"open","created_at":"2026-05-08T00:00:00Z","updated_at":"2026-05-10T00:00:00Z","merged_at":null,"closed_at":null,"merge_commit_sha":"","user":{"login":"u"},"base":{"ref":"main","sha":"a"},"head":{"ref":"x","sha":"b"}}
		]`,
	}
	srv := httptest.NewServer(fg.handler(t))
	defer srv.Close()
	client := NewClient(ClientConfig{Repo: "o/r", Token: "t", BaseURL: srv.URL})

	s, err := PullPREvents(client, store, 90)
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	if s.ReopensDetected != 1 {
		t.Errorf("ReopensDetected = %d, want 1", s.ReopensDetected)
	}
	got, _ := store.GetPRByNumber("o/r", 4)
	if got.ReopenedAt == nil {
		t.Error("reopened_at not stamped")
	}
	if got.State != "open" {
		t.Errorf("state = %s, want open", got.State)
	}
}

func TestPullPREvents_WatermarkFiltersOlderPRs(t *testing.T) {
	// ListPRs filters client-side: PRs with UpdatedAt < since are dropped.
	// Seed watermark = 2026-05-05; fake returns one PR updated 2026-05-10
	// (kept) and one 2026-04-01 (older than watermark-1h, dropped).
	fg := &fakeGitHub{
		prsJSON: `[
			{"number":10,"title":"recent","state":"open","created_at":"2026-05-10T00:00:00Z","updated_at":"2026-05-10T00:00:00Z","merged_at":null,"closed_at":null,"merge_commit_sha":"","user":{"login":"u"},"base":{"ref":"main","sha":"a"},"head":{"ref":"x","sha":"b"}},
			{"number":2,"title":"older","state":"closed","created_at":"2026-04-01T00:00:00Z","updated_at":"2026-04-01T00:00:00Z","merged_at":null,"closed_at":null,"merge_commit_sha":"","user":{"login":"u"},"base":{"ref":"main","sha":"a"},"head":{"ref":"y","sha":"c"}}
		]`,
	}
	srv := httptest.NewServer(fg.handler(t))
	defer srv.Close()
	client := NewClient(ClientConfig{Repo: "o/r", Token: "t", BaseURL: srv.URL})
	store := newTestDB(t)
	if err := store.SetSyncState(SyncStateKey, "2026-05-05T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	s, err := PullPREvents(client, store, 90)
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	if s.PRsPulled != 1 {
		t.Errorf("PRsPulled = %d, want 1 (older PR should be filtered)", s.PRsPulled)
	}
	if got, _ := store.GetPRByNumber("o/r", 10); got == nil {
		t.Error("PR#10 (recent) missing")
	}
	if got, _ := store.GetPRByNumber("o/r", 2); got != nil {
		t.Errorf("PR#2 (older than watermark) should not be persisted, got %+v", got)
	}
}

func TestMatchRevertTitle(t *testing.T) {
	cases := []struct {
		in       string
		want     string
		wantOK   bool
	}{
		{`Revert "feat: add login"`, "feat: add login", true},
		{`Revert "fix: bug #42"`, "fix: bug #42", true},
		{`revert "lowercase prefix"`, "", false},
		{`Revert: feat without quotes`, "", false},
		{`feat: normal PR`, "", false},
		{`Revert "" empty`, "", false},
	}
	for _, c := range cases {
		got, ok := matchRevertTitle(c.in)
		if ok != c.wantOK || got != c.want {
			t.Errorf("matchRevertTitle(%q) = (%q, %v), want (%q, %v)", c.in, got, ok, c.want, c.wantOK)
		}
	}
}
