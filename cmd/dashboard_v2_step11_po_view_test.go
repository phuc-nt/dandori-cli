package cmd

import (
	"strings"
	"testing"
)

// TestDashboardV2_POViewMarkup verifies the Phase 02 PO View persona tab is
// wired into the dashboard: nav item, section, 3 sub-tabs, all widgets
// (canvases / tile / spark), JS loaders, and the Tasks section with Sprint
// Board + Lifecycle Gantt.
func TestDashboardV2_POViewMarkup(t *testing.T) {
	page := dashboardHTMLForTest(t)
	cssBytes, err := dashboardFS.ReadFile("web/dashboard/style.css")
	if err != nil {
		t.Fatalf("read style.css: %v", err)
	}
	page += "\n" + string(cssBytes)

	wantHTML := []string{
		// Sidebar nav.
		`href="#po-view-section"`,
		`href="#tasks-section"`,
		`data-persona="po"`,
		`data-persona="tasks"`,
		// PO View section + sub-tabs.
		`id="po-view-section"`,
		`data-pview="velocity"`,
		`data-pview="cost"`,
		`data-pview="attribution"`,
		// PO widgets.
		`id="po-burndown-canvas"`,
		`id="po-leadtime-canvas"`,
		`id="po-cost-dept-canvas"`,
		`id="po-projection-tile"`,
		`id="po-projection-big"`,
		`id="po-projection-spark"`,
		`id="po-attribution-canvas"`,
		// Tasks section.
		`id="tasks-section"`,
		`data-tview="sprint"`,
		`data-tview="lifecycle"`,
		`id="tasks-sprint-board"`,
		`id="tasks-lifecycle-gantt"`,
	}
	for _, s := range wantHTML {
		if !strings.Contains(page, s) {
			t.Errorf("PO View HTML missing %q", s)
		}
	}

	wantJS := []string{
		"showPersonaSection",
		"setPOSubTab",
		"setTasksSubTab",
		"loadPOView",
		"renderPOBurndown",
		"renderPOLeadTime",
		"renderPOCostByDept",
		"renderPOCostProjection",
		"renderPOAttribution",
		"renderSprintBoard",
		"openTaskLifecycle",
		"renderLifecycleGantt",
		"/api/sprints/burndown",
		"/api/cost/department",
		"/api/cost/projection",
		"/api/attribution/timeline",
		"/api/runs/lead-time",
		"/api/tasks/lifecycle",
	}
	for _, s := range wantJS {
		if !strings.Contains(page, s) {
			t.Errorf("PO View JS missing %q", s)
		}
	}

	wantCSS := []string{
		`.persona-tabs {`,
		`.persona-tab.active`,
		`.grid-2col {`,
		`.projection-tile {`,
		`.sprint-board {`,
		`.task-gantt`,
	}
	for _, s := range wantCSS {
		if !strings.Contains(page, s) {
			t.Errorf("PO View CSS missing %q", s)
		}
	}
}
