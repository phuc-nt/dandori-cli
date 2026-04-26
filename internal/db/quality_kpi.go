package db

import (
	"fmt"
	"time"
)

// RegressionRow holds regression rate stats per group dimension.
type RegressionRow struct {
	GroupKey       string  `json:"group_key"`
	TotalTasks     int     `json:"total_tasks"`
	RegressedTasks int     `json:"regressed_tasks"`
	RegressionPct  float64 `json:"regression_pct"`
}

// BugRateRow holds bug rate stats per group dimension.
type BugRateRow struct {
	GroupKey   string  `json:"group_key"`
	Runs       int     `json:"runs"`
	Bugs       int     `json:"bugs"`
	BugsPerRun float64 `json:"bugs_per_run"`
}

// TaskCostRow holds quality-adjusted cost per Jira task.
// GroupKey reflects the requested dimension (agent/engineer/sprint).
type TaskCostRow struct {
	IssueKey       string  `json:"issue_key"`
	GroupKey       string  `json:"group_key"`
	TotalCostUSD   float64 `json:"total_cost_usd"`
	RunCount       int     `json:"run_count"`
	IterationCount int     `json:"iteration_count"`
	BugCount       int     `json:"bug_count"`
	IsClean        bool    `json:"is_clean"`
}

// resolveGroupCol maps a groupBy string to the runs column name.
// Mirrors IterationStats in event_analytics.go:144-154 — single place to add dims.
func resolveGroupCol(groupBy string) (string, error) {
	switch groupBy {
	case "engineer":
		return "engineer_name", nil
	case "sprint":
		return "jira_sprint_id", nil
	case "agent", "":
		return "agent_name", nil
	default:
		return "", fmt.Errorf("invalid group: %q (want agent|engineer|sprint)", groupBy)
	}
}

// RegressionRate returns regression rate per group dimension.
// A task is "regressed" when it has at least one task.iteration.start event.
// sinceDays=0 means all-time.
func (l *LocalDB) RegressionRate(groupBy string, sinceDays int) ([]RegressionRow, error) {
	groupCol, err := resolveGroupCol(groupBy)
	if err != nil {
		return nil, err
	}

	sinceClause := ""
	args := []any{}
	if sinceDays > 0 {
		sinceClause = "AND r.started_at >= ?"
		args = append(args, time.Now().AddDate(0, 0, -sinceDays).Format(time.RFC3339))
	}

	q := fmt.Sprintf(`
		WITH per_task AS (
			SELECT COALESCE(r.%s, '') AS group_key,
			       r.jira_issue_key,
			       1 + (SELECT COUNT(*) FROM events e
			            JOIN runs r2 ON e.run_id = r2.id
			            WHERE r2.jira_issue_key = r.jira_issue_key
			              AND e.event_type = 'task.iteration.start') AS rounds
			FROM runs r
			WHERE r.jira_issue_key IS NOT NULL AND r.jira_issue_key != ''
			  %s
			GROUP BY r.jira_issue_key
		)
		SELECT group_key,
		       COUNT(*) AS total_tasks,
		       SUM(CASE WHEN rounds > 1 THEN 1 ELSE 0 END) AS regressed_tasks,
		       ROUND(100.0 * SUM(CASE WHEN rounds > 1 THEN 1 ELSE 0 END) / COUNT(*), 1) AS regression_pct
		FROM per_task
		GROUP BY group_key
		ORDER BY regression_pct DESC, total_tasks DESC
	`, groupCol, sinceClause)

	rows, err := l.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("regression rate query: %w", err)
	}
	defer rows.Close()

	var out []RegressionRow
	for rows.Next() {
		var r RegressionRow
		if err := rows.Scan(&r.GroupKey, &r.TotalTasks, &r.RegressedTasks, &r.RegressionPct); err != nil {
			return nil, fmt.Errorf("scan regression row: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// BugRate returns bug rate (bugs per run) per group dimension.
// Bugs are DISTINCT bug_key values from bug.filed events.
// sinceDays=0 means all-time.
func (l *LocalDB) BugRate(groupBy string, sinceDays int) ([]BugRateRow, error) {
	groupCol, err := resolveGroupCol(groupBy)
	if err != nil {
		return nil, err
	}

	sinceClause := ""
	args := []any{}
	if sinceDays > 0 {
		sinceClause = "WHERE r.started_at >= ?"
		args = append(args, time.Now().AddDate(0, 0, -sinceDays).Format(time.RFC3339))
	}

	q := fmt.Sprintf(`
		SELECT COALESCE(r.%s, '') AS group_key,
		       COUNT(DISTINCT r.id) AS runs,
		       COUNT(DISTINCT json_extract(e.data, '$.bug_key')) AS bugs
		FROM runs r
		LEFT JOIN events e ON e.run_id = r.id AND e.event_type = 'bug.filed'
		%s
		GROUP BY group_key
		ORDER BY (CAST(bugs AS REAL) / NULLIF(runs, 0)) DESC
	`, groupCol, sinceClause)

	rows, err := l.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("bug rate query: %w", err)
	}
	defer rows.Close()

	var out []BugRateRow
	for rows.Next() {
		var r BugRateRow
		if err := rows.Scan(&r.GroupKey, &r.Runs, &r.Bugs); err != nil {
			return nil, fmt.Errorf("scan bug rate row: %w", err)
		}
		if r.Runs > 0 {
			r.BugsPerRun = float64(r.Bugs) / float64(r.Runs)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// QualityAdjustedCost returns per-task cost with iteration and bug counts.
// top caps the number of rows (0 or negative defaults to 50).
// groupBy only changes which column populates GroupKey (owner label).
// sinceDays=0 means all-time.
func (l *LocalDB) QualityAdjustedCost(groupBy string, sinceDays int, top int) ([]TaskCostRow, error) {
	groupCol, err := resolveGroupCol(groupBy)
	if err != nil {
		return nil, err
	}
	if top <= 0 {
		top = 50
	}

	sinceClause := ""
	args := []any{}
	if sinceDays > 0 {
		sinceClause = "AND r.started_at >= ?"
		args = append(args, time.Now().AddDate(0, 0, -sinceDays).Format(time.RFC3339))
	}
	args = append(args, top)

	q := fmt.Sprintf(`
		SELECT r.jira_issue_key,
		       COALESCE(MAX(r.%s), '') AS group_key,
		       SUM(r.cost_usd) AS total_cost_usd,
		       COUNT(DISTINCT r.id) AS run_count,
		       (SELECT COUNT(*) FROM events e
		        JOIN runs r2 ON e.run_id = r2.id
		        WHERE r2.jira_issue_key = r.jira_issue_key
		          AND e.event_type = 'task.iteration.start') AS iteration_count,
		       (SELECT COUNT(DISTINCT json_extract(e.data, '$.bug_key')) FROM events e
		        JOIN runs r2 ON e.run_id = r2.id
		        WHERE r2.jira_issue_key = r.jira_issue_key
		          AND e.event_type = 'bug.filed') AS bug_count
		FROM runs r
		WHERE r.jira_issue_key IS NOT NULL AND r.jira_issue_key != ''
		  %s
		GROUP BY r.jira_issue_key
		ORDER BY total_cost_usd DESC
		LIMIT ?
	`, groupCol, sinceClause)

	rows, err := l.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("quality adjusted cost query: %w", err)
	}
	defer rows.Close()

	var out []TaskCostRow
	for rows.Next() {
		var r TaskCostRow
		if err := rows.Scan(&r.IssueKey, &r.GroupKey, &r.TotalCostUSD, &r.RunCount, &r.IterationCount, &r.BugCount); err != nil {
			return nil, fmt.Errorf("scan cost row: %w", err)
		}
		r.IsClean = r.IterationCount == 0 && r.BugCount == 0
		out = append(out, r)
	}
	return out, rows.Err()
}
