package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/phuc-nt/dandori-cli/internal/attribution"
	"github.com/phuc-nt/dandori-cli/internal/config"
	"github.com/phuc-nt/dandori-cli/internal/confluence"
	"github.com/phuc-nt/dandori-cli/internal/db"
	"github.com/phuc-nt/dandori-cli/internal/event"
	"github.com/phuc-nt/dandori-cli/internal/jira"
	"github.com/phuc-nt/dandori-cli/internal/quality"
	"github.com/phuc-nt/dandori-cli/internal/taskcontext"
	"github.com/phuc-nt/dandori-cli/internal/util"
	"github.com/phuc-nt/dandori-cli/internal/verify"
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
	taskRunNoVerify  bool
)

func init() {
	taskCmd.AddCommand(taskRunCmd)
	taskRunCmd.Flags().BoolVar(&taskRunDryRun, "dry-run", false, "Preview context without running agent")
	taskRunCmd.Flags().BoolVar(&taskRunNoContext, "no-context", false, "Skip context injection")
	taskRunCmd.Flags().BoolVar(&taskRunNoSync, "no-sync", false, "Skip post-run Jira/Confluence sync")
	taskRunCmd.Flags().BoolVar(&taskRunNoVerify, "no-verify", false, "Bypass pre-sync verify gate (emergency override)")
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
		// Prefer dedicated confluence creds; fall back to Jira creds for
		// Cloud-style setups where one API token covers both.
		confUser := cfg.Confluence.User
		if confUser == "" {
			confUser = cfg.Jira.User
		}
		confToken := cfg.Confluence.Token
		if confToken == "" {
			confToken = cfg.Jira.Token
		}
		confClient = confluence.NewClient(confluence.ClientConfig{
			BaseURL: cfg.Confluence.BaseURL,
			User:    confUser,
			Token:   confToken,
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

	// Open DB up-front so we can pre-create the runs row and tag taskcontext
	// events with a real run_id (Layer-3 phase 02).
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

	// Pre-create runID + pending run row so context-fetch events can reference it.
	// engineerName is populated after fetch below; pre-create with empty string (NULL).
	runID := util.GenerateRunID()
	if err := insertPendingRun(localDB, runID, issueKey, ""); err != nil {
		// Non-fatal: tracking degrades to wrapper-managed insert.
		fmt.Printf("Warning: pre-create run row failed: %v\n", err)
	}
	recorder := event.NewRecorder(localDB)

	// Fetch task context
	var contextFile string
	var taskDescription string
	var taskLabels []string
	if !taskRunNoContext {
		fmt.Printf("Fetching context for %s...\n", issueKey)

		fetcher := taskcontext.NewFetcher(jiraClient, confClient).WithRecorder(recorder, runID)
		taskCtx, err := fetcher.Fetch(ctx, issueKey)
		if err != nil {
			return fmt.Errorf("fetch context: %w", err)
		}

		// Backfill engineer_name now that we have the Jira issue assignee.
		if taskCtx.Assignee != "" {
			if updErr := updateRunEngineerName(localDB, runID, taskCtx.Assignee); updErr != nil {
				fmt.Printf("Warning: set engineer_name failed: %v\n", updErr)
			}
		}

		taskDescription = taskCtx.Description // Save for AC extraction later
		taskLabels = taskCtx.Labels           // Save for verify gate (skip-verify label)

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
	finalCmd := injectClaudeContext(agentCmd, contextFile)

	fmt.Printf("Running: %v\n\n", finalCmd)

	// Get quality config
	qualityCfg := quality.DefaultConfig()
	if cfg.Quality.LintCommand != "" {
		qualityCfg = quality.Config{
			Enabled:     cfg.Quality.Enabled,
			LintCommand: cfg.Quality.LintCommand,
			TestCommand: cfg.Quality.TestCommand,
			Timeout:     cfg.Quality.Timeout,
		}
	}

	// Run with wrapper, reusing the pre-created runID so events stay linked.
	opts := wrapper.Options{
		Command:       finalCmd,
		JiraIssueKey:  issueKey,
		AgentName:     agentName,
		AgentType:     cfg.Agent.Type,
		QualityConfig: qualityCfg,
		RunID:         runID,
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

		// Get git changes (compare HEAD before vs after the run)
		gitHeadAfter := getGitHead()
		gitChanges := getGitChangesBetween(gitHeadBefore, gitHeadAfter)

		// Bug #3 — pre-sync gate (warn-mode per Q1: post comment but stay
		// In Progress when gate fails). The agent may produce exit code 0
		// while fabricating files unrelated to the spec; the gate catches
		// that before we transition to Done.
		var gateRes verify.PreSyncResult
		if taskRunNoVerify {
			// Emergency override: PO requested bypass via --no-verify.
			// Surface as skipped so the Jira comment records why.
			gateRes = verify.PreSyncResult{
				Pass:    true,
				Skipped: true,
				Reason:  "gate bypassed via --no-verify flag",
			}
		} else {
			gateCfg := verify.GateConfig{
				SemanticCheck: cfg.Verify.SemanticCheck,
				QualityGate:   cfg.Verify.QualityGate,
				SkipLabel:     cfg.Verify.SkipLabel,
			}
			gateRes = verify.PreSync(gateCfg, verify.PreSyncInput{
				TaskDescription: taskDescription,
				JiraLabels:      taskLabels,
				ChangedFiles:    gitChanges.FilesChanged,
				WorkspaceDir:    detectTaskWorkspaceDir(issueKey),
				Quality:         qualitySignal(result.QualityAfter),
			})
		}

		// Build comprehensive completion comment
		doneComment := buildCompletionComment(agentName, result, gitChanges, issueKey, taskDescription)

		// Append gate verdict to the comment so reviewers see it.
		doneComment = appendGateVerdict(doneComment, gateRes)

		jiraClient.AddComment(issueKey, doneComment)
		fmt.Println("  ✓ Jira updated")

		if gateRes.Pass {
			// Attribution must be computed BEFORE the Jira transition so
			// the row reflects the tree state the human is signing off on.
			// Failure here is non-fatal — attribution is observability.
			finalHead := getFullGitHead()
			if finalHead != "" {
				if err := attribution.ComputeAndPersist(localDB, issueKey, ".", finalHead); err != nil {
					fmt.Printf("Warning: attribution compute failed: %v\n", err)
				}
			}

			if err := jiraClient.TransitionToDone(issueKey, jira.DefaultStatusMapping); err != nil {
				fmt.Printf("Warning: could not transition to Done: %v\n", err)
			}
			if gateRes.Skipped {
				fmt.Printf("  ✓ Transitioned to Done (gate skipped: %s)\n", gateRes.Reason)
			} else {
				fmt.Println("  ✓ Transitioned to Done (gate passed)")
			}
		} else {
			fmt.Printf("  ⚠ Gate flagged: %s\n", gateRes.Reason)
			fmt.Println("  ⚠ Leaving Jira ticket In Progress for human review.")
		}

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

// getFullGitHead returns the full 40-char SHA. Attribution needs this because
// git blame emits full SHAs and we membership-test against rev-list output.
func getFullGitHead() string {
	if out, err := exec.Command("git", "rev-parse", "HEAD").Output(); err == nil {
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

// updateRunEngineerName sets engineer_name on an existing run row.
// Called after context fetch resolves the Jira assignee.
func updateRunEngineerName(localDB *db.LocalDB, runID, engineerName string) error {
	_, err := localDB.Exec(`UPDATE runs SET engineer_name = ? WHERE id = ?`, engineerName, runID)
	return err
}

// insertPendingRun reserves a runs row early so events emitted during
// taskcontext.Fetch (Layer-3 phase 02) carry a valid run_id. The wrapper
// upserts the row with full metadata once execution actually starts.
// engineerName is the Jira assignee DisplayName (may be empty for unassigned issues).
func insertPendingRun(localDB *db.LocalDB, runID, jiraKey, engineerName string) error {
	cwd, _ := os.Getwd()
	currentUser, _ := user.Current()
	hostname, _ := os.Hostname()
	username := "unknown"
	if currentUser != nil {
		username = currentUser.Username
	}
	// engineer_name may be empty — store NULL for unassigned so analytics shows "(unassigned)"
	var engineerNameVal interface{}
	if engineerName != "" {
		engineerNameVal = engineerName
	}
	_, err := localDB.Exec(`
		INSERT OR IGNORE INTO runs (
			id, jira_issue_key, agent_type, user, workstation_id,
			cwd, started_at, status, engineer_name
		) VALUES (?, ?, 'claude_code', ?, ?, ?, ?, 'pending', ?)
	`, runID, jiraKey, username, fmt.Sprintf("ws-%s", hostname), cwd, time.Now().Format(time.RFC3339), engineerNameVal)
	return err
}

// detectTaskWorkspaceDir returns the per-task workspace path used by the
// dogfooding convention: demo-workspace/{date}-{TASK-ID}/. The directory may
// not exist yet when called; callers use the path purely for prefix matching
// in the semantic check, so non-existence is fine.
//
// Returns "" when the convention doesn't apply (e.g. real production tasks
// outside dogfooding) so verify.PreSync falls back to bare path-matching.
func detectTaskWorkspaceDir(issueKey string) string {
	if issueKey == "" {
		return ""
	}
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	root := filepath.Join(cwd, "demo-workspace")
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	suffix := "-" + issueKey
	for _, e := range entries {
		if e.IsDir() && strings.HasSuffix(e.Name(), suffix) {
			return filepath.Join("demo-workspace", e.Name())
		}
	}
	return ""
}

// qualitySignalAdapter bridges *quality.Snapshot to verify.QualitySignal so
// the gate can read lint/test failure counts without verify/ depending on
// quality/. Returns nil when no snapshot was captured (e.g. quality disabled).
type qualitySignalAdapter struct{ snap *quality.Snapshot }

func (q qualitySignalAdapter) CountFailures() (int, int) {
	return q.snap.LintErrors, q.snap.TestsFailed
}

func qualitySignal(snap *quality.Snapshot) verify.QualitySignal {
	if snap == nil {
		return nil
	}
	return qualitySignalAdapter{snap: snap}
}

// appendGateVerdict adds a Jira-formatted section describing the verify gate
// outcome. Pass: small confirmation. Fail: clear call-to-action listing what
// the spec referenced that the diff did not touch.
func appendGateVerdict(comment string, res verify.PreSyncResult) string {
	var sb strings.Builder
	sb.WriteString(comment)
	sb.WriteString("\n\nh3. Verify Gate\n")
	if res.Skipped {
		sb.WriteString(fmt.Sprintf("(/) Skipped — %s\n", res.Reason))
		return sb.String()
	}
	if res.Pass {
		sb.WriteString("(/) Passed — diff aligns with task spec.\n")
		return sb.String()
	}
	sb.WriteString(fmt.Sprintf("(!) Flagged — %s\n", res.Reason))
	if len(res.Semantic.Missing) > 0 {
		sb.WriteString("\nSpec referenced but diff did not touch:\n")
		for _, m := range res.Semantic.Missing {
			sb.WriteString(fmt.Sprintf("* {{%s}}\n", m))
		}
	}
	sb.WriteString("\n_Ticket left In Progress — please review the diff and decide whether to transition manually._\n")
	return sb.String()
}

// injectClaudeContext prepends/appends the dandori context-file instructions
// onto a `claude` agent command. Returns the original command unchanged when
// contextFile is empty or the command isn't claude. Pure function — no I/O —
// so it's directly testable.
//
// Bug #1 fix — claude refuses to read the temp context file unless its
// directory is on the allowlist. Auto-inject `--add-dir <tempDir>` when the
// user hasn't already passed --add-dir or --dangerously-skip-permissions.
func injectClaudeContext(agentCmd []string, contextFile string) []string {
	if contextFile == "" || len(agentCmd) == 0 || agentCmd[0] != "claude" {
		return agentCmd
	}

	contextInstruction := fmt.Sprintf("IMPORTANT: First read the task context file at %s which contains the Jira issue details and linked Confluence documentation. Base your work on this context.", contextFile)

	out := append([]string{}, agentCmd...)
	hasPrompt := false
	promptIdx := -1
	hasAddDir := false
	hasSkipPerms := false
	for i, arg := range out {
		if arg == "-p" && i+1 < len(out) {
			hasPrompt = true
			promptIdx = i + 1
		}
		if arg == "--add-dir" {
			hasAddDir = true
		}
		if arg == "--dangerously-skip-permissions" {
			hasSkipPerms = true
		}
	}

	if hasPrompt {
		out[promptIdx] = contextInstruction + "\n\n" + out[promptIdx]
	} else {
		out = append(out, "-p", fmt.Sprintf("%s Then complete the task described in the context.", contextInstruction))
	}

	if !hasAddDir && !hasSkipPerms {
		out = append(out, "--add-dir", filepath.Dir(contextFile))
	}
	return out
}
