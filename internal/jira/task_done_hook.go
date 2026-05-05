package jira

import "strings"

// BugLinkStore is the storage half the task-done hook needs: persist one
// row per (bug, run) pair. Implemented by *db.LocalDB.InsertBuglink.
// Returns (1, nil) on real insert, (0, nil) when the row already existed.
type BugLinkStore interface {
	InsertBuglink(bugKey, runID, reason, linkedBy string) (int64, error)
}

// TaskDoneIssueFetcher loads a Jira issue (with links populated) by key.
// Implemented by *Client.GetIssue. Kept narrow so tests can stub it.
type TaskDoneIssueFetcher interface {
	GetIssue(issueKey string) (*Issue, error)
}

// RecordOnTaskDone is invoked when a task transitions to Done. If the
// task being closed is itself a Bug, walk its "is caused by" links and
// record one buglink row per resolved (bug, run) pair.
//
// Returns the number of rows inserted (idempotent: repeat invocations
// for the same bug return 0 because of the UNIQUE constraint). Errors
// are logged at the call site — this returns the first error to let
// the caller decide whether to surface or swallow.
//
// Non-bug tasks are a no-op (returns 0, nil). Tasks with no resolvable
// "is caused by" links are also a no-op.
func RecordOnTaskDone(fetcher TaskDoneIssueFetcher, store BugLinkStore, resolver BugLinkResolver, taskKey string) (int, error) {
	if fetcher == nil || store == nil || resolver == nil || taskKey == "" {
		return 0, nil
	}
	issue, err := fetcher.GetIssue(taskKey)
	if err != nil || issue == nil {
		return 0, err
	}
	if !isBugType(issue.IssueType) {
		return 0, nil
	}

	bug := &BugIssue{}
	bug.FromIssue(issue)

	inserted := 0
	for _, taskKey := range ParseLinkCandidates(bug) {
		runID, err := resolver.LatestRunForIssue(taskKey)
		if err != nil {
			return inserted, err
		}
		if runID == "" {
			continue
		}
		reason := "task-done: jira link from " + bug.Key + " (" + taskKey + ")"
		n, err := store.InsertBuglink(bug.Key, runID, reason, "task-done-hook")
		if err != nil {
			return inserted, err
		}
		inserted += int(n)
	}
	for _, prefix := range ParseDescriptionTags(bug.Description) {
		runID, err := resolver.FindRunByPrefix(prefix)
		if err != nil {
			return inserted, err
		}
		if runID == "" {
			continue
		}
		reason := "task-done: caused_by tag in " + bug.Key
		n, err := store.InsertBuglink(bug.Key, runID, reason, "task-done-hook")
		if err != nil {
			return inserted, err
		}
		inserted += int(n)
	}
	return inserted, nil
}

// isBugType matches Jira's "Bug" issue type case-insensitively. Some
// instances customize the name ("Defect", "Production Bug") — we match
// any type that contains "bug" so the hook is forgiving.
func isBugType(issueType string) bool {
	return strings.Contains(strings.ToLower(issueType), "bug")
}
