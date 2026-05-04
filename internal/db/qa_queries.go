// Package db — qa_queries.go: query helpers for Phase 04 QA View.
//
// All queries derive from existing tables: runs, quality_metrics, task_attribution.
// No new schema introduced (Phase 04 plan §6 risk: buglink table absent → use
// quality_metrics regression as proxy for "bug hotspot").
package db

import (
	"encoding/json"
	"time"
)

// QualityTimelinePoint is one weekly aggregate per project.
type QualityTimelinePoint struct {
	Week          string  `json:"week"`            // ISO week start (YYYY-MM-DD, Monday)
	Project       string  `json:"project"`         // jira project key (CLITEST1, ...)
	LintDelta     float64 `json:"lint_delta"`      // avg per-run lint_delta
	TestsDelta    float64 `json:"tests_delta"`     // avg per-run tests_delta
	CommitMsgQual float64 `json:"commit_msg_qual"` // avg commit_msg_quality
	Runs          int     `json:"runs"`
}

// QualityTimeline returns weekly avg deltas + commit msg quality for one project.
// Empty project → all projects aggregated together.
func (l *LocalDB) QualityTimeline(project string, weeks int) ([]QualityTimelinePoint, error) {
	if weeks <= 0 {
		weeks = 12
	}
	cutoff := time.Now().AddDate(0, 0, -weeks*7).Format(time.RFC3339)
	q := `
		SELECT
			date(r.started_at, 'weekday 0', '-6 days') AS week,
			COALESCE(substr(r.jira_issue_key, 1, instr(r.jira_issue_key, '-') - 1), '(none)') AS project,
			COALESCE(AVG(qm.lint_delta), 0) AS lint_delta,
			COALESCE(AVG(qm.tests_delta), 0) AS tests_delta,
			COALESCE(AVG(qm.commit_msg_quality), 0) AS msg_q,
			COUNT(*) AS runs
		FROM runs r
		LEFT JOIN quality_metrics qm ON qm.run_id = r.id
		WHERE r.started_at >= ?
	`
	args := []any{cutoff}
	if project != "" {
		q += " AND r.jira_issue_key LIKE ?"
		args = append(args, project+"-%")
	}
	q += " GROUP BY week, project ORDER BY week ASC"

	rows, err := l.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []QualityTimelinePoint{}
	for rows.Next() {
		var p QualityTimelinePoint
		if err := rows.Scan(&p.Week, &p.Project, &p.LintDelta, &p.TestsDelta, &p.CommitMsgQual, &p.Runs); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// CostQualityPoint is one (cost, quality_score, run_id) tuple for the scatter chart.
type CostQualityPoint struct {
	RunID   string  `json:"run_id"`
	Cost    float64 `json:"cost"`
	Quality float64 `json:"quality"`
	Status  string  `json:"status"`
}

// CostQualityScatter returns up to `limit` (cost, quality_score) pairs.
// Plan §10.4: caller should hexbin if >1000 returned (limit defaults to 2000).
func (l *LocalDB) CostQualityScatter(limit int) ([]CostQualityPoint, error) {
	if limit <= 0 {
		limit = 2000
	}
	rows, err := l.Query(`
		SELECT r.id, COALESCE(r.cost_usd, 0), COALESCE(qm.quality_score, 0), COALESCE(r.status, '')
		FROM runs r
		JOIN quality_metrics qm ON qm.run_id = r.id
		WHERE qm.quality_score IS NOT NULL
		ORDER BY r.started_at DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CostQualityPoint{}
	for rows.Next() {
		var p CostQualityPoint
		if err := rows.Scan(&p.RunID, &p.Cost, &p.Quality, &p.Status); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// CommitMsgBucket is one of 4 quality buckets.
type CommitMsgBucket struct {
	Bucket string `json:"bucket"` // "0-25", "25-50", "50-75", "75-100"
	Count  int    `json:"count"`
}

// CommitMsgDistribution buckets commit_msg_quality into 4 quartiles.
func (l *LocalDB) CommitMsgDistribution() ([]CommitMsgBucket, error) {
	rows, err := l.Query(`
		SELECT
			CASE
				WHEN commit_msg_quality < 25 THEN '0-25'
				WHEN commit_msg_quality < 50 THEN '25-50'
				WHEN commit_msg_quality < 75 THEN '50-75'
				ELSE '75-100'
			END AS bucket,
			COUNT(*) AS c
		FROM quality_metrics
		WHERE commit_msg_quality IS NOT NULL
		GROUP BY bucket
		ORDER BY bucket ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	got := map[string]int{}
	for rows.Next() {
		var b string
		var c int
		if err := rows.Scan(&b, &c); err != nil {
			return nil, err
		}
		got[b] = c
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Always return 4 buckets in fixed order, padded with zeros.
	order := []string{"0-25", "25-50", "50-75", "75-100"}
	out := make([]CommitMsgBucket, 0, 4)
	for _, b := range order {
		out = append(out, CommitMsgBucket{Bucket: b, Count: got[b]})
	}
	return out, nil
}

// BugHotspotCell is one (repo, week) cell counted from real Jira buglinks.
type BugHotspotCell struct {
	Repo  string `json:"repo"`
	Week  string `json:"week"`
	Count int    `json:"count"` // # distinct bugs whose offending run lives in this (repo, week)
}

// BugHotspots returns weekly bug-link counts grouped by repo (git_remote/cwd).
// One cell per (repo, week) where the count is the number of distinct Jira
// bugs whose linked offending run started in that week and lives in that repo.
//
// Source of truth is the buglinks table (fed by the task-done hook in
// cmd/task.go). The week is bucketed against the offending run's started_at
// because that's the moment the regression entered the codebase, regardless
// of when the bug was filed.
//
// Pre v10 this was a regression proxy (lint_delta>0 OR tests_delta<0) which
// was noisy — see plan §02 / phase-02.
func (l *LocalDB) BugHotspots(weeks int) ([]BugHotspotCell, error) {
	if weeks <= 0 {
		weeks = 8
	}
	cutoff := time.Now().AddDate(0, 0, -weeks*7).Format(time.RFC3339)
	rows, err := l.Query(`
		SELECT
			COALESCE(NULLIF(r.git_remote, ''), r.cwd) AS repo,
			date(r.started_at, 'weekday 0', '-6 days') AS week,
			COUNT(DISTINCT bl.jira_bug_key) AS c
		FROM buglinks bl
		JOIN runs r ON r.id = bl.run_id
		WHERE r.started_at >= ?
		  AND COALESCE(NULLIF(r.git_remote, ''), r.cwd) IS NOT NULL
		GROUP BY repo, week
		ORDER BY week ASC, repo ASC
	`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BugHotspotCell{}
	for rows.Next() {
		var c BugHotspotCell
		if err := rows.Scan(&c.Repo, &c.Week, &c.Count); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ReworkCause is a single bucket of session-rework reasons.
type ReworkCause struct {
	Cause string `json:"cause"`
	Count int    `json:"count"`
}

// ReworkCauses sums task_attribution.session_outcomes across all rows and
// returns one ReworkCause per RunOutcomeReason (in canonical ReasonOrder),
// padded with zeros. Post v8→v9 migration the JSON is map[enum]int — no
// string-match bucketing needed.
//
// Unknown keys (data written before migration finished, or malformed JSON)
// fold into ReasonOther so the dashboard never loses a count.
func (l *LocalDB) ReworkCauses() ([]ReworkCause, error) {
	rows, err := l.Query(`SELECT COALESCE(session_outcomes, '') FROM task_attribution`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[RunOutcomeReason]int{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		if raw == "" {
			continue
		}
		var m map[string]int
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			counts[ReasonOther]++
			continue
		}
		for k, v := range m {
			counts[normalizeReasonKey(k)] += v
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]ReworkCause, 0, len(ReasonOrder))
	for _, k := range ReasonOrder {
		out = append(out, ReworkCause{Cause: string(k), Count: counts[k]})
	}
	return out, nil
}

// normalizeReasonKey accepts a stored JSON key and returns a canonical enum
// value. Anything not in the enum becomes ReasonOther.
func normalizeReasonKey(k string) RunOutcomeReason {
	r := RunOutcomeReason(k)
	for _, v := range ReasonOrder {
		if r == v {
			return r
		}
	}
	return ReasonOther
}

// InterventionCell is one (engineer, hour) cell of the intervention heatmap.
type InterventionCell struct {
	Engineer string `json:"engineer"`
	Hour     int    `json:"hour"`
	Count    int    `json:"count"`
}

// InterventionHeatmap groups runs (where human_intervention_count > 0) by
// hour-of-day and engineer.
func (l *LocalDB) InterventionHeatmap(days int) ([]InterventionCell, error) {
	if days <= 0 {
		days = 28
	}
	cutoff := time.Now().AddDate(0, 0, -days).Format(time.RFC3339)
	rows, err := l.Query(`
		SELECT
			COALESCE(engineer_name, '(unknown)') AS eng,
			CAST(strftime('%H', started_at) AS INTEGER) AS hr,
			COUNT(*) AS c
		FROM runs
		WHERE started_at >= ? AND COALESCE(human_intervention_count, 0) > 0
		GROUP BY eng, hr
		ORDER BY eng ASC, hr ASC
	`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []InterventionCell{}
	for rows.Next() {
		var c InterventionCell
		if err := rows.Scan(&c.Engineer, &c.Hour, &c.Count); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
