package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/phuc-nt/dandori-cli/internal/confluence"
	"github.com/phuc-nt/dandori-cli/internal/config"
	"github.com/phuc-nt/dandori-cli/internal/db"
	"github.com/phuc-nt/dandori-cli/internal/jira"
	"github.com/phuc-nt/dandori-cli/internal/taskcontext"
	"github.com/phuc-nt/dandori-cli/internal/wrapper"
	"github.com/spf13/cobra"
)

var taskRunCmd = &cobra.Command{
	Use:   "run <issue-key> [-- <agent-command>]",
	Short: "Run agent with full task context from Jira + Confluence",
	Long: `Fetches task context from Jira (including linked Confluence docs),
injects it into the agent prompt, then runs the agent with tracking.

This is the recommended way to run agents on Jira tasks - it ensures the agent
has full context without manual copy-paste.

The command:
1. Fetches Jira issue (summary, description, acceptance criteria)
2. Extracts Confluence links from description
3. Fetches linked Confluence page content
4. Writes context to a temp file
5. Runs agent with --context-file pointing to the context
6. Tracks the run (tokens, cost, duration)
7. On completion, can auto-sync to Jira + Confluence

Examples:
  dandori task run PROJ-123                    # Uses default agent (claude)
  dandori task run PROJ-123 -- claude          # Explicit agent
  dandori task run PROJ-123 --dry-run          # Preview context without running
  dandori task run PROJ-123 --no-context       # Run without fetching context`,
	Args: cobra.MinimumNArgs(1),
	RunE: runTaskRun,
}

var (
	taskRunDryRun    bool
	taskRunNoContext bool
	taskRunNoSync    bool
)

func init() {
	taskCmd.AddCommand(taskRunCmd)
	taskRunCmd.Flags().BoolVar(&taskRunDryRun, "dry-run", false, "Preview context without running agent")
	taskRunCmd.Flags().BoolVar(&taskRunNoContext, "no-context", false, "Skip context injection")
	taskRunCmd.Flags().BoolVar(&taskRunNoSync, "no-sync", false, "Skip post-run Jira/Confluence sync")
}

func runTaskRun(cmd *cobra.Command, args []string) error {
	issueKey := args[0]

	// Parse agent command (after --)
	agentCmd := []string{"claude"}
	if cmd.ArgsLenAtDash() >= 0 && len(args) > 1 {
		agentCmd = args[1:]
	}

	cfg := Config()
	if cfg == nil {
		return fmt.Errorf("config not loaded - run 'dandori init' first")
	}

	// Create clients
	jiraClient := jira.NewClient(jira.ClientConfig{
		BaseURL: cfg.Jira.BaseURL,
		User:    cfg.Jira.User,
		Token:   cfg.Jira.Token,
		IsCloud: cfg.Jira.Cloud,
	})

	var confClient *confluence.Client
	if cfg.Confluence.BaseURL != "" {
		confClient = confluence.NewClient(confluence.ClientConfig{
			BaseURL: cfg.Confluence.BaseURL,
			User:    cfg.Jira.User,
			Token:   cfg.Jira.Token,
			IsCloud: cfg.Confluence.Cloud,
		})
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		cancel()
	}()

	// Fetch task context
	var contextFile string
	var taskDescription string
	if !taskRunNoContext {
		fmt.Printf("Fetching context for %s...\n", issueKey)

		fetcher := taskcontext.NewFetcher(jiraClient, confClient)
		taskCtx, err := fetcher.Fetch(ctx, issueKey)
		if err != nil {
			return fmt.Errorf("fetch context: %w", err)
		}

		taskDescription = taskCtx.Description // Save for AC extraction later

		fmt.Printf("  Issue: %s\n", taskCtx.Summary)
		fmt.Printf("  Type: %s | Priority: %s | Status: %s\n", taskCtx.IssueType, taskCtx.Priority, taskCtx.Status)
		if len(taskCtx.LinkedDocs) > 0 {
			fmt.Printf("  Linked docs: %d\n", len(taskCtx.LinkedDocs))
			for _, doc := range taskCtx.LinkedDocs {
				fmt.Printf("    - %s\n", doc.Title)
			}
		}

		// Write context to temp file
		contextMD := taskCtx.ToMarkdown()

		if taskRunDryRun {
			fmt.Println("\n--- Context Preview ---")
			fmt.Println(contextMD)
			fmt.Println("--- End Preview ---")
			return nil
		}

		tmpDir := os.TempDir()
		contextFile = filepath.Join(tmpDir, fmt.Sprintf("dandori-context-%s.md", issueKey))
		if err := os.WriteFile(contextFile, []byte(contextMD), 0600); err != nil {
			return fmt.Errorf("write context file: %w", err)
		}
		defer os.Remove(contextFile)

		fmt.Printf("  Context written to: %s\n\n", contextFile)
	}

	// Capture git HEAD before run
	gitHeadBefore := getGitHead()

	// Transition Jira to In Progress
	fmt.Printf("Starting task %s...\n", issueKey)
	if err := jiraClient.TransitionToRunning(issueKey, jira.DefaultStatusMapping); err != nil {
		fmt.Printf("Warning: could not transition to In Progress: %v\n", err)
	}

	// Add start comment
	agentName := cfg.Agent.Name
	if agentName == "" {
		agentName = "default"
	}
	startComment := fmt.Sprintf("🤖 *Agent %s starting work*\n\nContext loaded from Jira + Confluence.", agentName)
	jiraClient.AddComment(issueKey, startComment)

	// Build agent command with context
	finalCmd := agentCmd
	if contextFile != "" {
		// Inject context into claude command
		// Claude CLI uses -p for prompt, --system-prompt for system instructions
		contextInstruction := fmt.Sprintf("IMPORTANT: First read the task context file at %s which contains the Jira issue details and linked Confluence documentation. Base your work on this context.", contextFile)

		if len(finalCmd) >= 1 && finalCmd[0] == "claude" {
			// Check if user provided -p flag
			hasPrompt := false
			promptIdx := -1
			for i, arg := range finalCmd {
				if arg == "-p" && i+1 < len(finalCmd) {
					hasPrompt = true
					promptIdx = i + 1
					break
				}
			}

			if hasPrompt {
				// Prepend context instruction to user's prompt
				finalCmd[promptIdx] = contextInstruction + "\n\n" + finalCmd[promptIdx]
			} else {
				// No user prompt, add our own
				finalCmd = append(finalCmd, "-p", fmt.Sprintf("%s Then complete the task described in the context.", contextInstruction))
			}
		}
	}

	fmt.Printf("Running: %v\n\n", finalCmd)

	// Open database
	dbPath, err := config.DBPath()
	if err != nil {
		return fmt.Errorf("get db path: %w", err)
	}

	localDB, err := db.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer localDB.Close()

	if err := localDB.Migrate(); err != nil {
		return fmt.Errorf("migrate database: %w", err)
	}

	// Run with wrapper
	opts := wrapper.Options{
		Command:      finalCmd,
		JiraIssueKey: issueKey,
		AgentName:    agentName,
		AgentType:    cfg.Agent.Type,
	}

	result, err := wrapper.Run(ctx, localDB, opts)
	if err != nil {
		return fmt.Errorf("run agent: %w", err)
	}

	fmt.Printf("\n--- Run Complete ---\n")
	fmt.Printf("Run ID: %s\n", result.RunID)
	fmt.Printf("Duration: %s\n", result.Duration)
	fmt.Printf("Tokens: %d in / %d out\n", result.TokenUsage.Input, result.TokenUsage.Output)
	fmt.Printf("Cost: $%.4f\n", result.CostUSD)
	fmt.Printf("Exit code: %d\n", result.ExitCode)

	// Auto-sync if enabled
	if !taskRunNoSync && result.ExitCode == 0 {
		fmt.Println("\nSyncing to Jira...")

		// Transition to Done
		if err := jiraClient.TransitionToDone(issueKey, jira.DefaultStatusMapping); err != nil {
			fmt.Printf("Warning: could not transition to Done: %v\n", err)
		}

		// Get git changes for the report (compare before vs after)
		gitHeadAfter := getGitHead()
		gitChanges := getGitChangesBetween(gitHeadBefore, gitHeadAfter)

		// Build comprehensive completion comment
		doneComment := buildCompletionComment(agentName, result, gitChanges, issueKey, taskDescription)

		jiraClient.AddComment(issueKey, doneComment)
		fmt.Println("  ✓ Jira updated")

		// Write Confluence report if configured
		if cfg.Confluence.AutoPost && confClient != nil {
			fmt.Println("Writing Confluence report...")
			fmt.Printf("  → Run: dandori conf-write --run %s\n", result.RunID)
		}
	}

	os.Exit(result.ExitCode)
	return nil
}

// GitChanges holds information about code changes
type GitChanges struct {
	FilesChanged []string
	Commits      []string
	DiffSummary  string
	HeadBefore   string
	HeadAfter    string
}

// getGitHead gets current git HEAD
func getGitHead() string {
	if out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output(); err == nil {
		return strings.TrimSpace(string(out))
	}
	return ""
}

// getGitChangesBetween gets changes between two commits
func getGitChangesBetween(before, after string) GitChanges {
	var changes GitChanges
	changes.HeadBefore = before
	changes.HeadAfter = after

	if before == "" || after == "" || before == after {
		// No changes or can't determine
		return changes
	}

	// Get changed files between before and after
	if out, err := exec.Command("git", "diff", "--name-only", before, after).Output(); err == nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		for _, line := range lines {
			if line != "" {
				changes.FilesChanged = append(changes.FilesChanged, line)
			}
		}
	}

	// Get commits between before and after
	if out, err := exec.Command("git", "log", "--oneline", before+".."+after).Output(); err == nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		for _, line := range lines {
			if line != "" {
				changes.Commits = append(changes.Commits, line)
			}
		}
	}

	// Get diff stat
	if out, err := exec.Command("git", "diff", "--stat", before, after).Output(); err == nil {
		changes.DiffSummary = strings.TrimSpace(string(out))
	}

	return changes
}

// extractAcceptanceCriteria extracts AC items from task description
func extractAcceptanceCriteria(description string) []string {
	var acs []string
	lines := strings.Split(description, "\n")

	inACSection := false
	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Check if entering AC section
		if strings.Contains(strings.ToLower(line), "acceptance criteria") {
			inACSection = true
			continue
		}

		// Check if leaving AC section (new header)
		if inACSection && (strings.HasPrefix(line, "**") || strings.HasPrefix(line, "##") || strings.HasPrefix(line, "h3.")) {
			if !strings.Contains(strings.ToLower(line), "acceptance") {
				inACSection = false
				continue
			}
		}

		// Extract checkbox items
		if strings.HasPrefix(line, "- [ ]") || strings.HasPrefix(line, "- [x]") || strings.HasPrefix(line, "* [ ]") {
			ac := strings.TrimPrefix(line, "- [ ]")
			ac = strings.TrimPrefix(ac, "- [x]")
			ac = strings.TrimPrefix(ac, "* [ ]")
			ac = strings.TrimSpace(ac)
			if ac != "" {
				acs = append(acs, ac)
			}
		}
	}

	return acs
}

// buildCompletionComment creates a comprehensive Jira comment
func buildCompletionComment(agentName string, result *wrapper.Result, changes GitChanges, issueKey string, taskDescription string) string {
	var sb strings.Builder

	sb.WriteString("✅ *Agent Run Completed*\n\n")

	// Run stats
	sb.WriteString("h3. Run Statistics\n")
	sb.WriteString(fmt.Sprintf("||Agent||%s||\n", agentName))
	sb.WriteString(fmt.Sprintf("||Duration||%s||\n", result.Duration.Round(time.Second)))
	sb.WriteString(fmt.Sprintf("||Cost||$%.4f||\n", result.CostUSD))
	sb.WriteString(fmt.Sprintf("||Tokens||%d in / %d out||\n", result.TokenUsage.Input, result.TokenUsage.Output))
	sb.WriteString(fmt.Sprintf("||Model||%s||\n", result.TokenUsage.Model))
	sb.WriteString(fmt.Sprintf("||Run ID||%s||\n", result.RunID))
	if changes.HeadBefore != "" && changes.HeadAfter != "" {
		sb.WriteString(fmt.Sprintf("||Git||%s → %s||\n", changes.HeadBefore, changes.HeadAfter))
	}
	sb.WriteString("\n")

	// Files changed (only if there are actual changes in this run)
	if len(changes.FilesChanged) > 0 {
		sb.WriteString("h3. Files Changed\n")
		sb.WriteString("{code}\n")
		for _, f := range changes.FilesChanged {
			sb.WriteString(fmt.Sprintf("  %s\n", f))
		}
		sb.WriteString("{code}\n\n")
	} else if changes.HeadBefore == changes.HeadAfter {
		sb.WriteString("h3. Files Changed\n")
		sb.WriteString("_No code changes in this run_\n\n")
	}

	// Commits in this run
	if len(changes.Commits) > 0 {
		sb.WriteString("h3. Commits\n")
		sb.WriteString("{code}\n")
		for _, c := range changes.Commits {
			sb.WriteString(fmt.Sprintf("  %s\n", c))
		}
		sb.WriteString("{code}\n\n")
	}

	// Output location
	sb.WriteString("h3. Output Location\n")
	sb.WriteString("* *Code:* See commits/files above\n")
	sb.WriteString(fmt.Sprintf("* *Report:* {{dandori conf-write --run %s}}\n\n", result.RunID))

	// Acceptance Criteria from task
	acs := extractAcceptanceCriteria(taskDescription)
	if len(acs) > 0 {
		sb.WriteString("h3. Acceptance Criteria (from task)\n")
		sb.WriteString("_Please verify each item:_\n")
		for _, ac := range acs {
			sb.WriteString(fmt.Sprintf("* (?) %s\n", ac))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("---\n")
	sb.WriteString("_Generated by dandori-cli_")

	return sb.String()
}
