// Package metric computes DORA + Rework Rate from local run/event/Jira data.
//
// Single source of truth = Jira (per G6 architecture decision 2026-04-29):
// no CI/CD webhook, no incident-system adapter. Deploy events come from Jira
// status transitions; incidents from Jira issuetype/label query.
package metric

import "time"

// Rework Rate threshold for v1 milestone (2026-Q2). Q1 review will tighten
// toward 8% then 5% per Faros benchmark guidance. Documented in
// docs/goals-and-metrics/decision-260429-dora-plus-rework-baseline.md.
const (
	ReworkThresholdV1  = 0.10
	ReworkThresholdTag = "v1-2026Q2"
	DefaultWindowDays  = 28
)

// MetricWindow is a closed-open time interval [Start, End) used by all
// calculators. Both ends are stored as UTC; callers convert.
type MetricWindow struct {
	Start time.Time
	End   time.Time
}

// DefaultWindow returns the rolling 28-day window ending at now (UTC).
func DefaultWindow(now time.Time) MetricWindow {
	end := now.UTC()
	return MetricWindow{
		Start: end.AddDate(0, 0, -DefaultWindowDays),
		End:   end,
	}
}

// TeamFilter narrows queries to a specific team/department. Empty Team
// means "all teams"; runs with empty department are bucketed as
// "unassigned" downstream (not filtered out).
type TeamFilter struct {
	Team string
}

// JiraStatusConfig defines which Jira status names count as deploy events
// (transition INTO any ReleaseStatusNames) and which mark "in progress"
// for lead-time start (transition INTO any InProgressStatusNames). Names
// are matched case-insensitively. Per-team overrides come from config.yaml;
// DefaultJiraStatusConfig provides sensible defaults that work for most
// Jira instances using the default workflow.
type JiraStatusConfig struct {
	ReleaseStatusNames    []string
	InProgressStatusNames []string
}

func DefaultJiraStatusConfig() JiraStatusConfig {
	return JiraStatusConfig{
		ReleaseStatusNames:    []string{"Released", "Deployed", "Live", "Done"},
		InProgressStatusNames: []string{"In Progress"},
	}
}

// IncidentMatchConfig defines what counts as an incident in Jira. At least
// one of IssueTypes/Labels must be non-empty (validated by the caller). Both
// are OR-matched server-side via JQL; deduplication on issue key is implicit
// because Jira already returns distinct issues.
type IncidentMatchConfig struct {
	IssueTypes []string
	Labels     []string
}

// IncidentQuery selects incidents created in the window. JQLExtra mirrors
// DeployQuery for team scoping (e.g. `AND project = PAYMENTS`).
type IncidentQuery struct {
	Window     MetricWindow
	Match      IncidentMatchConfig
	JQLExtra   string
	MaxResults int
}
