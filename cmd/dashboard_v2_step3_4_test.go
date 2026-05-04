package cmd

import (
	"strings"
	"testing"
)

// TestDashboardV2_AlertCenterMarkup verifies the unified Alert Center UI
// (Step 3) is wired into the embedded HTML/JS. Catches accidental deletion
// during refactors.
func TestDashboardV2_AlertCenterMarkup(t *testing.T) {
	page := dashboardHTMLForTest(t)

	wantHTML := []string{
		`id="alert-center"`,
		`id="alert-center-btn"`,
		`id="alert-center-panel"`,
		`id="alert-center-list"`,
		`id="alert-badge"`,
		`onclick="toggleAlertCenter()"`,
	}
	for _, s := range wantHTML {
		if !strings.Contains(page, s) {
			t.Errorf("dashboard HTML missing alert-center marker %q", s)
		}
	}

	wantJS := []string{
		"loadAlertCenter",
		"renderAlertCenter",
		"dismissAlert",
		"toggleAlertCenter",
		"/api/alerts/ack",
	}
	for _, s := range wantJS {
		if !strings.Contains(page, s) {
			t.Errorf("dashboard JS missing alert-center symbol %q", s)
		}
	}

	// Old banners should be removed from active rendering paths.
	if strings.Contains(page, `id="org-alerts-banner"`) {
		t.Error("legacy org-alerts-banner element should be removed (moved to Alert Center)")
	}
}

// TestDashboardV2_StickyControlsMarkup verifies the sticky control bar (Step 4)
// wraps the header + filter pill bar so it stays pinned during scroll.
func TestDashboardV2_StickyControlsMarkup(t *testing.T) {
	page := dashboardHTMLForTest(t)

	if !strings.Contains(page, `class="sticky-controls"`) {
		t.Error("dashboard HTML missing sticky-controls wrapper")
	}

	// Order matters: sticky-controls must open BEFORE the header and close AFTER
	// the filter-pill-bar so both are pinned together.
	stickyOpen := strings.Index(page, `class="sticky-controls"`)
	headerOpen := strings.Index(page, `<header class="header">`)
	filterBar := strings.Index(page, `id="filter-pill-bar"`)
	stickyClose := strings.Index(page, "/.sticky-controls")

	if stickyOpen < 0 || headerOpen < 0 || filterBar < 0 || stickyClose < 0 {
		t.Fatalf("missing structural marker: stickyOpen=%d headerOpen=%d filterBar=%d stickyClose=%d",
			stickyOpen, headerOpen, filterBar, stickyClose)
	}
	if !(stickyOpen < headerOpen && headerOpen < filterBar && filterBar < stickyClose) {
		t.Errorf("sticky-controls must wrap header + filter-pill-bar (got order: sticky=%d header=%d filter=%d close=%d)",
			stickyOpen, headerOpen, filterBar, stickyClose)
	}

	// Clear-all behavior is JS-driven.
	if !strings.Contains(page, "clearFilterPills") {
		t.Error("dashboard JS missing clearFilterPills (Clear all button handler)")
	}
}
