package jira

import (
	"regexp"
	"strings"
)

// BugIssue is a focused subset of a Jira bug ticket — the fields ParseLinkCandidates
// and ParseDescriptionTags need. The poller wraps Jira's full issue payload into
// this so the parsers stay free of Jira API noise.
type BugIssue struct {
	Key         string
	Summary     string
	Description string
	Links       []IssueLink
}

// FromIssue adapts a poller-fetched Issue into a BugIssue. Used by the
// bugLinkCycle to feed DetectBugLinks.
func (b *BugIssue) FromIssue(i *Issue) {
	if i == nil {
		return
	}
	b.Key = i.Key
	b.Summary = i.Summary
	b.Description = i.Description
	b.Links = i.Links
}

// IssueLink represents one row from a Jira issue's "issuelinks" array. Only
// one of InwardKey / OutwardKey is set per Jira; we accept either since
// the relevant directional half differs by link type definition.
type IssueLink struct {
	Type       string
	InwardKey  string
	OutwardKey string
}

// causedByLinkTypes is matched case-insensitively. Includes the canonical
// link-type Name ("Caused" — what real Jira sends) plus the inward/outward
// descriptions for fixtures that use those forms. Other Jira instances may
// add custom names — make this configurable in REFACTOR (Step 8).
var causedByLinkTypes = map[string]bool{
	"caused":       true,
	"caused by":    true,
	"is caused by": true,
	"causes":       true,
}

// runIDPattern matches `caused_by:` followed by ≥12 hex chars. Intentionally
// strict — 8-char prefixes are too prone to collision in a multi-month dataset.
// The convention is to embed the full runID, prefix is the fallback.
var runIDPattern = regexp.MustCompile(`(?i)caused_by:\s*([a-f0-9]{12,})`)

// ParseDescriptionTags returns runID prefixes (≥12 hex) found in a bug's
// description after the literal `caused_by:` marker. Case-insensitive.
func ParseDescriptionTags(description string) []string {
	matches := runIDPattern.FindAllStringSubmatch(description, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make([]string, 0, len(matches))
	seen := map[string]bool{}
	for _, m := range matches {
		id := strings.ToLower(m[1])
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// BugLinkResolver lets DetectBugLinks resolve task→run lookups and dedupe
// without forcing the jira package to import internal/db. Implemented in the
// poller by adapting the LocalDB methods.
type BugLinkResolver interface {
	// LatestRunForIssue returns the runID for the most recent run on a given
	// Jira task key, or "" when there is none.
	LatestRunForIssue(issueKey string) (runID string, err error)
	// FindRunByPrefix resolves a hex prefix to a full runID.
	FindRunByPrefix(prefix string) (runID string, err error)
	// BugEventExists tells DetectBugLinks whether this bug has already been
	// recorded — used to dedupe across cycles.
	BugEventExists(bugKey string) (bool, error)
}

// BugLinkEvent is one bug.filed event payload waiting to be recorded.
type BugLinkEvent struct {
	RunID   string
	Payload map[string]any
}

// DetectBugLinks consumes one bug ticket, walks both link types (structured
// issuelinks first, description regex second), resolves each candidate to a
// runID via the resolver, and returns events to emit. Returns nil when the
// bug is already recorded or no runID can be resolved.
//
// Errors from the resolver are logged in the caller — DetectBugLinks itself
// returns the first error so the caller can decide whether to skip just this
// bug or abort the cycle.
func DetectBugLinks(bug *BugIssue, resolver BugLinkResolver) ([]BugLinkEvent, error) {
	if bug == nil || resolver == nil {
		return nil, nil
	}
	exists, err := resolver.BugEventExists(bug.Key)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, nil
	}

	var events []BugLinkEvent
	seenRuns := map[string]bool{}

	// Method 1: structured issue links — preferred because Jira enforces them.
	for _, taskKey := range ParseLinkCandidates(bug) {
		runID, err := resolver.LatestRunForIssue(taskKey)
		if err != nil {
			return nil, err
		}
		if runID == "" || seenRuns[runID] {
			continue
		}
		seenRuns[runID] = true
		events = append(events, BugLinkEvent{
			RunID: runID,
			Payload: map[string]any{
				"bug_key":          bug.Key,
				"bug_summary":      bug.Summary,
				"caused_by_run_id": runID,
				"link_type":        "jira_link",
				"task_key":         taskKey,
			},
		})
	}

	// Method 2: description tag — fallback / advisory.
	for _, prefix := range ParseDescriptionTags(bug.Description) {
		runID, err := resolver.FindRunByPrefix(prefix)
		if err != nil {
			return nil, err
		}
		if runID == "" || seenRuns[runID] {
			continue
		}
		seenRuns[runID] = true
		events = append(events, BugLinkEvent{
			RunID: runID,
			Payload: map[string]any{
				"bug_key":          bug.Key,
				"bug_summary":      bug.Summary,
				"caused_by_run_id": runID,
				"link_type":        "description_tag",
			},
		})
	}

	return events, nil
}

// ParseLinkCandidates returns issue keys linked to the bug via any "caused by"
// variant. Both inward and outward sides are inspected because Jira's link
// directionality depends on whether the bug is the source or target.
func ParseLinkCandidates(bug *BugIssue) []string {
	if bug == nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, link := range bug.Links {
		if !causedByLinkTypes[strings.ToLower(link.Type)] {
			continue
		}
		key := link.InwardKey
		if key == "" {
			key = link.OutwardKey
		}
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, key)
	}
	return out
}
