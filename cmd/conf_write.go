package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/phuc-nt/dandori-cli/internal/config"
	"github.com/phuc-nt/dandori-cli/internal/confluence"
	"github.com/phuc-nt/dandori-cli/internal/db"
	"github.com/spf13/cobra"
)

var confWriteCmd = &cobra.Command{
	Use:   "conf-write",
	Short: "Write agent run report to Confluence",
	Long: `Write an agent run report to Confluence.

Creates a new Confluence page under the configured reports parent page
with run metadata, files changed, decisions, and git diff.

Examples:
  dandori conf-write --run abc123
  dandori conf-write --task PROJ-123
  dandori conf-write --run abc123 --dry-run`,
	RunE: runConfWrite,
}

var (
	confWriteRunID   string
	confWriteTaskKey string
	confWriteDryRun  bool
)

func init() {
	confWriteCmd.Flags().StringVar(&confWriteRunID, "run", "", "Write report for this run ID")
	confWriteCmd.Flags().StringVar(&confWriteTaskKey, "task", "", "Write report for latest run on this task")
	confWriteCmd.Flags().BoolVar(&confWriteDryRun, "dry-run", false, "Print rendered page without posting")
	rootCmd.AddCommand(confWriteCmd)
}

func runConfWrite(cmd *cobra.Command, args []string) error {
	if confWriteRunID == "" && confWriteTaskKey == "" {
		return fmt.Errorf("either --run or --task is required")
	}

	cfg := Config()
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}

	// Open local database
	dbPath, err := config.DBPath()
	if err != nil {
		return fmt.Errorf("get db path: %w", err)
	}

	localDB, err := db.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer localDB.Close()

	// Find the run
	var runID, issueKey, agentName, status, model string
	var durationSec float64
	var costUSD float64
	var inputTokens, outputTokens int
	var gitHeadBefore, gitHeadAfter string
	var startedAtStr, endedAtStr string
	var startedAt, endedAt time.Time

	if confWriteRunID != "" {
		err = localDB.QueryRow(`
			SELECT id, COALESCE(jira_issue_key, ''), agent_name, status, COALESCE(model, ''),
			       COALESCE(duration_sec, 0), COALESCE(cost_usd, 0), COALESCE(input_tokens, 0), COALESCE(output_tokens, 0),
			       COALESCE(git_head_before, ''), COALESCE(git_head_after, ''), started_at, COALESCE(ended_at, '')
			FROM runs WHERE id = ?`, confWriteRunID).Scan(
			&runID, &issueKey, &agentName, &status, &model,
			&durationSec, &costUSD, &inputTokens, &outputTokens,
			&gitHeadBefore, &gitHeadAfter, &startedAtStr, &endedAtStr,
		)
	} else {
		err = localDB.QueryRow(`
			SELECT id, COALESCE(jira_issue_key, ''), agent_name, status, COALESCE(model, ''),
			       COALESCE(duration_sec, 0), COALESCE(cost_usd, 0), COALESCE(input_tokens, 0), COALESCE(output_tokens, 0),
			       COALESCE(git_head_before, ''), COALESCE(git_head_after, ''), started_at, COALESCE(ended_at, '')
			FROM runs WHERE jira_issue_key = ?
			ORDER BY started_at DESC LIMIT 1`, confWriteTaskKey).Scan(
			&runID, &issueKey, &agentName, &status, &model,
			&durationSec, &costUSD, &inputTokens, &outputTokens,
			&gitHeadBefore, &gitHeadAfter, &startedAtStr, &endedAtStr,
		)
	}

	if err == nil {
		startedAt, _ = time.Parse(time.RFC3339, startedAtStr)
		endedAt, _ = time.Parse(time.RFC3339, endedAtStr)
	}

	if err != nil {
		return fmt.Errorf("find run: %w", err)
	}

	// Get files changed from events
	var filesChanged []string
	rows, err := localDB.Query(`
		SELECT DISTINCT json_extract(payload, '$.file')
		FROM events
		WHERE run_id = ? AND event_type = 'file_edit' AND json_extract(payload, '$.file') IS NOT NULL`,
		runID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var file string
			if rows.Scan(&file) == nil && file != "" {
				filesChanged = append(filesChanged, file)
			}
		}
	}

	// Build run report
	report := confluence.RunReport{
		RunID:         runID,
		IssueKey:      issueKey,
		AgentName:     agentName,
		Status:        status,
		Duration:      time.Duration(durationSec * float64(time.Second)),
		CostUSD:       costUSD,
		InputTokens:   inputTokens,
		OutputTokens:  outputTokens,
		Model:         model,
		GitHeadBefore: gitHeadBefore,
		GitHeadAfter:  gitHeadAfter,
		FilesChanged:  filesChanged,
		StartedAt:     startedAt,
		EndedAt:       endedAt,
	}

	if confWriteDryRun {
		body := confluence.RenderReportTemplate(report)
		title := confluence.GenerateReportTitle(report)
		fmt.Printf("Title: %s\n\n", title)
		fmt.Printf("Body:\n%s\n", body)
		return nil
	}

	// Check Confluence config
	if cfg.Confluence.BaseURL == "" {
		return fmt.Errorf("confluence.base_url not configured")
	}
	if cfg.Confluence.SpaceKey == "" {
		return fmt.Errorf("confluence.space_key not configured")
	}

	// Create Confluence client
	client := confluence.NewClient(confluence.ClientConfig{
		BaseURL: cfg.Confluence.BaseURL,
		User:    cfg.Jira.User,
		Token:   cfg.Jira.Token,
		IsCloud: cfg.Confluence.Cloud,
	})

	writer := confluence.NewWriter(confluence.WriterConfig{
		Client:       client,
		SpaceKey:     cfg.Confluence.SpaceKey,
		ParentPageID: cfg.Confluence.ReportsParentPageID,
	})

	ctx := context.Background()
	page, err := writer.CreateReport(ctx, report)
	if err != nil {
		return fmt.Errorf("create report: %w", err)
	}

	fmt.Printf("Created report: %s\n", page.Title)
	fmt.Printf("Page ID: %s\n", page.ID)
	if cfg.Confluence.BaseURL != "" {
		fmt.Printf("URL: %s/pages/%s\n", cfg.Confluence.BaseURL, page.ID)
	}

	return nil
}
