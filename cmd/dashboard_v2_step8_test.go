package cmd

import (
	"strings"
	"testing"
)

// TestDashboardV2_ResponsiveBreakpoints verifies the dashboard ships the CSS
// rules required for the 375 / 768 / 1440 plan (Step 8): viewport meta tag,
// sidebar collapse < 768px, KPI strip 1-col < 480px, and run-drawer full-width
// on mobile.
func TestDashboardV2_ResponsiveBreakpoints(t *testing.T) {
	page := dashboardHTMLForTest(t)
	cssBytes, err := dashboardFS.ReadFile("web/dashboard/style.css")
	if err != nil {
		t.Fatalf("read style.css: %v", err)
	}
	page += "\n" + string(cssBytes)

	wantMarkers := []string{
		// Mobile viewport meta — required for media queries to fire on real devices.
		`name="viewport"`,
		`width=device-width`,
		// Sidebar must collapse below 768px (transform off-screen) and zero margin on .main.
		`@media (max-width: 768px)`,
		`transform: translateX(-100%)`,
		// KPI strip must drop to single column under 480px (375 viewport).
		`@media (max-width: 480px)`,
		`.kpi-strip { grid-template-columns: 1fr;`,
		// Run drawer must take full viewport width on small screens.
		`.run-drawer-panel { width: 100vw; }`,
		// DORA traffic light wraps via flex-wrap so it survives narrow viewports.
		`flex-wrap: wrap`,
	}
	for _, s := range wantMarkers {
		if !strings.Contains(page, s) {
			t.Errorf("dashboard missing responsive marker %q", s)
		}
	}
}
