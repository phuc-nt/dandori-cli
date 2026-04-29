package metric

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/phuc-nt/dandori-cli/internal/jira"
)

// stubReworkSrc is a tiny fake for the rework leg of the orchestrator.
// fakeJira already implements deploy + incident; pairing them gives the full
// ExportSources for table-style tests.
type stubReworkSrc struct {
	total  []string
	rework []string
	err    error
}

func (s *stubReworkSrc) TotalRunIDs(_, _ time.Time, _ string) ([]string, error) {
	return s.total, s.err
}
func (s *stubReworkSrc) ReworkRunIDs(_, _ time.Time, _ string) ([]string, error) {
	return s.rework, s.err
}

func defaultExportConfig() ExportConfig {
	return ExportConfig{
		Window:     defaultWindow(),
		StatusCfg:  DefaultJiraStatusConfig(),
		IncidentCf: IncidentMatchConfig{IssueTypes: []string{"Incident"}},
	}
}

func TestRun_HappyPath(t *testing.T) {
	cfg := defaultExportConfig()
	mid := cfg.Window.Start.Add(7 * 24 * time.Hour)
	jiraSrc := &fakeCFRSource{
		fakeJira: &fakeJira{
			issues: []jira.Issue{mkIssue("D-1"), mkIssue("D-2")},
			changelogs: map[string][]jira.StatusChange{
				"D-1": {mkChange("In Progress", mid.Add(-time.Hour)), mkChange("Released", mid)},
				"D-2": {mkChange("In Progress", mid.Add(-2*time.Hour)), mkChange("Released", mid.Add(time.Hour))},
			},
		},
		incidents: []jira.Incident{
			{Key: "I-1", CreatedAt: mid, ResolvedAt: resolved(mid.Add(time.Hour))},
		},
	}
	rework := &stubReworkSrc{
		total:  []string{"r1", "r2", "r3", "r4", "r5"},
		rework: []string{"r1"},
	}

	rep, err := Run(ExportSources{Jira: jiraSrc, Rework: rework}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Deploy.Count != 2 {
		t.Errorf("deploy count=%d want 2", rep.Deploy.Count)
	}
	if rep.LeadTime.SamplesUsed != 2 {
		t.Errorf("lead samples=%d want 2", rep.LeadTime.SamplesUsed)
	}
	if rep.CFR.Rate != 0.5 {
		t.Errorf("cfr=%v want 0.5", rep.CFR.Rate)
	}
	if rep.Rework.Rate != 0.2 {
		t.Errorf("rework=%v want 0.2", rep.Rework.Rate)
	}
	if len(rep.InsufficientData) != 0 {
		t.Errorf("expected no insufficient flags, got %v", rep.InsufficientData)
	}
}

func TestRun_NoIncidentConfigSkipsCFRMTTR(t *testing.T) {
	cfg := defaultExportConfig()
	cfg.IncidentCf = IncidentMatchConfig{} // empty
	jiraSrc := &fakeCFRSource{fakeJira: &fakeJira{}}
	rep, err := Run(ExportSources{Jira: jiraSrc, Rework: &stubReworkSrc{}}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(rep.InsufficientData, "change_failure_rate") || !contains(rep.InsufficientData, "time_to_restore_service") {
		t.Errorf("expected cfr+mttr in insufficient_data, got %v", rep.InsufficientData)
	}
}

func TestRun_DeployErrorPropagates(t *testing.T) {
	cfg := defaultExportConfig()
	jiraSrc := &fakeCFRSource{fakeJira: &fakeJira{searchErr: errors.New("boom")}}
	_, err := Run(ExportSources{Jira: jiraSrc, Rework: &stubReworkSrc{}}, cfg)
	if err == nil {
		t.Error("want err on deploy failure")
	}
}

func TestRun_NilReworkAddsInsufficient(t *testing.T) {
	cfg := defaultExportConfig()
	jiraSrc := &fakeCFRSource{fakeJira: &fakeJira{}}
	rep, _ := Run(ExportSources{Jira: jiraSrc, Rework: nil}, cfg)
	if !contains(rep.InsufficientData, "rework_rate") {
		t.Errorf("expected rework_rate in insufficient_data, got %v", rep.InsufficientData)
	}
}

func TestFormat_FarosShape(t *testing.T) {
	rep := minimalReport()
	body, err := FormatReport(rep, FormatFaros)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got["metric_set"] != "dora" || got["source_of_truth"] != "jira" {
		t.Errorf("missing identity fields: %v", got)
	}
	metrics, ok := got["metrics"].(map[string]any)
	if !ok {
		t.Fatalf("missing metrics block")
	}
	for _, k := range []string{"deployment_frequency", "lead_time_for_changes", "change_failure_rate", "time_to_restore_service", "rework_rate"} {
		if _, ok := metrics[k]; !ok {
			t.Errorf("missing metric %q", k)
		}
	}
}

func TestFormat_OobeyaSixLayers(t *testing.T) {
	body, err := FormatReport(minimalReport(), FormatOobeya)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(body, &got)
	layers := got["layers"].(map[string]any)
	want := []string{"productivity", "delivery", "quality", "reliability", "adoption", "roi"}
	for _, k := range want {
		if _, ok := layers[k]; !ok {
			t.Errorf("missing layer %q", k)
		}
	}
}

func TestFormat_RawIncludesJiraConfig(t *testing.T) {
	rep := minimalReport()
	rep.Config.IncidentCf = IncidentMatchConfig{IssueTypes: []string{"Incident"}, Labels: []string{"prod-bug"}}
	rep.Config.JQLExtra = "AND project = PAY"
	body, _ := FormatReport(rep, FormatRaw)
	var got map[string]any
	_ = json.Unmarshal(body, &got)
	jc, ok := got["jira_config"].(map[string]any)
	if !ok {
		t.Fatal("missing jira_config")
	}
	if jc["jql_extra"] != "AND project = PAY" {
		t.Errorf("jql_extra not echoed: %v", jc["jql_extra"])
	}
}

func TestFormat_Unknown(t *testing.T) {
	_, err := FormatReport(minimalReport(), Format("zzz"))
	if err == nil {
		t.Error("want err on unknown format")
	}
}

func TestFormat_NullableInsufficient(t *testing.T) {
	rep := minimalReport()
	rep.Deploy.InsufficientData = true
	rep.Deploy.PerDay = 0
	body, _ := FormatReport(rep, FormatFaros)
	if !strings.Contains(string(body), `"value": null`) {
		t.Errorf("expected null for insufficient deploy_freq, got: %s", body)
	}
}

func TestParseSinceFlag(t *testing.T) {
	now := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		in      string
		want    time.Time
		wantErr bool
	}{
		{"now", now, false},
		{"28d", now.AddDate(0, 0, -28), false},
		{"7d", now.AddDate(0, 0, -7), false},
		{"2026-04-01", time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC), false},
		{"", time.Time{}, true},
		{"junk", time.Time{}, true},
		{"-1d", time.Time{}, true},
	}
	for _, c := range cases {
		got, err := ParseSinceFlag(c.in, now)
		if c.wantErr {
			if err == nil {
				t.Errorf("%q want err, got %v", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q unexpected err: %v", c.in, err)
			continue
		}
		if !got.Equal(c.want) {
			t.Errorf("%q got %v want %v", c.in, got, c.want)
		}
	}
}

func minimalReport() ExportReport {
	return ExportReport{
		Config:      defaultExportConfig(),
		GeneratedAt: time.Date(2026, 4, 29, 0, 0, 0, 0, time.UTC),
	}
}

func contains(s []string, target string) bool {
	for _, v := range s {
		if v == target {
			return true
		}
	}
	return false
}
