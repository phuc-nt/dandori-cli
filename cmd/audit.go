package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/phuc-nt/dandori-cli/internal/config"
	"github.com/phuc-nt/dandori-cli/internal/confluence"
	"github.com/phuc-nt/dandori-cli/internal/db"
	"github.com/spf13/cobra"
)

// auditCmd is the parent for `dandori audit anchor` and
// `dandori audit verify [--with-anchor]`. Other audit subcommands can hang
// off this later.
var auditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Audit-log tools (anchor / verify)",
	Long: `Manage and verify the local audit_log hash chain.

The audit_log records every user-visible state change with a SHA-256 hash
chain. ` + "`dandori audit anchor`" + ` snapshots the current chain tip — a
local-only row by default, optionally also as a row appended to a witness
page on Confluence. ` + "`dandori audit verify --with-anchor`" + ` then
proves no recorded tip has drifted, which detects silent rewrites of the
local chain that internal-only verification cannot catch.`,
}

var (
	auditAnchorTitle string
	auditAnchorSpace string
	auditAnchorLocal bool

	auditVerifyWithAnchor bool
	auditVerifyLimit      int
)

func init() {
	rootCmd.AddCommand(auditCmd)

	auditCmd.AddCommand(auditAnchorCmd)
	auditAnchorCmd.Flags().StringVar(&auditAnchorTitle, "title", confluence.AuditAnchorPageTitle,
		"Title of the witness page on Confluence")
	auditAnchorCmd.Flags().StringVar(&auditAnchorSpace, "space", "",
		"Space key (defaults to confluence.space_key in config)")
	auditAnchorCmd.Flags().BoolVar(&auditAnchorLocal, "local-only", false,
		"Record the anchor locally only, even if Confluence is configured")

	auditCmd.AddCommand(auditVerifyCmd)
	auditVerifyCmd.Flags().BoolVar(&auditVerifyWithAnchor, "with-anchor", false,
		"Also assert that every recorded anchor still resolves")
	auditVerifyCmd.Flags().IntVar(&auditVerifyLimit, "limit", 0,
		"Verify at most N rows (default 1000; 0 means default)")
}

var auditAnchorCmd = &cobra.Command{
	Use:   "anchor",
	Short: "Snapshot the current audit-log tip (local + optionally Confluence)",
	RunE:  runAuditAnchor,
}

var auditVerifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Verify the audit-log hash chain",
	RunE:  runAuditVerify,
}

// confluenceClientFromConfig opens a Confluence client using the same fallback
// rules as conf-write (dedicated confluence.user/token, falling back to Jira
// creds). Returns (nil, "", nil) when Confluence isn't configured — callers
// can treat that as "fall back to local-only".
func confluenceClientFromConfig(cfg *config.Config) (*confluence.Client, string, error) {
	if cfg == nil || cfg.Confluence.BaseURL == "" || cfg.Confluence.SpaceKey == "" {
		return nil, "", nil
	}
	user := cfg.Confluence.User
	if user == "" {
		user = cfg.Jira.User
	}
	token := cfg.Confluence.Token
	if token == "" {
		token = cfg.Jira.Token
	}
	c := confluence.NewClient(confluence.ClientConfig{
		BaseURL: cfg.Confluence.BaseURL,
		User:    user,
		Token:   token,
		IsCloud: cfg.Confluence.Cloud,
	})
	return c, cfg.Confluence.SpaceKey, nil
}

func runAuditAnchor(cmd *cobra.Command, args []string) error {
	dbPath, err := config.DBPath()
	if err != nil {
		return fmt.Errorf("get db path: %w", err)
	}
	store, err := db.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer store.Close()
	if err := store.Migrate(); err != nil {
		return fmt.Errorf("migrate db: %w", err)
	}

	tipID, tipHash, err := store.LatestAuditTip()
	if err != nil {
		return fmt.Errorf("read audit tip: %w", err)
	}
	if tipID == 0 {
		fmt.Println("audit_log is empty — nothing to anchor")
		return nil
	}

	last, err := store.LatestAuditAnchor()
	if err != nil {
		return fmt.Errorf("latest anchor: %w", err)
	}
	if last != nil && last.LastAuditID == tipID {
		fmt.Printf("Already anchored at id=%d (anchor #%d, %s) — no-op\n",
			last.LastAuditID, last.ID, last.AnchoredAt)
		return nil
	}

	row := confluence.AuditAnchorRow{
		AnchoredAt:   time.Now().UTC().Format(time.RFC3339),
		LastAuditID:  tipID,
		LastCurrHash: tipHash,
	}

	pageID := ""
	pageVersion := 0
	status := "local-only"

	if !auditAnchorLocal {
		client, defaultSpace, _ := confluenceClientFromConfig(Config())
		if client != nil {
			space := auditAnchorSpace
			if space == "" {
				space = defaultSpace
			}
			page, err := confluence.UpsertAuditAnchorPage(context.Background(),
				client, space, auditAnchorTitle, row)
			if err != nil {
				return fmt.Errorf("upsert confluence anchor: %w", err)
			}
			pageID = page.ID
			pageVersion = page.Version.Number
			status = "anchored"
			fmt.Printf("Anchored on Confluence: page %s v%d (%s)\n",
				pageID, pageVersion, strings.TrimSpace(page.Title))
		}
	}

	id, err := store.InsertAuditAnchor(tipID, tipHash, pageID, pageVersion, status)
	if err != nil {
		return fmt.Errorf("record anchor: %w", err)
	}
	fmt.Printf("Local anchor #%d: last_audit_id=%d hash=%s status=%s\n",
		id, tipID, shortHashCLI(tipHash), status)
	return nil
}

func runAuditVerify(cmd *cobra.Command, args []string) error {
	dbPath, err := config.DBPath()
	if err != nil {
		return fmt.Errorf("get db path: %w", err)
	}
	store, err := db.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer store.Close()
	if err := store.Migrate(); err != nil {
		return fmt.Errorf("migrate db: %w", err)
	}

	var res *db.AuditVerifyResult
	if auditVerifyWithAnchor {
		res, err = store.VerifyAuditChainWithAnchors(auditVerifyLimit)
	} else {
		res, err = store.VerifyAuditChain(auditVerifyLimit)
	}
	if err != nil {
		return fmt.Errorf("verify: %w", err)
	}

	if res.Valid {
		if auditVerifyWithAnchor {
			anchors, _ := store.ListAuditAnchors(0)
			fmt.Printf("✓ chain valid (%d entries) · %d anchors cross-checked\n",
				res.Entries, len(anchors))
		} else {
			fmt.Printf("✓ chain valid (%d entries)\n", res.Entries)
		}
		return nil
	}
	return fmt.Errorf("✗ chain INVALID at id=%d (idx %d): %s",
		res.BrokenAt, res.BrokenIndex, res.Reason)
}

func shortHashCLI(h string) string {
	h = strings.TrimSpace(h)
	if len(h) <= 12 {
		return h
	}
	return h[:12] + "…"
}
