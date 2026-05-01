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

// TestExperimentalFlagOff_KeepsLegacyMux asserts that without --experimental
// the G9 routes are not registered.
// The legacy mux uses a catch-all "/" handler, so unregistered paths return 200
// with HTML (the dashboard page). We verify /api/g9/dora returns HTML (not JSON)
// while /api/overview returns valid JSON.
func TestExperimentalFlagOff_KeepsLegacyMux(t *testing.T) {
	store := setupDashboardDB(t)
	defer store.Close()

	mux := newDashboardMux(store, "https://jira.example.com")

	// legacy route returns valid JSON
	status, body := getJSON(t, mux, "/api/overview")
	if status != http.StatusOK {
		t.Errorf("/api/overview status=%d, want 200", status)
	}
	var overview map[string]any
	if err := json.Unmarshal(body, &overview); err != nil {
		t.Errorf("/api/overview body not JSON: %s", body)
	}

	// /api/g9/dora on legacy mux hits the catch-all "/" → returns HTML, not JSON.
	_, doraBody := getJSON(t, mux, "/api/g9/dora")
	var doraJSON map[string]any
	if err := json.Unmarshal(doraBody, &doraJSON); err == nil {
		// If it parses as JSON, G9 routes were unexpectedly registered.
		t.Errorf("/api/g9/dora returned JSON on legacy mux (G9 routes should not be registered)")
	}
}

// TestExperimentalFlagOn_RegistersG9Routes asserts that when experimental is
// true, all four /api/g9/* routes return 200.
func TestExperimentalFlagOn_RegistersG9Routes(t *testing.T) {
	store := setupDashboardDB(t)
	defer store.Close()

	mux := newExperimentalDashboardMux(store, "https://jira.example.com")

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
		status, body := getJSON(t, mux, route)
		if status != http.StatusOK {
			t.Errorf("%s status=%d, want 200; body=%s", route, status, body)
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
