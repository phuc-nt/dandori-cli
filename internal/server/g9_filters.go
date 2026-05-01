// Package server — g9_filters.go: attribution computation and scope-filter helpers
// for G9 dashboard handlers. Extracted from g9_routes.go to stay under 500 lines.
package server

import (
	"time"

	"github.com/phuc-nt/dandori-cli/internal/db"
	"github.com/phuc-nt/dandori-cli/internal/metric"
)

// countRunsInWindow counts runs within [cur.Start, cur.End) optionally scoped
// to a project key when role="project".
func countRunsInWindow(store *db.LocalDB, cur *Window, role, id string) (int, error) {
	startStr := cur.Start.UTC().Format(time.RFC3339)
	endStr := cur.End.UTC().Format(time.RFC3339)

	var count int
	var err error

	if role == "project" && id != "" {
		prefix := id + "-%"
		rows, qerr := store.Query(
			`SELECT COUNT(*) FROM runs WHERE started_at >= ? AND started_at < ? AND jira_issue_key LIKE ?`,
			startStr, endStr, prefix,
		)
		if qerr != nil {
			return 0, qerr
		}
		defer rows.Close()
		if rows.Next() {
			err = rows.Scan(&count)
		}
		if rerr := rows.Err(); rerr != nil {
			return 0, rerr
		}
	} else {
		rows, qerr := store.Query(
			`SELECT COUNT(*) FROM runs WHERE started_at >= ? AND started_at < ?`,
			startStr, endStr,
		)
		if qerr != nil {
			return 0, qerr
		}
		defer rows.Close()
		if rows.Next() {
			err = rows.Scan(&count)
		}
		if rerr := rows.Err(); rerr != nil {
			return 0, rerr
		}
	}
	return count, err
}

// computeAttribution returns the attribution map for the given engineer (empty = org)
// and time window. Called by handleG9Attribution for both current and prior windows.
func computeAttribution(store *db.LocalDB, engineer string, w *Window) (map[string]any, error) {
	metricWindow := metric.MetricWindow{Start: w.Start, End: w.End}
	windowDays := int(w.End.Sub(w.Start).Hours() / 24)

	if engineer == "" {
		result, err := metric.AggregateAttribution(store, metricWindow)
		if err != nil {
			return nil, err
		}
		sparkline, err := attributionSparkline(store, w.End, "")
		if err != nil {
			sparkline = []float64{0, 0, 0, 0}
		}
		resp := map[string]any{
			"authored_pct": result.AgentAutonomyRate,
			"retained_pct": result.RetentionP50,
			"sparkline":    sparkline,
			"window_days":  windowDays,
			"scope":        "org",
		}
		if result.InsufficientData {
			resp["insufficient_data"] = true
		}
		return resp, nil
	}

	// Engineer scope.
	retained, authored, err := engineerAttributionForWindow(store, engineer, w)
	if err != nil {
		return nil, err
	}
	sparkline, _ := attributionSparkline(store, w.End, engineer)
	return map[string]any{
		"authored_pct": authored,
		"retained_pct": retained,
		"sparkline":    sparkline,
		"window_days":  windowDays,
		"scope":        "engineer",
		"engineer":     engineer,
	}, nil
}

// engineerAttributionForWindow queries task_attribution rows for the given
// engineer within an explicit Window. Returns (retentionP50, autonomyRate, error).
func engineerAttributionForWindow(store *db.LocalDB, engineer string, w *Window) (float64, float64, error) {
	windowStart := w.Start.UTC().Format(time.RFC3339)
	windowEnd := w.End.UTC().Format(time.RFC3339)

	rows, err := store.Query(`
		SELECT ta.lines_attributed_agent, ta.lines_attributed_human,
		       ta.intervention_rate, ta.total_human_messages
		FROM task_attribution ta
		WHERE ta.jira_done_at >= ? AND ta.jira_done_at < ?
		  AND EXISTS (
		      SELECT 1 FROM runs r
		      WHERE r.jira_issue_key = ta.jira_issue_key
		        AND COALESCE(r.engineer_name, '') = ?
		  )
	`, windowStart, windowEnd, engineer)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()

	var retentions, interventionRates []float64
	var autonomousCount, classifiableCount int

	for rows.Next() {
		var agentLines, humanLines int
		var intRate float64
		var humanMsgs int
		if err := rows.Scan(&agentLines, &humanLines, &intRate, &humanMsgs); err != nil {
			return 0, 0, err
		}
		if total := agentLines + humanLines; total > 0 {
			retentions = append(retentions, float64(agentLines)/float64(total))
		}
		if humanMsgs > 0 {
			interventionRates = append(interventionRates, intRate)
			classifiableCount++
			if intRate < 0.2 {
				autonomousCount++
			}
		}
	}
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}

	retainedP50 := p50(retentions)
	autonomyRate := 0.0
	if classifiableCount > 0 {
		autonomyRate = float64(autonomousCount) / float64(classifiableCount)
	}
	_ = interventionRates

	return retainedP50, autonomyRate, nil
}

// attributionSparkline returns 4 weekly retention p50 values, oldest first.
// Each bucket is a 7-day window ending at now. Engineer="" means org scope.
func attributionSparkline(store *db.LocalDB, now time.Time, engineer string) ([]float64, error) {
	buckets := make([]float64, 4)
	for i := 0; i < 4; i++ {
		weekEnd := now.AddDate(0, 0, -(3-i)*7)
		weekStart := weekEnd.AddDate(0, 0, -7)
		w := metric.MetricWindow{Start: weekStart, End: weekEnd}

		if engineer == "" {
			res, err := metric.AggregateAttribution(store, w)
			if err != nil || res.InsufficientData {
				buckets[i] = 0
				continue
			}
			buckets[i] = res.RetentionP50
		} else {
			ret, err := engineerRetentionForWindow(store, engineer, w)
			if err != nil {
				buckets[i] = 0
				continue
			}
			buckets[i] = ret
		}
	}
	return buckets, nil
}

// engineerRetentionForWindow computes p50 line retention for one engineer in one
// metric.MetricWindow. Used by attributionSparkline to avoid circular calls.
func engineerRetentionForWindow(store *db.LocalDB, engineer string, w metric.MetricWindow) (float64, error) {
	rows, err := store.Query(`
		SELECT ta.lines_attributed_agent, ta.lines_attributed_human
		FROM task_attribution ta
		WHERE ta.jira_done_at >= ? AND ta.jira_done_at < ?
		  AND EXISTS (
		      SELECT 1 FROM runs r
		      WHERE r.jira_issue_key = ta.jira_issue_key
		        AND COALESCE(r.engineer_name, '') = ?
		  )
	`,
		w.Start.UTC().Format(time.RFC3339),
		w.End.UTC().Format(time.RFC3339),
		engineer,
	)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var retentions []float64
	for rows.Next() {
		var agentLines, humanLines int
		if err := rows.Scan(&agentLines, &humanLines); err != nil {
			return 0, err
		}
		if total := agentLines + humanLines; total > 0 {
			retentions = append(retentions, float64(agentLines)/float64(total))
		}
	}
	return p50(retentions), rows.Err()
}

// p50 returns the median of vals (sorted in-place). Returns 0 for empty slice.
func p50(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	for i := 1; i < len(vals); i++ {
		for j := i; j > 0 && vals[j] < vals[j-1]; j-- {
			vals[j], vals[j-1] = vals[j-1], vals[j]
		}
	}
	n := len(vals)
	if n%2 == 0 {
		return (vals[n/2-1] + vals[n/2]) / 2
	}
	return vals[n/2]
}
