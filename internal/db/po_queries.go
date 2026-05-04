// Package db — po_queries.go: query helpers for Phase 02 PO View endpoints.
//
// Covers:
//   - sprint listing (distinct jira_sprint_id seen in runs)
//   - sprint burndown (daily run/cost progression in a sprint window)
//   - cost by department (group by department + day)
//   - cost projection input (14d daily cost series for linear regression)
//   - attribution timeline (authorship %, retention %, intervention rate over time)
//   - task lifecycle (all runs of a single PBI, ordered)
//   - lead time distribution (run duration buckets)
//
// All queries respect the common PO filter (from/to/sprint/dept/project/engineer).
package db

import (
	"fmt"
	"strings"
	"time"
)

// POFilter is the common filter shape for PO endpoints.
// Empty string fields are treated as "no restriction".
// Project filter matches the Jira issue key prefix (e.g., "CLITEST1" matches "CLITEST1-42").
type POFilter struct {
	From     time.Time
	To       time.Time
	Sprint   string
	Dept     string
	Project  string
	Engineer string
}

// applyFilter appends WHERE clauses + args for the filter onto an existing
// query that already starts with `WHERE 1=1`. Returns the augmented query
// + args slice.
func (f POFilter) applyFilter(q string, args []any) (string, []any) {
	if !f.From.IsZero() {
		q += " AND started_at >= ?"
		args = append(args, f.From.Format(time.RFC3339))
	}
	if !f.To.IsZero() {
		q += " AND started_at < ?"
		args = append(args, f.To.Format(time.RFC3339))
	}
	if f.Sprint != "" {
		q += " AND jira_sprint_id = ?"
		args = append(args, f.Sprint)
	}
	if f.Dept != "" {
		q += " AND department = ?"
		args = append(args, f.Dept)
	}
	if f.Project != "" {
		q += " AND jira_issue_key LIKE ?"
		args = append(args, f.Project+"-%")
	}
	if f.Engineer != "" {
		q += " AND engineer_name = ?"
		args = append(args, f.Engineer)
	}
	return q, args
}

// SprintInfo is one row in /api/sprints output.
type SprintInfo struct {
	ID          string  `json:"id"`
	ProjectKey  string  `json:"project_key"`
	RunCount    int     `json:"run_count"`
	StartedAt   string  `json:"started_at"`   // earliest run in this sprint
	EndedAt     string  `json:"ended_at"`     // latest run in this sprint
	TotalCost   float64 `json:"total_cost"`
	IssueCount  int     `json:"issue_count"`  // distinct jira_issue_key
}

// ListSprints returns one entry per distinct jira_sprint_id, oldest run last.
// Project key is derived from the most common issue prefix in the sprint.
func (l *LocalDB) ListSprints() ([]SprintInfo, error) {
	rows, err := l.Query(`
		SELECT
			jira_sprint_id,
			COUNT(*) AS run_count,
			MIN(started_at) AS first_run,
			MAX(started_at) AS last_run,
			COALESCE(SUM(cost_usd), 0) AS total_cost,
			COUNT(DISTINCT jira_issue_key) AS issue_count
		FROM runs
		WHERE jira_sprint_id IS NOT NULL AND jira_sprint_id != ''
		GROUP BY jira_sprint_id
		ORDER BY MAX(started_at) DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []SprintInfo{}
	for rows.Next() {
		var s SprintInfo
		if err := rows.Scan(&s.ID, &s.RunCount, &s.StartedAt, &s.EndedAt, &s.TotalCost, &s.IssueCount); err != nil {
			return nil, err
		}
		// Best-effort project key: split on first '-' of sprint id (e.g. "CLITEST1-S1" → "CLITEST1").
		if idx := strings.LastIndex(s.ID, "-"); idx > 0 {
			s.ProjectKey = s.ID[:idx]
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SprintBurndownDay is one daily progress row.
type SprintBurndownDay struct {
	Day        string  `json:"day"`
	Runs       int     `json:"runs"`
	IssuesDone int     `json:"issues_done"` // distinct issues with at least 1 run that day
	Cost       float64 `json:"cost"`
}

// SprintBurndown returns daily progress for the named sprint.
func (l *LocalDB) SprintBurndown(sprintID string) ([]SprintBurndownDay, error) {
	if sprintID == "" {
		return nil, fmt.Errorf("sprint id required")
	}
	rows, err := l.Query(`
		SELECT
			date(started_at) AS day,
			COUNT(*) AS runs,
			COUNT(DISTINCT jira_issue_key) AS issues_done,
			COALESCE(SUM(cost_usd), 0) AS cost
		FROM runs
		WHERE jira_sprint_id = ?
		GROUP BY date(started_at)
		ORDER BY day ASC
	`, sprintID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []SprintBurndownDay{}
	for rows.Next() {
		var d SprintBurndownDay
		if err := rows.Scan(&d.Day, &d.Runs, &d.IssuesDone, &d.Cost); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// SprintRunRow is a single run scoped to a sprint, used by the Sprint Board.
type SprintRunRow struct {
	ID           string  `json:"id"`
	JiraIssueKey string  `json:"jira_issue_key"`
	JiraSprintID string  `json:"jira_sprint_id"`
	AgentName    string  `json:"agent_name"`
	Status       string  `json:"status"`
	CostUSD      float64 `json:"cost_usd"`
}

// SprintRuns returns runs belonging to the given sprint, latest first.
func (l *LocalDB) SprintRuns(sprintID string) ([]SprintRunRow, error) {
	if sprintID == "" {
		return nil, fmt.Errorf("sprint id required")
	}
	rows, err := l.Query(`
		SELECT
			id,
			COALESCE(jira_issue_key, '') as jira_issue_key,
			COALESCE(jira_sprint_id, '') as jira_sprint_id,
			COALESCE(agent_name, '') as agent_name,
			COALESCE(status, '') as status,
			COALESCE(cost_usd, 0) as cost_usd
		FROM runs
		WHERE jira_sprint_id = ?
		ORDER BY started_at DESC
	`, sprintID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []SprintRunRow{}
	for rows.Next() {
		var r SprintRunRow
		if err := rows.Scan(&r.ID, &r.JiraIssueKey, &r.JiraSprintID, &r.AgentName, &r.Status, &r.CostUSD); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CostByDeptDay is one (department, day) bucket.
type CostByDeptDay struct {
	Day        string  `json:"day"`
	Department string  `json:"department"`
	Cost       float64 `json:"cost"`
	Runs       int     `json:"runs"`
}

// CostByDepartment returns a daily series grouped by department, oldest day first.
func (l *LocalDB) CostByDepartment(f POFilter) ([]CostByDeptDay, error) {
	q := `
		SELECT
			date(started_at) AS day,
			COALESCE(department, 'unassigned') AS dept,
			COALESCE(SUM(cost_usd), 0) AS cost,
			COUNT(*) AS runs
		FROM runs
		WHERE 1=1`
	q, args := f.applyFilter(q, []any{})
	q += `
		GROUP BY day, dept
		ORDER BY day ASC, dept ASC`

	rows, err := l.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []CostByDeptDay{}
	for rows.Next() {
		var d CostByDeptDay
		if err := rows.Scan(&d.Day, &d.Department, &d.Cost, &d.Runs); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// DailyCost is one (day, total cost) bucket used for projection regression.
type DailyCost struct {
	Day  string  `json:"day"`
	Cost float64 `json:"cost"`
}

// DailyCostSeries returns the last `days` calendar days of total cost,
// padded with zero rows so the consumer can fit a regression directly.
func (l *LocalDB) DailyCostSeries(days int) ([]DailyCost, error) {
	if days <= 0 {
		days = 14
	}
	now := time.Now().UTC()
	from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).
		AddDate(0, 0, -(days - 1))

	rows, err := l.Query(`
		SELECT date(started_at) AS day, COALESCE(SUM(cost_usd), 0) AS cost
		FROM runs
		WHERE started_at >= ?
		GROUP BY date(started_at)
	`, from.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byDay := map[string]float64{}
	for rows.Next() {
		var d, _ = "", 0.0
		var cost float64
		if err := rows.Scan(&d, &cost); err != nil {
			return nil, err
		}
		byDay[d] = cost
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]DailyCost, 0, days)
	for i := 0; i < days; i++ {
		key := from.AddDate(0, 0, i).Format("2006-01-02")
		out = append(out, DailyCost{Day: key, Cost: byDay[key]})
	}
	return out, nil
}

// AttributionTimelinePoint represents a weekly aggregate of authorship,
// retention, and intervention rates.
type AttributionTimelinePoint struct {
	WeekStart       string  `json:"week_start"`
	AuthorshipAgent float64 `json:"authorship_agent"` // 0..1 fraction of lines attributed to agent
	AuthorshipHuman float64 `json:"authorship_human"`
	InterventionAvg float64 `json:"intervention_rate_avg"`
	TaskCount       int     `json:"task_count"`
}

// AttributionTimeline returns weekly buckets of attribution data sourced
// from task_attribution table joined to runs for the filter window.
func (l *LocalDB) AttributionTimeline(f POFilter) ([]AttributionTimelinePoint, error) {
	q := `
		SELECT
			date(jira_done_at, 'weekday 0', '-6 days') AS week_start,
			COALESCE(SUM(lines_attributed_agent), 0) AS agent_lines,
			COALESCE(SUM(lines_attributed_human), 0) AS human_lines,
			COALESCE(AVG(intervention_rate), 0) AS intervention_avg,
			COUNT(*) AS task_count
		FROM task_attribution
		WHERE 1=1`
	args := []any{}
	if !f.From.IsZero() {
		q += " AND jira_done_at >= ?"
		args = append(args, f.From.Format(time.RFC3339))
	}
	if !f.To.IsZero() {
		q += " AND jira_done_at < ?"
		args = append(args, f.To.Format(time.RFC3339))
	}
	if f.Project != "" {
		q += " AND jira_issue_key LIKE ?"
		args = append(args, f.Project+"-%")
	}
	q += `
		GROUP BY week_start
		ORDER BY week_start ASC`

	rows, err := l.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []AttributionTimelinePoint{}
	for rows.Next() {
		var p AttributionTimelinePoint
		var agentLines, humanLines int
		if err := rows.Scan(&p.WeekStart, &agentLines, &humanLines, &p.InterventionAvg, &p.TaskCount); err != nil {
			return nil, err
		}
		total := agentLines + humanLines
		if total > 0 {
			p.AuthorshipAgent = float64(agentLines) / float64(total)
			p.AuthorshipHuman = float64(humanLines) / float64(total)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// LifecycleRun is one run in a PBI's lifecycle.
type LifecycleRun struct {
	ID           string  `json:"id"`
	AgentName    string  `json:"agent_name"`
	EngineerName string  `json:"engineer_name"`
	StartedAt    string  `json:"started_at"`
	EndedAt      string  `json:"ended_at"`
	DurationSec  float64 `json:"duration_sec"`
	Status       string  `json:"status"`
	CostUSD      float64 `json:"cost_usd"`
	QualityScore float64 `json:"quality_score"`
}

// TaskLifecycle returns all runs of a given PBI ordered by started_at.
func (l *LocalDB) TaskLifecycle(issueKey string) ([]LifecycleRun, error) {
	if issueKey == "" {
		return nil, fmt.Errorf("issue key required")
	}
	rows, err := l.Query(`
		SELECT
			r.id,
			COALESCE(r.agent_name, '') AS agent_name,
			COALESCE(r.engineer_name, '') AS engineer_name,
			r.started_at,
			COALESCE(r.ended_at, '') AS ended_at,
			COALESCE(r.duration_sec, 0) AS duration_sec,
			r.status,
			COALESCE(r.cost_usd, 0) AS cost_usd,
			COALESCE(q.quality_score, 0) AS quality_score
		FROM runs r
		LEFT JOIN quality_metrics q ON q.run_id = r.id
		WHERE r.jira_issue_key = ?
		ORDER BY r.started_at ASC
	`, issueKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []LifecycleRun{}
	for rows.Next() {
		var lr LifecycleRun
		if err := rows.Scan(&lr.ID, &lr.AgentName, &lr.EngineerName, &lr.StartedAt, &lr.EndedAt,
			&lr.DurationSec, &lr.Status, &lr.CostUSD, &lr.QualityScore); err != nil {
			return nil, err
		}
		out = append(out, lr)
	}
	return out, rows.Err()
}

// LeadTimeBucket represents one duration bucket in the lead-time histogram.
type LeadTimeBucket struct {
	Label string `json:"label"`
	Min   int    `json:"min_sec"`
	Max   int    `json:"max_sec"` // -1 for open-ended
	Count int    `json:"count"`
}

// LeadTimeDistribution returns 4 standard buckets: 0-1h, 1-4h, 4-12h, 12h+.
func (l *LocalDB) LeadTimeDistribution(f POFilter) ([]LeadTimeBucket, error) {
	buckets := []LeadTimeBucket{
		{Label: "0-1h", Min: 0, Max: 3600},
		{Label: "1-4h", Min: 3600, Max: 14400},
		{Label: "4-12h", Min: 14400, Max: 43200},
		{Label: "12h+", Min: 43200, Max: -1},
	}

	q := `SELECT COALESCE(duration_sec, 0) FROM runs WHERE 1=1`
	q, args := f.applyFilter(q, []any{})

	rows, err := l.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var dur float64
		if err := rows.Scan(&dur); err != nil {
			return nil, err
		}
		secs := int(dur)
		for i := range buckets {
			b := &buckets[i]
			if secs >= b.Min && (b.Max == -1 || secs < b.Max) {
				b.Count++
				break
			}
		}
	}
	return buckets, rows.Err()
}
