package metric

import (
	"fmt"
	"strings"

	"github.com/phuc-nt/dandori-cli/internal/jira"
)

// jiraIncidentSource is the slice of jira.Client used by CFR/MTTR. Same
// pattern as jiraDeploySource — narrow interface for fakes.
type jiraIncidentSource interface {
	SearchIncidents(jql string, maxResults int) ([]jira.Incident, error)
}

// jiraCFRSource composes deploy + incident interfaces; CFR reuses the deploy
// query path from phase 03.
type jiraCFRSource interface {
	jiraDeploySource
	jiraIncidentSource
}

type CFRResult struct {
	Window           MetricWindow
	Rate             float64 // incidents / deploys
	IncidentCount    int
	DeployCount      int
	InsufficientData bool // true if deploy count is 0
}

// ComputeChangeFailureRate computes aggregate ratio (no per-deploy linkage).
// Trade-off documented in phase-04-cfr-mttr.md: heuristic linkers are risky;
// org-level ratio is meaningful for trend tracking even without causal link.
func ComputeChangeFailureRate(src jiraCFRSource, deployQ DeployQuery, incidentQ IncidentQuery) (CFRResult, error) {
	if !deployQ.Window.End.After(deployQ.Window.Start) {
		return CFRResult{}, fmt.Errorf("invalid deploy window")
	}
	if !incidentQ.Window.End.After(incidentQ.Window.Start) {
		return CFRResult{}, fmt.Errorf("invalid incident window")
	}
	if len(incidentQ.Match.IssueTypes) == 0 && len(incidentQ.Match.Labels) == 0 {
		return CFRResult{}, fmt.Errorf("incident match config is empty: set issue_types or labels")
	}

	deploy, err := ComputeDeployFreq(src, deployQ)
	if err != nil {
		return CFRResult{}, fmt.Errorf("deploy freq: %w", err)
	}

	incidents, err := fetchIncidents(src, incidentQ)
	if err != nil {
		return CFRResult{}, fmt.Errorf("fetch incidents: %w", err)
	}

	res := CFRResult{
		Window:        deployQ.Window,
		IncidentCount: len(incidents),
		DeployCount:   deploy.Count,
	}
	if deploy.Count == 0 {
		res.InsufficientData = true
		return res, nil
	}
	res.Rate = float64(len(incidents)) / float64(deploy.Count)
	return res, nil
}

// fetchIncidents builds incident JQL and calls Jira. Filters to incidents
// CREATED in window — consistent with old plan; alternative would be
// resolved-in-window which under-counts ongoing.
func fetchIncidents(src jiraIncidentSource, q IncidentQuery) ([]jira.Incident, error) {
	jql := buildIncidentJQL(q)
	return src.SearchIncidents(jql, q.MaxResults)
}

func buildIncidentJQL(q IncidentQuery) string {
	var matchClauses []string
	if len(q.Match.IssueTypes) > 0 {
		matchClauses = append(matchClauses, fmt.Sprintf("issuetype IN (%s)", joinQuoted(q.Match.IssueTypes)))
	}
	if len(q.Match.Labels) > 0 {
		matchClauses = append(matchClauses, fmt.Sprintf("labels IN (%s)", joinQuoted(q.Match.Labels)))
	}
	jql := "(" + strings.Join(matchClauses, " OR ") + ")"
	jql += fmt.Sprintf(` AND created >= "%s" AND created < "%s"`,
		q.Window.Start.UTC().Format("2006-01-02 15:04"),
		q.Window.End.UTC().Format("2006-01-02 15:04"),
	)
	if q.JQLExtra != "" {
		jql += " " + q.JQLExtra
	}
	return jql
}
