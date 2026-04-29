package metric

import (
	"fmt"
	"time"

	"github.com/phuc-nt/dandori-cli/internal/jira"
)

// ExportConfig captures all knobs that drive a single export run. Built from
// CLI flags + dandori config.yaml; passed unchanged to the orchestrator so
// reproducibility lives in one place (raw format echoes this back).
type ExportConfig struct {
	Window     MetricWindow
	Team       string
	JQLExtra   string
	StatusCfg  JiraStatusConfig
	IncidentCf IncidentMatchConfig
	MaxResults int
}

// ExportSources is the set of data dependencies the orchestrator needs.
// Splitting Jira (DORA + CFR + MTTR) from ReworkSrc (LocalDB-backed)
// lets tests and the CLI wire fakes/real clients independently.
type ExportSources struct {
	Jira   jiraCFRSource // includes deploy + incident
	Rework reworkSource  // LocalDB
}

// ExportReport is the in-memory aggregate of all 5 metrics for one run.
// Formatters (Faros/Oobeya/Raw) project this into their wire format —
// they never re-fetch.
type ExportReport struct {
	Config           ExportConfig
	GeneratedAt      time.Time
	Deploy           DeployFreqResult
	LeadTime         LeadTimeResult
	CFR              CFRResult
	MTTR             MTTRResult
	Rework           ReworkResult
	InsufficientData []string // metric IDs that lacked data (faros + oobeya consume)
	Warnings         []string
}

// Run executes the 5 compute functions sequentially. Sequential by choice —
// errors propagate cleanly and the bottleneck is the per-issue changelog
// fetch (which the deploy/lead share); parallelizing would only save the
// rework + cfr/mttr legs (~30%) at the cost of error-handling complexity.
// Phase 06 benchmarks; can revisit then.
func Run(src ExportSources, cfg ExportConfig) (ExportReport, error) {
	if !cfg.Window.End.After(cfg.Window.Start) {
		return ExportReport{}, fmt.Errorf("invalid window")
	}
	rep := ExportReport{Config: cfg, GeneratedAt: time.Now().UTC()}

	deployQ := DeployQuery{
		Window: cfg.Window, StatusCfg: cfg.StatusCfg,
		JQLExtra: cfg.JQLExtra, MaxResults: cfg.MaxResults,
	}
	incidentQ := IncidentQuery{
		Window: cfg.Window, Match: cfg.IncidentCf,
		JQLExtra: cfg.JQLExtra, MaxResults: cfg.MaxResults,
	}
	reworkQ := ReworkQuery{Window: cfg.Window, Filter: TeamFilter{Team: cfg.Team}}

	deploy, err := ComputeDeployFreq(src.Jira, deployQ)
	if err != nil {
		return rep, fmt.Errorf("deploy_freq: %w", err)
	}
	rep.Deploy = deploy
	if deploy.InsufficientData {
		rep.InsufficientData = append(rep.InsufficientData, "deployment_frequency")
	}

	lead, err := ComputeLeadTime(src.Jira, deployQ)
	if err != nil {
		return rep, fmt.Errorf("lead_time: %w", err)
	}
	rep.LeadTime = lead
	if lead.InsufficientData {
		rep.InsufficientData = append(rep.InsufficientData, "lead_time_for_changes")
	}
	if lead.TicketsWithoutInProgres > 0 {
		rep.Warnings = append(rep.Warnings, fmt.Sprintf(
			"%d tickets deployed without 'In Progress' transition (skipped from lead time)",
			lead.TicketsWithoutInProgres))
	}

	// CFR + MTTR only run if incident config is set; otherwise skip cleanly.
	if len(cfg.IncidentCf.IssueTypes) > 0 || len(cfg.IncidentCf.Labels) > 0 {
		cfr, err := ComputeChangeFailureRate(src.Jira, deployQ, incidentQ)
		if err != nil {
			return rep, fmt.Errorf("cfr: %w", err)
		}
		rep.CFR = cfr
		if cfr.InsufficientData {
			rep.InsufficientData = append(rep.InsufficientData, "change_failure_rate")
		}

		mttr, err := ComputeTimeToRestore(src.Jira, incidentQ)
		if err != nil {
			return rep, fmt.Errorf("mttr: %w", err)
		}
		rep.MTTR = mttr
		if mttr.InsufficientData {
			rep.InsufficientData = append(rep.InsufficientData, "time_to_restore_service")
		}
	} else {
		rep.Warnings = append(rep.Warnings, "incident config empty: cfr + mttr skipped")
		rep.InsufficientData = append(rep.InsufficientData, "change_failure_rate", "time_to_restore_service")
		rep.CFR.InsufficientData = true
		rep.MTTR.InsufficientData = true
		rep.CFR.Window = cfg.Window
		rep.MTTR.Window = cfg.Window
	}

	if src.Rework != nil {
		rework, err := ComputeReworkRate(src.Rework, reworkQ)
		if err != nil {
			return rep, fmt.Errorf("rework: %w", err)
		}
		rep.Rework = rework
		if rework.InsufficientData {
			rep.InsufficientData = append(rep.InsufficientData, "rework_rate")
		}
	} else {
		rep.InsufficientData = append(rep.InsufficientData, "rework_rate")
		rep.Rework.InsufficientData = true
		rep.Rework.Window = cfg.Window
	}

	return rep, nil
}

// realJiraSource adapts *jira.Client to the orchestrator interfaces.
// Adapter exists so the CLI doesn't expose private interface names to
// downstream packages.
type realJiraSource struct{ *jira.Client }

func NewJiraSource(c *jira.Client) jiraCFRSource { return realJiraSource{c} }
