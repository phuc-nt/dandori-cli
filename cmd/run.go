package cmd

import (
	"context"
	"fmt"
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

	os.Exit(result.ExitCode)
	return nil
}
