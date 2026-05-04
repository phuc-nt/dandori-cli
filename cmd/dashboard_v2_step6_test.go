package cmd

import (
	"strings"
	"testing"
)

// TestDashboardV2_DoraTrafficLightMarkup verifies the collapsed DORA traffic
// light row (Step 6) and its expand-modal are wired into HTML/JS.
func TestDashboardV2_DoraTrafficLightMarkup(t *testing.T) {
	page := dashboardHTMLForTest(t)

	wantHTML := []string{
		`class="dora-traffic-light"`,
		`id="dora-traffic-dots"`,
		`id="dora-traffic-summary"`,
		`onclick="openDoraModal()"`,
		`id="dora-modal"`,
		`class="dora-modal-panel"`,
		`onclick="closeDoraModal()"`,
		// The detailed grid still exists but is now inside the modal.
		`id="dora-grid"`,
	}
	for _, s := range wantHTML {
		if !strings.Contains(page, s) {
			t.Errorf("dashboard HTML missing DORA traffic-light marker %q", s)
		}
	}

	wantJS := []string{
		"renderDoraTrafficLight",
		"openDoraModal",
		"closeDoraModal",
		"doraRatingDot",
	}
	for _, s := range wantJS {
		if !strings.Contains(page, s) {
			t.Errorf("dashboard JS missing DORA traffic-light symbol %q", s)
		}
	}

	// Modal must be hidden by default so it doesn't take over the page.
	if !strings.Contains(page, `id="dora-modal" class="dora-modal" hidden`) {
		t.Error("dora-modal should be hidden by default")
	}
}
