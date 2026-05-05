package db

import (
	"fmt"
)

// LatestRunIDForIssue is the BugLinkResolver-shaped variant of
// LatestRunForIssue: returns just the run ID (or "" when none) so the
// jira package's resolver interface stays string-only.
func (l *LocalDB) LatestRunIDForIssue(issueKey string) (string, error) {
	r, err := l.LatestRunForIssue(issueKey)
	if err != nil || r == nil {
		return "", err
	}
	return r.ID, nil
}

// FindRunByPrefix resolves a runID prefix (≥12 hex per parser convention) to
// a full runID. Returns ("", nil) when no match. Returns an error when the
// prefix matches >1 row — caller must surface this so the bug can be
// re-tagged with a longer prefix.
func (l *LocalDB) FindRunByPrefix(prefix string) (string, error) {
	if prefix == "" {
		return "", nil
	}
	rows, err := l.db.Query(`SELECT id FROM runs WHERE id LIKE ? LIMIT 2`, prefix+"%")
	if err != nil {
		return "", fmt.Errorf("find run by prefix: %w", err)
	}
	defer rows.Close()

	var matches []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", err
		}
		matches = append(matches, id)
	}
	if len(matches) == 0 {
		return "", nil
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("ambiguous run prefix %q matches multiple runs", prefix)
	}
	return matches[0], nil
}

// InsertBuglink records a (bug, run) pairing in the buglinks table.
// linkedBy is the actor (typically "task-done-hook"); reason is free text
// captured from the Jira link or hook context. Idempotent via the
// UNIQUE(jira_bug_key, run_id) constraint — re-runs are silent no-ops.
// Returns (1, nil) when the row was inserted, (0, nil) when it already existed.
func (l *LocalDB) InsertBuglink(bugKey, runID, reason, linkedBy string) (int64, error) {
	if linkedBy == "" {
		linkedBy = "task-done-hook"
	}
	res, err := l.Exec(`
		INSERT OR IGNORE INTO buglinks (jira_bug_key, run_id, reason, linked_by)
		VALUES (?, ?, ?, ?)
	`, bugKey, runID, reason, linkedBy)
	if err != nil {
		return 0, fmt.Errorf("insert buglink: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// BugEventExists returns true when a bug.filed event has already been recorded
// for the given Jira bug key. Used to dedupe across poller cycles.
func (l *LocalDB) BugEventExists(bugKey string) (bool, error) {
	var n int
	err := l.db.QueryRow(`
		SELECT COUNT(*) FROM events
		WHERE event_type = 'bug.filed'
		  AND json_extract(data, '$.bug_key') = ?
	`, bugKey).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("bug event exists: %w", err)
	}
	return n > 0, nil
}
