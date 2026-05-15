// cmd/analytics_pr_cycle.go — dandori analytics pr-cycle subcommand (v0.13).
//
// Usage:
//
//	dandori analytics pr-cycle [--days 28] [--format table|json]
//
// Prints PR Review Cycle Time: median + p75 hours from PR submission to
// first APPROVED review, over PRs merged in the rolling window. Diagnostic
// only — used by framework §8 to disambiguate Trust+Deploy quadrants.
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var analyticsPRCycleCmd = &cobra.Command{
	Use:   "pr-cycle",
	Short: "PR Review Cycle Time (median + p75 first-approval latency)",
	Long: `Compute PR Review Cycle Time — diagnostic metric measuring time
from PR submission to first APPROVED review.

Empty state ("no data") means either no merged PRs in window, or none
of them had an approving review (solo engineer / auto-merge teams).

Examples:
  dandori analytics pr-cycle
  dandori analytics pr-cycle --days 56
  dandori analytics pr-cycle --format json`,
	RunE: runAnalyticsPRCycle,
}

var (
	analyticsPRCycleDays int
	analyticsPRCycleRepo string
)

func init() {
	analyticsCmd.AddCommand(analyticsPRCycleCmd)
	analyticsPRCycleCmd.Flags().IntVar(&analyticsPRCycleDays, "days", 28, "Lookback window in days (default 28)")
	analyticsPRCycleCmd.Flags().StringVar(&analyticsPRCycleRepo, "repo", "", "Scope to a single repo (owner/name)")
}

func runAnalyticsPRCycle(_ *cobra.Command, _ []string) error {
	store, err := getLocalDB()
	if err != nil {
		return err
	}
	defer store.Close()

	res, err := store.GetPRReviewCycleTimeByRepo(analyticsPRCycleDays, analyticsPRCycleRepo)
	if err != nil {
		return fmt.Errorf("pr-cycle: %w", err)
	}

	if analyticsFormat == "json" {
		return json.NewEncoder(os.Stdout).Encode(res)
	}

	if !res.HasData {
		fmt.Printf("PR Review Cycle Time: no data in last %d days.\n", res.WindowDays)
		if res.MergedTotal > 0 {
			fmt.Printf("  %d PR(s) merged but none had an approving review.\n", res.MergedTotal)
			fmt.Println("  (Solo engineer or auto-merge team — review latency not applicable.)")
		} else {
			fmt.Println("  No merged PRs found. Run `dandori sync --github-only` to ingest GitHub.")
		}
		return nil
	}

	fmt.Printf("PR Review Cycle Time (last %d days)\n\n", res.WindowDays)

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "METRIC\tVALUE")
	fmt.Fprintln(w, "------\t-----")
	fmt.Fprintf(w, "Median (p50)\t%.1f h\n", res.MedianHours)
	fmt.Fprintf(w, "p75\t%.1f h\n", res.P75Hours)
	fmt.Fprintf(w, "Coverage\t%d / %d merged PRs reviewed\n", res.WithApproval, res.MergedTotal)
	if res.HasLinesData {
		fmt.Fprintf(w, "Median lines changed\t%d (additions + deletions)\n", res.MedianLinesChanged)
	}
	return w.Flush()
}
