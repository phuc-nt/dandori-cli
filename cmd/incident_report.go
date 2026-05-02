package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/phuc-nt/dandori-cli/internal/db"
	"github.com/phuc-nt/dandori-cli/internal/intent"
	"github.com/spf13/cobra"
)

var (
	incidentRunID string
	incidentTask  string
	incidentOut   string
	incidentSince string
)

var incidentReportCmd = &cobra.Command{
	Use:   "incident-report",
	Short: "Generate a markdown post-mortem report for a run or Jira task",
	Long: `Aggregate intent, decisions, diff stats, tool usage, and quality metrics
into a single markdown incident report for root-cause analysis (RCA).

Exactly one of --run or --task is required.

Examples:
  dandori incident-report --run a1b2c3d4
  dandori incident-report --task CLITEST-42
  dandori incident-report --task CLITEST-42 --since 2026-04-01
  dandori incident-report --run a1b2c3d4 --out reports/rca.md`,
	RunE: runIncidentReport,
}

func init() {
	incidentReportCmd.Flags().StringVar(&incidentRunID, "run", "", "Run ID (UUID) for single-run report")
	incidentReportCmd.Flags().StringVar(&incidentTask, "task", "", "Jira issue key — reports over all runs for that task")
	incidentReportCmd.Flags().StringVar(&incidentOut, "out", "", "Write markdown to file instead of stdout")
	incidentReportCmd.Flags().StringVar(&incidentSince, "since", "", "With --task: limit to runs on or after this date (YYYY-MM-DD)")
	rootCmd.AddCommand(incidentReportCmd)
}

func runIncidentReport(cmd *cobra.Command, args []string) error {
	// Validate: exactly one of --run / --task.
	if incidentRunID == "" && incidentTask == "" {
		return fmt.Errorf("one of --run or --task is required")
	}
	if incidentRunID != "" && incidentTask != "" {
		return fmt.Errorf("--run and --task are mutually exclusive")
	}

	store, err := getLocalDB()
	if err != nil {
		return err
	}
	defer store.Close()

	var markdown string

	if incidentRunID != "" {
		markdown, err = buildSingleRunReport(store, incidentRunID)
	} else {
		markdown, err = buildTaskReport(store, incidentTask, incidentSince)
	}
	if err != nil {
		return err
	}

	return writeOutput(markdown, incidentOut)
}

// buildSingleRunReport loads all data for one run and renders the report.
func buildSingleRunReport(store *db.LocalDB, runID string) (string, error) {
	data, err := loadRunData(store, runID)
	if err != nil {
		return "", err
	}
	return intent.BuildRunReport(data), nil
}

// parseSinceDateImpl parses an optional YYYY-MM-DD string into a time.Time.
// Empty string returns zero time (no filter). Exported name uses Impl suffix
// so the test package can call it without re-declaring a local wrapper.
func parseSinceDateImpl(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid --since date %q: want YYYY-MM-DD", s)
	}
	return t, nil
}

// buildTaskReport loads all runs for a Jira key and renders the multi-run report.
func buildTaskReport(store *db.LocalDB, jiraKey, sinceStr string) (string, error) {
	since, err := parseSinceDateImpl(sinceStr)
	if err != nil {
		return "", err
	}

	runs, err := store.GetRunsForJiraKey(jiraKey, since)
	if err != nil {
		return "", fmt.Errorf("query runs for %s: %w", jiraKey, err)
	}
	if len(runs) == 0 {
		return "", fmt.Errorf("no runs found for task %q", jiraKey)
	}

	var reportRuns []*intent.ReportData
	for _, r := range runs {
		data, err := loadRunDataFromRecord(store, r)
		if err != nil {
			// Fail-soft: skip runs that error on secondary data load.
			continue
		}
		reportRuns = append(reportRuns, data)
	}

	taskData := &intent.TaskReportData{
		JiraKey: jiraKey,
		Runs:    reportRuns,
	}
	return intent.BuildTaskReport(taskData), nil
}

// loadRunData fetches run record then delegates to loadRunDataFromRecord.
func loadRunData(store *db.LocalDB, runID string) (*intent.ReportData, error) {
	run, err := store.GetRunRecord(runID)
	if err != nil {
		return nil, err
	}
	return loadRunDataFromRecord(store, run)
}

// loadRunDataFromRecord fetches intent events, quality metrics, tools, and
// reasoning for an already-fetched RunRecord. All secondary queries are
// fail-soft: errors return nil for that field rather than aborting.
func loadRunDataFromRecord(store *db.LocalDB, run *db.RunRecord) (*intent.ReportData, error) {
	intentEvents, err := store.GetIntentEvents(run.ID)
	if err != nil {
		// Fail-soft: present empty intent section.
		intentEvents = &db.RunIntentEvents{}
	}

	quality, _ := store.GetQualityMetricsForReport(run.ID) // nil on error or absence

	tools, _ := store.GetToolUsageForRun(run.ID, 5) // nil on error

	reasoning, _ := store.GetReasoningTrace(run.ID, 5, 300) // nil on error

	return &intent.ReportData{
		Run:       run,
		Intent:    intentEvents,
		Quality:   quality,
		Tools:     tools,
		Reasoning: reasoning,
	}, nil
}

// writeOutput writes the markdown to stdout or to the specified file.
// When writing to a file, stdout is silent except for the confirmation line.
func writeOutput(markdown, outPath string) error {
	if outPath == "" {
		fmt.Print(markdown)
		return nil
	}

	// Create parent directories if needed.
	if dir := filepath.Dir(outPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create output directory: %w", err)
		}
	}

	if err := os.WriteFile(outPath, []byte(markdown), 0o644); err != nil {
		return fmt.Errorf("write report file: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Report written to %s\n", outPath)
	return nil
}
