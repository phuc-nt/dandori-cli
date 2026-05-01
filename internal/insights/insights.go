// Package insights implements the G9 heuristic engine — 5 SQL-based signal detectors
// that surface actionable cards from local run data without requiring ML.
// All heuristics are scoped per projectKey (empty = org-wide).
package insights

import (
	"database/sql"

	"github.com/phuc-nt/dandori-cli/internal/db"
)

// Card is a single actionable insight surfaced by a heuristic.
type Card struct {
	ID       string `json:"id"`
	Severity string `json:"severity"`          // "low" | "medium" | "high"
	Title    string `json:"title"`
	Body     string `json:"body"`
	Action   string `json:"action,omitempty"`   // dashboard view to drill into
	ActionID string `json:"action_id,omitempty"` // optional ID for action target
}

// Store is the minimal interface the insights package needs from the DB layer.
// *db.LocalDB satisfies this interface directly.
type Store interface {
	Query(query string, args ...any) (*sql.Rows, error)
	LatestSnapshot(team, format string) (*db.MetricSnapshot, error)
}

// Compute runs all 5 heuristics and returns the combined card slice.
// An empty slice (never nil) is returned when no signals are detected.
// projectKey filters all heuristics to runs whose jira_issue_key starts with
// "projectKey-"; pass "" for org-wide scope.
func Compute(store Store, projectKey string) ([]Card, error) {
	var out []Card

	if cards, err := wowCostSpike(store, projectKey); err != nil {
		return nil, err
	} else {
		out = append(out, cards...)
	}

	if cards, err := retentionDecay(store, projectKey); err != nil {
		return nil, err
	} else {
		out = append(out, cards...)
	}

	if cards, err := interventionCluster(store, projectKey); err != nil {
		return nil, err
	} else {
		out = append(out, cards...)
	}

	if cards, err := costOutlierTask(store, projectKey); err != nil {
		return nil, err
	} else {
		out = append(out, cards...)
	}

	if cards, err := doraTrafficLight(store); err != nil {
		return nil, err
	} else {
		out = append(out, cards...)
	}

	if out == nil {
		out = []Card{}
	}
	return out, nil
}

// projectFilter returns a SQL WHERE clause fragment and args for scoping to a
// project key. Returns ("", nil) when projectKey is empty (org-wide).
// The returned clause starts with " AND " so it can be appended to an existing WHERE.
func projectFilter(projectKey string) (string, []any) {
	if projectKey == "" {
		return "", nil
	}
	return " AND jira_issue_key LIKE ?", []any{projectKey + "-%"}
}
