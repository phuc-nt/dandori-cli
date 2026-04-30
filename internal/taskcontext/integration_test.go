//go:build integration
// +build integration

package taskcontext

import (
	"context"
	"os"
	"testing"

	"github.com/phuc-nt/dandori-cli/internal/config"
	"github.com/phuc-nt/dandori-cli/internal/confluence"
	"github.com/phuc-nt/dandori-cli/internal/jira"
)

func TestFetchRealContext(t *testing.T) {
	// Skip if not in integration mode
	if os.Getenv("DANDORI_INTEGRATION_TEST") != "1" {
		t.Skip("skipping integration test; set DANDORI_INTEGRATION_TEST=1")
	}

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.Jira.BaseURL == "" {
		t.Skip("jira not configured")
	}

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

	fetcher := NewFetcher(jiraClient, confClient)

	// Test with CLITEST-21 (created by fixtures script)
	ctx := context.Background()
	taskCtx, err := fetcher.Fetch(ctx, "CLITEST-21")
	if err != nil {
		t.Fatalf("fetch context: %v", err)
	}

	// Verify issue fields
	if taskCtx.IssueKey != "CLITEST-21" {
		t.Errorf("issue key: got %q, want CLITEST-21", taskCtx.IssueKey)
	}

	if taskCtx.Summary == "" {
		t.Error("summary is empty")
	}

	if taskCtx.Description == "" {
		t.Error("description is empty")
	}

	// Verify Confluence link was extracted and fetched
	if len(taskCtx.LinkedDocs) == 0 {
		t.Error("no linked docs found, expected Auth Module Architecture")
	} else {
		found := false
		for _, doc := range taskCtx.LinkedDocs {
			if doc.Title == "Auth Module Architecture" {
				found = true
				if doc.Content == "" {
					t.Error("doc content is empty")
				}
				if doc.PageID == "" {
					t.Error("doc pageID is empty")
				}
			}
		}
		if !found {
			t.Error("Auth Module Architecture doc not found in linked docs")
		}
	}

	// Verify markdown generation
	md := taskCtx.ToMarkdown()
	if md == "" {
		t.Error("markdown is empty")
	}

	// Check markdown contains key elements
	checks := []string{
		"# Task: CLITEST-21",
		"Fix auth token refresh",
		"## Related Documentation",
		"Auth Module Architecture",
		"TokenService",
	}

	for _, check := range checks {
		if !contains(md, check) {
			t.Errorf("markdown missing: %q", check)
		}
	}

	t.Logf("Fetched context with %d linked docs", len(taskCtx.LinkedDocs))
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr, 0))
}

func containsAt(s, substr string, start int) bool {
	for i := start; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
