package db

import "fmt"

// MixRow represents one row of the human+agent mix leaderboard
// (engineer × agent). Bob human-only has Agent == "".
type MixRow struct {
	Engineer    string  `json:"engineer"`
	Agent       string  `json:"agent"`
	RunCount    int     `json:"run_count"`
	TotalCost   float64 `json:"total_cost"`
	AvgCost     float64 `json:"avg_cost"`
	AvgDuration float64 `json:"avg_duration_sec"`
}

// GetCostByEngineer groups runs by engineer_name (human owner).
// NULL engineer → "(unassigned)".
func (l *LocalDB) GetCostByEngineer() ([]LocalCostGroup, error) {
	return l.queryCostGroups(`
		SELECT
			COALESCE(engineer_name, '(unassigned)') as grp,
			COALESCE(SUM(cost_usd), 0) as cost,
			COUNT(*) as run_count,
			COALESCE(SUM(input_tokens + output_tokens), 0) as tokens
		FROM runs
		GROUP BY COALESCE(engineer_name, '(unassigned)')
		ORDER BY cost DESC, run_count DESC
	`)
}

// GetCostByDepartment groups runs by department config label.
func (l *LocalDB) GetCostByDepartment() ([]LocalCostGroup, error) {
	return l.queryCostGroups(`
		SELECT
			COALESCE(department, '(unassigned)') as grp,
			COALESCE(SUM(cost_usd), 0) as cost,
			COUNT(*) as run_count,
			COALESCE(SUM(input_tokens + output_tokens), 0) as tokens
		FROM runs
		GROUP BY COALESCE(department, '(unassigned)')
		ORDER BY cost DESC, run_count DESC
	`)
}

// GetMixLeaderboard returns human+agent pair leaderboard for last `sinceDays`.
// Each unique (engineer, agent) is one row. Pure human rows have agent="".
// Ordered by run_count DESC to match the blog's "tasks closed" column.
func (l *LocalDB) GetMixLeaderboard(sinceDays int) ([]MixRow, error) {
	if sinceDays <= 0 {
		sinceDays = 30
	}
	query := fmt.Sprintf(`
		SELECT
			COALESCE(engineer_name, '(unassigned)') as engineer,
			COALESCE(agent_name, '') as agent,
			COUNT(*) as run_count,
			COALESCE(SUM(cost_usd), 0) as total_cost,
			COALESCE(AVG(cost_usd), 0) as avg_cost,
			COALESCE(AVG(duration_sec), 0) as avg_duration
		FROM runs
		WHERE started_at >= datetime('now', '-%d days')
		GROUP BY COALESCE(engineer_name, '(unassigned)'), COALESCE(agent_name, '')
		ORDER BY run_count DESC, total_cost DESC
	`, sinceDays)

	rows, err := l.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("mix leaderboard: %w", err)
	}
	defer rows.Close()

	var out []MixRow
	for rows.Next() {
		var r MixRow
		if err := rows.Scan(&r.Engineer, &r.Agent, &r.RunCount, &r.TotalCost, &r.AvgCost, &r.AvgDuration); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
