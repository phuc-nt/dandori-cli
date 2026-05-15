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
// AI-CFR (v0.13+) is the DORA-style ratio over `pr_events`:
//
//	cfr = COUNT(DISTINCT PRs where reopened_within_7d OR reverted_post_deploy)
//	      / COUNT(merged PRs in window)
//
// When the window has no merged PRs (GitHub disabled, or simply a quiet
// period for users who DO have GitHub on), the calculation falls back to
// the v0.12 proxy SUM(total_iterations > 1) / COUNT(tasks) over
// task_attribution. The fallback preserves Trust scores for users not yet
// on GitHub integration. `CFRSource` in TrustComponents records which
// path produced the number so CLI + dashboard can flag the fallback.
package db

import (
	"database/sql"
	"fmt"
	"math"
	"time"
)

// TrustComponents are the 3 raw inputs (each 0..1 ratio, NOT percent).
// CFRSource records which path produced AICFR:
//   - "pr_events" — true DORA ratio over pr_events table (v0.13+)
//   - "proxy"     — v0.12 iteration-based fallback
//   - "none"      — neither source had usable data
type TrustComponents struct {
	Acceptance       float64 `json:"acceptance"`        // 0..1, agent-line share
	AICFR            float64 `json:"ai_cfr"`            // 0..1
	InterventionRate float64 `json:"intervention_rate"` // 0..1, clamped
	CFRSource        string  `json:"cfr_source"`        // pr_events|proxy|none
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
	// Repo is the per-repo filter applied (v0.14+). Empty when the
	// caller asked for an org-wide aggregate.
	Repo string `json:"repo,omitempty"`
	// RepoScope flags how broadly Repo applied — "all" when Repo is
	// empty, "cfr_only" when set (Acceptance + Intervention come from
	// task_attribution / runs which have no repo column yet, so they
	// remain org-wide). Surfaced so the dashboard can show a tooltip.
	RepoScope string `json:"repo_scope,omitempty"`
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
//
// Equivalent to GetTrustIndexByRepo(days, "") — kept for back-compat with
// existing call sites and the legacy `dandori analytics trust` CLI.
func (l *LocalDB) GetTrustIndex(days int) (TrustResult, error) {
	return l.GetTrustIndexByRepo(days, "")
}

// GetTrustIndexByRepo is the v0.14 multi-repo variant. When repo is
// non-empty the AI-CFR term is scoped to `pr_events.repo = ?`; the other
// two components (Acceptance, Intervention) come from tables that don't
// yet carry a repo column and therefore stay org-wide. Result advertises
// this honestly via RepoScope = "cfr_only".
func (l *LocalDB) GetTrustIndexByRepo(days int, repo string) (TrustResult, error) {
	if days <= 0 {
		days = 28
	}
	since := time.Now().UTC().AddDate(0, 0, -days).Format(time.RFC3339)

	c, hasData, err := l.fetchTrustComponents(since, repo)
	if err != nil {
		return TrustResult{}, err
	}
	res := composeTrust(c, days, hasData)
	if repo != "" {
		res.Repo = repo
		res.RepoScope = "cfr_only"
	} else {
		res.RepoScope = "all"
	}
	return res, nil
}

// fetchTrustComponents runs three cheap queries (task_attribution, runs,
// pr_events) and returns the 3 ratios. hasData is false when the inputs
// can't produce any of the components — see in-line HasData rule.
func (l *LocalDB) fetchTrustComponents(since, repo string) (TrustComponents, bool, error) {
	var c TrustComponents

	// task_attribution → acceptance + cfr proxy (fallback source)
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

	// pr_events → true AI-CFR. Returns zeros + hasMerged=false when no PRs
	// merged in window (GitHub disabled OR quiet window).
	cfr, hasMerged, err := l.queryAICFR(since, repo)
	if err != nil {
		return c, false, fmt.Errorf("trust: pr_events scan: %w", err)
	}

	// HasData rule: need ≥ 1 task with non-zero line attribution AND ≥ 1 run.
	// pr_events absence is acceptable — we fall back to the iteration proxy.
	totalLines := agentLines + humanLines
	if totalLines == 0 || tasks == 0 || runs == 0 {
		c.CFRSource = "none"
		return c, false, nil
	}

	c.Acceptance = float64(agentLines) / float64(totalLines)
	c.InterventionRate = float64(interventions) / float64(runs)

	switch {
	case hasMerged:
		c.AICFR = cfr
		c.CFRSource = "pr_events"
	default:
		c.AICFR = float64(reopenedTasks) / float64(tasks)
		c.CFRSource = "proxy"
	}
	return c, true, nil
}

// queryAICFR computes the true DORA-style AI Change Failure Rate from
// pr_events in the [since, now) window. Returns (cfr, true, nil) when at
// least one PR was merged in window; (0, false, nil) when no merged PRs
// exist — callers fall back to the iteration proxy.
//
// Numerator dedup via COUNT(DISTINCT pr_number): a PR that was both
// reopened (within 7d) AND reverted counts once. Boundary: reopen exactly
// at 7 days IS counted; 7d+1s is not (`<= 7` uses julianday math, which
// treats 7.0 as the inclusive edge).
func (l *LocalDB) queryAICFR(since, repo string) (float64, bool, error) {
	var merged, failed int
	// Two literal queries keeps the prepared statement boring and the
	// repo filter parameterised (no string concat). KISS over a clever
	// builder for one optional WHERE.
	var row *sql.Row
	if repo == "" {
		row = l.QueryRow(`
			SELECT
				COUNT(*) AS merged_prs,
				COUNT(DISTINCT CASE
					WHEN is_reverted = 1 AND reverted_at IS NOT NULL
					     AND reverted_at >= merged_at THEN pr_number
					WHEN reopened_at IS NOT NULL AND closed_at IS NOT NULL
					     AND julianday(reopened_at) - julianday(closed_at) <= 7
					THEN pr_number
				END) AS failed_prs
			FROM pr_events
			WHERE merged_at IS NOT NULL AND merged_at >= ?
		`, since)
	} else {
		row = l.QueryRow(`
			SELECT
				COUNT(*) AS merged_prs,
				COUNT(DISTINCT CASE
					WHEN is_reverted = 1 AND reverted_at IS NOT NULL
					     AND reverted_at >= merged_at THEN pr_number
					WHEN reopened_at IS NOT NULL AND closed_at IS NOT NULL
					     AND julianday(reopened_at) - julianday(closed_at) <= 7
					THEN pr_number
				END) AS failed_prs
			FROM pr_events
			WHERE merged_at IS NOT NULL AND merged_at >= ? AND repo = ?
		`, since, repo)
	}
	if err := row.Scan(&merged, &failed); err != nil {
		return 0, false, err
	}
	if merged == 0 {
		return 0, false, nil
	}
	return float64(failed) / float64(merged), true, nil
}
