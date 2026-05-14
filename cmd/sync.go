package cmd

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/phuc-nt/dandori-cli/internal/config"
	"github.com/phuc-nt/dandori-cli/internal/db"
	githubclient "github.com/phuc-nt/dandori-cli/internal/github"
	"github.com/phuc-nt/dandori-cli/internal/sync"
	"github.com/spf13/cobra"
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Upload pending runs and events to monitoring server",
	RunE:  runSync,
}

var (
	syncForce      bool
	syncDryRun     bool
	syncGitHubOnly bool
)

func init() {
	syncCmd.Flags().BoolVar(&syncForce, "force", false, "Sync immediately")
	syncCmd.Flags().BoolVar(&syncDryRun, "dry-run", false, "Show what would be synced")
	syncCmd.Flags().BoolVar(&syncGitHubOnly, "github-only", false, "Skip upload; pull GitHub PR events only")
	rootCmd.AddCommand(syncCmd)
}

func runSync(cmd *cobra.Command, args []string) error {
	cfg := Config()
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}
	if !syncGitHubOnly && cfg.ServerURL == "" {
		return fmt.Errorf("server_url not configured")
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

	if !syncGitHubOnly {
		if err := runUploadPath(cfg, localDB); err != nil {
			return err
		}
	}

	// GitHub pull is best-effort: upload reliability must not depend on it.
	// On failure we warn and continue so cron-driven sync stays green.
	if cfg.GitHub.Enabled && cfg.GitHub.Token != "" && cfg.GitHub.Repo != "" {
		if err := runGitHubPull(cfg, localDB); err != nil {
			slog.Warn("github pull failed", "err", err)
			fmt.Printf("GitHub pull failed: %v (upload unaffected)\n", err)
		}
	}
	return nil
}

func runUploadPath(cfg *config.Config, localDB *db.LocalDB) error {
	var unsyncedRuns, unsyncedEvents int
	localDB.QueryRow(`SELECT COUNT(*) FROM runs WHERE synced = 0`).Scan(&unsyncedRuns)
	localDB.QueryRow(`SELECT COUNT(*) FROM events WHERE synced = 0`).Scan(&unsyncedEvents)

	if unsyncedRuns == 0 && unsyncedEvents == 0 {
		fmt.Println("Nothing to sync.")
		return nil
	}

	fmt.Printf("Pending: %d runs, %d events\n", unsyncedRuns, unsyncedEvents)

	if syncDryRun {
		fmt.Printf("Would sync to: %s\n", cfg.ServerURL)
		return nil
	}

	hostname, _ := os.Hostname()
	workstationID := fmt.Sprintf("ws-%s", hostname)

	uploader := sync.NewUploader(cfg.ServerURL, cfg.APIKey, workstationID)

	batchSize := cfg.Sync.BatchSize
	if batchSize == 0 {
		batchSize = 100
	}

	resp, err := uploader.Sync(localDB, batchSize)
	if err != nil {
		return fmt.Errorf("sync failed: %w", err)
	}

	fmt.Printf("Synced: %d accepted, %d errors\n", resp.Accepted, resp.Errors)
	return nil
}

func runGitHubPull(cfg *config.Config, localDB *db.LocalDB) error {
	client := githubclient.NewClient(githubclient.ClientConfig{
		Repo:  cfg.GitHub.Repo,
		Token: cfg.GitHub.Token,
	})
	summary, err := githubclient.PullPREvents(client, localDB, githubclient.DefaultBackfillDays)
	if err != nil {
		return err
	}
	fmt.Printf("GitHub: pulled %d PRs (%d reviews), %d reverts, %d reopens [%s]\n",
		summary.PRsPulled, summary.ReviewsFetched,
		summary.RevertsDetected, summary.ReopensDetected,
		summary.Duration.Round(1e6))
	return nil
}
