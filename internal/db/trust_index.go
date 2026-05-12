// Package db — trust_index.go: composite KR combining Code Acceptance Rate,
// AI Change Failure Rate (proxy), and Human Intervention Rate into a single
// 0-100 score that bands the agent's autonomy posture.
//
// Formula (per docs/reference/04-metric-framework.md §3):
//
//	Trust = 0.40 * acceptance
//	      + 0.35 * (1 − ai_cfr)
//	      + 0.25 * (1 − clamp(intervention, 0..1))
//
// Bands:
//
//	≥ 80 → "autonomous"   (agent owns complex features; human PR-stage review)
//	60-79 → "co-own"      (pair design review; human validates approach)
//	< 60  → "copilot"     (human leads; agent assists)
//	no-data → "no-data"   (≥ 1 component lacks data in the window)
//
// AI-CFR is the v0.12 INTERIM proxy: SUM(total_iterations > 1) / COUNT(tasks)
// over task_attribution windowed by jira_done_at. True AI-CFR (DORA-style
// "reverted within 7d") lands in v0.13 alongside PR/deploy event capture.
package db

import (
	"fmt"
	"math"
	"time"
)

// TrustComponents are the 3 raw inputs (each 0..1 ratio, NOT percent).
type TrustComponents struct {
	Acceptance       float64 `json:"acceptance"`        // 0..1, agent-line share
	AICFR            float64 `json:"ai_cfr"`            // 0..1, proxy for now
	InterventionRate float64 `json:"intervention_rate"` // 0..1, clamped
}

// TrustResult is one window's composite. HasData=false when any input had
// zero denominator — the band degrades to "no-data" so the consumer renders
// a neutral state rather than a misleading 0.
type TrustResult struct {
	Value      int             `json:"value"`       // 0..100, rounded
	Band       string          `json:"band"`        // autonomous|co-own|copilot|no-data
	Components TrustComponents `json:"components"`
	WindowDays int             `json:"window_days"`
	HasData    bool            `json:"has_data"`
}

// trustWeight is the §3 mix; kept as a struct so tests can document intent.
var trustWeight = struct {
	Acceptance   float64
	Stability    float64 // applied to (1 − cfr)
	Intervention float64 // applied to (1 − intervention)
}{0.40, 0.35, 0.25}

// bandFor classifies a 0..100 Trust value. Boundaries are inclusive at the
// lower edge (60 → "co-own", 80 → "autonomous").
func bandFor(v int) string {
	switch {
	case v >= 80:
		return "autonomous"
	case v >= 60:
		return "co-own"
	default:
		return "copilot"
	}
}

// composeTrust runs the formula on already-fetched components. Exposed
// separately from the SQL-fetching wrapper so tests can pin specific inputs.
func composeTrust(c TrustComponents, days int, hasData bool) TrustResult {
	if !hasData {
		return TrustResult{
			Value:      0,
			Band:       "no-data",
			Components: c,
			WindowDays: days,
			HasData:    false,
		}
	}
	// Intervention rate is runs-weighted (Σ interventions / N runs) and can
	// legitimately exceed 1.0 when avg interventions per run > 1. Clamp the
	// (1 − rate) term to [0, 1] so a noisy agent can't drive the composite
	// negative.
	stability := 1.0 - c.AICFR
	if stability < 0 {
		stability = 0
	}
	autonomy := 1.0 - c.InterventionRate
	if autonomy < 0 {
		autonomy = 0
	}
	raw := trustWeight.Acceptance*c.Acceptance +
		trustWeight.Stability*stability +
		trustWeight.Intervention*autonomy
	v := int(math.Round(raw * 100))
	if v < 0 {
		v = 0
	}
	if v > 100 {
		v = 100
	}
	return TrustResult{
		Value:      v,
		Band:       bandFor(v),
		Components: c,
		WindowDays: days,
		HasData:    true,
	}
}

// GetTrustIndex computes the composite over a [now − days, now) window.
// Returns HasData=false (band="no-data") when the window has no task_attribution
// rows or no runs — Trust is undefined in those cases, not zero.
func (l *LocalDB) GetTrustIndex(days int) (TrustResult, error) {
	if days <= 0 {
		days = 28
	}
	since := time.Now().UTC().AddDate(0, 0, -days).Format(time.RFC3339)

	c, hasData, err := l.fetchTrustComponents(since)
	if err != nil {
		return TrustResult{}, err
	}
	return composeTrust(c, days, hasData), nil
}

// fetchTrustComponents runs two cheap queries (task_attribution + runs) and
// returns the 3 ratios. hasData is false when EITHER source has zero rows.
func (l *LocalDB) fetchTrustComponents(since string) (TrustComponents, bool, error) {
	var c TrustComponents

	// task_attribution → acceptance + cfr proxy
	var (
		agentLines, humanLines int
		tasks, reopenedTasks   int
	)
	row := l.QueryRow(`
		SELECT
			COALESCE(SUM(lines_attributed_agent), 0)        AS agent_lines,
			COALESCE(SUM(lines_attributed_human), 0)        AS human_lines,
			COUNT(*)                                        AS tasks,
			COALESCE(SUM(CASE WHEN total_iterations > 1
			                  THEN 1 ELSE 0 END), 0)        AS reopened
		FROM task_attribution
		WHERE jira_done_at >= ?
	`, since)
	if err := row.Scan(&agentLines, &humanLines, &tasks, &reopenedTasks); err != nil {
		return c, false, fmt.Errorf("trust: task_attribution scan: %w", err)
	}

	// runs → intervention rate
	var runs, interventions int
	row = l.QueryRow(`
		SELECT
			COUNT(*) AS runs,
			COALESCE(SUM(human_intervention_count), 0) AS interventions
		FROM runs
		WHERE started_at >= ?
	`, since)
	if err := row.Scan(&runs, &interventions); err != nil {
		return c, false, fmt.Errorf("trust: runs scan: %w", err)
	}

	// HasData rule: need ≥ 1 task with non-zero line attribution AND ≥ 1 run.
	totalLines := agentLines + humanLines
	if totalLines == 0 || tasks == 0 || runs == 0 {
		return c, false, nil
	}

	c.Acceptance = float64(agentLines) / float64(totalLines)
	c.AICFR = float64(reopenedTasks) / float64(tasks)
	c.InterventionRate = float64(interventions) / float64(runs)
	return c, true, nil
}
