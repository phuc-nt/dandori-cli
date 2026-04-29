package jira

import (
	"fmt"
	"time"
)

// StatusChange represents a single Jira status transition row from the
// issue changelog. Used by metric package to compute deployment frequency
// and lead time per G6 (DORA) — see docs/goals-and-metrics/.
type StatusChange struct {
	From  string
	To    string
	When  time.Time
	Actor string
}

// changelogResponse mirrors the shape returned by GET /rest/api/2/issue/{key}?expand=changelog.
// Only the fields needed for status transitions are decoded; everything else
// is ignored to keep the parser cheap.
type changelogResponse struct {
	Changelog struct {
		Histories []changelogHistory `json:"histories"`
	} `json:"changelog"`
}

type changelogHistory struct {
	Created string `json:"created"`
	Author  struct {
		DisplayName string `json:"displayName"`
	} `json:"author"`
	Items []changelogItem `json:"items"`
}

type changelogItem struct {
	Field      string `json:"field"`
	FromString string `json:"fromString"`
	ToString   string `json:"toString"`
}

// GetIssueChangelog fetches the status-transition history for one issue.
// Returns entries in chronological order (oldest first). Non-status field
// changes are filtered out — callers only care about status transitions.
//
// Atlassian Cloud and Data Center both expose ?expand=changelog on the
// classic v2 issue endpoint; no separate endpoint needed.
func (c *Client) GetIssueChangelog(issueKey string) ([]StatusChange, error) {
	var resp changelogResponse
	path := fmt.Sprintf("/rest/api/2/issue/%s?expand=changelog&fields=summary", issueKey)
	if err := c.get(path, &resp); err != nil {
		return nil, fmt.Errorf("fetch changelog %s: %w", issueKey, err)
	}

	var out []StatusChange
	for _, h := range resp.Changelog.Histories {
		when, err := parseChangelogTime(h.Created)
		if err != nil {
			continue
		}
		for _, item := range h.Items {
			if item.Field != "status" {
				continue
			}
			out = append(out, StatusChange{
				From:  item.FromString,
				To:    item.ToString,
				When:  when,
				Actor: h.Author.DisplayName,
			})
		}
	}
	// Jira returns histories oldest-first already, but be defensive.
	sortByWhen(out)
	return out, nil
}

func parseChangelogTime(s string) (time.Time, error) {
	// Atlassian uses ISO-8601 with millis + offset, e.g. 2026-04-15T10:23:45.000+0700
	for _, layout := range []string{
		"2006-01-02T15:04:05.000-0700",
		"2006-01-02T15:04:05-0700",
		time.RFC3339Nano,
		time.RFC3339,
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognised timestamp %q", s)
}

func sortByWhen(s []StatusChange) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1].When.After(s[j].When); j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
