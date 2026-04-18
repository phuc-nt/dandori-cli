//go:build e2e

package integration

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/phuc-nt/dandori-cli/internal/assignment"
	"github.com/phuc-nt/dandori-cli/internal/confluence"
	"github.com/phuc-nt/dandori-cli/internal/jira"
)

func getJiraClient(t *testing.T) *jira.Client {
	baseURL := os.Getenv("DANDORI_JIRA_URL")
	user := os.Getenv("DANDORI_JIRA_USER")
	token := os.Getenv("DANDORI_JIRA_TOKEN")

	if baseURL == "" || user == "" || token == "" {
		t.Skip("DANDORI_JIRA_URL, DANDORI_JIRA_USER, DANDORI_JIRA_TOKEN required")
	}

	return jira.NewClient(jira.ClientConfig{
		BaseURL: baseURL,
		User:    user,
		Token:   token,
		IsCloud: true,
	})
}

func getConfluenceClient(t *testing.T) *confluence.Client {
	baseURL := os.Getenv("DANDORI_JIRA_URL")
	if baseURL != "" {
		baseURL += "/wiki"
	}
	user := os.Getenv("DANDORI_JIRA_USER")
	token := os.Getenv("DANDORI_JIRA_TOKEN")

	if baseURL == "" || user == "" || token == "" {
		t.Skip("Confluence credentials required")
	}

	return confluence.NewClient(confluence.ClientConfig{
		BaseURL: baseURL,
		User:    user,
		Token:   token,
		IsCloud: true,
	})
}

// TestE2E_FullSprintCycle tests the complete flow:
// 1. Fetch sprint issues from Jira
// 2. Score and suggest agents
// 3. Post suggestion comment
// 4. Simulate run completion
// 5. Write report to Confluence
// 6. Verify data flow
func TestE2E_FullSprintCycle(t *testing.T) {
	jiraClient := getJiraClient(t)
	confClient := getConfluenceClient(t)
	ctx := context.Background()

	t.Log("=== Phase 1: Fetch Sprint Issues ===")

	// Get active sprint
	sprint, err := jiraClient.GetActiveSprint(3) // CLITEST board
	if err != nil {
		t.Fatalf("GetActiveSprint: %v", err)
	}
	if sprint == nil {
		t.Fatal("No active sprint")
	}
	t.Logf("Active sprint: %d - %s", sprint.ID, sprint.Name)

	// Get sprint issues
	issues, err := jiraClient.GetSprintIssues(sprint.ID)
	if err != nil {
		t.Fatalf("GetSprintIssues: %v", err)
	}
	t.Logf("Found %d issues in sprint", len(issues))

	if len(issues) == 0 {
		t.Fatal("No issues in sprint")
	}

	t.Log("=== Phase 2: Agent Assignment ===")

	// Define test agents
	agents := []assignment.AgentConfig{
		{
			Name:                "alpha",
			Capabilities:        []string{"backend", "api", "go"},
			PreferredIssueTypes: []string{"Bug", "Task"},
			MaxConcurrent:       3,
			Active:              true,
		},
		{
			Name:                "beta",
			Capabilities:        []string{"frontend", "react", "typescript"},
			PreferredIssueTypes: []string{"Story"},
			MaxConcurrent:       3,
			Active:              true,
		},
	}

	engine := assignment.NewEngine(nil)

	for _, issue := range issues {
		task := assignment.Task{
			IssueKey:  issue.Key,
			Summary:   issue.Summary,
			IssueType: issue.IssueType,
			Priority:  issue.Priority,
			Labels:    issue.Labels,
		}

		suggestions := engine.Suggest(task, agents)
		if len(suggestions) > 0 {
			top := suggestions[0]
			t.Logf("  %s (%s): suggest %s (%d%%) - %s",
				issue.Key, issue.IssueType, top.AgentName, top.Score, top.Reason)
		}
	}

	t.Log("=== Phase 3: Post Suggestion Comment ===")

	// Pick first issue for full test
	testIssue := issues[0]
	task := assignment.Task{
		IssueKey:  testIssue.Key,
		Summary:   testIssue.Summary,
		IssueType: testIssue.IssueType,
		Labels:    testIssue.Labels,
	}

	suggestions := engine.Suggest(task, agents)
	if len(suggestions) == 0 {
		t.Fatal("No suggestions")
	}

	topAgent := suggestions[0]
	comment := "🤖 *E2E Test - Agent Suggestion*\n\n" +
		"*Suggested:* " + topAgent.AgentName + " (" + itoa(topAgent.Score) + "%)\n" +
		"*Reason:* " + topAgent.Reason + "\n\n" +
		"_This is an automated E2E test comment_"

	if err := jiraClient.AddComment(testIssue.Key, comment); err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	t.Logf("Posted suggestion comment on %s", testIssue.Key)

	t.Log("=== Phase 4: Simulate Run ===")

	// Simulate a completed run
	runReport := confluence.RunReport{
		RunID:        fmt.Sprintf("e%d", time.Now().UnixNano()),
		IssueKey:     testIssue.Key,
		AgentName:    topAgent.AgentName,
		Status:       "done",
		Duration:     45 * time.Second,
		CostUSD:      0.85,
		InputTokens:  3500,
		OutputTokens: 1200,
		Model:        "claude-sonnet-4-5-20250514",
		FilesChanged: []string{"main.go", "handler.go", "test_handler.go"},
		Decisions:    []string{"Used table-driven tests", "Added error handling"},
		GitDiff:      "+func TestHandler(t *testing.T) {\n+\t// test implementation\n+}",
		Summary:      "Implemented handler with comprehensive test coverage",
		StartedAt:    time.Now().Add(-45 * time.Second),
		EndedAt:      time.Now(),
	}

	t.Logf("Simulated run: %s on %s by %s", runReport.RunID, runReport.IssueKey, runReport.AgentName)

	t.Log("=== Phase 5: Write Confluence Report ===")

	writer := confluence.NewWriter(confluence.WriterConfig{
		Client:   confClient,
		SpaceKey: "CLITEST",
	})

	page, err := writer.CreateReport(ctx, runReport)
	if err != nil {
		t.Fatalf("CreateReport: %v", err)
	}
	t.Logf("Created report page: %s (ID: %s)", page.Title, page.ID)

	t.Log("=== Phase 6: Post Completion Comment ===")

	completionComment := "✅ *Agent Run Complete*\n\n" +
		"*Agent:* " + runReport.AgentName + "\n" +
		"*Duration:* 45s\n" +
		"*Cost:* $0.85\n" +
		"*Tokens:* 3500 in / 1200 out\n\n" +
		"[View Report|https://fooknt.atlassian.net/wiki/pages/" + page.ID + "]"

	if err := jiraClient.AddComment(testIssue.Key, completionComment); err != nil {
		t.Fatalf("AddComment completion: %v", err)
	}
	t.Logf("Posted completion comment on %s", testIssue.Key)

	t.Log("=== Phase 7: Verify Report Content ===")

	// Fetch the created page and verify content
	fetchedPage, err := confClient.GetPage(ctx, page.ID)
	if err != nil {
		t.Fatalf("GetPage: %v", err)
	}

	body := fetchedPage.Body.Storage.Value
	if body == "" {
		t.Error("Page body is empty")
	}

	// Convert to markdown and check content
	md := confluence.StorageToMarkdown(body)
	t.Logf("Report content (%d chars):\n%s", len(md), truncate(md, 500))

	t.Log("=== E2E Test Complete ===")
	t.Logf("Summary:")
	t.Logf("  - Sprint: %s (%d issues)", sprint.Name, len(issues))
	t.Logf("  - Test issue: %s", testIssue.Key)
	t.Logf("  - Suggested agent: %s (%d%%)", topAgent.AgentName, topAgent.Score)
	t.Logf("  - Report page: %s", page.Title)
}

// TestE2E_ConfluenceContextFetch tests fetching Confluence pages for context
func TestE2E_ConfluenceContextFetch(t *testing.T) {
	confClient := getConfluenceClient(t)
	ctx := context.Background()

	t.Log("=== Fetch Confluence Pages for Context ===")

	// Search for pages in CLITEST space
	pages, err := confClient.SearchPages(ctx, "CLITEST", "")
	if err != nil {
		t.Fatalf("SearchPages: %v", err)
	}
	t.Logf("Found %d pages in CLITEST space", len(pages))

	if len(pages) == 0 {
		t.Skip("No pages to test")
	}

	// Create reader with cache
	cacheDir := t.TempDir()
	reader := confluence.NewReader(confluence.ReaderConfig{
		Client:   confClient,
		CacheDir: cacheDir,
		TTL:      time.Hour,
	})

	// Fetch and cache each page
	for _, page := range pages[:min(3, len(pages))] {
		cachePath, err := reader.FetchAndCache(ctx, page.ID)
		if err != nil {
			t.Errorf("FetchAndCache %s: %v", page.ID, err)
			continue
		}

		content, _ := reader.GetCachedMarkdown(page.ID)
		t.Logf("  %s: %s (%d chars cached to %s)",
			page.ID, page.Title, len(content), cachePath)
	}

	t.Log("=== Context Assembly ===")

	// Assemble context for a task
	assembler := confluence.NewContextAssembler(confluence.ContextAssemblerConfig{
		Reader:     reader,
		ContextDir: t.TempDir(),
	})

	pageIDs := make([]string, 0)
	for _, p := range pages[:min(2, len(pages))] {
		pageIDs = append(pageIDs, p.ID)
	}

	contextPath, err := assembler.AssembleContext(ctx, "CLITEST-1", pageIDs, "Test context assembly")
	if err != nil {
		t.Fatalf("AssembleContext: %v", err)
	}

	contextContent, _ := os.ReadFile(contextPath)
	t.Logf("Context assembled at %s (%d bytes)", contextPath, len(contextContent))
}

// TestE2E_JiraPollerFlow tests the Jira poller detecting tasks
func TestE2E_JiraPollerFlow(t *testing.T) {
	jiraClient := getJiraClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	t.Log("=== Jira Poller Flow ===")

	var detectedTasks []jira.Issue
	var suggestedTasks []string

	agents := []assignment.AgentConfig{
		{Name: "alpha", Capabilities: []string{"backend"}, Active: true, MaxConcurrent: 3},
	}
	engine := assignment.NewEngine(nil)

	poller := jira.NewPoller(jira.PollerConfig{
		Client:   jiraClient,
		BoardID:  3,
		Interval: 5 * time.Second,
		OnNewTask: func(issue jira.Issue) {
			detectedTasks = append(detectedTasks, issue)
			t.Logf("Detected: %s - %s", issue.Key, issue.Summary)
		},
		OnSuggestAgent: func(issue jira.Issue) (string, int, string) {
			task := assignment.Task{
				IssueKey:  issue.Key,
				IssueType: issue.IssueType,
				Labels:    issue.Labels,
			}
			suggestions := engine.Suggest(task, agents)
			if len(suggestions) > 0 {
				suggestedTasks = append(suggestedTasks, issue.Key)
				return suggestions[0].AgentName, suggestions[0].Score, suggestions[0].Reason
			}
			return "", 0, ""
		},
	})

	// Single poll
	if err := poller.Poll(ctx); err != nil {
		t.Fatalf("Poll: %v", err)
	}

	t.Logf("Detected %d tasks", len(detectedTasks))
	t.Logf("Would suggest agents for %d tasks", len(suggestedTasks))

	if len(detectedTasks) == 0 {
		t.Error("Expected to detect tasks")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if neg {
		digits = append([]byte{'-'}, digits...)
	}
	return string(digits)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
