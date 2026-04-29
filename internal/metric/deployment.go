package metric

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/phuc-nt/dandori-cli/internal/jira"
)

// jiraDeploySource is the narrow slice of jira.Client the deploy/lead time
// computation needs. Defined as an interface so tests inject a fake instead
// of standing up an HTTP server.
type jiraDeploySource interface {
	SearchIssues(jql string, maxResults int) ([]jira.Issue, error)
	GetIssueChangelog(issueKey string) ([]jira.StatusChange, error)
}

// DeployQuery selects which Jira tickets count as deploy events for the
// window. Team is optional; when set it's appended to JQL via project/label
// (caller-provided in JQLExtra to keep this package agnostic to org schema).
type DeployQuery struct {
	Window     MetricWindow
	StatusCfg  JiraStatusConfig
	JQLExtra   string // optional, e.g. `AND project = PAYMENTS`
	MaxResults int    // 0 → 50 default in jira client
}

type DeployEvent struct {
	IssueKey string
	At       time.Time
}

type DeployFreqResult struct {
	Window           MetricWindow
	Count            int
	PerDay           float64
	Events           []DeployEvent
	InsufficientData bool
}

// LeadTimeQuery reuses DeployQuery semantics; lead time is computed for the
// same population of "deployed in window" tickets.
type LeadTimeQuery = DeployQuery

type LeadTimeResult struct {
	Window                  MetricWindow
	SamplesUsed             int // tickets with both in-progress + deploy timestamps
	TicketsWithoutInProgres int
	P50Seconds              float64
	P75Seconds              float64
	P90Seconds              float64
	InsufficientData        bool
}

// ComputeDeployFreq counts unique tickets that first entered any release
// status during the window. Re-deploys (transition out → back in) only count
// the first entry within the window — the changelog is replayed in
// chronological order and we stop at the first match per ticket.
func ComputeDeployFreq(src jiraDeploySource, q DeployQuery) (DeployFreqResult, error) {
	if !q.Window.End.After(q.Window.Start) {
		return DeployFreqResult{}, fmt.Errorf("invalid window: end must be after start")
	}

	issues, err := searchDeployedIssues(src, q)
	if err != nil {
		return DeployFreqResult{}, err
	}

	events := make([]DeployEvent, 0, len(issues))
	for _, iss := range issues {
		cl, err := src.GetIssueChangelog(iss.Key)
		if err != nil {
			return DeployFreqResult{}, fmt.Errorf("changelog %s: %w", iss.Key, err)
		}
		if t, ok := firstEntryWithin(cl, q.StatusCfg.ReleaseStatusNames, q.Window); ok {
			events = append(events, DeployEvent{IssueKey: iss.Key, At: t})
		}
	}

	days := q.Window.End.Sub(q.Window.Start).Hours() / 24.0
	res := DeployFreqResult{
		Window:           q.Window,
		Count:            len(events),
		Events:           events,
		InsufficientData: len(events) == 0,
	}
	if days > 0 {
		res.PerDay = float64(len(events)) / days
	}
	return res, nil
}

// ComputeLeadTime: for each ticket deployed in window, find the first
// in-progress entry (any time, even before window) and the deploy entry
// within window. Tickets without an in-progress entry are counted in
// TicketsWithoutInProgres but excluded from percentiles.
func ComputeLeadTime(src jiraDeploySource, q LeadTimeQuery) (LeadTimeResult, error) {
	if !q.Window.End.After(q.Window.Start) {
		return LeadTimeResult{}, fmt.Errorf("invalid window: end must be after start")
	}

	issues, err := searchDeployedIssues(src, q)
	if err != nil {
		return LeadTimeResult{}, err
	}

	res := LeadTimeResult{Window: q.Window}
	durations := make([]float64, 0, len(issues))

	for _, iss := range issues {
		cl, err := src.GetIssueChangelog(iss.Key)
		if err != nil {
			return LeadTimeResult{}, fmt.Errorf("changelog %s: %w", iss.Key, err)
		}
		end, ok := firstEntryWithin(cl, q.StatusCfg.ReleaseStatusNames, q.Window)
		if !ok {
			continue
		}
		start, ok := firstEntryBefore(cl, q.StatusCfg.InProgressStatusNames, end)
		if !ok {
			res.TicketsWithoutInProgres++
			continue
		}
		durations = append(durations, end.Sub(start).Seconds())
	}

	res.SamplesUsed = len(durations)
	if len(durations) == 0 {
		res.InsufficientData = true
		return res, nil
	}
	sort.Float64s(durations)
	res.P50Seconds = percentile(durations, 50)
	res.P75Seconds = percentile(durations, 75)
	res.P90Seconds = percentile(durations, 90)
	return res, nil
}

// searchDeployedIssues runs JQL `status was in (...) DURING (since, until)`
// to narrow the candidate set; full filtering is done per-ticket via
// changelog inspection (JQL function granularity is not always reliable
// across Cloud/DC).
func searchDeployedIssues(src jiraDeploySource, q DeployQuery) ([]jira.Issue, error) {
	if len(q.StatusCfg.ReleaseStatusNames) == 0 {
		return nil, fmt.Errorf("release_status_names empty")
	}
	jql := fmt.Sprintf(
		`status was in (%s) DURING ("%s", "%s")`,
		joinQuoted(q.StatusCfg.ReleaseStatusNames),
		q.Window.Start.UTC().Format("2006-01-02 15:04"),
		q.Window.End.UTC().Format("2006-01-02 15:04"),
	)
	if q.JQLExtra != "" {
		jql += " " + q.JQLExtra
	}
	return src.SearchIssues(jql, q.MaxResults)
}

// firstEntryWithin returns the earliest changelog entry whose To matches
// any of names (case-insensitive) and falls in [window.Start, window.End).
// Assumes cl is chronological (StatusChange.GetIssueChangelog already sorts).
func firstEntryWithin(cl []jira.StatusChange, names []string, window MetricWindow) (time.Time, bool) {
	for _, sc := range cl {
		if !matchesAny(sc.To, names) {
			continue
		}
		if sc.When.Before(window.Start) || !sc.When.Before(window.End) {
			continue
		}
		return sc.When, true
	}
	return time.Time{}, false
}

// firstEntryBefore returns the earliest changelog entry whose To matches
// any of names AND occurred at or before cutoff. Used to find the
// "in progress" start that preceded a deploy.
func firstEntryBefore(cl []jira.StatusChange, names []string, cutoff time.Time) (time.Time, bool) {
	for _, sc := range cl {
		if !matchesAny(sc.To, names) {
			continue
		}
		if sc.When.After(cutoff) {
			continue
		}
		return sc.When, true
	}
	return time.Time{}, false
}

func matchesAny(s string, names []string) bool {
	for _, n := range names {
		if strings.EqualFold(strings.TrimSpace(s), strings.TrimSpace(n)) {
			return true
		}
	}
	return false
}

func joinQuoted(names []string) string {
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = fmt.Sprintf("%q", n)
	}
	return strings.Join(parts, ", ")
}

// percentile computes the p-th percentile of a pre-sorted slice using
// linear interpolation between the two surrounding ranks (NIST method).
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}
	rank := (p / 100.0) * float64(len(sorted)-1)
	lo := int(math.Floor(rank))
	hi := int(math.Ceil(rank))
	if lo == hi {
		return sorted[lo]
	}
	frac := rank - float64(lo)
	return sorted[lo] + frac*(sorted[hi]-sorted[lo])
}
