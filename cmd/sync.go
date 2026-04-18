package cmd

import (
	"fmt"
	"os"

	"github.com/phuc-nt/dandori-cli/internal/config"
	"github.com/phuc-nt/dandori-cli/internal/db"
	"github.com/phuc-nt/dandori-cli/internal/sync"
	"github.com/spf13/cobra"
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Upload pending runs and events to monitoring server",
	RunE:  runSync,
}

var (
	syncForce  bool
	syncDryRun bool
)

func init() {
	syncCmd.Flags().BoolVar(&syncForce, "force", false, "Sync immediately")
	syncCmd.Flags().BoolVar(&syncDryRun, "dry-run", false, "Show what would be synced")
	rootCmd.AddCommand(syncCmd)
}

func runSync(cmd *cobra.Command, args []string) error {
	cfg := Config()
	if cfg == nil || cfg.ServerURL == "" {
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
