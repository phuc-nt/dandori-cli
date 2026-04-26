package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var (
	analyticsEventsSince int
	analyticsEventsTop   int
	analyticsEventsBy    string
)

var analyticsToolsCmd = &cobra.Command{
	Use:   "tools",
	Short: "Top tools used by agents (Layer-3 events)",
	Long: `Aggregate tool.use / tool.result events from agent sessions.

Examples:
  dandori analytics tools
  dandori analytics tools --top 10 --since 30
  dandori analytics tools --format json`,
	RunE: runAnalyticsTools,
}

var analyticsContextCmd = &cobra.Command{
	Use:   "context",
	Short: "Top Confluence pages read as task context",
	Long: `Aggregate confluence.read events from taskcontext fetcher.

Examples:
  dandori analytics context
  dandori analytics context --top 5 --since 14`,
	RunE: runAnalyticsContext,
}

var analyticsBugsCmd = &cobra.Command{
	Use:   "bugs",
	Short: "Bugs filed per agent / task (Layer-3 bug.filed events)",
	Long: `Aggregate bug.filed events emitted by the bug-link cycle.

Each bug.filed event carries a bug_key + caused_by_run_id. Counts are
DISTINCT per bug_key so the same bug linked via two methods (Jira link
+ description tag) registers once.

Examples:
  dandori analytics bugs                 # by agent
  dandori analytics bugs --by task       # by Jira task key
  dandori analytics bugs --since 30 --format json`,
	RunE: runAnalyticsBugs,
}

var analyticsIterationsCmd = &cobra.Command{
	Use:   "iterations",
	Short: "Average feedback rounds per agent / engineer / sprint",
	Long: `Compute round count per Jira task as 1 + (count of task.iteration.start
events for that issue), then aggregate per group dimension.

Examples:
  dandori analytics iterations
  dandori analytics iterations --by engineer
  dandori analytics iterations --by sprint --format json`,
	RunE: runAnalyticsIterations,
}

func init() {
	analyticsCmd.AddCommand(analyticsToolsCmd)
	analyticsCmd.AddCommand(analyticsContextCmd)
	analyticsCmd.AddCommand(analyticsIterationsCmd)
	analyticsCmd.AddCommand(analyticsBugsCmd)

	analyticsToolsCmd.Flags().IntVar(&analyticsEventsSince, "since", 0, "Window in days (0 = all time)")
	analyticsToolsCmd.Flags().IntVar(&analyticsEventsTop, "top", 20, "Limit to top K tools")
	analyticsToolsCmd.Flags().StringVar(&analyticsFormat, "format", "table", "Output format: table, json")

	analyticsContextCmd.Flags().IntVar(&analyticsEventsSince, "since", 0, "Window in days (0 = all time)")
	analyticsContextCmd.Flags().IntVar(&analyticsEventsTop, "top", 20, "Limit to top K pages")
	analyticsContextCmd.Flags().StringVar(&analyticsFormat, "format", "table", "Output format: table, json")

	analyticsIterationsCmd.Flags().StringVar(&analyticsEventsBy, "by", "agent", "Group by: agent, engineer, sprint")
	analyticsIterationsCmd.Flags().StringVar(&analyticsFormat, "format", "table", "Output format: table, json")

	analyticsBugsCmd.Flags().StringVar(&analyticsEventsBy, "by", "agent", "Group by: agent, task")
	analyticsBugsCmd.Flags().IntVar(&analyticsEventsSince, "since", 0, "Window in days (0 = all time)")
	analyticsBugsCmd.Flags().StringVar(&analyticsFormat, "format", "table", "Output format: table, json")
}

func runAnalyticsTools(cmd *cobra.Command, args []string) error {
	store, err := getLocalDB()
	if err != nil {
		return err
	}
	defer store.Close()

	rows, err := store.ToolUsage(analyticsEventsSince, analyticsEventsTop)
	if err != nil {
		return fmt.Errorf("tool usage: %w", err)
	}
	if len(rows) == 0 {
		fmt.Println("No tool events yet. Run a tracked agent task to populate Layer-3 events.")
		return nil
	}
	if analyticsFormat == "json" {
		return json.NewEncoder(os.Stdout).Encode(rows)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "TOOL\tUSES\tSUCCESS%\tLAST USED")
	fmt.Fprintln(w, "----\t----\t--------\t---------")
	for _, r := range rows {
		successCol := "n/a"
		if r.SuccessRate >= 0 {
			successCol = fmt.Sprintf("%.1f%%", r.SuccessRate)
		}
		fmt.Fprintf(w, "%s\t%d\t%s\t%s\n", r.Tool, r.UseCount, successCol, r.LastUsedAt.Format("2006-01-02 15:04"))
	}
	return w.Flush()
}

func runAnalyticsContext(cmd *cobra.Command, args []string) error {
	store, err := getLocalDB()
	if err != nil {
		return err
	}
	defer store.Close()

	rows, err := store.ContextUsage(analyticsEventsSince, analyticsEventsTop)
	if err != nil {
		return fmt.Errorf("context usage: %w", err)
	}
	if len(rows) == 0 {
		fmt.Println("No confluence.read events yet. Tracked tasks with Confluence links populate this.")
		return nil
	}
	if analyticsFormat == "json" {
		return json.NewEncoder(os.Stdout).Encode(rows)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PAGE\tTITLE\tREADS\tLAST READ")
	fmt.Fprintln(w, "----\t-----\t-----\t---------")
	for _, r := range rows {
		title := r.Title
		if title == "" {
			title = "(untitled)"
		}
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\n", r.PageID, title, r.UseCount, r.LastUsedAt.Format("2006-01-02 15:04"))
	}
	return w.Flush()
}

func runAnalyticsBugs(cmd *cobra.Command, args []string) error {
	store, err := getLocalDB()
	if err != nil {
		return err
	}
	defer store.Close()

	rows, err := store.BugStats(analyticsEventsBy, analyticsEventsSince)
	if err != nil {
		return fmt.Errorf("bug stats: %w", err)
	}
	if len(rows) == 0 {
		fmt.Println("No bug.filed events yet. Bug-link cycle requires Jira Bug tickets with `caused by` links or `caused_by:<runid>` description tags.")
		return nil
	}
	if analyticsFormat == "json" {
		return json.NewEncoder(os.Stdout).Encode(rows)
	}

	header := "AGENT"
	if analyticsEventsBy == "task" {
		header = "TASK"
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "%s\tBUGS\tLAST FILED\n", header)
	fmt.Fprintf(w, "%s\t----\t----------\n", "-----")
	for _, r := range rows {
		key := r.GroupKey
		if key == "" {
			key = "(none)"
		}
		fmt.Fprintf(w, "%s\t%d\t%s\n", key, r.BugCount, r.LastFiled.Format("2006-01-02 15:04"))
	}
	return w.Flush()
}

func runAnalyticsIterations(cmd *cobra.Command, args []string) error {
	store, err := getLocalDB()
	if err != nil {
		return err
	}
	defer store.Close()

	stats, err := store.IterationStats(analyticsEventsBy)
	if err != nil {
		return fmt.Errorf("iteration stats: %w", err)
	}
	if len(stats) == 0 {
		fmt.Println("No iteration data yet. PO must reopen a Done task for the poller to detect a round.")
		return nil
	}
	if analyticsFormat == "json" {
		return json.NewEncoder(os.Stdout).Encode(stats)
	}

	header := "AGENT"
	switch analyticsEventsBy {
	case "engineer":
		header = "ENGINEER"
	case "sprint":
		header = "SPRINT"
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "%s\tAVG ROUND\tMAX ROUND\tTASKS\n", header)
	fmt.Fprintf(w, "%s\t---------\t---------\t-----\n", "-----")
	for _, s := range stats {
		key := s.GroupKey
		if key == "" {
			key = "(none)"
		}
		fmt.Fprintf(w, "%s\t%.2f\t%d\t%d\n", key, s.AvgRound, s.MaxRound, s.TaskCount)
	}
	return w.Flush()
}
