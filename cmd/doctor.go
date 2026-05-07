package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/phuc-nt/dandori-cli/internal/config"
	"github.com/phuc-nt/dandori-cli/internal/confluence"
	"github.com/phuc-nt/dandori-cli/internal/jira"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Health check: config, Jira, Confluence, DB, claude binary",
	Long: `Diagnose the dandori-cli installation. Runs the same connection tests as
'dandori init' but on the existing config, plus DB writability and claude
binary presence.

Exit code 0 if all checks pass, 1 otherwise. Useful after a long break
(token expired, space renamed) or before raising a support ticket.`,
	RunE: runDoctor,
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}

// check holds the result of a single doctor probe.
type check struct {
	name   string
	ok     bool
	detail string
}

// checkResult extends check with an optional "skipped" state that doesn't
// count as failure (used for optional integrations like Confluence).
type checkResult struct {
	check
	skipped bool
}

func runDoctor(cmd *cobra.Command, args []string) error {
	results := []checkResult{
		{check: checkConfig(cfg)},
		{check: checkClaudeBinary()},
		{check: checkDB()},
		{check: checkJira(cfg)},
		checkConfluenceResult(cfg),
	}

	allOK := true
	for _, r := range results {
		var mark string
		switch {
		case r.skipped:
			mark = "-"
		case r.ok:
			mark = "✓"
		default:
			mark = "✗"
			allOK = false
		}
		fmt.Printf("%s %s — %s\n", mark, r.name, r.detail)
	}

	fmt.Println()
	if allOK {
		fmt.Println("All checks passed.")
		return nil
	}
	return fmt.Errorf("one or more checks failed")
}

func checkConfig(cfg *config.Config) check {
	if cfg == nil {
		return check{"config", false, "not loaded (PersistentPreRunE failed)"}
	}
	if cfg.Jira.BaseURL == "" || cfg.Jira.User == "" || cfg.Jira.Token == "" {
		return check{"config", false, "incomplete: Jira credentials missing — run 'dandori init'"}
	}
	if cfg.Confluence.BaseURL != "" || cfg.Confluence.SpaceKey != "" {
		return check{"config", true, "loaded with Jira + Confluence credentials"}
	}
	return check{"config", true, "loaded with Jira credentials (Confluence not configured)"}
}

func checkClaudeBinary() check {
	path, err := exec.LookPath("claude")
	if err != nil {
		return check{"claude binary", false, "not found in PATH — install Claude Code CLI"}
	}
	return check{"claude binary", true, path}
}

func checkDB() check {
	dbPath, err := config.DBPath()
	if err != nil {
		return check{"database", false, fmt.Sprintf("path resolution failed: %v", err)}
	}
	// If file exists, check it is writable. If not, check the parent dir is writable.
	if info, err := os.Stat(dbPath); err == nil && !info.IsDir() {
		f, err := os.OpenFile(dbPath, os.O_WRONLY|os.O_APPEND, 0)
		if err != nil {
			return check{"database", false, fmt.Sprintf("%s exists but not writable: %v", dbPath, err)}
		}
		_ = f.Close()
		return check{"database", true, dbPath}
	}
	// File does not exist; verify we can create it
	f, err := os.OpenFile(dbPath, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return check{"database", false, fmt.Sprintf("cannot create %s: %v", dbPath, err)}
	}
	_ = f.Close()
	_ = os.Remove(dbPath) // clean up the empty probe file we just created
	return check{"database", true, fmt.Sprintf("%s (will be created on first run)", dbPath)}
}

func checkJira(cfg *config.Config) check {
	if cfg == nil || cfg.Jira.BaseURL == "" {
		return check{"Jira API", false, "skipped (config incomplete)"}
	}
	name, err := jira.TestConnection(cfg.Jira.BaseURL, cfg.Jira.User, cfg.Jira.Token)
	if err != nil {
		return check{"Jira API", false, err.Error()}
	}
	return check{"Jira API", true, fmt.Sprintf("authenticated as %s", name)}
}

func checkConfluence(cfg *config.Config) check {
	if cfg == nil || cfg.Confluence.BaseURL == "" || cfg.Confluence.SpaceKey == "" {
		return check{"Confluence API", false, "skipped (config incomplete)"}
	}
	// Confluence API uses the same Jira credentials (Atlassian Cloud SSO).
	name, err := confluence.TestConnection(cfg.Confluence.BaseURL, cfg.Confluence.SpaceKey, cfg.Jira.User, cfg.Jira.Token)
	if err != nil {
		return check{"Confluence API", false, err.Error()}
	}
	return check{"Confluence API", true, fmt.Sprintf("space %q readable (%s)", cfg.Confluence.SpaceKey, name)}
}

// checkConfluenceResult distinguishes "not configured (solo mode)" from
// "configured but unreachable". When not configured, it returns skipped=true
// so the doctor exit code stays 0 — Confluence is optional for solo users.
func checkConfluenceResult(cfg *config.Config) checkResult {
	if cfg == nil || cfg.Confluence.BaseURL == "" || cfg.Confluence.SpaceKey == "" {
		return checkResult{
			check:   check{"Confluence API", true, "skipped (not configured — only needed for team docs)"},
			skipped: true,
		}
	}
	name, err := confluence.TestConnection(cfg.Confluence.BaseURL, cfg.Confluence.SpaceKey, cfg.Jira.User, cfg.Jira.Token)
	if err != nil {
		return checkResult{check: check{"Confluence API", false, err.Error()}}
	}
	return checkResult{check: check{"Confluence API", true, fmt.Sprintf("space %q readable (%s)", cfg.Confluence.SpaceKey, name)}}
}
