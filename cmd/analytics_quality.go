package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/phuc-nt/dandori-cli/internal/db"
	"github.com/spf13/cobra"
)

var (
	analyticsKPIKind  string // --kpi regression|bugs|cost
	analyticsKPIBy    string // --by agent|engineer|sprint
	analyticsKPISince int    // --since N days
	analyticsKPITop   int    // --top N (cost only)
)

var analyticsKPICmd = &cobra.Command{
	Use:   "kpi",
	Short: "Quality KPIs: regression rate, bug rate, quality-adjusted cost",
	Long: `Composite quality KPIs joining Layer-3 events with run cost.

KPIs:
  regression  - % of tasks the PO reopened (task.iteration.start events)
  bugs        - bugs per run (DISTINCT bug_key from bug.filed events)
  cost        - per-task cost with iteration + bug counts

Examples:
  dandori analytics kpi
  dandori analytics kpi --kpi bugs --by engineer
  dandori analytics kpi --kpi cost --by sprint --top 20
  dandori analytics kpi --kpi regression --since 30 --format json`,
	RunE: runAnalyticsKPI,
}

func init() {
	analyticsCmd.AddCommand(analyticsKPICmd)
	analyticsKPICmd.Flags().StringVar(&analyticsKPIKind, "kpi", "regression", "KPI to show: regression, bugs, cost")
	analyticsKPICmd.Flags().StringVar(&analyticsKPIBy, "by", "agent", "Group by: agent, engineer, sprint")
	analyticsKPICmd.Flags().IntVar(&analyticsKPISince, "since", 0, "Window in days (0 = all time)")
	analyticsKPICmd.Flags().IntVar(&analyticsKPITop, "top", 50, "Limit rows (cost KPI only)")
	analyticsKPICmd.Flags().StringVar(&analyticsFormat, "format", "table", "Output format: table, json")
}

// dimLabel returns the uppercase column header for the --by dimension.
func dimLabel(by string) string {
	switch strings.ToLower(by) {
	case "engineer":
		return "ENGINEER"
	case "sprint":
		return "SPRINT"
	default:
		return "AGENT"
	}
}

func runAnalyticsKPI(cmd *cobra.Command, args []string) error {
	store, err := getLocalDB()
	if err != nil {
		return err
	}
	defer store.Close()

	switch analyticsKPIKind {
	case "regression":
		return runKPIRegression(store)
	case "bugs":
		return runKPIBugs(store)
	case "cost":
		return runKPICost(store)
	default:
		return fmt.Errorf("unknown --kpi %q (want regression|bugs|cost)", analyticsKPIKind)
	}
}

func runKPIRegression(store *db.LocalDB) error {
	rows, err := store.RegressionRate(analyticsKPIBy, analyticsKPISince)
	if err != nil {
		return fmt.Errorf("regression rate: %w", err)
	}
	if len(rows) == 0 {
		fmt.Println("No regression data yet. Tasks need task.iteration.start events (PO reopen cycle).")
		return nil
	}
	if analyticsFormat == "json" {
		return json.NewEncoder(os.Stdout).Encode(rows)
	}
	dim := dimLabel(analyticsKPIBy)
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "%s\tTASKS\tREGRESSED\tREGRESSION%%\n", dim)
	fmt.Fprintf(w, "%s\t-----\t---------\t-----------\n", strings.Repeat("-", len(dim)))
	for _, r := range rows {
		key := r.GroupKey
		if key == "" {
			key = "(none)"
		}
		fmt.Fprintf(w, "%s\t%d\t%d\t%.1f%%\n", key, r.TotalTasks, r.RegressedTasks, r.RegressionPct)
	}
	return w.Flush()
}

func runKPIBugs(store *db.LocalDB) error {
	rows, err := store.BugRate(analyticsKPIBy, analyticsKPISince)
	if err != nil {
		return fmt.Errorf("bug rate: %w", err)
	}
	if len(rows) == 0 {
		fmt.Println("No bug rate data yet. Bug-link cycle requires bug.filed events.")
		return nil
	}
	if analyticsFormat == "json" {
		return json.NewEncoder(os.Stdout).Encode(rows)
	}
	dim := dimLabel(analyticsKPIBy)
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "%s\tRUNS\tBUGS\tBUGS/RUN\n", dim)
	fmt.Fprintf(w, "%s\t----\t----\t--------\n", strings.Repeat("-", len(dim)))
	for _, r := range rows {
		key := r.GroupKey
		if key == "" {
			key = "(none)"
		}
		fmt.Fprintf(w, "%s\t%d\t%d\t%.2f\n", key, r.Runs, r.Bugs, r.BugsPerRun)
	}
	return w.Flush()
}

func runKPICost(store *db.LocalDB) error {
	rows, err := store.QualityAdjustedCost(analyticsKPIBy, analyticsKPISince, analyticsKPITop)
	if err != nil {
		return fmt.Errorf("quality adjusted cost: %w", err)
	}
	if len(rows) == 0 {
		fmt.Println("No cost data yet. Runs need jira_issue_key to appear here.")
		return nil
	}
	if analyticsFormat == "json" {
		return json.NewEncoder(os.Stdout).Encode(rows)
	}
	dim := dimLabel(analyticsKPIBy)
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "TASK\t%s\tCOST\tRUNS\tITERATIONS\tBUGS\tCLEAN\n", dim)
	fmt.Fprintf(w, "----\t%s\t----\t----\t----------\t----\t-----\n", strings.Repeat("-", len(dim)))
	for _, r := range rows {
		key := r.GroupKey
		if key == "" {
			key = "(none)"
		}
		clean := "no"
		if r.IsClean {
			clean = "yes"
		}
		fmt.Fprintf(w, "%s\t%s\t$%.4f\t%d\t%d\t%d\t%s\n",
			r.IssueKey, key, r.TotalCostUSD, r.RunCount, r.IterationCount, r.BugCount, clean)
	}
	return w.Flush()
}
