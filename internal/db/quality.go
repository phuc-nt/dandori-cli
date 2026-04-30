package db

import (
	"database/sql"
	"fmt"

	"github.com/phuc-nt/dandori-cli/internal/quality"
)

// InsertQualityMetrics stores quality metrics for a run
func (l *LocalDB) InsertQualityMetrics(m *quality.Metrics) error {
	_, err := l.Exec(`
		INSERT INTO quality_metrics (
			run_id,
			lint_errors_before, lint_errors_after,
			lint_warnings_before, lint_warnings_after,
			tests_total_before, tests_passed_before, tests_failed_before,
			tests_total_after, tests_passed_after, tests_failed_after,
			lint_delta, tests_delta,
			lines_added, lines_removed, files_changed,
			commit_count, commit_msg_quality
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		m.RunID,
		m.LintErrorsBefore, m.LintErrorsAfter,
		m.LintWarningsBefore, m.LintWarningsAfter,
		m.TestsTotalBefore, m.TestsPassedBefore, m.TestsFailedBefore,
		m.TestsTotalAfter, m.TestsPassedAfter, m.TestsFailedAfter,
		m.LintDelta, m.TestsDelta,
		m.LinesAdded, m.LinesRemoved, m.FilesChanged,
		m.CommitCount, m.CommitMsgQuality,
	)
	if err != nil {
		return fmt.Errorf("insert quality metrics: %w", err)
	}
	return nil
}

// GetQualityMetrics retrieves quality metrics for a run
func (l *LocalDB) GetQualityMetrics(runID string) (*quality.Metrics, error) {
	m := &quality.Metrics{RunID: runID}

	err := l.QueryRow(`
		SELECT
			lint_errors_before, lint_errors_after,
			lint_warnings_before, lint_warnings_after,
			tests_total_before, tests_passed_before, tests_failed_before,
			tests_total_after, tests_passed_after, tests_failed_after,
			lint_delta, tests_delta,
			COALESCE(lines_added, 0), COALESCE(lines_removed, 0), COALESCE(files_changed, 0),
			COALESCE(commit_count, 0), COALESCE(commit_msg_quality, 0)
		FROM quality_metrics WHERE run_id = ?
	`, runID).Scan(
		&m.LintErrorsBefore, &m.LintErrorsAfter,
		&m.LintWarningsBefore, &m.LintWarningsAfter,
		&m.TestsTotalBefore, &m.TestsPassedBefore, &m.TestsFailedBefore,
		&m.TestsTotalAfter, &m.TestsPassedAfter, &m.TestsFailedAfter,
		&m.LintDelta, &m.TestsDelta,
		&m.LinesAdded, &m.LinesRemoved, &m.FilesChanged,
		&m.CommitCount, &m.CommitMsgQuality,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get quality metrics: %w", err)
	}

	return m, nil
}

// QualityStats holds aggregate quality statistics
type QualityStats struct {
	AgentName        string
	RunCount         int
	AvgLintDelta     float64
	AvgTestsDelta    float64
	ImprovedCount    int
	ImprovedPercent  float64
	AvgLinesChanged  float64
	AvgCommitQuality float64
	TotalCommits     int
}

// GetQualityStatsByAgent returns quality statistics grouped by agent
func (l *LocalDB) GetQualityStatsByAgent() ([]QualityStats, error) {
	rows, err := l.Query(`
		SELECT
			COALESCE(r.agent_name, '(human)') as agent_name,
			COUNT(*) as run_count,
			AVG(q.lint_delta) as avg_lint_delta,
			AVG(q.tests_delta) as avg_tests_delta,
			SUM(CASE WHEN q.lint_delta < 0 OR q.tests_delta > 0 THEN 1 ELSE 0 END) as improved_count,
			AVG(COALESCE(q.lines_added, 0) + COALESCE(q.lines_removed, 0)) as avg_lines_changed,
			AVG(COALESCE(q.commit_msg_quality, 0)) as avg_commit_quality,
			SUM(COALESCE(q.commit_count, 0)) as total_commits
		FROM quality_metrics q
		JOIN runs r ON q.run_id = r.id
		GROUP BY COALESCE(r.agent_name, '(human)')
		ORDER BY avg_tests_delta DESC, avg_lint_delta ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query quality stats: %w", err)
	}
	defer rows.Close()

	var stats []QualityStats
	for rows.Next() {
		var s QualityStats
		if err := rows.Scan(
			&s.AgentName,
			&s.RunCount,
			&s.AvgLintDelta,
			&s.AvgTestsDelta,
			&s.ImprovedCount,
			&s.AvgLinesChanged,
			&s.AvgCommitQuality,
			&s.TotalCommits,
		); err != nil {
			return nil, fmt.Errorf("scan quality stats: %w", err)
		}
		if s.RunCount > 0 {
			s.ImprovedPercent = float64(s.ImprovedCount) / float64(s.RunCount) * 100
		}
		stats = append(stats, s)
	}

	return stats, rows.Err()
}
