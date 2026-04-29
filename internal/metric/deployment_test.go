package metric

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/phuc-nt/dandori-cli/internal/jira"
)

type fakeJira struct {
	issues     []jira.Issue
	changelogs map[string][]jira.StatusChange
	searchErr  error
	logErr     error
	lastJQL    string
}

func (f *fakeJira) SearchIssues(jql string, _ int) ([]jira.Issue, error) {
	f.lastJQL = jql
	if f.searchErr != nil {
		return nil, f.searchErr
	}
	return f.issues, nil
}

func (f *fakeJira) GetIssueChangelog(key string) ([]jira.StatusChange, error) {
	if f.logErr != nil {
		return nil, f.logErr
	}
	return f.changelogs[key], nil
}

func mkIssue(key string) jira.Issue { return jira.Issue{Key: key} }

func mkChange(to string, when time.Time) jira.StatusChange {
	return jira.StatusChange{To: to, When: when}
}

func defaultWindow() MetricWindow {
	end := time.Date(2026, 4, 29, 0, 0, 0, 0, time.UTC)
	return MetricWindow{Start: end.AddDate(0, 0, -28), End: end}
}

func TestComputeDeployFreq_Happy(t *testing.T) {
	w := defaultWindow()
	mid := w.Start.Add(7 * 24 * time.Hour)
	f := &fakeJira{
		issues: []jira.Issue{mkIssue("K-1"), mkIssue("K-2"), mkIssue("K-3"), mkIssue("K-4"), mkIssue("K-5")},
		changelogs: map[string][]jira.StatusChange{
			"K-1": {mkChange("In Progress", mid.Add(-2*time.Hour)), mkChange("Released", mid)},
			"K-2": {mkChange("Released", mid.Add(time.Hour))},
			"K-3": {mkChange("Deployed", mid.Add(2*time.Hour))},
			"K-4": {mkChange("Live", mid.Add(3*time.Hour))},
			"K-5": {mkChange("RELEASED", mid.Add(4*time.Hour))}, // case insensitive
		},
	}
	got, err := ComputeDeployFreq(f, DeployQuery{Window: w, StatusCfg: DefaultJiraStatusConfig()})
	if err != nil {
		t.Fatal(err)
	}
	if got.Count != 5 {
		t.Errorf("count=%d want 5", got.Count)
	}
	wantPerDay := 5.0 / 28.0
	if math.Abs(got.PerDay-wantPerDay) > 1e-9 {
		t.Errorf("per_day=%v want %v", got.PerDay, wantPerDay)
	}
	if got.InsufficientData {
		t.Error("should not be insufficient")
	}
}

func TestComputeDeployFreq_Empty(t *testing.T) {
	w := defaultWindow()
	f := &fakeJira{issues: nil, changelogs: map[string][]jira.StatusChange{}}
	got, err := ComputeDeployFreq(f, DeployQuery{Window: w, StatusCfg: DefaultJiraStatusConfig()})
	if err != nil {
		t.Fatal(err)
	}
	if got.Count != 0 || !got.InsufficientData {
		t.Errorf("got=%+v want count=0 insufficient=true", got)
	}
}

func TestComputeDeployFreq_RedeployedCountedOnce(t *testing.T) {
	w := defaultWindow()
	mid := w.Start.Add(10 * 24 * time.Hour)
	f := &fakeJira{
		issues: []jira.Issue{mkIssue("K-1")},
		changelogs: map[string][]jira.StatusChange{
			"K-1": {
				mkChange("Released", mid),
				mkChange("In Progress", mid.Add(time.Hour)),
				mkChange("Released", mid.Add(2*time.Hour)),
			},
		},
	}
	got, _ := ComputeDeployFreq(f, DeployQuery{Window: w, StatusCfg: DefaultJiraStatusConfig()})
	if got.Count != 1 {
		t.Errorf("re-deploy should count once, got %d", got.Count)
	}
	if !got.Events[0].At.Equal(mid) {
		t.Errorf("should pick first entry; got %v want %v", got.Events[0].At, mid)
	}
}

func TestComputeDeployFreq_OutsideWindowExcluded(t *testing.T) {
	w := defaultWindow()
	f := &fakeJira{
		issues: []jira.Issue{mkIssue("K-old"), mkIssue("K-future")},
		changelogs: map[string][]jira.StatusChange{
			"K-old":    {mkChange("Released", w.Start.Add(-time.Hour))},
			"K-future": {mkChange("Released", w.End.Add(time.Hour))},
		},
	}
	got, _ := ComputeDeployFreq(f, DeployQuery{Window: w, StatusCfg: DefaultJiraStatusConfig()})
	if got.Count != 0 {
		t.Errorf("out-of-window deploys should be excluded, got %d", got.Count)
	}
}

func TestComputeDeployFreq_InvalidWindow(t *testing.T) {
	end := time.Now()
	bad := MetricWindow{Start: end, End: end} // zero-width
	_, err := ComputeDeployFreq(&fakeJira{}, DeployQuery{Window: bad, StatusCfg: DefaultJiraStatusConfig()})
	if err == nil {
		t.Error("want err for invalid window")
	}
}

func TestComputeDeployFreq_SearchError(t *testing.T) {
	f := &fakeJira{searchErr: errors.New("boom")}
	_, err := ComputeDeployFreq(f, DeployQuery{Window: defaultWindow(), StatusCfg: DefaultJiraStatusConfig()})
	if err == nil {
		t.Error("want err propagated")
	}
}

func TestComputeDeployFreq_JQLExtraAppended(t *testing.T) {
	f := &fakeJira{}
	q := DeployQuery{Window: defaultWindow(), StatusCfg: DefaultJiraStatusConfig(), JQLExtra: `AND project = PAY`}
	_, _ = ComputeDeployFreq(f, q)
	if f.lastJQL == "" || f.lastJQL[len(f.lastJQL)-len("AND project = PAY"):] != "AND project = PAY" {
		t.Errorf("jql=%q missing extra clause", f.lastJQL)
	}
}

func TestComputeLeadTime_Happy(t *testing.T) {
	w := defaultWindow()
	deployAt := w.Start.Add(10 * 24 * time.Hour)
	f := &fakeJira{
		issues: []jira.Issue{mkIssue("K-1"), mkIssue("K-2"), mkIssue("K-3")},
		changelogs: map[string][]jira.StatusChange{
			// 5 hours
			"K-1": {mkChange("In Progress", deployAt.Add(-5*time.Hour)), mkChange("Released", deployAt)},
			// 10 hours
			"K-2": {mkChange("In Progress", deployAt.Add(-10*time.Hour)), mkChange("Released", deployAt.Add(time.Hour))},
			// 20 hours
			"K-3": {mkChange("In Progress", deployAt.Add(-20*time.Hour)), mkChange("Released", deployAt.Add(2*time.Hour))},
		},
	}
	got, err := ComputeLeadTime(f, LeadTimeQuery{Window: w, StatusCfg: DefaultJiraStatusConfig()})
	if err != nil {
		t.Fatal(err)
	}
	if got.SamplesUsed != 3 {
		t.Fatalf("samples=%d want 3", got.SamplesUsed)
	}
	// durations are 5h, 11h, 22h (end+1h - start-10h, end+2h - start-20h)
	// p50 = 11h = 39600s
	if math.Abs(got.P50Seconds-39600) > 1 {
		t.Errorf("p50=%v want ~39600", got.P50Seconds)
	}
}

func TestComputeLeadTime_SkipsTicketsWithoutInProgress(t *testing.T) {
	w := defaultWindow()
	deployAt := w.Start.Add(5 * 24 * time.Hour)
	f := &fakeJira{
		issues: []jira.Issue{mkIssue("K-skip"), mkIssue("K-good")},
		changelogs: map[string][]jira.StatusChange{
			"K-skip": {mkChange("Released", deployAt)}, // backlog → released
			"K-good": {mkChange("In Progress", deployAt.Add(-time.Hour)), mkChange("Released", deployAt)},
		},
	}
	got, _ := ComputeLeadTime(f, LeadTimeQuery{Window: w, StatusCfg: DefaultJiraStatusConfig()})
	if got.SamplesUsed != 1 {
		t.Errorf("samples=%d want 1", got.SamplesUsed)
	}
	if got.TicketsWithoutInProgres != 1 {
		t.Errorf("missing-in-progress=%d want 1", got.TicketsWithoutInProgres)
	}
}

func TestComputeLeadTime_InsufficientData(t *testing.T) {
	w := defaultWindow()
	f := &fakeJira{issues: nil, changelogs: map[string][]jira.StatusChange{}}
	got, _ := ComputeLeadTime(f, LeadTimeQuery{Window: w, StatusCfg: DefaultJiraStatusConfig()})
	if !got.InsufficientData {
		t.Error("want insufficient_data=true on 0 deploys")
	}
}

func TestComputeLeadTime_ChangelogError(t *testing.T) {
	w := defaultWindow()
	f := &fakeJira{
		issues: []jira.Issue{mkIssue("K-1")},
		logErr: errors.New("net down"),
	}
	_, err := ComputeLeadTime(f, LeadTimeQuery{Window: w, StatusCfg: DefaultJiraStatusConfig()})
	if err == nil {
		t.Error("want err propagated")
	}
}

func TestPercentile(t *testing.T) {
	cases := []struct {
		in   []float64
		p    float64
		want float64
	}{
		{[]float64{1, 2, 3, 4, 5}, 50, 3},
		{[]float64{1, 2, 3, 4, 5}, 0, 1},
		{[]float64{1, 2, 3, 4, 5}, 100, 5},
		{[]float64{10}, 75, 10},
	}
	for _, c := range cases {
		got := percentile(c.in, c.p)
		if math.Abs(got-c.want) > 1e-9 {
			t.Errorf("p%v of %v = %v, want %v", c.p, c.in, got, c.want)
		}
	}
}
