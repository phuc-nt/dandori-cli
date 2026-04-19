//go:build ignore

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

	confClient := confluence.NewClient(confluence.ClientConfig{
		BaseURL: cfg.Confluence.BaseURL,
		User:    cfg.Jira.User,
		Token:   cfg.Jira.Token,
		IsCloud: cfg.Confluence.Cloud,
	})

	jiraClient := jira.NewClient(jira.ClientConfig{
		BaseURL: cfg.Jira.BaseURL,
		User:    cfg.Jira.User,
		Token:   cfg.Jira.Token,
		IsCloud: cfg.Jira.Cloud,
	})

	ctx := context.Background()
	var tasks []map[string]string

	// 1. Create API Design doc
	apiDoc, err := confClient.CreatePage(ctx, confluence.CreatePageRequest{
		SpaceKey: cfg.Confluence.SpaceKey,
		Title:    "REST API Design Guidelines",
		Body: `<h2>API Conventions</h2>
<ul>
<li>Use kebab-case for URLs: /api/user-profiles</li>
<li>Use camelCase for JSON fields: {"firstName": "John"}</li>
<li>Always return proper HTTP status codes</li>
<li>Include pagination for list endpoints</li>
</ul>
<h2>Error Response Format</h2>
<pre><code>{
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "Invalid input",
    "details": [{"field": "email", "issue": "invalid format"}]
  }
}</code></pre>
<h2>Authentication</h2>
<p>All endpoints require Bearer token in Authorization header.</p>`,
	})
	if err != nil {
		log.Printf("WARN: create API doc: %v", err)
	} else {
		fmt.Printf("Created Confluence: %s (ID: %s)\n", apiDoc.Title, apiDoc.ID)
	}

	// 2. Create Database Schema doc
	dbDoc, err := confClient.CreatePage(ctx, confluence.CreatePageRequest{
		SpaceKey: cfg.Confluence.SpaceKey,
		Title:    "Database Schema v2",
		Body: `<h2>Users Table</h2>
<table>
<tr><th>Column</th><th>Type</th><th>Description</th></tr>
<tr><td>id</td><td>UUID</td><td>Primary key</td></tr>
<tr><td>email</td><td>VARCHAR(255)</td><td>Unique, indexed</td></tr>
<tr><td>password_hash</td><td>VARCHAR(255)</td><td>bcrypt hash</td></tr>
<tr><td>created_at</td><td>TIMESTAMP</td><td>Auto-set</td></tr>
</table>
<h2>Sessions Table</h2>
<p>Stores active user sessions with TTL of 7 days.</p>
<h2>Migrations</h2>
<p>Run: <code>make db-migrate</code></p>`,
	})
	if err != nil {
		log.Printf("WARN: create DB doc: %v", err)
	} else {
		fmt.Printf("Created Confluence: %s (ID: %s)\n", dbDoc.Title, dbDoc.ID)
	}

	// === JIRA TASKS ===

	// Task 1: Multiple Confluence links
	if apiDoc != nil && dbDoc != nil {
		issue, err := jiraClient.CreateIssue(jira.CreateIssueRequest{
			ProjectKey: cfg.Project.Key,
			Summary:    "Implement user registration endpoint",
			Description: fmt.Sprintf(`Implement POST /api/users endpoint for user registration.

**API Design:** %s/pages/%s
**Database Schema:** %s/pages/%s

**Acceptance Criteria:**
- [ ] Validates email format
- [ ] Hashes password with bcrypt
- [ ] Returns 201 with user object (no password)
- [ ] Returns 409 if email exists
- [ ] Follows API conventions from design doc

**Technical Notes:**
- Use existing DB connection pool
- Add rate limiting (10 req/min per IP)`,
				cfg.Confluence.BaseURL, apiDoc.ID,
				cfg.Confluence.BaseURL, dbDoc.ID),
			IssueType: "Task",
		})
		if err != nil {
			log.Printf("ERROR: create task 1: %v", err)
		} else {
			fmt.Printf("Created Jira: %s - %s\n", issue.Key, issue.Summary)
			tasks = append(tasks, map[string]string{
				"key":  issue.Key,
				"type": "multiple_confluence_links",
			})
		}
	}

	// Task 2: Bug with steps to reproduce
	issue2, err := jiraClient.CreateIssue(jira.CreateIssueRequest{
		ProjectKey: cfg.Project.Key,
		Summary:    "Login fails silently on network timeout",
		Description: `**Environment:** Production, Chrome 120, macOS

**Steps to Reproduce:**
1. Open login page
2. Enter valid credentials
3. Disconnect network briefly during submit
4. Reconnect network

**Expected:** Error message shown, retry button available
**Actual:** Spinner keeps spinning, no feedback

**Logs:**
` + "```" + `
2024-04-19 10:23:45 ERROR: fetch timeout after 30s
2024-04-19 10:23:45 WARN: retry queue full, dropping request
` + "```" + `

**Impact:** High - users think system is broken
**Workaround:** Refresh page manually

**Root Cause Analysis:**
The fetch wrapper doesn't handle AbortError properly. See internal/api/client.ts:45`,
		IssueType: "Bug",
	})
	if err != nil {
		log.Printf("ERROR: create task 2: %v", err)
	} else {
		fmt.Printf("Created Jira: %s - %s\n", issue2.Key, issue2.Summary)
		tasks = append(tasks, map[string]string{
			"key":  issue2.Key,
			"type": "bug_with_steps",
		})
	}

	// Task 3: Story with user story format
	issue3, err := jiraClient.CreateIssue(jira.CreateIssueRequest{
		ProjectKey: cfg.Project.Key,
		Summary:    "User can export data as CSV",
		Description: `**User Story:**
As a user, I want to export my data as CSV so that I can analyze it in Excel.

**Acceptance Criteria:**
- [ ] Export button visible on dashboard
- [ ] CSV includes all visible columns
- [ ] Date format: YYYY-MM-DD
- [ ] Numbers without formatting (no commas)
- [ ] UTF-8 encoding with BOM for Excel compatibility
- [ ] Max 10,000 rows per export
- [ ] Progress indicator for large exports

**Out of Scope:**
- PDF export (separate ticket)
- Scheduled exports (Phase 2)

**Design:** [Figma link placeholder]

**Technical Approach:**
1. Add export button to DataTable component
2. Use streaming for large datasets
3. Generate in worker thread to avoid UI freeze`,
		IssueType: "Story",
	})
	if err != nil {
		log.Printf("ERROR: create task 3: %v", err)
	} else {
		fmt.Printf("Created Jira: %s - %s\n", issue3.Key, issue3.Summary)
		tasks = append(tasks, map[string]string{
			"key":  issue3.Key,
			"type": "story_user_format",
		})
	}

	// Task 4: Simple task - no Confluence, minimal description
	issue4, err := jiraClient.CreateIssue(jira.CreateIssueRequest{
		ProjectKey:  cfg.Project.Key,
		Summary:     "Update copyright year to 2026",
		Description: `Change copyright in footer from 2025 to 2026. Files: src/components/Footer.tsx`,
		IssueType:   "Task",
	})
	if err != nil {
		log.Printf("ERROR: create task 4: %v", err)
	} else {
		fmt.Printf("Created Jira: %s - %s\n", issue4.Key, issue4.Summary)
		tasks = append(tasks, map[string]string{
			"key":  issue4.Key,
			"type": "simple_minimal",
		})
	}

	// Task 5: Technical task with code context
	issue5, err := jiraClient.CreateIssue(jira.CreateIssueRequest{
		ProjectKey: cfg.Project.Key,
		Summary:    "Refactor logger to use structured logging",
		Description: `**Current Implementation:**
` + "```go" + `
log.Printf("User %s logged in from %s", userID, ip)
` + "```" + `

**Target Implementation:**
` + "```go" + `
slog.Info("user login",
    slog.String("user_id", userID),
    slog.String("ip", ip),
    slog.Time("at", time.Now()))
` + "```" + `

**Files to Update:**
- internal/auth/login.go
- internal/api/middleware.go
- internal/worker/processor.go

**Migration Steps:**
1. Add slog import
2. Replace log.Printf with slog.Info/Warn/Error
3. Convert string interpolation to structured fields
4. Update tests to check structured output`,
		IssueType: "Task",
	})
	if err != nil {
		log.Printf("ERROR: create task 5: %v", err)
	} else {
		fmt.Printf("Created Jira: %s - %s\n", issue5.Key, issue5.Summary)
		tasks = append(tasks, map[string]string{
			"key":  issue5.Key,
			"type": "technical_with_code",
		})
	}

	// Task 6: Task with checklist and subtasks description
	issue6, err := jiraClient.CreateIssue(jira.CreateIssueRequest{
		ProjectKey: cfg.Project.Key,
		Summary:    "Implement rate limiting middleware",
		Description: `**Objective:** Prevent API abuse by limiting requests per client.

**Implementation Checklist:**
- [ ] Create RateLimiter middleware
- [ ] Use sliding window algorithm
- [ ] Store counters in Redis
- [ ] Return 429 with Retry-After header
- [ ] Add bypass for internal services (X-Internal-Token)
- [ ] Log rate limit hits for monitoring
- [ ] Add Prometheus metrics

**Configuration:**
| Endpoint | Limit | Window |
|----------|-------|--------|
| /api/auth/* | 10 | 1min |
| /api/users/* | 100 | 1min |
| /api/data/* | 1000 | 1min |

**Testing:**
1. Unit test the sliding window logic
2. Integration test with Redis
3. Load test with k6`,
		IssueType: "Task",
	})
	if err != nil {
		log.Printf("ERROR: create task 6: %v", err)
	} else {
		fmt.Printf("Created Jira: %s - %s\n", issue6.Key, issue6.Summary)
		tasks = append(tasks, map[string]string{
			"key":  issue6.Key,
			"type": "checklist_detailed",
		})
	}

	// Output summary
	fmt.Println("\n=== Summary ===")
	output := map[string]interface{}{
		"tasks":           tasks,
		"confluence_docs": []string{},
	}
	if apiDoc != nil {
		output["confluence_docs"] = append(output["confluence_docs"].([]string), apiDoc.ID)
	}
	if dbDoc != nil {
		output["confluence_docs"] = append(output["confluence_docs"].([]string), dbDoc.ID)
	}

	json.NewEncoder(os.Stdout).Encode(output)
}
