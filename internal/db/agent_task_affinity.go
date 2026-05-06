// Package db — agent_task_affinity.go: agent × task-type affinity matrix query.
//
// v0.11 Phase 01. Answers "which agent gives best success rate for which task type?"
//
// The runs table has NO task_type column. Task type is derived at query time:
//  1. resolveTaskType checks an in-memory cache (populated during the query).
//  2. Falls back to Jira prefix parse: "BUG-123" → "Bug", "FEAT-1" → "Feat".
//  3. Final fallback: "(unknown)" bucket.
//
// No schema changes. No Jira API calls (cache-only during lifetime of function).
package db

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// AffinityCell is one (agent, task_type) bucket in the matrix.
type AffinityCell struct {
	Agent       string  `json:"agent"`
	TaskType    string  `json:"task_type"`
	Runs        int     `json:"runs"`
	SuccessRate float64 `json:"success_rate"` // 0–100
}

// rawAffinityRow is an intermediate struct for raw DB scan.
type rawAffinityRow struct {
	agent        string
	jiraIssueKey string
	exitCode     int
}

// jiraPrefixRe matches the leading uppercase prefix of a Jira key, e.g. "BUG" from "BUG-123".
var jiraPrefixRe = regexp.MustCompile(`^([A-Z][A-Z0-9]*)[-]`)

// resolveTaskType derives a human-readable task type from a Jira issue key.
// cache: caller-provided map[issueKey]taskType — populated lazily.
// Strategy (priority order):
//  1. Cache hit → return cached value.
//  2. Regex prefix parse: "CLITEST-99" → "Clitest", "BUG-1" → "Bug".
//  3. Fallback "(unknown)".
//
// The cache is intentionally NOT a global so every call to GetAgentTaskAffinity
// gets a fresh cache (consistent within a single query, no stale-Jira risk).
func resolveTaskType(issueKey string, cache map[string]string) string {
	if issueKey == "" {
		return "(unknown)"
	}
	if v, ok := cache[issueKey]; ok {
		return v
	}
	var result string
	if m := jiraPrefixRe.FindStringSubmatch(issueKey); m != nil {
		// Title-case the prefix so "BUG" → "Bug", "FEAT" → "Feat".
		prefix := m[1]
		result = strings.ToUpper(prefix[:1]) + strings.ToLower(prefix[1:])
	} else {
		result = "(unknown)"
	}
	cache[issueKey] = result
	return result
}

// GetAgentTaskAffinity returns the agent × task-type affinity matrix for runs
// started at or after `since`. Empty slice (never nil) is returned when the
// database has no matching runs.
//
// Performance: single SQL SELECT + Go-side aggregation. On a solo 10k-row runs
// table this completes well under 500 ms.
func (l *LocalDB) GetAgentTaskAffinity(since time.Time) ([]AffinityCell, error) {
	q := `
		SELECT
			COALESCE(agent_name, '') AS agent,
			COALESCE(jira_issue_key, '') AS jira_issue_key,
			COALESCE(exit_code, -1) AS exit_code
		FROM runs
		WHERE started_at >= ?
		ORDER BY agent ASC
	`
	rows, err := l.Query(q, since.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("agent task affinity query: %w", err)
	}
	defer rows.Close()

	// Aggregate: map[(agent, task_type)] → {total, success}
	type bucket struct{ total, success int }
	agg := map[string]*bucket{}        // key = "agent\x00task_type"
	cache := map[string]string{}       // issueKey → taskType

	for rows.Next() {
		var r rawAffinityRow
		if err := rows.Scan(&r.agent, &r.jiraIssueKey, &r.exitCode); err != nil {
			return nil, fmt.Errorf("scan affinity row: %w", err)
		}
		taskType := resolveTaskType(r.jiraIssueKey, cache)
		key := r.agent + "\x00" + taskType
		b, ok := agg[key]
		if !ok {
			b = &bucket{}
			agg[key] = b
		}
		b.total++
		if r.exitCode == 0 {
			b.success++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("agent task affinity scan: %w", err)
	}

	out := make([]AffinityCell, 0, len(agg))
	for key, b := range agg {
		parts := strings.SplitN(key, "\x00", 2)
		if len(parts) != 2 {
			continue
		}
		var rate float64
		if b.total > 0 {
			rate = float64(b.success) / float64(b.total) * 100
		}
		out = append(out, AffinityCell{
			Agent:       parts[0],
			TaskType:    parts[1],
			Runs:        b.total,
			SuccessRate: roundTo1(rate),
		})
	}

	// Sort deterministically: agent asc, task_type asc.
	sortAffinityCells(out)
	return out, nil
}

// roundTo1 rounds a float64 to 1 decimal place.
func roundTo1(v float64) float64 {
	return float64(int(v*10+0.5)) / 10
}

// sortAffinityCells sorts by agent then task_type (lexicographic).
func sortAffinityCells(cells []AffinityCell) {
	for i := 1; i < len(cells); i++ {
		for j := i; j > 0; j-- {
			a, b := cells[j-1], cells[j]
			if a.Agent > b.Agent || (a.Agent == b.Agent && a.TaskType > b.TaskType) {
				cells[j-1], cells[j] = cells[j], cells[j-1]
			} else {
				break
			}
		}
	}
}
