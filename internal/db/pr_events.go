// Package db — pr_events.go: storage helpers for the pr_events + sync_state
// tables introduced in v12. These back v0.13's True AI-CFR and PR Review
// Cycle Time metrics. Pulls from GitHub are idempotent: UpsertPR is keyed
// on (repo, pr_number) so re-running `dandori sync` produces no duplicates.
package db

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// PREvent mirrors one row in pr_events. Nullable timestamps are pointers
// so the caller can distinguish "not yet set" from "epoch".
type PREvent struct {
	ID              int64
	Repo            string
	PRNumber        int
	Title           string
	State           string
	Author          string
	CreatedAt       string
	SubmittedAt     string
	MergedAt        *string
	ClosedAt        *string
	FirstApprovalAt *string
	MergeCommitSHA  string
	IsReverted      bool
	RevertedByPR    *int
	RevertedAt      *string
	ReopenedAt      *string
	JiraIssueKeys   string
	LastSyncedAt    string
	// Additions/Deletions are pointers so NULL ("detail not fetched yet")
	// stays distinct from 0 ("PR is a no-op"). Populated by sync when
	// github.fetch_pr_size is enabled.
	Additions *int
	Deletions *int
}

// UpsertPR inserts or updates a PR row keyed by (repo, pr_number).
// Mutable fields (state, *_at, sha, is_reverted, first_approval_at,
// last_synced_at) are refreshed on conflict; immutable history fields
// (created_at, author) are preserved by using INSERT-side values only.
func (l *LocalDB) UpsertPR(p PREvent) error {
	if p.LastSyncedAt == "" {
		p.LastSyncedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if p.SubmittedAt == "" {
		p.SubmittedAt = p.CreatedAt
	}
	_, err := l.Exec(`
		INSERT INTO pr_events (
			repo, pr_number, title, state, author,
			created_at, submitted_at, merged_at, closed_at, first_approval_at,
			merge_commit_sha, is_reverted, reverted_by_pr, reverted_at, reopened_at,
			jira_issue_keys, last_synced_at, additions, deletions
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(repo, pr_number) DO UPDATE SET
			title             = excluded.title,
			state             = excluded.state,
			merged_at         = excluded.merged_at,
			closed_at         = excluded.closed_at,
			first_approval_at = excluded.first_approval_at,
			merge_commit_sha  = excluded.merge_commit_sha,
			last_synced_at    = excluded.last_synced_at,
			additions         = COALESCE(excluded.additions, pr_events.additions),
			deletions         = COALESCE(excluded.deletions, pr_events.deletions)
	`,
		p.Repo, p.PRNumber, p.Title, p.State, p.Author,
		p.CreatedAt, p.SubmittedAt, p.MergedAt, p.ClosedAt, p.FirstApprovalAt,
		p.MergeCommitSHA, boolToInt(p.IsReverted), p.RevertedByPR, p.RevertedAt, p.ReopenedAt,
		p.JiraIssueKeys, p.LastSyncedAt, p.Additions, p.Deletions,
	)
	if err != nil {
		return fmt.Errorf("upsert pr_events: %w", err)
	}
	return nil
}

// GetPRByNumber returns the row for (repo, pr_number) or nil if absent.
func (l *LocalDB) GetPRByNumber(repo string, prNumber int) (*PREvent, error) {
	row := l.QueryRow(`
		SELECT id, repo, pr_number, title, state, author,
		       created_at, submitted_at, merged_at, closed_at, first_approval_at,
		       merge_commit_sha, is_reverted, reverted_by_pr, reverted_at, reopened_at,
		       jira_issue_keys, last_synced_at, additions, deletions
		FROM pr_events WHERE repo = ? AND pr_number = ?
	`, repo, prNumber)
	return scanPREvent(row)
}

// GetPRByTitle returns the most-recently-merged PR matching the exact title
// within the last `sinceDays` days, or nil if no match. Used by revert
// detection: when a new `Revert "X"` PR lands, we look up X to mark it.
// Restricting to recent merges avoids false positives on long-lived
// duplicate titles.
func (l *LocalDB) GetPRByTitle(repo, title string, sinceDays int) (*PREvent, error) {
	if sinceDays <= 0 {
		sinceDays = 30
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -sinceDays).Format(time.RFC3339)
	row := l.QueryRow(`
		SELECT id, repo, pr_number, title, state, author,
		       created_at, submitted_at, merged_at, closed_at, first_approval_at,
		       merge_commit_sha, is_reverted, reverted_by_pr, reverted_at, reopened_at,
		       jira_issue_keys, last_synced_at, additions, deletions
		FROM pr_events
		WHERE repo = ? AND title = ? AND merged_at IS NOT NULL AND merged_at >= ?
		ORDER BY merged_at DESC
		LIMIT 1
	`, repo, title, cutoff)
	return scanPREvent(row)
}

// MarkReverted flags an original PR as reverted by another PR.
// Idempotent — running twice with the same (repo, prNumber, revertedByPR)
// leaves the row unchanged after the first call.
func (l *LocalDB) MarkReverted(repo string, prNumber, revertedByPR int) error {
	ts := time.Now().UTC().Format(time.RFC3339)
	_, err := l.Exec(`
		UPDATE pr_events
		SET is_reverted = 1,
		    reverted_by_pr = ?,
		    reverted_at = COALESCE(reverted_at, ?)
		WHERE repo = ? AND pr_number = ?
	`, revertedByPR, ts, repo, prNumber)
	if err != nil {
		return fmt.Errorf("mark reverted: %w", err)
	}
	return nil
}

// MarkReopened stamps reopened_at if not already set. Used when sync
// observes a PR transitioning from closed to open.
func (l *LocalDB) MarkReopened(repo string, prNumber int, ts string) error {
	if ts == "" {
		ts = time.Now().UTC().Format(time.RFC3339)
	}
	_, err := l.Exec(`
		UPDATE pr_events
		SET reopened_at = COALESCE(reopened_at, ?)
		WHERE repo = ? AND pr_number = ?
	`, ts, repo, prNumber)
	if err != nil {
		return fmt.Errorf("mark reopened: %w", err)
	}
	return nil
}

// CountPRs returns the total number of rows for the given repo.
// Convenience for idempotency tests + sync summary lines.
func (l *LocalDB) CountPRs(repo string) (int, error) {
	var n int
	err := l.QueryRow(`SELECT COUNT(*) FROM pr_events WHERE repo = ?`, repo).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count pr_events: %w", err)
	}
	return n, nil
}

// GetSyncState reads a sync watermark. Returns ("", false, nil) when the
// key has never been set — callers treat that as "fresh install, use
// default backfill window".
func (l *LocalDB) GetSyncState(key string) (string, bool, error) {
	var v string
	err := l.QueryRow(`SELECT value FROM sync_state WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("get sync_state: %w", err)
	}
	return v, true, nil
}

// SetSyncState upserts a sync watermark. Always refreshes updated_at.
func (l *LocalDB) SetSyncState(key, value string) error {
	_, err := l.Exec(`
		INSERT INTO sync_state (key, value, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			value = excluded.value,
			updated_at = excluded.updated_at
	`, key, value, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("set sync_state: %w", err)
	}
	return nil
}

func scanPREvent(row *sql.Row) (*PREvent, error) {
	var p PREvent
	var isReverted int
	err := row.Scan(
		&p.ID, &p.Repo, &p.PRNumber, &p.Title, &p.State, &p.Author,
		&p.CreatedAt, &p.SubmittedAt, &p.MergedAt, &p.ClosedAt, &p.FirstApprovalAt,
		&p.MergeCommitSHA, &isReverted, &p.RevertedByPR, &p.RevertedAt, &p.ReopenedAt,
		&p.JiraIssueKeys, &p.LastSyncedAt, &p.Additions, &p.Deletions,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan pr_events: %w", err)
	}
	p.IsReverted = isReverted != 0
	return &p, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
