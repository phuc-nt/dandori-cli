package metric

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/phuc-nt/dandori-cli/internal/jira"
)

type fakeCFRSource struct {
	*fakeJira
	incidents       []jira.Incident
	incidentErr     error
	lastIncidentJQL string
}

func (f *fakeCFRSource) SearchIncidents(jql string, _ int) ([]jira.Incident, error) {
	f.lastIncidentJQL = jql
	if f.incidentErr != nil {
		return nil, f.incidentErr
	}
	return f.incidents, nil
}

func defaultIncidentQuery(window MetricWindow) IncidentQuery {
	return IncidentQuery{
		Window: window,
		Match:  IncidentMatchConfig{IssueTypes: []string{"Incident"}, Labels: []string{"prod-bug"}},
	}
}

func TestComputeCFR_Happy(t *testing.T) {
	w := defaultWindow()
	mid := w.Start.Add(10 * 24 * time.Hour)
	deployIssues := []jira.Issue{}
	deployLogs := map[string][]jira.StatusChange{}
	for i := 0; i < 10; i++ {
		key := "D-" + string(rune('0'+i))
		deployIssues = append(deployIssues, mkIssue(key))
		deployLogs[key] = []jira.StatusChange{mkChange("Released", mid.Add(time.Duration(i)*time.Hour))}
	}
	src := &fakeCFRSource{
		fakeJira: &fakeJira{issues: deployIssues, changelogs: deployLogs},
		incidents: []jira.Incident{
			{Key: "I-1", CreatedAt: mid},
			{Key: "I-2", CreatedAt: mid.Add(2 * time.Hour)},
		},
	}
	got, err := ComputeChangeFailureRate(src,
		DeployQuery{Window: w, StatusCfg: DefaultJiraStatusConfig()},
		defaultIncidentQuery(w),
	)
	if err != nil {
		t.Fatal(err)
	}
	if got.DeployCount != 10 || got.IncidentCount != 2 {
		t.Errorf("counts=(%d, %d) want (10, 2)", got.DeployCount, got.IncidentCount)
	}
	if got.Rate != 0.2 {
		t.Errorf("rate=%v want 0.2", got.Rate)
	}
	if got.InsufficientData {
		t.Error("should not be insufficient")
	}
}

func TestComputeCFR_ZeroDeploysInsufficient(t *testing.T) {
	w := defaultWindow()
	src := &fakeCFRSource{
		fakeJira:  &fakeJira{},
		incidents: []jira.Incident{{Key: "I-1", CreatedAt: w.Start.Add(time.Hour)}},
	}
	got, _ := ComputeChangeFailureRate(src,
		DeployQuery{Window: w, StatusCfg: DefaultJiraStatusConfig()},
		defaultIncidentQuery(w),
	)
	if !got.InsufficientData {
		t.Error("want insufficient_data=true on 0 deploys")
	}
	if got.Rate != 0 {
		t.Errorf("rate=%v want 0 (no division by zero)", got.Rate)
	}
}

func TestComputeCFR_ZeroIncidents(t *testing.T) {
	w := defaultWindow()
	mid := w.Start.Add(time.Hour)
	src := &fakeCFRSource{
		fakeJira: &fakeJira{
			issues:     []jira.Issue{mkIssue("D-1")},
			changelogs: map[string][]jira.StatusChange{"D-1": {mkChange("Released", mid)}},
		},
		incidents: nil,
	}
	got, _ := ComputeChangeFailureRate(src,
		DeployQuery{Window: w, StatusCfg: DefaultJiraStatusConfig()},
		defaultIncidentQuery(w),
	)
	if got.Rate != 0 || got.IncidentCount != 0 {
		t.Errorf("got %+v want rate=0", got)
	}
	if got.InsufficientData {
		t.Error("0 incidents with deploys is meaningful (CFR=0), not insufficient")
	}
}

func TestComputeCFR_EmptyMatchConfigErrors(t *testing.T) {
	w := defaultWindow()
	src := &fakeCFRSource{fakeJira: &fakeJira{}}
	bad := IncidentQuery{Window: w, Match: IncidentMatchConfig{}}
	_, err := ComputeChangeFailureRate(src, DeployQuery{Window: w, StatusCfg: DefaultJiraStatusConfig()}, bad)
	if err == nil {
		t.Error("want err on empty match config")
	}
}

func TestComputeCFR_IncidentErrorPropagates(t *testing.T) {
	w := defaultWindow()
	src := &fakeCFRSource{fakeJira: &fakeJira{}, incidentErr: errors.New("boom")}
	_, err := ComputeChangeFailureRate(src, DeployQuery{Window: w, StatusCfg: DefaultJiraStatusConfig()}, defaultIncidentQuery(w))
	if err == nil {
		t.Error("want err propagated")
	}
}

func TestBuildIncidentJQL_TypeOrLabels(t *testing.T) {
	w := defaultWindow()
	q := IncidentQuery{
		Window: w,
		Match: IncidentMatchConfig{
			IssueTypes: []string{"Incident", "Bug"},
			Labels:     []string{"prod-bug"},
		},
		JQLExtra: `AND project = PAY`,
	}
	jql := buildIncidentJQL(q)
	if !strings.Contains(jql, `issuetype IN ("Incident", "Bug")`) {
		t.Errorf("missing type clause: %s", jql)
	}
	if !strings.Contains(jql, `labels IN ("prod-bug")`) {
		t.Errorf("missing label clause: %s", jql)
	}
	if !strings.Contains(jql, ` OR `) {
		t.Errorf("missing OR between type and labels: %s", jql)
	}
	if !strings.HasSuffix(jql, "AND project = PAY") {
		t.Errorf("missing JQL extra: %s", jql)
	}
}

func TestBuildIncidentJQL_LabelsOnly(t *testing.T) {
	jql := buildIncidentJQL(IncidentQuery{
		Window: defaultWindow(),
		Match:  IncidentMatchConfig{Labels: []string{"prod-bug"}},
	})
	if strings.Contains(jql, "issuetype") {
		t.Errorf("should not include issuetype clause: %s", jql)
	}
	if !strings.Contains(jql, `labels IN ("prod-bug")`) {
		t.Errorf("missing label clause: %s", jql)
	}
}
