package cmd

import (
	"strings"
	"testing"
)

// TestDashboardV2_SprintPickerMarkup verifies the Phase 02 Step 2 sprint picker
// is wired into the embedded dashboard: dropdown control, change handler,
// state machine support (?sprint=), and CSS styling.
func TestDashboardV2_SprintPickerMarkup(t *testing.T) {
	page := dashboardHTMLForTest(t)
	cssBytes, err := dashboardFS.ReadFile("web/dashboard/style.css")
	if err != nil {
		t.Fatalf("read style.css: %v", err)
	}
	page += "\n" + string(cssBytes)

	wantHTML := []string{
		`id="sprint-picker"`,
		`class="sprint-picker"`,
		`onchange="onSprintChange`,
		`All sprints`,
	}
	for _, s := range wantHTML {
		if !strings.Contains(page, s) {
			t.Errorf("dashboard HTML missing sprint-picker marker %q", s)
		}
	}

	wantJS := []string{
		"onSprintChange",
		"loadSprintPicker",
		"/api/sprints",
		`p.set('sprint'`,
		`p.get('sprint')`,
	}
	for _, s := range wantJS {
		if !strings.Contains(page, s) {
			t.Errorf("dashboard JS missing sprint-picker symbol %q", s)
		}
	}

	wantCSS := []string{
		`.sprint-picker {`,
		`.sprint-picker.active`,
	}
	for _, s := range wantCSS {
		if !strings.Contains(page, s) {
			t.Errorf("dashboard CSS missing sprint-picker rule %q", s)
		}
	}
}
