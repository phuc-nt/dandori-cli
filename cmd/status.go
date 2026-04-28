package cmd

import (
	"fmt"
	"log/slog"

	"github.com/phuc-nt/dandori-cli/internal/config"
	"github.com/phuc-nt/dandori-cli/internal/db"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show status of recent runs and tasks",
	RunE:  runStatus,
}

var statusLimit int

func init() {
	statusCmd.Flags().IntVarP(&statusLimit, "limit", "n", 10, "number of runs to show")
	rootCmd.AddCommand(statusCmd)
}

func runStatus(cmd *cobra.Command, args []string) error {
	dbPath, err := config.DBPath()
	if err != nil {
		return err
	}

	localDB, err := db.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer localDB.Close()

	// COALESCE(agent_name, '') — Bug #2: rows from older schema versions or
	// failed task-run inserts may have NULL agent_name, which breaks
	// rows.Scan into a string. Empty string is fine; the print path renders
	// '-' for blanks already.
	rows, err := localDB.Query(`
		SELECT id, COALESCE(agent_name, ''), status, jira_issue_key, started_at, duration_sec, cost_usd
		FROM runs
		ORDER BY started_at DESC
		LIMIT ?
	`, statusLimit)
	if err != nil {
		return fmt.Errorf("query runs: %w", err)
	}
	defer rows.Close()

	fmt.Printf("%-16s %-12s %-10s %-12s %-20s %10s %10s\n",
		"RUN ID", "AGENT", "STATUS", "JIRA", "STARTED", "DURATION", "COST")
	fmt.Println("--------------------------------------------------------------------------------------------------------")

	count := 0
	for rows.Next() {
		var id, agentName, status, startedAt string
		var jiraKey, durationSec, costUSD interface{}

		if err := rows.Scan(&id, &agentName, &status, &jiraKey, &startedAt, &durationSec, &costUSD); err != nil {
			slog.Error("scan row", "error", err)
			continue
		}

		jiraStr := "-"
		if jiraKey != nil {
			jiraStr = fmt.Sprintf("%v", jiraKey)
		}

		durationStr := "-"
		if durationSec != nil {
			durationStr = fmt.Sprintf("%.1fs", durationSec)
		}

		costStr := "-"
		if costUSD != nil {
			costStr = fmt.Sprintf("$%.4f", costUSD)
		}

		fmt.Printf("%-16s %-12s %-10s %-12s %-20s %10s %10s\n",
			id, agentName, status, jiraStr, startedAt, durationStr, costStr)
		count++
	}

	if count == 0 {
		fmt.Println("No runs found. Use 'dandori run -- claude ...' to start tracking.")
	}

	var unsynced int
	localDB.QueryRow(`SELECT COUNT(*) FROM runs WHERE synced = 0`).Scan(&unsynced)
	if unsynced > 0 {
		fmt.Printf("\n%d runs pending sync. Run 'dandori sync' to upload.\n", unsynced)
	}

	return nil
}
