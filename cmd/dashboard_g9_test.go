package cmd

import (
	"encoding/json"
	"net/http"
	"testing"
)

// TestExtractProjectKey validates issue key prefix extraction.
func TestExtractProjectKey(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"CLITEST-99", "CLITEST"},
		{"FOO-BAR-1", "FOO"},
		{"noprefix", ""},
		{"", ""},
		{"ABC-123", "ABC"},
		{"MYPROJECT-0", "MYPROJECT"},
	}
	for _, tc := range cases {
		got := extractProjectKey(tc.in)
		if got != tc.want {
			t.Errorf("extractProjectKey(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestG9RoutesAlwaysRegistered asserts that after GA cutover the default
// dashboard mux registers all G9 routes — no flag required.
func TestG9RoutesAlwaysRegistered(t *testing.T) {
	store := setupDashboardDB(t)
	defer store.Close()

	mux := newDashboardMux(store, "https://jira.example.com")

	// Legacy route still works.
	status, body := getJSON(t, mux, "/api/overview")
	if status != http.StatusOK {
		t.Errorf("/api/overview status=%d, want 200", status)
	}
	var overview map[string]any
	if err := json.Unmarshal(body, &overview); err != nil {
		t.Errorf("/api/overview body not JSON: %s", body)
	}

	// All G9 routes return 200.
	routes := []string{
		"/api/g9/dora",
		"/api/g9/attribution",
		"/api/g9/intent",
		"/api/g9/level",
		"/api/g9/landing",
		"/api/g9/iterations",
		"/api/g9/insights",
	}
	for _, route := range routes {
		st, b := getJSON(t, mux, route)
		if st != http.StatusOK {
			t.Errorf("%s status=%d, want 200; body=%s", route, st, b)
		}
	}
}

// TestDetectDashboardLanding_FallsBackToOrgOnEmptyDB asserts that with no
// runs in the DB the landing helper returns {Role:"org"} regardless of cwd.
func TestDetectDashboardLanding_FallsBackToOrgOnEmptyDB(t *testing.T) {
	store := setupDashboardDB(t)
	defer store.Close()

	got := detectDashboardLanding(store)
	if got.Role != "org" {
		t.Errorf("Role = %q; want \"org\" on empty DB", got.Role)
	}
}
