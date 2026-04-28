package db

import (
	"database/sql"
	"fmt"
	"time"
)

// LocalAgentStat represents local analytics for an agent
type LocalAgentStat struct {
	AgentName   string
	RunCount    int
	SuccessRate float64
	TotalCost   float64
	AvgCost     float64
	AvgDuration float64
	TotalTokens int
}

// LocalCostGroup represents cost grouped by dimension
type LocalCostGroup struct {
	Group    string
	Cost     float64
	RunCount int
	Tokens   int
}

// LocalRunSummary represents a run summary
type LocalRunSummary struct {
	ID           string
	JiraIssueKey string
	AgentName    string
	Status       string
	Duration     float64
	Cost         float64
	Tokens       int
	StartedAt    time.Time
}

// GetAgentStats returns analytics for all agents from local SQLite
func (l *LocalDB) GetAgentStats() ([]LocalAgentStat, error) {
	query := `
		SELECT
			COALESCE(agent_name, '') as agent_name,
			COUNT(*) as run_count,
			ROUND(CAST(SUM(CASE WHEN exit_code = 0 THEN 1 ELSE 0 END) AS REAL) / COUNT(*) * 100, 1) as success_rate,
			COALESCE(SUM(cost_usd), 0) as total_cost,
			COALESCE(AVG(cost_usd), 0) as avg_cost,
			COALESCE(AVG(duration_sec), 0) as avg_duration,
			COALESCE(SUM(input_tokens + output_tokens), 0) as total_tokens
		FROM runs
		GROUP BY COALESCE(agent_name, '')
		ORDER BY total_cost DESC
	`

	rows, err := l.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("query agent stats: %w", err)
	}
	defer rows.Close()

	var stats []LocalAgentStat
	for rows.Next() {
		var s LocalAgentStat
		if err := rows.Scan(&s.AgentName, &s.RunCount, &s.SuccessRate,
			&s.TotalCost, &s.AvgCost, &s.AvgDuration, &s.TotalTokens); err != nil {
			return nil, err
		}
		stats = append(stats, s)
	}
	return stats, nil
}

// GetCostByAgent returns cost breakdown by agent
func (l *LocalDB) GetCostByAgent() ([]LocalCostGroup, error) {
	return l.getCostBy("agent_name")
}

// GetCostByTask returns cost breakdown by Jira task
func (l *LocalDB) GetCostByTask() ([]LocalCostGroup, error) {
	return l.getCostBy("jira_issue_key")
}

// GetCostByDay returns cost breakdown by day
func (l *LocalDB) GetCostByDay() ([]LocalCostGroup, error) {
	query := `
		SELECT
			date(started_at) as day,
			COALESCE(SUM(cost_usd), 0) as cost,
			COUNT(*) as run_count,
			COALESCE(SUM(input_tokens + output_tokens), 0) as tokens
		FROM runs
		WHERE started_at IS NOT NULL
		GROUP BY date(started_at)
		ORDER BY day DESC
		LIMIT 30
	`
	return l.queryCostGroups(query)
}

func (l *LocalDB) getCostBy(column string) ([]LocalCostGroup, error) {
	query := fmt.Sprintf(`
		SELECT
			COALESCE(%s, 'unknown') as grp,
			COALESCE(SUM(cost_usd), 0) as cost,
			COUNT(*) as run_count,
			COALESCE(SUM(input_tokens + output_tokens), 0) as tokens
		FROM runs
		GROUP BY %s
		ORDER BY cost DESC
	`, column, column)
	return l.queryCostGroups(query)
}

func (l *LocalDB) queryCostGroups(query string) ([]LocalCostGroup, error) {
	rows, err := l.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("query cost: %w", err)
	}
	defer rows.Close()

	var groups []LocalCostGroup
	for rows.Next() {
		var g LocalCostGroup
		if err := rows.Scan(&g.Group, &g.Cost, &g.RunCount, &g.Tokens); err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	return groups, nil
}

// GetRecentRuns returns recent runs
func (l *LocalDB) GetRecentRuns(limit int) ([]LocalRunSummary, error) {
	if limit <= 0 {
		limit = 20
	}

	query := `
		SELECT
			id,
			COALESCE(jira_issue_key, '') as jira_issue_key,
			COALESCE(agent_name, '') as agent_name,
			status,
			COALESCE(duration_sec, 0) as duration,
			COALESCE(cost_usd, 0) as cost,
			COALESCE(input_tokens + output_tokens, 0) as tokens,
			started_at
		FROM runs
		ORDER BY started_at DESC
		LIMIT ?
	`

	rows, err := l.db.Query(query, limit)
	if err != nil {
		return nil, fmt.Errorf("query recent runs: %w", err)
	}
	defer rows.Close()

	var runs []LocalRunSummary
	for rows.Next() {
		var r LocalRunSummary
		var startedAt string
		if err := rows.Scan(&r.ID, &r.JiraIssueKey, &r.AgentName, &r.Status,
			&r.Duration, &r.Cost, &r.Tokens, &startedAt); err != nil {
			return nil, err
		}
		r.StartedAt, _ = time.Parse(time.RFC3339, startedAt)
		runs = append(runs, r)
	}
	return runs, nil
}

// GetTotalStats returns overall statistics
func (l *LocalDB) GetTotalStats() (runCount int, totalCost float64, totalTokens int, err error) {
	query := `
		SELECT
			COUNT(*) as run_count,
			COALESCE(SUM(cost_usd), 0) as total_cost,
			COALESCE(SUM(input_tokens + output_tokens), 0) as total_tokens
		FROM runs
	`
	err = l.db.QueryRow(query).Scan(&runCount, &totalCost, &totalTokens)
	if err == sql.ErrNoRows {
		return 0, 0, 0, nil
	}
	return
}

// SprintSummary represents sprint-level statistics
type SprintSummary struct {
	SprintID    string
	TaskCount   int
	RunCount    int
	SuccessRate float64
	TotalCost   float64
	TotalTokens int
	Agents      map[string]float64
}

// GetCostBySprint returns cost breakdown by sprint
func (l *LocalDB) GetCostBySprint() ([]LocalCostGroup, error) {
	query := `
		SELECT
			COALESCE(jira_sprint_id, 'no-sprint') as grp,
			COALESCE(SUM(cost_usd), 0) as cost,
			COUNT(*) as run_count,
			COALESCE(SUM(input_tokens + output_tokens), 0) as tokens
		FROM runs
		GROUP BY jira_sprint_id
		ORDER BY cost DESC
	`
	return l.queryCostGroups(query)
}

// SyncableRun represents a run that needs to be synced to Jira
type SyncableRun struct {
	ID           string
	JiraIssueKey string
	AgentName    string
	Status       string
	Duration     float64
	Cost         float64
	Tokens       int
}

// GetRunsToSync returns completed runs that haven't been synced to Jira
func (l *LocalDB) GetRunsToSync(taskFilter string) ([]SyncableRun, error) {
	query := `
		SELECT id, COALESCE(jira_issue_key, ''), COALESCE(agent_name, ''), status,
			COALESCE(duration_sec, 0), COALESCE(cost_usd, 0),
			COALESCE(input_tokens + output_tokens, 0)
		FROM runs
		WHERE status IN ('done', 'failed')
		AND jira_issue_key IS NOT NULL
		AND jira_issue_key != ''
		AND COALESCE(synced, 0) = 0
	`
	args := []any{}

	if taskFilter != "" {
		query += " AND jira_issue_key = ?"
		args = append(args, taskFilter)
	}

	query += " ORDER BY started_at DESC"

	rows, err := l.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query runs to sync: %w", err)
	}
	defer rows.Close()

	var runs []SyncableRun
	for rows.Next() {
		var r SyncableRun
		if err := rows.Scan(&r.ID, &r.JiraIssueKey, &r.AgentName, &r.Status,
			&r.Duration, &r.Cost, &r.Tokens); err != nil {
			return nil, err
		}
		runs = append(runs, r)
	}
	return runs, nil
}

// MarkRunSynced marks a run as synced to Jira
func (l *LocalDB) MarkRunSynced(runID string) error {
	_, err := l.db.Exec("UPDATE runs SET synced = 1 WHERE id = ?", runID)
	return err
}

// GetSprintSummary returns detailed stats for a specific sprint
func (l *LocalDB) GetSprintSummary(sprintID string) (*SprintSummary, error) {
	summary := &SprintSummary{
		SprintID: sprintID,
		Agents:   make(map[string]float64),
	}

	// Get overall sprint stats
	query := `
		SELECT
			COUNT(DISTINCT jira_issue_key) as task_count,
			COUNT(*) as run_count,
			COALESCE(ROUND(CAST(SUM(CASE WHEN exit_code = 0 THEN 1 ELSE 0 END) AS REAL) / NULLIF(COUNT(*), 0) * 100, 1), 0) as success_rate,
			COALESCE(SUM(cost_usd), 0) as total_cost,
			COALESCE(SUM(input_tokens + output_tokens), 0) as total_tokens
		FROM runs
		WHERE jira_sprint_id = ?
	`
	err := l.db.QueryRow(query, sprintID).Scan(
		&summary.TaskCount, &summary.RunCount, &summary.SuccessRate,
		&summary.TotalCost, &summary.TotalTokens)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("query sprint stats: %w", err)
	}

	// Get cost by agent for this sprint
	agentQuery := `
		SELECT agent_name, COALESCE(SUM(cost_usd), 0) as cost
		FROM runs
		WHERE jira_sprint_id = ?
		GROUP BY agent_name
		ORDER BY cost DESC
	`
	rows, err := l.db.Query(agentQuery, sprintID)
	if err != nil {
		return nil, fmt.Errorf("query agent costs: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var agent string
		var cost float64
		if err := rows.Scan(&agent, &cost); err != nil {
			return nil, err
		}
		summary.Agents[agent] = cost
	}

	return summary, nil
}
