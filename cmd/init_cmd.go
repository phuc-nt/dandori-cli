package cmd

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"syscall"

	"github.com/mattn/go-isatty"
	"github.com/phuc-nt/dandori-cli/internal/config"
	"github.com/phuc-nt/dandori-cli/internal/confluence"
	"github.com/phuc-nt/dandori-cli/internal/db"
	"github.com/phuc-nt/dandori-cli/internal/jira"
	"github.com/phuc-nt/dandori-cli/internal/shellrc"
	"github.com/phuc-nt/dandori-cli/internal/util"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// initUninstallShell is set by the --uninstall-shell flag.
var initUninstallShell bool

var initTimeout int // hidden flag, seconds

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize dandori configuration and database",
	Long: `Creates ~/.dandori/ directory with config.yaml and local SQLite database.
Prompts for Jira/Confluence credentials and tests the connection live.

v0.9.0: no longer installs shell aliases. Use 'dandori claude' for ad-hoc
tracking, or 'dandori init --uninstall-shell' to remove the v0.8 alias block.`,
	RunE: runInit,
}

func init() {
	initCmd.Flags().BoolVar(&initUninstallShell, "uninstall-shell", false, "Remove the dandori-managed alias block from your shell rc file")
	initCmd.Flags().IntVar(&initTimeout, "init-timeout", 10, "Connection test timeout in seconds")
	_ = initCmd.Flags().MarkHidden("init-timeout")
	rootCmd.AddCommand(initCmd)
}

// runInit is the entry point for `dandori init`.
func runInit(cmd *cobra.Command, args []string) error {
	configDir, err := config.ConfigDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	slog.Info("created config directory", "path", configDir)

	configPath, err := config.ConfigPath()
	if err != nil {
		return err
	}

	cfg, isNew, err := resolveConfig(configPath)
	if err != nil {
		return err
	}

	if isNew {
		if err := runWizard(cfg); err != nil {
			return err
		}
		if err := config.Save(cfg, configPath); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
		slog.Info("created config file", "path", configPath)
	} else {
		slog.Info("config file already exists, skipped wizard", "path", configPath)
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
	if err := localDB.Migrate(); err != nil {
		return fmt.Errorf("migrate database: %w", err)
	}
	slog.Info("initialized database", "path", dbPath)

	wsID := util.GenerateWorkstationID()
	fmt.Printf("\nInitialization complete!\n")
	fmt.Printf("  Config:      %s\n", configPath)
	fmt.Printf("  Database:    %s\n", dbPath)
	fmt.Printf("  Workstation: %s\n", wsID)

	handleShellRC()

	if isNew {
		printNextSteps()
	}

	return nil
}

// handleShellRC deals with the legacy alias block introduced in v0.8.
// If --uninstall-shell was passed: remove the block.
// Otherwise: detect and warn the user if the block is still present.
func handleShellRC() {
	rcFile, err := shellrc.RCFilePath()
	if err != nil {
		// Unknown / unsupported shell — nothing to do.
		return
	}

	if initUninstallShell {
		if err := shellrc.UninstallAliases(rcFile); err != nil {
			fmt.Fprintf(os.Stderr, "\nWarning: could not remove alias block from %s: %v\n", rcFile, err)
			return
		}
		fmt.Printf("\nShell alias block removed from %s\n", rcFile)
		fmt.Println("  'claude' now resolves to your system binary again.")
		return
	}

	// Detect legacy block and warn.
	if shellrc.HasAliasBlock(rcFile) {
		fmt.Printf("\nNote: v0.9.0 removed the global 'claude' alias.\n")
		fmt.Printf("  Your rc file %s still contains the dandori alias block.\n", rcFile)
		fmt.Printf("  Run 'dandori init --uninstall-shell' to remove it.\n")
	}
}

// resolveConfig loads the existing config or returns a fresh default.
// isNew=true means no config file existed (or user confirmed overwrite).
func resolveConfig(configPath string) (cfg *config.Config, isNew bool, err error) {
	_, statErr := os.Stat(configPath)
	if os.IsNotExist(statErr) {
		return config.DefaultConfig(), true, nil
	}
	if statErr != nil {
		return nil, false, fmt.Errorf("stat config: %w", statErr)
	}

	// Config already exists — ask user.
	fmt.Printf("Config already exists: %s\n", configPath)
	fmt.Print("Overwrite with new wizard? [y/N]: ")
	reader := bufio.NewReader(os.Stdin)
	ans, _ := reader.ReadString('\n')
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(ans)), "y") {
		return config.DefaultConfig(), true, nil
	}

	existing, loadErr := config.Load(configPath)
	if loadErr != nil {
		return nil, false, loadErr
	}
	return existing, false, nil
}

// runWizard drives all interactive prompts and live connection tests.
func runWizard(cfg *config.Config) error {
	reader := bufio.NewReader(os.Stdin)

	// ── Step 1: Server URL ──────────────────────────────────────────────────
	fmt.Printf("Server URL [%s]: ", cfg.ServerURL)
	if v := readLine(reader); v != "" {
		cfg.ServerURL = v
	}

	// ── Step 2: Jira base URL ───────────────────────────────────────────────
	fmt.Print("Jira Base URL (e.g. https://acme.atlassian.net): ")
	jiraBaseURL := readLine(reader)
	if jiraBaseURL != "" {
		cfg.Jira.BaseURL = jiraBaseURL
	}

	// ── Step 3: Jira email ──────────────────────────────────────────────────
	fmt.Print("Jira Email: ")
	jiraEmail := readLine(reader)
	if jiraEmail != "" {
		cfg.Jira.User = jiraEmail
	}

	// ── Step 4: Jira API token (masked) ────────────────────────────────────
	fmt.Printf("Jira API Token (https://id.atlassian.com/manage-profile/security/api-tokens): ")
	jiraToken, err := readSecret(reader)
	if err != nil {
		return fmt.Errorf("read token: %w", err)
	}
	fmt.Println() // newline after masked input
	if jiraToken != "" {
		cfg.Jira.Token = jiraToken
	}
	cfg.Jira.Cloud = true // Cloud-first default

	// ── Step 5: Jira project key ────────────────────────────────────────────
	fmt.Print("Jira Project Key (e.g. PROJ): ")
	if v := readLine(reader); v != "" {
		cfg.Project.Key = v
	}

	// ── Step 6: Test Jira connection ────────────────────────────────────────
	if cfg.Jira.BaseURL != "" && cfg.Jira.User != "" && cfg.Jira.Token != "" {
		fmt.Print("Testing Jira connection... ")
		displayName, connErr := jira.TestConnection(cfg.Jira.BaseURL, cfg.Jira.User, cfg.Jira.Token)
		if connErr != nil {
			fmt.Printf("✗ FAILED: %v\n", connErr)
			if !askSaveAnyway(reader) {
				return fmt.Errorf("init aborted by user after Jira connection failure")
			}
		} else {
			fmt.Printf("✓ Connected as %s\n", displayName)
		}
	}

	// ── Step 7: Confluence enable ───────────────────────────────────────────
	fmt.Print("Enable Confluence integration? [Y/n]: ")
	confAns := readLine(reader)
	enableConf := !strings.HasPrefix(strings.ToLower(confAns), "n")

	if enableConf {
		// ── Step 8: Confluence base URL (auto-fill from Jira on Cloud) ──────
		defaultConfURL := deriveConfluenceURL(cfg.Jira.BaseURL)
		if defaultConfURL != "" {
			fmt.Printf("Confluence Base URL [%s]: ", defaultConfURL)
		} else {
			fmt.Print("Confluence Base URL (e.g. https://acme.atlassian.net/wiki): ")
		}
		confBaseURL := readLine(reader)
		switch {
		case confBaseURL != "":
			cfg.Confluence.BaseURL = confBaseURL
		case defaultConfURL != "":
			cfg.Confluence.BaseURL = defaultConfURL
		}

		// ── Step 9: Confluence space key ────────────────────────────────────
		fmt.Print("Confluence Space Key (e.g. ENG): ")
		if v := readLine(reader); v != "" {
			cfg.Confluence.SpaceKey = v
		}

		// Cloud uses same creds as Jira — no separate prompt.
		cfg.Confluence.Cloud = true

		// ── Step 10: Test Confluence connection ─────────────────────────────
		if cfg.Confluence.BaseURL != "" && cfg.Confluence.SpaceKey != "" {
			email := cfg.Jira.User
			token := cfg.Jira.Token
			fmt.Print("Testing Confluence connection... ")
			spaceName, connErr := confluence.TestConnection(
				cfg.Confluence.BaseURL, cfg.Confluence.SpaceKey, email, token,
			)
			if connErr != nil {
				fmt.Printf("✗ FAILED: %v\n", connErr)
				if !askSaveAnyway(reader) {
					return fmt.Errorf("init aborted by user after Confluence connection failure")
				}
			} else {
				fmt.Printf("✓ Space: %s\n", spaceName)
			}
		}
	}

	// ── Step 11: Agent name ─────────────────────────────────────────────────
	fmt.Printf("Agent Name [%s]: ", cfg.Agent.Name)
	if v := readLine(reader); v != "" {
		cfg.Agent.Name = v
	}

	// ── Step 12: Quality tracking ───────────────────────────────────────────
	fmt.Print("Enable quality tracking (lint/test after each run) [y/N]: ")
	if strings.HasPrefix(strings.ToLower(readLine(reader)), "y") {
		cfg.Quality.Enabled = true
	}

	// ── Step 13: Background watch daemon hint ──────────────────────────────
	fmt.Print("Enable background watch daemon? [Y/n]: ")
	watchAns := readLine(reader)
	if !strings.HasPrefix(strings.ToLower(watchAns), "n") {
		fmt.Println("  Hint: Run `dandori watch enable` to start the background daemon.")
	}

	return nil
}

// deriveConfluenceURL returns the default Confluence Cloud URL derived from
// the Jira base URL (appends /wiki for *.atlassian.net).
func deriveConfluenceURL(jiraBaseURL string) string {
	if strings.HasSuffix(strings.TrimSuffix(jiraBaseURL, "/"), ".atlassian.net") ||
		strings.Contains(jiraBaseURL, ".atlassian.net/") {
		base := strings.TrimSuffix(jiraBaseURL, "/")
		// Avoid double /wiki if someone passed the wiki URL as Jira URL.
		if !strings.HasSuffix(base, "/wiki") {
			return base + "/wiki"
		}
		return base
	}
	return ""
}

// readLine trims whitespace from a buffered line read.
func readLine(r *bufio.Reader) string {
	line, _ := r.ReadString('\n')
	return strings.TrimSpace(line)
}

// readSecret reads a password/token without echo when stdin is a real tty.
// In non-tty environments (pipes, CI) it falls back to reading a plain line
// from the shared bufio.Reader so the caller's buffered input is not lost.
func readSecret(fallback *bufio.Reader) (string, error) {
	fd := int(syscall.Stdin)
	if isatty.IsTerminal(uintptr(fd)) {
		b, err := term.ReadPassword(fd)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	// Non-tty (pipe / CI): use the caller's shared reader.
	line, err := fallback.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// askSaveAnyway prompts "save anyway?" and returns true if the user agrees.
func askSaveAnyway(r *bufio.Reader) bool {
	fmt.Print("Save config anyway and continue? [y/N]: ")
	return strings.HasPrefix(strings.ToLower(readLine(r)), "y")
}

// printNextSteps prints the post-wizard guidance.
func printNextSteps() {
	fmt.Printf("\nNext steps:\n")
	fmt.Printf("  1. Run `dandori claude \"your prompt\"` — ad-hoc tracking without Jira context\n")
	fmt.Printf("  2. Run `dandori task run PROJ-123` — full Jira-driven flow with context injection\n")
	fmt.Printf("  3. View analytics: `dandori analytics all`\n")
}
