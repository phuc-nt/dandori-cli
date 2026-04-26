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
	"github.com/phuc-nt/dandori-cli/internal/event"
	"github.com/phuc-nt/dandori-cli/internal/jira"
	"github.com/spf13/cobra"
)

var (
	jiraPollOnce            bool
	jiraPollInterval        int
	jiraPollBugLinkInterval int
	jiraPollSkipBugs        bool
)

var jiraPollCmd = &cobra.Command{
	Use:   "jira-poll",
	Short: "Run the Jira sprint + bug-link poller (Layer-3 tracking daemon)",
	Long: `Run the Jira poller as a foreground daemon.

The poller does two things on independent schedules:
  - Sprint cycle: detects new tasks, posts agent suggestions, detects task
    iteration regressions (status done -> active again).
  - Bug-link cycle: searches recently-created Jira Bug tickets, links them
    back to the run that caused them via "is caused by" links or
    "caused_by:<runid>" description tags. Emits bug.filed events.

Run in foreground with Ctrl-C to stop, or use --once for a single pass
(useful in cron / launchd / systemd timers).

Examples:
  dandori jira-poll
  dandori jira-poll --interval 60 --bug-interval 1800
  dandori jira-poll --once
  dandori jira-poll --skip-bugs    # only the sprint cycle`,
	RunE: runJiraPoll,
}

func init() {
	jiraPollCmd.Flags().BoolVar(&jiraPollOnce, "once", false, "Run one cycle (sprint + bug-link) and exit")
	jiraPollCmd.Flags().IntVar(&jiraPollInterval, "interval", 30, "Sprint poll interval in seconds")
	jiraPollCmd.Flags().IntVar(&jiraPollBugLinkInterval, "bug-interval", 3600, "Bug-link cycle interval in seconds (0 disables; default 1h)")
	jiraPollCmd.Flags().BoolVar(&jiraPollSkipBugs, "skip-bugs", false, "Skip the bug-link cycle entirely")
	rootCmd.AddCommand(jiraPollCmd)
}

func runJiraPoll(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if cfg.Jira.BaseURL == "" {
		return fmt.Errorf("jira not configured (set jira.base_url in config.yaml)")
	}
	if cfg.Jira.BoardID == 0 {
		return fmt.Errorf("jira.board_id missing — required for sprint cycle")
	}

	dbPath, err := config.DBPath()
	if err != nil {
		return err
	}
	localDB, err := db.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer localDB.Close()
	if err := localDB.Migrate(); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	client := jira.NewClient(jira.ClientConfig{
		BaseURL: cfg.Jira.BaseURL,
		User:    cfg.Jira.User,
		Token:   cfg.Jira.Token,
		IsCloud: cfg.Jira.Cloud,
	})
	recorder := event.NewRecorder(localDB)

	bugInterval := time.Duration(jiraPollBugLinkInterval) * time.Second
	if jiraPollSkipBugs {
		// 24h essentially disables it for foreground runs while keeping
		// the field non-zero so NewPoller doesn't apply its 1h default.
		bugInterval = 24 * time.Hour
	}

	poller := jira.NewPoller(jira.PollerConfig{
		Client:          client,
		BoardID:         cfg.Jira.BoardID,
		Interval:        time.Duration(jiraPollInterval) * time.Second,
		BugLinkInterval: bugInterval,
		LocalDB:         localDB,
		Recorder:        recorder,
	})

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if jiraPollOnce {
		if err := poller.Poll(ctx); err != nil {
			return fmt.Errorf("sprint poll: %w", err)
		}
		if !jiraPollSkipBugs {
			if err := poller.BugLinkCycleOnce(ctx); err != nil {
				return fmt.Errorf("bug link cycle: %w", err)
			}
		}
		return nil
	}

	if err := poller.Run(ctx); err != nil && err != context.Canceled {
		return err
	}
	return nil
}
