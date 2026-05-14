// Package db — pr_cycle_time.go: PR Review Cycle Time diagnostic metric
// over `pr_events` (v0.13+). Measures median + p75 hours between PR
// submission (submitted_at == GitHub created_at) and the first APPROVED
// review (first_approval_at). Surfaces coverage so users see how many
// merged PRs in the window actually had a review.
//
// Diagnostic only — not a KR. Used by framework §8's "High Trust + Low
// Deploy → process bottleneck" interpretation.
package db

import (
	"fmt"
	"sort"
)

// PRCycleResult is one window's snapshot. HasData=false when MergedTotal
// is zero OR no merged PR in the window had an approving review (solo
// engineers + auto-merge teams land here, so the empty state is intentional).
type PRCycleResult struct {
	MedianHours  float64 `json:"median_hours"`
	P75Hours     float64 `json:"p75_hours"`
	MergedTotal  int     `json:"merged_total"`
	WithApproval int     `json:"with_approval"`
	WindowDays   int     `json:"window_days"`
	HasData      bool    `json:"has_data"`
}

// GetPRReviewCycleTime computes the median + p75 first-approval latency
// over PRs merged in [now − days, now). Single SELECT pulls deltas; the
// percentile pick runs in Go because SQLite lacks a builtin MEDIAN.
func (l *LocalDB) GetPRReviewCycleTime(days int) (PRCycleResult, error) {
	if days <= 0 {
		days = 28
	}
	out := PRCycleResult{WindowDays: days}

	rows, err := l.Query(`
		SELECT
			CASE
				WHEN first_approval_at IS NOT NULL AND submitted_at != ''
				THEN (julianday(first_approval_at) - julianday(submitted_at)) * 24.0
				ELSE NULL
			END AS hours
		FROM pr_events
		WHERE merged_at IS NOT NULL
		  AND merged_at >= datetime('now', ?)
	`, fmt.Sprintf("-%d days", days))
	if err != nil {
		return out, fmt.Errorf("pr-cycle query: %w", err)
	}
	defer rows.Close()

	var deltas []float64
	for rows.Next() {
		var h *float64
		if err := rows.Scan(&h); err != nil {
			return out, fmt.Errorf("pr-cycle scan: %w", err)
		}
		out.MergedTotal++
		if h != nil && *h >= 0 {
			out.WithApproval++
			deltas = append(deltas, *h)
		}
	}
	if err := rows.Err(); err != nil {
		return out, fmt.Errorf("pr-cycle rows: %w", err)
	}

	if out.MergedTotal == 0 || out.WithApproval == 0 {
		return out, nil
	}

	sort.Float64s(deltas)
	out.MedianHours = percentileFloat(deltas, 0.50)
	out.P75Hours = percentileFloat(deltas, 0.75)
	out.HasData = true
	return out, nil
}

// percentileFloat picks the `p`-quantile from a sorted slice using linear
// interpolation between the two nearest ranks. For our tiny windows (~65
// PRs) this is plenty accurate and matches what users expect from
// dashboard tools like Datadog or Grafana's `percentile()`.
//
// Distinct from `percentile` in eng_queries.go which takes an int 0..100
// and uses ceil-rank — kept separate to avoid touching that call site.
func percentileFloat(sorted []float64, p float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n == 1 {
		return sorted[0]
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 1 {
		return sorted[n-1]
	}
	pos := p * float64(n-1)
	lo := int(pos)
	hi := lo + 1
	if hi >= n {
		return sorted[n-1]
	}
	frac := pos - float64(lo)
	return sorted[lo] + frac*(sorted[hi]-sorted[lo])
}
