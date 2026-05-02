package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/phuc-nt/dandori-cli/internal/config"
	"github.com/spf13/cobra"
)

var claudeCmd = &cobra.Command{
	Use:   "claude [args...]",
	Short: "Run claude with dandori tracking (no Jira context)",
	Long: `Equivalent to 'dandori run -- claude [args...]'.

Wraps the claude binary with 3-layer instrumentation (run tracking, token
counting, cost) without fetching Jira context. All arguments are passed
through to claude unchanged.

Use 'dandori task run KEY' for the full Jira-driven flow with context injection.

Examples:
  dandori claude "fix the auth bug"
  dandori claude -q "run tests and summarise failures"
  dandori claude --task PROJ-123 "implement the feature"`,
	// DisableFlagParsing allows arbitrary flags (e.g. --dangerously-skip-permissions)
	// to pass through to claude without cobra interpreting them.
	DisableFlagParsing: true,
	RunE:               runClaude,
}

func init() {
	rootCmd.AddCommand(claudeCmd)
}

// runClaude delegates to runRun, prepending "claude" to the arg list so the
// wrapper receives ["claude", <user args...>] — identical to
// 'dandori run -- claude <user args...>'.
//
// Because DisableFlagParsing is true, cobra forwards every token (including
// dandori's own persistent flags like -q/-v/--config placed after "claude") as
// raw args. Strip those out and apply them manually before delegating; the
// remaining args go through to claude untouched.
func runClaude(cmd *cobra.Command, args []string) error {
	claudeArgs := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-q" || a == "--quiet":
			quiet = true
		case a == "-v" || a == "--verbose":
			verbose = true
		case a == "--config":
			if i+1 < len(args) {
				cfgFile = args[i+1]
				i++
			}
		case strings.HasPrefix(a, "--config="):
			cfgFile = strings.TrimPrefix(a, "--config=")
		default:
			claudeArgs = append(claudeArgs, a)
		}
	}

	if quiet && verbose {
		return fmt.Errorf("cannot use --quiet and --verbose together")
	}

	if cfgFile != "" {
		c, err := config.Load(cfgFile)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		cfg = c
	}

	logLevel, err := config.ParseLogLevel(cfg.LogLevel)
	if err != nil {
		return fmt.Errorf("parse log level: %w", err)
	}
	if verbose {
		logLevel = slog.LevelDebug
	}
	if quiet {
		logLevel = slog.LevelError
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel})))

	return runRun(cmd, append([]string{"claude"}, claudeArgs...))
}
