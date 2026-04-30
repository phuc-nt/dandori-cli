//go:build ignore
// +build ignore

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/phuc-nt/dandori-cli/internal/config"
	"github.com/phuc-nt/dandori-cli/internal/confluence"
	"github.com/phuc-nt/dandori-cli/internal/jira"
)

func main() {
	cfg, err := config.Load("")
	if err != nil {
		log.Fatal("load config:", err)
	}

	// Create Confluence client
	confClient := confluence.NewClient(confluence.ClientConfig{
		BaseURL: cfg.Confluence.BaseURL,
		User:    cfg.Jira.User,
		Token:   cfg.Jira.Token,
		IsCloud: cfg.Confluence.Cloud,
	})

	ctx := context.Background()

	// Create Confluence page with auth architecture spec
	confPage, err := confClient.CreatePage(ctx, confluence.CreatePageRequest{
		SpaceKey: cfg.Confluence.SpaceKey,
		Title:    "Auth Module Architecture",
		Body: `<h2>Overview</h2>
<p>The authentication module handles user login, token management, and session lifecycle.</p>

<h2>Components</h2>
<ul>
  <li><strong>TokenService</strong> - JWT generation and validation (internal/auth/token.go)</li>
  <li><strong>SessionStore</strong> - Redis-backed session storage (internal/auth/session.go)</li>
  <li><strong>AuthMiddleware</strong> - Request authentication (internal/middleware/auth.go)</li>
</ul>

<h2>Token Flow</h2>
<ol>
  <li>User submits credentials to /api/auth/login</li>
  <li>TokenService generates access_token (15min) + refresh_token (7d)</li>
  <li>Tokens stored in HttpOnly cookies</li>
  <li>AuthMiddleware validates on each request</li>
  <li>Auto-refresh when access_token expires but refresh_token valid</li>
</ol>

<h2>Known Issues</h2>
<p><strong>BUG:</strong> When refresh_token is used, the old access_token is not invalidated immediately, causing a race condition window where both tokens are valid.</p>

<h2>Acceptance Criteria for Fix</h2>
<ul>
  <li>Old access_token must be invalidated within 1 second of refresh</li>
  <li>No 401 errors during normal token refresh flow</li>
  <li>Add unit tests for race condition scenario</li>
</ul>`,
	})
	if err != nil {
		log.Fatal("create confluence page:", err)
	}
	fmt.Printf("Created Confluence page: %s (ID: %s)\n", confPage.Title, confPage.ID)

	// Create Jira client
	jiraClient := jira.NewClient(jira.ClientConfig{
		BaseURL: cfg.Jira.BaseURL,
		User:    cfg.Jira.User,
		Token:   cfg.Jira.Token,
		IsCloud: cfg.Jira.Cloud,
	})

	// Create Jira issue with Confluence link
	confURL := fmt.Sprintf("%s/pages/%s", cfg.Confluence.BaseURL, confPage.ID)

	issue, err := jiraClient.CreateIssue(jira.CreateIssueRequest{
		ProjectKey: cfg.Project.Key,
		Summary:    "Fix auth token refresh race condition",
		Description: fmt.Sprintf(`Users report intermittent 401 errors during active sessions.

**Root cause analysis:**
When the access_token expires and refresh happens, there's a brief window where the old token is still valid.

**Technical spec:**
See architecture doc: %s

**Acceptance Criteria:**
- [ ] Old access_token invalidated within 1 second of refresh
- [ ] No 401 during valid session
- [ ] Unit tests for race condition

**Files likely affected:**
- internal/auth/token.go
- internal/auth/session.go
- internal/middleware/auth.go`, confURL),
		IssueType: "Bug",
	})
	if err != nil {
		log.Fatal("create jira issue:", err)
	}
	fmt.Printf("Created Jira issue: %s - %s\n", issue.Key, issue.Summary)

	// Output as JSON for test
	output := map[string]string{
		"confluence_page_id": confPage.ID,
		"confluence_url":     confURL,
		"jira_key":           issue.Key,
	}
	json.NewEncoder(os.Stdout).Encode(output)
}
