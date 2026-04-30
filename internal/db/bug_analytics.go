package db

import (
	"fmt"
	"time"
)

// BugStatRow groups bug.filed events by a chosen dimension (agent or
// jira_issue_key). Returned by BugStats for the analytics command.
type BugStatRow struct {
	GroupKey  string    `json:"group_key"`
	BugCount  int       `json:"bug_count"`
	LastFiled time.Time `json:"last_filed_at"`
}

// BugStats counts distinct bug_key values from bug.filed events,
// grouped per the requested dimension. groupBy ∈ {"agent", "task"}:
//   - "agent" — uses runs.agent_name (for analytics)
//   - "task"  — uses runs.jira_issue_key (drill-down per ticket)
//
// sinceDays = 0 means all-time.
func (l *LocalDB) BugStats(groupBy string, sinceDays int) ([]BugStatRow, error) {
	var groupCol string
	switch groupBy {
	case "task":
		groupCol = "r.jira_issue_key"
	case "agent", "":
		groupCol = "r.agent_name"
	default:
		return nil, fmt.Errorf("invalid groupBy %q (expected agent|task)", groupBy)
	}

	since := ""
	args := []any{}
	if sinceDays > 0 {
		since = "AND e.ts >= ?"
		args = append(args, time.Now().AddDate(0, 0, -sinceDays).Format(time.RFC3339))
	}

	q := fmt.Sprintf(`
		SELECT %s AS group_key,
		       COUNT(DISTINCT json_extract(e.data, '$.bug_key')) AS bug_count,
		       MAX(e.ts) AS last_filed
		FROM events e
		JOIN runs r ON r.id = e.run_id
		WHERE e.event_type = 'bug.filed' %s
		GROUP BY group_key
		HAVING group_key IS NOT NULL AND group_key != ''
		ORDER BY bug_count DESC, last_filed DESC
	`, groupCol, since)

	rows, err := l.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("bug stats query: %w", err)
	}
	defer rows.Close()

	var out []BugStatRow
	for rows.Next() {
		var r BugStatRow
		var lastStr string
		if err := rows.Scan(&r.GroupKey, &r.BugCount, &lastStr); err != nil {
			return nil, err
		}
		if t, err := time.Parse(time.RFC3339, lastStr); err == nil {
			r.LastFiled = t
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
