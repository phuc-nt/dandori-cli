package server_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/phuc-nt/dandori-cli/internal/server"
)

// TestParsePeriod_DefaultPerRole verifies default window durations per role
// when no ?period= query param is provided.
func TestParsePeriod_DefaultPerRole(t *testing.T) {
	cases := []struct {
		role     string
		wantDays int
	}{
		{"engineer", 7},
		{"project", 28},
		{"org", 90},
	}

	for _, tc := range cases {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		cur, prior, err := server.ParsePeriodWindow(r, tc.role)
		if err != nil {
			t.Errorf("role=%s: unexpected error: %v", tc.role, err)
			continue
		}
		if prior != nil {
			t.Errorf("role=%s: expected prior=nil when compare not set, got %v", tc.role, prior)
		}
		wantDur := time.Duration(tc.wantDays) * 24 * time.Hour
		gotDur := cur.End.Sub(cur.Start)
		// Allow 1-minute tolerance for clock drift during test.
		if diff := gotDur - wantDur; diff < -time.Minute || diff > time.Minute {
			t.Errorf("role=%s: window duration=%v, want %v", tc.role, gotDur, wantDur)
		}
	}
}

// TestParsePeriod_NamedRanges verifies that ?period=7d, 28d, 90d produce the
// correct durations regardless of role default.
func TestParsePeriod_NamedRanges(t *testing.T) {
	cases := []struct {
		period   string
		wantDays int
	}{
		{"7d", 7},
		{"28d", 28},
		{"90d", 90},
	}

	for _, tc := range cases {
		r := httptest.NewRequest(http.MethodGet, "/?period="+tc.period, nil)
		cur, _, err := server.ParsePeriodWindow(r, "org")
		if err != nil {
			t.Errorf("period=%s: unexpected error: %v", tc.period, err)
			continue
		}
		wantDur := time.Duration(tc.wantDays) * 24 * time.Hour
		gotDur := cur.End.Sub(cur.Start)
		if diff := gotDur - wantDur; diff < -time.Minute || diff > time.Minute {
			t.Errorf("period=%s: window duration=%v, want %v", tc.period, gotDur, wantDur)
		}
	}
}

// TestParsePeriod_CustomFromTo verifies that ?period=custom&from=...&to=...
// parses into exact UTC midnight dates.
func TestParsePeriod_CustomFromTo(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/?period=custom&from=2026-04-01&to=2026-04-15", nil)
	cur, _, err := server.ParsePeriodWindow(r, "org")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantStart := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC)

	if !cur.Start.Equal(wantStart) {
		t.Errorf("Start=%v, want %v", cur.Start, wantStart)
	}
	if !cur.End.Equal(wantEnd) {
		t.Errorf("End=%v, want %v", cur.End, wantEnd)
	}
}

// TestParsePeriod_CustomInvalid_Returns400 verifies that malformed ?from= values
// return a non-nil error (handlers map this to HTTP 400).
func TestParsePeriod_CustomInvalid_Returns400(t *testing.T) {
	cases := []string{
		"/?period=custom&from=not-a-date&to=2026-04-15",
		"/?period=custom&from=2026-04-01&to=oops",
		"/?period=custom&from=&to=2026-04-15",
	}

	for _, url := range cases {
		r := httptest.NewRequest(http.MethodGet, url, nil)
		_, _, err := server.ParsePeriodWindow(r, "org")
		if err == nil {
			t.Errorf("url=%q: expected error for malformed date, got nil", url)
		}
	}
}

// TestParsePeriod_PriorWindow_MirrorsCurrent verifies that when ?compare=true,
// the prior window immediately precedes the current window with the same duration.
// Example: current=Apr 1–Apr 30 (29d) → prior=Mar 3–Apr 1 (29d).
func TestParsePeriod_PriorWindow_MirrorsCurrent(t *testing.T) {
	// custom window: Apr 1 – Apr 30 (29 days)
	r := httptest.NewRequest(http.MethodGet, "/?period=custom&from=2026-04-01&to=2026-04-30&compare=true", nil)
	cur, prior, err := server.ParsePeriodWindow(r, "org")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prior == nil {
		t.Fatal("expected prior window when compare=true, got nil")
	}

	// Duration must match.
	curDur := cur.End.Sub(cur.Start)
	priorDur := prior.End.Sub(prior.Start)
	if curDur != priorDur {
		t.Errorf("prior duration=%v, want %v (same as current)", priorDur, curDur)
	}

	// Prior window ends where current begins.
	if !prior.End.Equal(cur.Start) {
		t.Errorf("prior.End=%v, want cur.Start=%v", prior.End, cur.Start)
	}

	// Prior window starts dur before current start.
	wantPriorStart := cur.Start.Add(-curDur)
	if !prior.Start.Equal(wantPriorStart) {
		t.Errorf("prior.Start=%v, want %v", prior.Start, wantPriorStart)
	}
}
