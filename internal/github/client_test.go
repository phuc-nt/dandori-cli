package github

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseNextLink(t *testing.T) {
	cases := []struct {
		name   string
		header string
		want   string
	}{
		{
			name:   "next+last",
			header: `<https://api.github.com/repos/o/r/pulls?page=2>; rel="next", <https://api.github.com/repos/o/r/pulls?page=5>; rel="last"`,
			want:   "https://api.github.com/repos/o/r/pulls?page=2",
		},
		{
			name:   "only last",
			header: `<https://api.github.com/repos/o/r/pulls?page=5>; rel="last"`,
			want:   "",
		},
		{
			name:   "empty",
			header: "",
			want:   "",
		},
		{
			name:   "malformed",
			header: `<broken; rel="next"`,
			want:   "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseNextLink(tc.header)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestListPRs_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "token test-pat" {
			t.Errorf("missing or wrong auth header: %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[
			{"number":7,"title":"feat: add X","state":"closed","created_at":"2026-05-10T10:00:00Z","updated_at":"2026-05-11T10:00:00Z","merged_at":"2026-05-11T09:30:00Z","merge_commit_sha":"abc","user":{"login":"phuc"},"base":{"ref":"main","sha":"a"},"head":{"ref":"feat","sha":"b"}},
			{"number":6,"title":"fix: bug","state":"closed","created_at":"2026-05-09T10:00:00Z","updated_at":"2026-05-09T11:00:00Z","merged_at":null,"merge_commit_sha":"","user":{"login":"phuc"},"base":{"ref":"main","sha":"a"},"head":{"ref":"fix","sha":"c"}}
		]`)
	}))
	defer srv.Close()

	c := NewClient(ClientConfig{Repo: "owner/repo", Token: "test-pat", BaseURL: srv.URL})
	prs, err := c.ListPRs(time.Time{}, StateAll)
	if err != nil {
		t.Fatalf("ListPRs: %v", err)
	}
	if len(prs) != 2 {
		t.Fatalf("want 2 PRs, got %d", len(prs))
	}
	if prs[0].Number != 7 || prs[0].MergedAt == nil {
		t.Errorf("PR[0] unexpected: %+v", prs[0])
	}
	if prs[1].MergedAt != nil {
		t.Errorf("PR[1] should have nil MergedAt, got %v", prs[1].MergedAt)
	}
}

func TestListPRs_SinceCutoff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[
			{"number":3,"title":"new","state":"closed","created_at":"2026-05-10T00:00:00Z","updated_at":"2026-05-10T00:00:00Z","merged_at":null,"merge_commit_sha":"","user":{"login":"u"},"base":{"ref":"main","sha":"a"},"head":{"ref":"x","sha":"b"}},
			{"number":2,"title":"older","state":"closed","created_at":"2026-04-01T00:00:00Z","updated_at":"2026-04-01T00:00:00Z","merged_at":null,"merge_commit_sha":"","user":{"login":"u"},"base":{"ref":"main","sha":"a"},"head":{"ref":"y","sha":"c"}}
		]`)
	}))
	defer srv.Close()

	c := NewClient(ClientConfig{Repo: "o/r", Token: "t", BaseURL: srv.URL})
	since, _ := time.Parse(time.RFC3339, "2026-05-01T00:00:00Z")
	prs, err := c.ListPRs(since, StateAll)
	if err != nil {
		t.Fatalf("ListPRs: %v", err)
	}
	if len(prs) != 1 || prs[0].Number != 3 {
		t.Errorf("want only PR#3 after cutoff, got %v", prs)
	}
}

func TestListPRs_Pagination(t *testing.T) {
	var hits atomic.Int32
	var srvURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		page := r.URL.Query().Get("page")
		if page == "" {
			w.Header().Set("Link", fmt.Sprintf(`<%s/repos/o/r/pulls?page=2&per_page=100&sort=updated&direction=desc&state=all>; rel="next"`, srvURL))
			fmt.Fprint(w, `[{"number":2,"title":"p1","state":"closed","created_at":"2026-05-10T00:00:00Z","updated_at":"2026-05-10T00:00:00Z","merged_at":null,"merge_commit_sha":"","user":{"login":"u"},"base":{"ref":"main","sha":"a"},"head":{"ref":"x","sha":"b"}}]`)
			return
		}
		fmt.Fprint(w, `[{"number":1,"title":"p2","state":"closed","created_at":"2026-05-09T00:00:00Z","updated_at":"2026-05-09T00:00:00Z","merged_at":null,"merge_commit_sha":"","user":{"login":"u"},"base":{"ref":"main","sha":"a"},"head":{"ref":"x","sha":"b"}}]`)
	}))
	defer srv.Close()
	srvURL = srv.URL

	c := NewClient(ClientConfig{Repo: "o/r", Token: "t", BaseURL: srv.URL})
	prs, err := c.ListPRs(time.Time{}, StateAll)
	if err != nil {
		t.Fatalf("ListPRs: %v", err)
	}
	if len(prs) != 2 {
		t.Errorf("want 2 PRs across 2 pages, got %d", len(prs))
	}
	if hits.Load() != 2 {
		t.Errorf("want 2 server hits, got %d", hits.Load())
	}
}

func TestDo_RetryOn429(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		if n == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[]`)
	}))
	defer srv.Close()

	c := NewClient(ClientConfig{Repo: "o/r", Token: "t", BaseURL: srv.URL})
	_, err := c.ListPRs(time.Time{}, StateAll)
	if err != nil {
		t.Fatalf("ListPRs after retry: %v", err)
	}
	if hits.Load() != 2 {
		t.Errorf("want 2 hits (1 fail + 1 success), got %d", hits.Load())
	}
}

func TestDo_AbuseDetection403(t *testing.T) {
	// 403 with X-RateLimit-Remaining: 0 = secondary rate limit. Must retry.
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		if n == 1 {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(time.Second).Unix(), 10))
			w.WriteHeader(http.StatusForbidden)
			return
		}
		fmt.Fprint(w, `[]`)
	}))
	defer srv.Close()

	c := NewClient(ClientConfig{Repo: "o/r", Token: "t", BaseURL: srv.URL})
	_, err := c.ListPRs(time.Time{}, StateAll)
	if err != nil {
		t.Fatalf("ListPRs after 403 retry: %v", err)
	}
	if hits.Load() != 2 {
		t.Errorf("want 2 hits, got %d", hits.Load())
	}
}

func TestDo_PermanentError(t *testing.T) {
	// 401 must NOT retry — surface immediately.
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"message":"Bad credentials"}`)
	}))
	defer srv.Close()

	c := NewClient(ClientConfig{Repo: "o/r", Token: "bad", BaseURL: srv.URL})
	_, err := c.ListPRs(time.Time{}, StateAll)
	if err == nil {
		t.Fatal("expected error on 401, got nil")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error missing 401 context: %v", err)
	}
	if hits.Load() != 1 {
		t.Errorf("401 must not retry, got %d hits", hits.Load())
	}
}

func TestGetPRReviews(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/pulls/42/reviews") {
			t.Errorf("wrong path: %s", r.URL.Path)
		}
		fmt.Fprint(w, `[
			{"id":1,"state":"APPROVED","submitted_at":"2026-05-11T08:00:00Z","user":{"login":"reviewer"}},
			{"id":2,"state":"COMMENTED","submitted_at":"2026-05-11T07:00:00Z","user":{"login":"reviewer"}}
		]`)
	}))
	defer srv.Close()

	c := NewClient(ClientConfig{Repo: "o/r", Token: "t", BaseURL: srv.URL})
	revs, err := c.GetPRReviews(42)
	if err != nil {
		t.Fatalf("GetPRReviews: %v", err)
	}
	if len(revs) != 2 || revs[0].State != "APPROVED" {
		t.Errorf("unexpected reviews: %+v", revs)
	}
}
