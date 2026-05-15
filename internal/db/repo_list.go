// Package db — repo_list.go: lists repos with merged PRs in window.
// Powers the dashboard's per-repo dropdown (v0.14+). Hidden when only
// one repo has merges — solo-engineer DBs see no UI noise.
package db

import "fmt"

// RepoSummary is one row of the multi-repo breakdown — repo full name +
// how many PRs merged in the lookback window.
type RepoSummary struct {
	Repo        string `json:"repo"`
	MergedCount int    `json:"merged_count"`
}

// ListReposWithMergedPRs returns repos that have at least one merged PR
// in [now − days, now), ordered by count desc (most active first).
// Empty result is valid — caller renders the dropdown only when len ≥ 2.
func (l *LocalDB) ListReposWithMergedPRs(days int) ([]RepoSummary, error) {
	if days <= 0 {
		days = 28
	}
	rows, err := l.Query(`
		SELECT repo, COUNT(*) AS merged_count
		FROM pr_events
		WHERE merged_at IS NOT NULL
		  AND merged_at >= datetime('now', ?)
		GROUP BY repo
		ORDER BY merged_count DESC, repo ASC
	`, fmt.Sprintf("-%d days", days))
	if err != nil {
		return nil, fmt.Errorf("list repos: %w", err)
	}
	defer rows.Close()

	var out []RepoSummary
	for rows.Next() {
		var r RepoSummary
		if err := rows.Scan(&r.Repo, &r.MergedCount); err != nil {
			return nil, fmt.Errorf("scan repo summary: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
