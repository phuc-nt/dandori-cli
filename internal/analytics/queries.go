//go:build server

package analytics

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Querier struct {
	pool *pgxpool.Pool
}

func NewQuerier(pool *pgxpool.Pool) *Querier {
	return &Querier{pool: pool}
}

func (q *Querier) AgentStats(ctx context.Context, f Filters, groupBy string) ([]AgentStat, error) {
	query := `
		SELECT
			agent_name,
			COUNT(*) as run_count,
			COALESCE(ROUND(COUNT(*) FILTER (WHERE exit_code = 0)::numeric / NULLIF(COUNT(*), 0) * 100, 1), 0) as success_rate,
			COALESCE(SUM(cost_usd), 0) as total_cost,
			COALESCE(AVG(cost_usd), 0) as avg_cost,
			COALESCE(AVG(duration_sec), 0) as avg_duration
		FROM runs
		WHERE 1=1
	`
	args := []any{}
	argNum := 1

	query, args, argNum = appendFilters(query, args, argNum, f)
	query += " GROUP BY agent_name ORDER BY total_cost DESC"

	rows, err := q.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("agent stats query: %w", err)
	}
	defer rows.Close()

	var stats []AgentStat
	for rows.Next() {
		var s AgentStat
		if err := rows.Scan(&s.AgentName, &s.RunCount, &s.SuccessRate, &s.TotalCost, &s.AvgCost, &s.AvgDuration); err != nil {
			return nil, err
		}
		stats = append(stats, s)
	}
	return stats, nil
}

func (q *Querier) AgentStatsDaily(ctx context.Context, f Filters) ([]AgentStat, error) {
	query := `
		SELECT
			agent_name,
			date_trunc('day', started_at) as day,
			COUNT(*) as run_count,
			COALESCE(ROUND(COUNT(*) FILTER (WHERE exit_code = 0)::numeric / NULLIF(COUNT(*), 0) * 100, 1), 0) as success_rate,
			COALESCE(SUM(cost_usd), 0) as total_cost,
			COALESCE(AVG(cost_usd), 0) as avg_cost,
			COALESCE(AVG(duration_sec), 0) as avg_duration
		FROM runs
		WHERE 1=1
	`
	args := []any{}
	argNum := 1

	query, args, argNum = appendFilters(query, args, argNum, f)
	query += " GROUP BY agent_name, date_trunc('day', started_at) ORDER BY day DESC"

	rows, err := q.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("agent daily stats query: %w", err)
	}
	defer rows.Close()

	var stats []AgentStat
	for rows.Next() {
		var s AgentStat
		if err := rows.Scan(&s.AgentName, &s.Day, &s.RunCount, &s.SuccessRate, &s.TotalCost, &s.AvgCost, &s.AvgDuration); err != nil {
			return nil, err
		}
		stats = append(stats, s)
	}
	return stats, nil
}

func (q *Querier) AgentCompare(ctx context.Context, agents []string, f Filters) ([]AgentComparison, error) {
	query := `
		SELECT
			r.agent_name,
			COUNT(*) as run_count,
			COALESCE(ROUND(COUNT(*) FILTER (WHERE r.exit_code = 0)::numeric / NULLIF(COUNT(*), 0) * 100, 1), 0) as success_rate,
			COALESCE(SUM(r.cost_usd), 0) as total_cost,
			COALESCE(AVG(r.cost_usd), 0) as avg_cost,
			COALESCE(AVG(r.duration_sec), 0) as avg_duration,
			COALESCE(SUM(jt.story_points) FILTER (WHERE jt.status = 'Done'), 0) as points_completed
		FROM runs r
		LEFT JOIN jira_tasks jt ON r.jira_issue_key = jt.issue_key
		WHERE r.agent_name = ANY($1)
	`
	args := []any{agents}
	argNum := 2

	if f.SprintID != "" {
		query += fmt.Sprintf(" AND r.jira_sprint_id = $%d", argNum)
		args = append(args, f.SprintID)
		argNum++
	}
	if f.From != nil {
		query += fmt.Sprintf(" AND r.started_at >= $%d", argNum)
		args = append(args, *f.From)
		argNum++
	}
	if f.To != nil {
		query += fmt.Sprintf(" AND r.started_at <= $%d", argNum)
		args = append(args, *f.To)
	}

	query += " GROUP BY r.agent_name ORDER BY total_cost DESC"

	rows, err := q.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("agent compare query: %w", err)
	}
	defer rows.Close()

	var results []AgentComparison
	for rows.Next() {
		var c AgentComparison
		if err := rows.Scan(&c.AgentName, &c.RunCount, &c.SuccessRate, &c.TotalCost, &c.AvgCost, &c.AvgDuration, &c.PointsCompleted); err != nil {
			return nil, err
		}
		results = append(results, c)
	}
	return results, nil
}

func (q *Querier) TaskTypeStats(ctx context.Context, f Filters) ([]TaskTypeStat, error) {
	query := `
		SELECT
			jt.issue_type,
			COUNT(r.*) as run_count,
			COALESCE(ROUND(COUNT(r.*) FILTER (WHERE r.exit_code = 0)::numeric / NULLIF(COUNT(r.*), 0) * 100, 1), 0) as success_rate,
			COALESCE(AVG(r.cost_usd), 0) as avg_cost,
			COALESCE(AVG(r.duration_sec), 0) as avg_duration,
			COALESCE(SUM(r.cost_usd), 0) as total_cost
		FROM runs r
		JOIN jira_tasks jt ON r.jira_issue_key = jt.issue_key
		WHERE jt.issue_type IS NOT NULL
	`
	args := []any{}
	argNum := 1

	if f.Project != "" {
		query += fmt.Sprintf(" AND jt.issue_key LIKE $%d", argNum)
		args = append(args, f.Project+"-%")
		argNum++
	}
	if f.From != nil {
		query += fmt.Sprintf(" AND r.started_at >= $%d", argNum)
		args = append(args, *f.From)
		argNum++
	}
	if f.To != nil {
		query += fmt.Sprintf(" AND r.started_at <= $%d", argNum)
		args = append(args, *f.To)
	}

	query += " GROUP BY jt.issue_type ORDER BY total_cost DESC"

	rows, err := q.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("task type stats query: %w", err)
	}
	defer rows.Close()

	var stats []TaskTypeStat
	for rows.Next() {
		var s TaskTypeStat
		if err := rows.Scan(&s.IssueType, &s.RunCount, &s.SuccessRate, &s.AvgCost, &s.AvgDuration, &s.TotalCost); err != nil {
			return nil, err
		}
		stats = append(stats, s)
	}
	return stats, nil
}

func (q *Querier) CostBreakdown(ctx context.Context, f Filters, groupBy string) ([]CostGroup, error) {
	var groupCol string
	switch groupBy {
	case "agent":
		groupCol = "agent_name"
	case "sprint":
		groupCol = "jira_sprint_id"
	case "task":
		groupCol = "jira_issue_key"
	case "day":
		groupCol = "date_trunc('day', started_at)::text"
	case "week":
		groupCol = "date_trunc('week', started_at)::text"
	case "month":
		groupCol = "date_trunc('month', started_at)::text"
	default:
		groupCol = "agent_name"
	}

	query := fmt.Sprintf(`
		SELECT
			COALESCE(%s::text, 'unknown') as grp,
			COALESCE(SUM(cost_usd), 0) as cost,
			COUNT(*) as run_count,
			COALESCE(SUM(input_tokens + output_tokens), 0) as tokens
		FROM runs
		WHERE 1=1
	`, groupCol)

	args := []any{}
	argNum := 1

	query, args, argNum = appendFilters(query, args, argNum, f)
	query += fmt.Sprintf(" GROUP BY %s ORDER BY cost DESC", groupCol)

	rows, err := q.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("cost breakdown query: %w", err)
	}
	defer rows.Close()

	var groups []CostGroup
	for rows.Next() {
		var g CostGroup
		if err := rows.Scan(&g.Group, &g.Cost, &g.RunCount, &g.Tokens); err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	return groups, nil
}

func (q *Querier) CostTrend(ctx context.Context, period string, depth int) ([]TrendPoint, error) {
	var interval string
	switch period {
	case "day":
		interval = "1 day"
	case "week":
		interval = "1 week"
	case "month":
		interval = "1 month"
	default:
		interval = "1 week"
	}

	query := fmt.Sprintf(`
		WITH periods AS (
			SELECT generate_series(
				date_trunc('%s', now() - interval '%d %s'),
				date_trunc('%s', now()),
				interval '%s'
			) as period_start
		),
		costs AS (
			SELECT
				date_trunc('%s', started_at) as period,
				SUM(cost_usd) as cost
			FROM runs
			WHERE started_at >= now() - interval '%d %s'
			GROUP BY date_trunc('%s', started_at)
		)
		SELECT
			p.period_start,
			COALESCE(c.cost, 0) as cost,
			COALESCE(LAG(c.cost) OVER (ORDER BY p.period_start), 0) as prev_cost
		FROM periods p
		LEFT JOIN costs c ON c.period = p.period_start
		ORDER BY p.period_start
	`, period, depth, interval, period, interval, period, depth, interval, period)

	rows, err := q.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("cost trend query: %w", err)
	}
	defer rows.Close()

	var points []TrendPoint
	for rows.Next() {
		var p TrendPoint
		if err := rows.Scan(&p.PeriodStart, &p.Cost, &p.PrevCost); err != nil {
			return nil, err
		}
		if p.PrevCost > 0 {
			p.ChangePct = ((p.Cost - p.PrevCost) / p.PrevCost) * 100
		}
		points = append(points, p)
	}
	return points, nil
}

func (q *Querier) SprintSummary(ctx context.Context, sprintID string) (*SprintSummary, error) {
	query := `
		SELECT
			jt.sprint_id,
			COALESCE(jt.sprint_name, '') as sprint_name,
			COUNT(DISTINCT jt.issue_key) as task_count,
			COUNT(DISTINCT jt.issue_key) FILTER (WHERE jt.status = 'Done') as completed_count,
			COUNT(DISTINCT r.agent_name) as agent_count,
			COUNT(r.*) as total_runs,
			COALESCE(SUM(r.cost_usd), 0) as total_cost,
			COALESCE(SUM(jt.story_points) FILTER (WHERE jt.status = 'Done'), 0) as points_completed
		FROM jira_tasks jt
		LEFT JOIN runs r ON r.jira_issue_key = jt.issue_key
		WHERE jt.sprint_id = $1
		GROUP BY jt.sprint_id, jt.sprint_name
	`

	var s SprintSummary
	err := q.pool.QueryRow(ctx, query, sprintID).Scan(
		&s.SprintID, &s.SprintName, &s.TaskCount, &s.CompletedCount,
		&s.AgentCount, &s.TotalRuns, &s.TotalCost, &s.PointsCompleted,
	)
	if err != nil {
		return nil, fmt.Errorf("sprint summary query: %w", err)
	}

	if s.TotalCost > 0 {
		s.PointsPerDollar = s.PointsCompleted / s.TotalCost
	}

	return &s, nil
}

func (q *Querier) TaskCostBreakdown(ctx context.Context, issueKey string) (*TaskCost, error) {
	query := `
		SELECT
			id, agent_name, cost_usd, COALESCE(duration_sec, 0), status
		FROM runs
		WHERE jira_issue_key = $1
		ORDER BY started_at DESC
	`

	rows, err := q.pool.Query(ctx, query, issueKey)
	if err != nil {
		return nil, fmt.Errorf("task cost query: %w", err)
	}
	defer rows.Close()

	tc := &TaskCost{IssueKey: issueKey}
	for rows.Next() {
		var r TaskCostRun
		if err := rows.Scan(&r.RunID, &r.Agent, &r.Cost, &r.Duration, &r.Status); err != nil {
			return nil, err
		}
		tc.TotalCost += r.Cost
		tc.Runs = append(tc.Runs, r)
	}
	return tc, nil
}

func appendFilters(query string, args []any, argNum int, f Filters) (string, []any, int) {
	if f.Agent != "" {
		query += fmt.Sprintf(" AND agent_name = $%d", argNum)
		args = append(args, f.Agent)
		argNum++
	}
	if f.SprintID != "" {
		query += fmt.Sprintf(" AND jira_sprint_id = $%d", argNum)
		args = append(args, f.SprintID)
		argNum++
	}
	if f.From != nil {
		query += fmt.Sprintf(" AND started_at >= $%d", argNum)
		args = append(args, *f.From)
		argNum++
	}
	if f.To != nil {
		query += fmt.Sprintf(" AND started_at <= $%d", argNum)
		args = append(args, *f.To)
		argNum++
	}
	return query, args, argNum
}

func ParseTimeFilter(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t, err = time.Parse("2006-01-02", s)
		if err != nil {
			return nil
		}
	}
	return &t
}
