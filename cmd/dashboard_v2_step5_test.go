package cmd

import (
	"strings"
	"testing"
)

// TestDashboardV2_KPIStripMarkup verifies the KPI strip + sparkline + WoW delta
// (Step 5) is wired into the embedded HTML/JS.
func TestDashboardV2_KPIStripMarkup(t *testing.T) {
	page := dashboardHTMLForTest(t)

	wantHTML := []string{
		`class="kpi-strip"`,
		`data-metric="runs"`,
		`data-metric="cost"`,
		`data-metric="tokens"`,
		`data-metric="avg"`,
		`id="kpi-runs-spark"`,
		`id="kpi-cost-spark"`,
		`id="kpi-tokens-spark"`,
		`id="kpi-avg-spark"`,
		`class="kpi-spark"`,
	}
	for _, s := range wantHTML {
		if !strings.Contains(page, s) {
			t.Errorf("dashboard HTML missing KPI-strip marker %q", s)
		}
	}

	wantJS := []string{
		"loadKPIStrip",
		"renderKPIStrip",
		"renderSpark",
		"setKPI",
		"/api/kpi/strip",
	}
	for _, s := range wantJS {
		if !strings.Contains(page, s) {
			t.Errorf("dashboard JS missing KPI-strip symbol %q", s)
		}
	}
}
