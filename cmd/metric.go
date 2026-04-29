package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/phuc-nt/dandori-cli/internal/db"
	"github.com/phuc-nt/dandori-cli/internal/metric"
	"github.com/spf13/cobra"
)

var metricCmd = &cobra.Command{
	Use:   "metric",
	Short: "DORA + Rework Rate metrics from Jira + local events",
	Long: `Compute and export team-level engineering metrics.

Single source of truth = Jira (status transitions for deploys, issuetype/labels
for incidents) + local SQLite (Layer-3 task.iteration events for rework).

Examples:
  dandori metric export --format faros --since 28d
  dandori metric export --format oobeya --since 2026-04-01 --team payments
  dandori metric export --format raw --output report.json`,
}

var metricExportCmd = &cobra.Command{
	Use:           "export",
	Short:         "Export DORA + Rework metrics for a window",
	RunE:          runMetricExport,
	SilenceUsage:  true,
	SilenceErrors: true,
}

var (
	metricFormat             string
	metricSince              string
	metricUntil              string
	metricTeam               string
	metricOutput             string
	metricMaxResults         int
	metricIncludeAttribution bool
)

func init() {
	rootCmd.AddCommand(metricCmd)
	metricCmd.AddCommand(metricExportCmd)

	metricExportCmd.Flags().StringVar(&metricFormat, "format", "raw", "Output format: faros|oobeya|raw")
	metricExportCmd.Flags().StringVar(&metricSince, "since", "28d", "Window start: e.g. 28d, 2026-04-01, RFC3339")
	metricExportCmd.Flags().StringVar(&metricUntil, "until", "now", "Window end: 'now', YYYY-MM-DD, or RFC3339")
	metricExportCmd.Flags().StringVar(&metricTeam, "team", "", "Filter by team/department (empty = all)")
	metricExportCmd.Flags().StringVar(&metricOutput, "output", "stdout", "Output: 'stdout' or path to write")
	metricExportCmd.Flags().IntVar(&metricMaxResults, "max-results", 200, "Max Jira issues per query")
	metricExportCmd.Flags().BoolVar(&metricIncludeAttribution, "include-attribution", false, "Include per-task agent vs human attribution block")
}

func runMetricExport(cmd *cobra.Command, args []string) error {
	now := time.Now().UTC()
	start, err := metric.ParseSinceFlag(metricSince, now)
	if err != nil {
		return fmt.Errorf("--since: %w", err)
	}
	end, err := metric.ParseSinceFlag(metricUntil, now)
	if err != nil {
		return fmt.Errorf("--until: %w", err)
	}
	if !end.After(start) {
		return fmt.Errorf("--until (%s) must be after --since (%s)",
			end.Format(time.RFC3339), start.Format(time.RFC3339))
	}

	jc, err := getJiraClient()
	if err != nil {
		return fmt.Errorf("jira: %w (check config: jira.base_url, user, token)", err)
	}

	cfg := buildExportConfig(start, end)

	store, err := db.Open("")
	if err != nil {
		return fmt.Errorf("local db: %w", err)
	}
	defer store.Close()
	if err := store.Migrate(); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	cfg.IncludeAttribution = metricIncludeAttribution

	src := metric.ExportSources{
		Jira:        metric.NewJiraSource(jc),
		Rework:      store,
		Attribution: store,
	}

	rep, err := metric.Run(src, cfg)
	if err != nil {
		return fmt.Errorf("export: %w", err)
	}

	body, err := metric.FormatReport(rep, metric.Format(metricFormat))
	if err != nil {
		return err
	}

	if metricOutput == "stdout" || metricOutput == "" {
		_, err = os.Stdout.Write(append(body, '\n'))
		return err
	}
	return os.WriteFile(metricOutput, body, 0o644)
}

// buildExportConfig merges CLI flags with config.yaml. CLI flags only
// override window + team + format (they're per-invocation). Status names
// and incident match come from config.yaml; missing values fall back to
// metric.DefaultJiraStatusConfig (release/in-progress) — incident config
// has NO default to force operators to opt in for CFR/MTTR.
func buildExportConfig(start, end time.Time) metric.ExportConfig {
	c := Config()
	statusCfg := metric.DefaultJiraStatusConfig()
	var incidentCfg metric.IncidentMatchConfig
	jqlExtra := ""

	if c != nil {
		mc := c.Metric
		if len(mc.ReleaseStatusNames) > 0 {
			statusCfg.ReleaseStatusNames = mc.ReleaseStatusNames
		}
		if len(mc.InProgressStatusNames) > 0 {
			statusCfg.InProgressStatusNames = mc.InProgressStatusNames
		}
		incidentCfg = metric.IncidentMatchConfig{
			IssueTypes: mc.IncidentIssueTypes,
			Labels:     mc.IncidentLabels,
		}
		jqlExtra = mc.JQLExtra
	}

	return metric.ExportConfig{
		Window:     metric.MetricWindow{Start: start, End: end},
		Team:       metricTeam,
		JQLExtra:   jqlExtra,
		StatusCfg:  statusCfg,
		IncidentCf: incidentCfg,
		MaxResults: metricMaxResults,
	}
}
