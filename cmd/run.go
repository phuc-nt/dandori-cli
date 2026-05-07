package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/phuc-nt/dandori-cli/internal/config"
	"github.com/phuc-nt/dandori-cli/internal/db"
	"github.com/phuc-nt/dandori-cli/internal/quality"
	"github.com/phuc-nt/dandori-cli/internal/wrapper"
	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run [flags] -- <command> [args...]",
	Short: "Run an agent command with tracking",
	Long: `Wraps agent execution with 3-layer instrumentation:
  Layer 1 (Wrapper): Process lifecycle - run_id, timing, exit code, git HEAD
  Layer 2 (Tailer): Session log parsing - tokens, model, cost
  Layer 3 (Skill): Agent-reported events - decisions, file changes

Examples:
  dandori run -- claude "fix the auth bug"
  dandori run --task PROJ-123 -- claude "implement login"
  dandori run --auto-task -- claude "fix tests"`,
	RunE:               runRun,
	DisableFlagParsing: false,
}

var (
	taskFlag     string
	autoTaskFlag bool
	noTailerFlag bool
	dryRunFlag   bool
	noWaitFlag   bool
)

func init() {
	runCmd.Flags().StringVar(&taskFlag, "task", "", "Link run to Jira issue (e.g., PROJ-123)")
	runCmd.Flags().BoolVar(&autoTaskFlag, "auto-task", false, "Auto-detect Jira key from git branch")
	runCmd.Flags().BoolVar(&noTailerFlag, "no-tailer", false, "Disable session log parsing (Layer 2)")
	runCmd.Flags().BoolVar(&dryRunFlag, "dry-run", false, "Print what would happen without executing")
	runCmd.Flags().BoolVar(&noWaitFlag, "no-wait", false, "Skip post-exit wait for session log (CI/scripts)")
	rootCmd.AddCommand(runCmd)
}

func runRun(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("no command specified. Usage: dandori run -- <command> [args...]")
	}

	dbPath, err := config.DBPath()
	if err != nil {
		return err
	}

	localDB, err := db.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer localDB.Close()

	if err := localDB.Migrate(); err != nil {
		return fmt.Errorf("migrate database: %w", err)
	}

	cfg := Config()
	agentName := "default"
	agentType := "claude_code"
	if cfg != nil {
		if cfg.Agent.Name != "" {
			agentName = cfg.Agent.Name
		}
		if cfg.Agent.Type != "" {
			agentType = cfg.Agent.Type
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		cancel()
	}()

	// Get quality config
	qualityCfg := quality.DefaultConfig()
	if cfg != nil && cfg.Quality.LintCommand != "" {
		qualityCfg = quality.Config{
			Enabled:     cfg.Quality.Enabled,
			LintCommand: cfg.Quality.LintCommand,
			TestCommand: cfg.Quality.TestCommand,
			Timeout:     cfg.Quality.Timeout,
		}
	}

	postExitTimeout := wrapper.DefaultPostExitTimeout
	if cfg != nil && cfg.Wrapper.PostExitTimeout != "" {
		if d, err := time.ParseDuration(cfg.Wrapper.PostExitTimeout); err == nil {
			postExitTimeout = d
		}
	}
	if noWaitFlag {
		postExitTimeout = 0
	}

	opts := wrapper.Options{
		Command:         args,
		JiraIssueKey:    taskFlag,
		AutoTask:        autoTaskFlag,
		NoTailer:        noTailerFlag,
		DryRun:          dryRunFlag,
		AgentName:       agentName,
		AgentType:       agentType,
		QualityConfig:   qualityCfg,
		PostExitTimeout: postExitTimeout,
	}

	result, err := wrapper.Run(ctx, localDB, opts)
	if err != nil {
		return err
	}

	if dryRunFlag {
		if !Quiet() {
			fmt.Printf("Would execute: %v\n", args)
			fmt.Printf("Run ID: %s\n", result.RunID)
		}
		return nil
	}

	// Print run summary to stderr so piped stdout from wrapped command is clean.
	// Only suppress when --quiet / -q is set.
	if !Quiet() {
		printRunSummary(os.Stderr, result)
		printFirstRunTip(os.Stderr, localDB, result.RunID)
	}

	os.Exit(result.ExitCode)
	return nil
}

// printRunSummary writes a one-line tracking confirmation to w.
// Format:  ✓ Run tracked (id: <id>, cost: $X.XX, duration: Ys)
//
//	View: http://localhost:8088
func printRunSummary(w io.Writer, result *wrapper.Result) {
	cost := formatRunCost(result.CostUSD)
	dur := formatRunDuration(result.Duration)
	fmt.Fprintf(w, "✓ Run tracked (id: %s, cost: %s, duration: %s)\n", result.RunID, cost, dur)
	fmt.Fprintf(w, "  View: http://localhost:8088\n")
}

// printFirstRunTip prints a discovery tip when this is the user's first
// completed run (no other rows in the runs table except the just-finished one).
// Uses COUNT(*) WHERE id != runID — if 0, this was the first run.
func printFirstRunTip(w io.Writer, localDB *db.LocalDB, runID string) {
	var count int
	row := localDB.QueryRow(`SELECT COUNT(*) FROM runs WHERE id != ?`, runID)
	if err := row.Scan(&count); err != nil || count > 0 {
		return
	}
	fmt.Fprintf(w, "  Tip: try 'dandori analytics trend' once you have ~5 runs to see your improvement curve.\n")
}

// formatRunCost formats a USD float as "$X.XX".
func formatRunCost(usd float64) string {
	return fmt.Sprintf("$%.2f", usd)
}

// formatRunDuration formats a duration as "Xs" or "XmYYs".
func formatRunDuration(d time.Duration) string {
	secs := int(d.Seconds())
	if secs < 60 {
		return fmt.Sprintf("%ds", secs)
	}
	m := secs / 60
	s := secs % 60
	return fmt.Sprintf("%dm%02ds", m, s)
}
