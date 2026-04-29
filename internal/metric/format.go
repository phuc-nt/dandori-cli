package metric

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Format identifies a wire shape for the export. Faros = compact DORA
// schema; Oobeya = 6-layer mapping; Raw = full report incl. Jira config.
type Format string

const (
	FormatFaros  Format = "faros"
	FormatOobeya Format = "oobeya"
	FormatRaw    Format = "raw"
)

// FormatReport encodes rep into the requested format. Returned JSON is
// indented for human-readable output; consumers can re-encode if needed.
func FormatReport(rep ExportReport, f Format) ([]byte, error) {
	switch f {
	case FormatFaros, "":
		return json.MarshalIndent(buildFaros(rep), "", "  ")
	case FormatOobeya:
		return json.MarshalIndent(buildOobeya(rep), "", "  ")
	case FormatRaw:
		return json.MarshalIndent(buildRaw(rep), "", "  ")
	default:
		return nil, fmt.Errorf("unknown format %q (want faros|oobeya|raw)", f)
	}
}

func buildFaros(r ExportReport) map[string]any {
	period := map[string]string{
		"start": r.Config.Window.Start.UTC().Format(time.RFC3339),
		"end":   r.Config.Window.End.UTC().Format(time.RFC3339),
	}
	metrics := map[string]any{
		"deployment_frequency": map[string]any{
			"value":   nullableFloat(r.Deploy.PerDay, r.Deploy.InsufficientData),
			"unit":    "per_day",
			"samples": r.Deploy.Count,
		},
		"lead_time_for_changes": map[string]any{
			"p50_seconds": nullableFloat(r.LeadTime.P50Seconds, r.LeadTime.InsufficientData),
			"p75_seconds": nullableFloat(r.LeadTime.P75Seconds, r.LeadTime.InsufficientData),
			"p90_seconds": nullableFloat(r.LeadTime.P90Seconds, r.LeadTime.InsufficientData),
			"samples":     r.LeadTime.SamplesUsed,
		},
		"change_failure_rate": map[string]any{
			"value":     nullableFloat(r.CFR.Rate, r.CFR.InsufficientData),
			"unit":      "ratio",
			"deploys":   r.CFR.DeployCount,
			"incidents": r.CFR.IncidentCount,
		},
		"time_to_restore_service": map[string]any{
			"p50_seconds": nullableFloat(r.MTTR.P50Seconds, r.MTTR.InsufficientData),
			"p90_seconds": nullableFloat(r.MTTR.P90Seconds, r.MTTR.InsufficientData),
			"samples":     r.MTTR.SamplesUsed,
			"ongoing":     r.MTTR.OngoingIncidents,
		},
		"rework_rate": map[string]any{
			"value":             nullableFloat(r.Rework.Rate, r.Rework.InsufficientData),
			"unit":              "ratio",
			"samples":           r.Rework.TotalCount,
			"exceeds_threshold": r.Rework.ExceedsThreshold,
			"threshold":         r.Rework.Threshold,
			"threshold_version": r.Rework.ThresholdVersion,
		},
	}
	insuff := r.InsufficientData
	if r.Config.IncludeAttribution && r.Attribution != nil && r.Attribution.InsufficientData {
		already := false
		for _, s := range insuff {
			if s == "task_attribution" {
				already = true
				break
			}
		}
		if !already {
			insuff = append(insuff, "task_attribution")
		}
	}
	dataQuality := map[string]any{
		"insufficient_data":            stringListOrEmpty(insuff),
		"tickets_without_in_progress":  r.LeadTime.TicketsWithoutInProgres,
		"warnings":                     stringListOrEmpty(r.Warnings),
		"human_jira_update_assumption": "metrics rely on humans transitioning Jira status promptly",
	}
	out := map[string]any{
		"metric_set":      "dora",
		"version":         "1.0",
		"source_of_truth": "jira",
		"generated_at":    r.GeneratedAt.UTC().Format(time.RFC3339),
		"period":          period,
		"metrics":         metrics,
		"data_quality":    dataQuality,
	}
	if r.Config.Team != "" {
		out["team"] = r.Config.Team
	}
	if r.Config.IncludeAttribution {
		out["task_attribution"] = attributionBlock(r.Attribution)
	}
	return out
}

// attributionBlock projects the AttributionResult into the wire shape, or
// nil when data is insufficient (consumer-safe — dashboards render N/A).
func attributionBlock(a *AttributionResult) any {
	if a == nil || a.InsufficientData {
		return nil
	}
	return map[string]any{
		"tasks_total":                    a.TasksTotal,
		"tasks_with_session":             a.TasksWithSession,
		"agent_autonomy_rate":            a.AgentAutonomyRate,
		"agent_code_retention_p50":       a.RetentionP50,
		"agent_code_retention_p90":       a.RetentionP90,
		"intervention_rate_p50":          a.InterventionRateP50,
		"iterations_p50":                 a.IterationsP50,
		"iterations_p90":                 a.IterationsP90,
		"cost_per_retained_line_usd_p50": a.CostPerRetainedLineP50,
		"session_outcomes":               a.SessionOutcomes,
	}
}

func buildOobeya(r ExportReport) map[string]any {
	faros := buildFaros(r)
	metrics := faros["metrics"].(map[string]any)
	productivity := map[string]any{"deployment_frequency": metrics["deployment_frequency"]}
	if r.Config.IncludeAttribution {
		productivity["task_attribution"] = attributionBlock(r.Attribution)
	}
	layers := map[string]any{
		"productivity": productivity,
		"delivery":     map[string]any{"lead_time_for_changes": metrics["lead_time_for_changes"]},
		"quality": map[string]any{
			"change_failure_rate": metrics["change_failure_rate"],
			"rework_rate":         metrics["rework_rate"],
		},
		"reliability": map[string]any{"time_to_restore_service": metrics["time_to_restore_service"]},
		"adoption":    map[string]any{},
		"roi":         map[string]any{},
	}
	return map[string]any{
		"metric_set":      "dora",
		"version":         "1.0",
		"source_of_truth": "jira",
		"generated_at":    faros["generated_at"],
		"period":          faros["period"],
		"team":            r.Config.Team,
		"layers":          layers,
		"data_quality":    faros["data_quality"],
	}
}

func buildRaw(r ExportReport) map[string]any {
	jiraCfg := map[string]any{
		"release_status_names":     r.Config.StatusCfg.ReleaseStatusNames,
		"in_progress_status_names": r.Config.StatusCfg.InProgressStatusNames,
		"incident_issue_types":     r.Config.IncidentCf.IssueTypes,
		"incident_labels":          r.Config.IncidentCf.Labels,
		"jql_extra":                r.Config.JQLExtra,
	}
	out := map[string]any{
		"generated_at":      r.GeneratedAt.UTC().Format(time.RFC3339),
		"window":            r.Config.Window,
		"team":              r.Config.Team,
		"jira_config":       jiraCfg,
		"deploy":            r.Deploy,
		"lead_time":         r.LeadTime,
		"cfr":               r.CFR,
		"mttr":              r.MTTR,
		"rework":            r.Rework,
		"insufficient_data": stringListOrEmpty(r.InsufficientData),
		"warnings":          stringListOrEmpty(r.Warnings),
	}
	if r.Config.IncludeAttribution {
		out["task_attribution"] = attributionBlock(r.Attribution)
	}
	return out
}

// nullableFloat returns nil when insufficient → JSON marshals as null.
// This is consumer-safe: dashboards that gate on nil show "N/A" rather
// than charting a misleading 0.
func nullableFloat(v float64, insufficient bool) any {
	if insufficient {
		return nil
	}
	return v
}

func stringListOrEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// ParseSinceFlag accepts either "<N>d" (relative days back) or RFC3339
// date (e.g. 2026-04-01) and returns the corresponding start time.
// "now" returns now in UTC. Empty string is rejected.
func ParseSinceFlag(s string, now time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty since")
	}
	if s == "now" {
		return now.UTC(), nil
	}
	if strings.HasSuffix(s, "d") {
		var days int
		if _, err := fmt.Sscanf(s, "%dd", &days); err != nil || days <= 0 {
			return time.Time{}, fmt.Errorf("invalid relative window %q (want e.g. 28d)", s)
		}
		return now.UTC().AddDate(0, 0, -days), nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("unrecognised time %q (want Nd, YYYY-MM-DD, or RFC3339)", s)
}
