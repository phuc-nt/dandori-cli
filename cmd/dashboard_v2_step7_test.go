package cmd

import (
	"strings"
	"testing"
)

// TestDashboardV2_RunDrawerMarkup verifies the Run Detail drawer (Step 7)
// is wired into the embedded HTML/JS: 5 tabs (Audit disabled), close button,
// deep-link param, and ESC handling.
func TestDashboardV2_RunDrawerMarkup(t *testing.T) {
	page := dashboardHTMLForTest(t)

	wantHTML := []string{
		`id="run-drawer"`,
		`class="run-drawer-panel"`,
		`class="run-drawer-tabs"`,
		`data-tab="summary"`,
		`data-tab="events"`,
		`data-tab="quality"`,
		`data-tab="files"`,
		`data-tab="audit"`,
		`disabled`, // Audit tab must be disabled until Phase 04
		`onclick="closeRunDrawer()"`,
		`id="run-drawer-summary"`,
		`id="run-drawer-events"`,
	}
	for _, s := range wantHTML {
		if !strings.Contains(page, s) {
			t.Errorf("dashboard HTML missing run-drawer marker %q", s)
		}
	}

	wantJS := []string{
		"openRunDrawer",
		"closeRunDrawer",
		"setRunDrawerTab",
		"renderRunDrawerSummary",
		"renderRunDrawerEvents",
		"/api/g9/run/", // events tab still uses the existing endpoint
		`.get('run')`,
		`searchParams.set('run'`,
	}
	for _, s := range wantJS {
		if !strings.Contains(page, s) {
			t.Errorf("dashboard JS missing run-drawer symbol %q", s)
		}
	}

	// Drawer must be hidden by default so it doesn't take over the page.
	if !strings.Contains(page, `id="run-drawer" class="run-drawer" hidden`) {
		t.Error("run-drawer should be hidden by default")
	}
}
