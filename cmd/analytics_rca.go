// cmd/analytics_rca.go — dandori analytics rca subcommand (v0.11 Phase 02).
//
// Usage:
//
//	dandori analytics rca [--since 28d] [--format table|json]
//
// Prints a ranked breakdown of rework root causes with WoW delta and
// top contributing agent/task-type. `--since` accepts an integer number
// of days (with or without trailing "d").
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

var analyticsRcaCmd = &cobra.Command{
	Use:   "rca",
	Short: "Structured root-cause analysis of rework events",
	Long: `Aggregate rework causes from task_attribution.session_outcomes and rank
by frequency. Shows week-over-week delta, top contributing agent, and top
task type per cause.

Examples:
  dandori analytics rca
  dandori analytics rca --since 90d
  dandori analytics rca --format json`,
	RunE: runAnalyticsRca,
}

var analyticsRcaSince int

func init() {
	analyticsCmd.AddCommand(analyticsRcaCmd)
	analyticsRcaCmd.Flags().IntVar(&analyticsRcaSince, "since", 28, "Window in days")
	analyticsRcaCmd.Flags().StringVar(&analyticsFormat, "format", "table", "Output format: table, json")
}

func runAnalyticsRca(_ *cobra.Command, _ []string) error {
	store, err := getLocalDB()
	if err != nil {
		return err
	}
	defer store.Close()

	since := time.Now().AddDate(0, 0, -analyticsRcaSince)
	rows, err := store.GetRcaBreakdown(since)
	if err != nil {
		return fmt.Errorf("rca breakdown: %w", err)
	}

	if len(rows) == 0 {
		fmt.Printf("No rework events in last %d days — try a wider --since window.\n", analyticsRcaSince)
		return nil
	}

	if analyticsFormat == "json" {
		return json.NewEncoder(os.Stdout).Encode(rows)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "RCA Breakdown (last %d days)\n\n", analyticsRcaSince)
	fmt.Fprintln(w, "CAUSE\tCOUNT\tPCT\tWoW Δ\tTOP AGENT\tTOP TYPE")
	fmt.Fprintln(w, "-----\t-----\t---\t------\t---------\t--------")
	for _, r := range rows {
		if r.Count == 0 {
			continue
		}
		wow := formatWoW(r.WoWDelta)
		agent := r.TopAgent
		if agent == "" {
			agent = "—"
		}
		taskType := r.TopTaskType
		if taskType == "" {
			taskType = "—"
		}
		fmt.Fprintf(w, "%s\t%d\t%.1f%%\t%s\t%s\t%s\n",
			r.Cause, r.Count, r.Pct, wow, agent, taskType)
	}
	return w.Flush()
}

// formatWoW formats a week-over-week delta as "↑ N pp", "↓ N pp", or "—".
func formatWoW(delta float64) string {
	if delta == 0 {
		return "—"
	}
	if delta > 0 {
		return fmt.Sprintf("↑ %.1fpp", delta)
	}
	return fmt.Sprintf("↓ %.1fpp", -delta)
}
