package jira

// BugLinkStore is the storage half the task-done hook needs: persist one
// row per (bug, run) pair. Implemented by *db.LocalDB.InsertBuglink.
type BugLinkStore interface {
	InsertBuglink(bugKey, runID, reason, linkedBy string) error
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
		if err := store.InsertBuglink(bug.Key, runID, reason, "task-done-hook"); err != nil {
			return inserted, err
		}
		inserted++
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
		if err := store.InsertBuglink(bug.Key, runID, reason, "task-done-hook"); err != nil {
			return inserted, err
		}
		inserted++
	}
	return inserted, nil
}

// isBugType matches Jira's "Bug" issue type case-insensitively. Some
// instances customize the name ("Defect", "Production Bug") — we match
// any type that contains "bug" so the hook is forgiving.
func isBugType(issueType string) bool {
	t := lowerASCIIHook(issueType)
	return containsHook(t, "bug")
}

// lowerASCIIHook / containsHook: small ASCII-only helpers. Inlined
// instead of importing strings to keep the package's import footprint
// stable (matches the convention in run_outcome_reason.go).
func lowerASCIIHook(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

func containsHook(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	if len(substr) > len(s) {
		return false
	}
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
