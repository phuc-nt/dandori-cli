// Package db — rca_aggregation.go: structured RCA (root cause analysis) aggregation (v0.11 Phase 02).
//
// Answers: "Out of N rework events this month, where should I focus?"
//
// Data source: task_attribution.session_outcomes (JSON map[reason]int) joined
// with runs via jira_issue_key for agent attribution and task-type derivation.
//
// No schema changes. Reuses existing RunOutcomeReason enum + normalizeReasonKey.
// resolveTaskType is defined in agent_task_affinity.go (same package).
package db

import (
	"encoding/json"
	"fmt"
	"math"
	"time"
)

// RcaRow is one cause bucket in the aggregated RCA breakdown.
type RcaRow struct {
	Cause       string  `json:"cause"`         // RunOutcomeReason string
	Count       int     `json:"count"`         // total occurrences in window
	Pct         float64 `json:"pct"`           // percentage of total rework events (0–100)
	TopAgent    string  `json:"top_agent"`     // agent with most occurrences of this cause
	TopTaskType string  `json:"top_task_type"` // task type with most occurrences
	WoWDelta    float64 `json:"wow_delta"`     // pct-point change vs prior equal-length window; NaN → 0
}

// attributionRow holds the raw data for one task_attribution row that we need.
type attributionRow struct {
	jiraIssueKey string
	outcomes     string // JSON
	doneAt       string
}

// GetRcaBreakdown aggregates rework cause codes for attributions whose
// jira_done_at falls within [since, now). Correlates each cause with the
// top contributing agent (via runs JOIN on jira_issue_key) and task type.
// Returns rows in ReasonOrder (canonical display order), omitting causes with
// count == 0 in both current and prior windows.
func (l *LocalDB) GetRcaBreakdown(since time.Time) ([]RcaRow, error) {
	now := time.Now().UTC()
	windowLen := now.Sub(since)
	priorStart := since.Add(-windowLen)

	// Fetch attribution rows for current + prior window combined so we only
	// make one SQL round-trip. We tag them in Go by done_at.
	rows, err := l.fetchAttributionRows(priorStart, now)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return []RcaRow{}, nil
	}

	// Build agent-lookup: jira_issue_key → last agent_name from runs.
	agentByKey, err := l.buildAgentLookup(priorStart, now)
	if err != nil {
		return nil, err
	}

	// Task-type cache (prefix parse, same as Phase 01).
	typeCache := map[string]string{}

	// Aggregate: cause → {total, agentCount map, typeCount map} for current + prior.
	type aggrBucket struct {
		total    int
		agentCnt map[string]int
		typeCnt  map[string]int
	}
	newBucket := func() *aggrBucket {
		return &aggrBucket{agentCnt: map[string]int{}, typeCnt: map[string]int{}}
	}

	curr := map[RunOutcomeReason]*aggrBucket{}
	prior := map[RunOutcomeReason]*aggrBucket{}

	for _, row := range rows {
		if row.outcomes == "" {
			continue
		}
		var m map[string]int
		if err := json.Unmarshal([]byte(row.outcomes), &m); err != nil {
			// Malformed JSON → fold into "other" bucket with count=1.
			m = map[string]int{string(ReasonOther): 1}
		}

		agent := agentByKey[row.jiraIssueKey]
		taskType := resolveTaskType(row.jiraIssueKey, typeCache)

		inCurrent := !row.parsedDoneAt().Before(since)

		for k, v := range m {
			reason := normalizeReasonKey(k)
			target := prior
			if inCurrent {
				target = curr
			}
			b, ok := target[reason]
			if !ok {
				b = newBucket()
				target[reason] = b
			}
			b.total += v
			if agent != "" {
				b.agentCnt[agent] += v
			}
			b.typeCnt[taskType] += v
		}
	}

	// Sum totals for percentage calculation.
	var currTotal, priorTotal int
	for _, b := range curr {
		currTotal += b.total
	}
	for _, b := range prior {
		priorTotal += b.total
	}

	// Build output in ReasonOrder, skipping all-zero rows.
	out := make([]RcaRow, 0, len(ReasonOrder))
	for _, reason := range ReasonOrder {
		cb := curr[reason]
		pb := prior[reason]
		currCount := 0
		priorCount := 0
		if cb != nil {
			currCount = cb.total
		}
		if pb != nil {
			priorCount = pb.total
		}
		if currCount == 0 && priorCount == 0 {
			continue
		}

		var pct float64
		if currTotal > 0 {
			pct = roundTo1(float64(currCount) / float64(currTotal) * 100)
		}

		var wowDelta float64
		if priorTotal > 0 {
			priorPct := float64(priorCount) / float64(priorTotal) * 100
			wowDelta = roundTo1(pct - priorPct)
		}
		// If wowDelta is NaN or Inf (shouldn't happen but guard anyway) → 0.
		if math.IsNaN(wowDelta) || math.IsInf(wowDelta, 0) {
			wowDelta = 0
		}

		row := RcaRow{
			Cause:    string(reason),
			Count:    currCount,
			Pct:      pct,
			WoWDelta: wowDelta,
		}
		if cb != nil {
			row.TopAgent = topKey(cb.agentCnt)
			row.TopTaskType = topKey(cb.typeCnt)
		}
		out = append(out, row)
	}

	return out, nil
}

// fetchAttributionRows returns task_attribution rows with done_at in [from, to).
func (l *LocalDB) fetchAttributionRows(from, to time.Time) ([]attributionRow, error) {
	q := `
		SELECT
			jira_issue_key,
			COALESCE(session_outcomes, ''),
			jira_done_at
		FROM task_attribution
		WHERE jira_done_at >= ? AND jira_done_at < ?
		ORDER BY jira_done_at ASC
	`
	rows, err := l.Query(q,
		from.UTC().Format(time.RFC3339),
		to.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return nil, fmt.Errorf("rca fetch attribution: %w", err)
	}
	defer rows.Close()

	var out []attributionRow
	for rows.Next() {
		var r attributionRow
		if err := rows.Scan(&r.jiraIssueKey, &r.outcomes, &r.doneAt); err != nil {
			return nil, fmt.Errorf("rca scan attribution: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// buildAgentLookup returns a map[jiraIssueKey]agentName using the most recent
// run per issue key in the given window (runs with non-empty agent_name).
func (l *LocalDB) buildAgentLookup(from, to time.Time) (map[string]string, error) {
	q := `
		SELECT jira_issue_key, agent_name
		FROM runs
		WHERE jira_issue_key IS NOT NULL
		  AND jira_issue_key != ''
		  AND agent_name IS NOT NULL
		  AND agent_name != ''
		  AND started_at >= ? AND started_at < ?
		ORDER BY started_at DESC
	`
	rows, err := l.Query(q,
		from.UTC().Format(time.RFC3339),
		to.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return nil, fmt.Errorf("rca agent lookup: %w", err)
	}
	defer rows.Close()

	m := map[string]string{}
	for rows.Next() {
		var key, agent string
		if err := rows.Scan(&key, &agent); err != nil {
			return nil, fmt.Errorf("rca scan agent: %w", err)
		}
		if _, already := m[key]; !already {
			m[key] = agent // first row = most recent (ORDER BY started_at DESC)
		}
	}
	return m, rows.Err()
}

// topKey returns the map key with the highest integer value.
// Returns "" when the map is empty.
func topKey(m map[string]int) string {
	best := ""
	bestV := -1
	for k, v := range m {
		if v > bestV || (v == bestV && k < best) {
			best = k
			bestV = v
		}
	}
	return best
}

// parsedDoneAt parses the row's doneAt string into time.Time. On failure
// returns the zero time (which sorts before any real since, landing in prior).
func (r attributionRow) parsedDoneAt() time.Time {
	t, err := time.Parse(time.RFC3339, r.doneAt)
	if err != nil {
		// Try without timezone (older rows may be stored as "2006-01-02T15:04:05").
		t, err = time.ParseInLocation("2006-01-02T15:04:05", r.doneAt, time.UTC)
		if err != nil {
			return time.Time{}
		}
	}
	return t
}
