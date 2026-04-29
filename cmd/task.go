package cmd

import (
	"fmt"

	"github.com/phuc-nt/dandori-cli/internal/attribution"
	"github.com/phuc-nt/dandori-cli/internal/config"
	"github.com/phuc-nt/dandori-cli/internal/db"
	"github.com/phuc-nt/dandori-cli/internal/jira"
	"github.com/spf13/cobra"
)

var taskCmd = &cobra.Command{
	Use:   "task",
	Short: "Manage Jira tasks",
	Long: `Commands for managing Jira task lifecycle.

Examples:
  dandori task start CLITEST-4    # Move to In Progress + add comment
  dandori task done CLITEST-4     # Move to Done + add comment
  dandori task info CLITEST-4     # Show task details`,
}

var taskStartCmd = &cobra.Command{
	Use:   "start <issue-key>",
	Short: "Start working on a task (transition to In Progress)",
	Args:  cobra.ExactArgs(1),
	RunE:  runTaskStart,
}

var taskDoneCmd = &cobra.Command{
	Use:   "done <issue-key>",
	Short: "Mark task as done",
	Args:  cobra.ExactArgs(1),
	RunE:  runTaskDone,
}

var taskInfoCmd = &cobra.Command{
	Use:   "info <issue-key>",
	Short: "Show task details",
	Args:  cobra.ExactArgs(1),
	RunE:  runTaskInfo,
}

func init() {
	rootCmd.AddCommand(taskCmd)
	taskCmd.AddCommand(taskStartCmd)
	taskCmd.AddCommand(taskDoneCmd)
	taskCmd.AddCommand(taskInfoCmd)
}

func getJiraClient() (*jira.Client, error) {
	cfg := Config()
	if cfg == nil || cfg.Jira.BaseURL == "" {
		return nil, fmt.Errorf("jira not configured")
	}
	return jira.NewClient(jira.ClientConfig{
		BaseURL: cfg.Jira.BaseURL,
		User:    cfg.Jira.User,
		Token:   cfg.Jira.Token,
		IsCloud: cfg.Jira.Cloud,
	}), nil
}

func runTaskStart(cmd *cobra.Command, args []string) error {
	issueKey := args[0]

	client, err := getJiraClient()
	if err != nil {
		return err
	}

	// Get issue info
	issue, err := client.GetIssue(issueKey)
	if err != nil {
		return fmt.Errorf("get issue: %w", err)
	}

	fmt.Printf("Starting: %s - %s\n", issue.Key, issue.Summary)
	fmt.Printf("Current status: %s\n", issue.Status)

	// Transition to In Progress
	if err := client.TransitionToRunning(issueKey, jira.DefaultStatusMapping); err != nil {
		fmt.Printf("Warning: could not transition (may already be in progress): %v\n", err)
	} else {
		fmt.Println("→ Transitioned to In Progress")
	}

	// Add comment
	agentName := "default"
	if cfg := Config(); cfg != nil && cfg.Agent.Name != "" {
		agentName = cfg.Agent.Name
	}

	comment := fmt.Sprintf("🤖 *Agent %s starting work*\n\nTask picked up by AI agent.", agentName)
	if err := client.AddComment(issueKey, comment); err != nil {
		fmt.Printf("Warning: could not add comment: %v\n", err)
	}

	fmt.Printf("\nReady! Run:\n  dandori run --task %s -- claude \"<your prompt>\"\n", issueKey)
	return nil
}

func runTaskDone(cmd *cobra.Command, args []string) error {
	issueKey := args[0]

	client, err := getJiraClient()
	if err != nil {
		return err
	}

	// Attribution snapshot before the transition. Non-fatal: a missing or
	// non-git workspace must not block a manual Jira move.
	if dbPath, err := config.DBPath(); err == nil {
		if localDB, openErr := db.Open(dbPath); openErr == nil {
			if migErr := localDB.Migrate(); migErr == nil {
				if finalHead := getFullGitHead(); finalHead != "" {
					if err := attribution.ComputeAndPersist(localDB, issueKey, ".", finalHead); err != nil {
						fmt.Printf("Warning: attribution compute failed: %v\n", err)
					}
				}
			}
			localDB.Close()
		}
	}

	// Transition to Done
	if err := client.TransitionToDone(issueKey, jira.DefaultStatusMapping); err != nil {
		return fmt.Errorf("transition: %w", err)
	}

	fmt.Printf("✓ %s marked as Done\n", issueKey)

	// Add comment
	comment := "✅ *Task completed*\n\nManually marked as done."
	client.AddComment(issueKey, comment)

	return nil
}

func runTaskInfo(cmd *cobra.Command, args []string) error {
	issueKey := args[0]

	client, err := getJiraClient()
	if err != nil {
		return err
	}

	issue, err := client.GetIssue(issueKey)
	if err != nil {
		return fmt.Errorf("get issue: %w", err)
	}

	fmt.Printf("Key:      %s\n", issue.Key)
	fmt.Printf("Summary:  %s\n", issue.Summary)
	fmt.Printf("Type:     %s\n", issue.IssueType)
	fmt.Printf("Status:   %s\n", issue.Status)
	fmt.Printf("Priority: %s\n", issue.Priority)
	if issue.Assignee != "" {
		fmt.Printf("Assignee: %s\n", issue.Assignee)
	}
	if len(issue.Labels) > 0 {
		fmt.Printf("Labels:   %v\n", issue.Labels)
	}

	return nil
}
