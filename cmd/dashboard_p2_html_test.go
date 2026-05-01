package cmd

import (
	"strings"
	"testing"
)

// TestDashboardHTMLv2_ContainsProjectViewMarkup asserts that dashboardHTML
// contains the required P2 UI elements. This catches accidental deletion during
// future edits without requiring a running browser.
func TestDashboardHTMLv2_ContainsProjectViewMarkup(t *testing.T) {
	t.Helper()
	html := dashboardHTML

	checks := []struct {
		desc    string
		snippet string
	}{
		// Role switcher must have the project option.
		{"project role option", `value="project"`},
		// Period selector must be present.
		{"period selector element", `id="period-selector"`},
		// Compare toggle must be present.
		{"compare toggle element", `id="compare-toggle"`},
		// Filter pill bar.
		{"filter pill bar", `id="filter-pill-bar"`},
		// Project view section.
		{"project view section", `id="project-view"`},
		// Project hero tiles.
		{"project cost tile", `id="proj-cost"`},
		{"project tasks tile", `id="proj-tasks"`},
		{"project avg cost tile", `id="proj-avg-cost"`},
		{"project dora mini tile", `id="proj-dora-light"`},
		// Project DORA scorecard grid.
		{"project dora grid", `id="proj-dora-grid"`},
		// Cost burn chart canvas.
		{"project burn chart canvas", `id="project-burn-chart"`},
		// Project tasks table.
		{"project tasks table", `id="project-tasks-table"`},
		// Project selector (inline).
		{"project selector wrap", `id="project-selector-wrap"`},
		// Iteration histogram canvas (P3 — no longer TBD).
		{"iteration histogram canvas", `id="iteration-histogram-chart"`},
		// Insight grid (project + org).
		{"project insights grid", `id="project-insights-grid"`},
		{"org insights grid", `id="org-insights-grid"`},
		// JS hooks added in P3.
		{"loadInsights function", `function loadInsights(`},
		{"loadIterationHistogram function", `function loadIterationHistogram(`},
		{"renderDelta function", `function renderDelta(`},
		// URL state machine functions.
		{"readState function", `function readState(`},
		{"writeState function", `function writeState(`},
		{"updateState function", `function updateState(`},
		{"buildAPIQuery function", `function buildAPIQuery(`},
		{"syncUIToState function", `function syncUIToState(`},
		// Landing fetch on init.
		{"landing API fetch", `/api/g9/landing`},
	}

	for _, c := range checks {
		if !strings.Contains(html, c.snippet) {
			t.Errorf("dashboardHTML missing %s: expected to find %q", c.desc, c.snippet)
		}
	}
}

// TestDashboardHTML_ContainsG9Markers asserts the G9 boundary comments are
// present (regression guard so future edits don't accidentally strip them).
func TestDashboardHTML_ContainsG9Markers(t *testing.T) {
	if !strings.Contains(dashboardHTML, "G9-") {
		t.Error("dashboardHTML missing G9- marker comments")
	}
	if !strings.Contains(dashboardHTML, "G9 Analytics") {
		t.Error("dashboardHTML missing G9 Analytics sidebar badge")
	}
}

// TestDashboardHTMLv2_PeriodDefaultValues asserts that the period selector
// option values match the spec (7d, 28d, 90d, custom).
func TestDashboardHTMLv2_PeriodDefaultValues(t *testing.T) {
	html := dashboardHTML
	for _, val := range []string{`value="7d"`, `value="28d"`, `value="90d"`, `value="custom"`} {
		if !strings.Contains(html, val) {
			t.Errorf("dashboardHTML period selector missing option %s", val)
		}
	}
}
