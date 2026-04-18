//go:build integration

package jira

import (
	"os"
	"strings"
	"testing"
	"time"
)

func getIntegrationClient(t *testing.T) *Client {
	baseURL := os.Getenv("DANDORI_JIRA_URL")
	user := os.Getenv("DANDORI_JIRA_USER")
	token := os.Getenv("DANDORI_JIRA_TOKEN")

	if baseURL == "" || user == "" || token == "" {
		t.Skip("DANDORI_JIRA_URL, DANDORI_JIRA_USER, DANDORI_JIRA_TOKEN required")
	}

	return NewClient(ClientConfig{
		BaseURL: baseURL,
		User:    user,
		Token:   token,
		IsCloud: true,
	})
}

// === Issue Tests ===

func TestIntegration_GetIssue(t *testing.T) {
	client := getIntegrationClient(t)

	issue, err := client.GetIssue("CLITEST-1")
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}

	if issue.Key != "CLITEST-1" {
		t.Errorf("key = %s, want CLITEST-1", issue.Key)
	}
	if issue.Summary == "" {
		t.Error("summary should not be empty")
	}
	t.Logf("Issue: %s - %s (%s)", issue.Key, issue.Summary, issue.IssueType)
}

func TestIntegration_GetIssueNotFound(t *testing.T) {
	client := getIntegrationClient(t)

	_, err := client.GetIssue("CLITEST-99999")
	if err == nil {
		t.Error("should return error for non-existent issue")
	}
	if !strings.Contains(err.Error(), "404") && !strings.Contains(err.Error(), "not found") {
		t.Logf("Error (expected 404): %v", err)
	}
}

func TestIntegration_GetIssueFields(t *testing.T) {
	client := getIntegrationClient(t)

	issue, err := client.GetIssue("CLITEST-1")
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}

	// Verify all fields are populated
	t.Logf("Key: %s", issue.Key)
	t.Logf("Summary: %s", issue.Summary)
	t.Logf("IssueType: %s", issue.IssueType)
	t.Logf("Status: %s", issue.Status)
	t.Logf("Priority: %s", issue.Priority)
	t.Logf("Labels: %v", issue.Labels)
	t.Logf("CreatedAt: %v", issue.CreatedAt)

	if issue.IssueType == "" {
		t.Error("IssueType should not be empty")
	}
	if issue.Status == "" {
		t.Error("Status should not be empty")
	}
}

// === Board Tests ===

func TestIntegration_GetBoards(t *testing.T) {
	client := getIntegrationClient(t)

	boards, err := client.GetBoards("CLITEST")
	if err != nil {
		t.Fatalf("GetBoards: %v", err)
	}

	if len(boards) == 0 {
		t.Error("expected at least 1 board")
	}
	for _, b := range boards {
		t.Logf("Board: %d - %s (type: %s)", b.ID, b.Name, b.Type)
	}
}

func TestIntegration_GetBoardsInvalidProject(t *testing.T) {
	client := getIntegrationClient(t)

	boards, err := client.GetBoards("NONEXISTENT")
	if err != nil {
		t.Logf("Error (expected): %v", err)
		return
	}

	if len(boards) != 0 {
		t.Error("should return empty boards for non-existent project")
	}
}

// === Sprint Tests ===

func TestIntegration_GetActiveSprint(t *testing.T) {
	client := getIntegrationClient(t)

	boardID := 3 // CLITEST board
	sprint, err := client.GetActiveSprint(boardID)
	if err != nil {
		t.Fatalf("GetActiveSprint: %v", err)
	}

	if sprint == nil {
		t.Log("No active sprint")
		return
	}

	t.Logf("Sprint: %d - %s (state: %s)", sprint.ID, sprint.Name, sprint.State)
	if sprint.State != "active" {
		t.Errorf("state = %s, want active", sprint.State)
	}
}

func TestIntegration_GetActiveSprintInvalidBoard(t *testing.T) {
	client := getIntegrationClient(t)

	_, err := client.GetActiveSprint(99999)
	if err == nil {
		t.Error("should return error for invalid board")
	}
}

func TestIntegration_GetSprintIssues(t *testing.T) {
	client := getIntegrationClient(t)

	sprintID := 4 // CLITEST sprint
	issues, err := client.GetSprintIssues(sprintID)
	if err != nil {
		t.Fatalf("GetSprintIssues: %v", err)
	}

	t.Logf("Found %d issues in sprint", len(issues))
	for _, iss := range issues {
		t.Logf("  %s (%s) - %s [%s]", iss.Key, iss.IssueType, iss.Summary, iss.Status)
	}

	if len(issues) == 0 {
		t.Error("expected issues in sprint")
	}
}

func TestIntegration_GetSprintIssuesTypes(t *testing.T) {
	client := getIntegrationClient(t)

	issues, err := client.GetSprintIssues(4)
	if err != nil {
		t.Fatalf("GetSprintIssues: %v", err)
	}

	typeCount := make(map[string]int)
	for _, iss := range issues {
		typeCount[iss.IssueType]++
	}

	t.Logf("Issue types in sprint:")
	for typ, count := range typeCount {
		t.Logf("  %s: %d", typ, count)
	}
}

// === Search Tests ===

func TestIntegration_SearchIssues(t *testing.T) {
	client := getIntegrationClient(t)

	issues, err := client.SearchIssues("project = CLITEST", 10)
	if err != nil {
		t.Fatalf("SearchIssues: %v", err)
	}

	if len(issues) == 0 {
		t.Error("expected at least 1 issue")
	}
	t.Logf("Found %d issues", len(issues))
}

func TestIntegration_SearchIssuesByType(t *testing.T) {
	client := getIntegrationClient(t)

	issues, err := client.SearchIssues("project = CLITEST AND issuetype = Bug", 10)
	if err != nil {
		t.Fatalf("SearchIssues: %v", err)
	}

	for _, iss := range issues {
		if iss.IssueType != "Bug" {
			t.Errorf("issue %s type = %s, want Bug", iss.Key, iss.IssueType)
		}
	}
	t.Logf("Found %d Bug issues", len(issues))
}

func TestIntegration_SearchIssuesByStatus(t *testing.T) {
	client := getIntegrationClient(t)

	issues, err := client.SearchIssues("project = CLITEST AND status = 'To Do'", 10)
	if err != nil {
		t.Fatalf("SearchIssues: %v", err)
	}

	t.Logf("Found %d 'To Do' issues", len(issues))
}

func TestIntegration_SearchIssuesInvalidJQL(t *testing.T) {
	client := getIntegrationClient(t)

	_, err := client.SearchIssues("invalid jql syntax !!!", 10)
	if err == nil {
		t.Error("should return error for invalid JQL")
	}
	t.Logf("Error (expected): %v", err)
}

// === Comment Tests ===

func TestIntegration_AddComment(t *testing.T) {
	client := getIntegrationClient(t)

	comment := "Integration test comment - " + time.Now().Format("2006-01-02 15:04:05")
	err := client.AddComment("CLITEST-1", comment)
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	t.Log("Comment added successfully")
}

func TestIntegration_AddCommentWithMarkdown(t *testing.T) {
	client := getIntegrationClient(t)

	comment := "*Bold text*\n\n" +
		"- Item 1\n" +
		"- Item 2\n\n" +
		"{{code}}\nsome code\n{{code}}"

	err := client.AddComment("CLITEST-1", comment)
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	t.Log("Markdown comment added")
}

func TestIntegration_AddCommentInvalidIssue(t *testing.T) {
	client := getIntegrationClient(t)

	err := client.AddComment("CLITEST-99999", "test")
	if err == nil {
		t.Error("should return error for invalid issue")
	}
}

// === Remote Links Tests ===

func TestIntegration_GetRemoteLinks(t *testing.T) {
	client := getIntegrationClient(t)

	links, err := client.GetRemoteLinks("CLITEST-1")
	if err != nil {
		t.Fatalf("GetRemoteLinks: %v", err)
	}

	t.Logf("Found %d remote links", len(links))
	for _, link := range links {
		t.Logf("  %s: %s", link.Object.Title, link.Object.URL)
	}
}

// === Transition Tests ===

func TestIntegration_GetTransitions(t *testing.T) {
	client := getIntegrationClient(t)

	issue, err := client.GetIssue("CLITEST-1")
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}

	t.Logf("Current status: %s", issue.Status)
	// Transitions are included in issue expand
}

// === Label Tests ===

func TestIntegration_AddLabel(t *testing.T) {
	client := getIntegrationClient(t)

	label := "test-label-" + time.Now().Format("150405")
	err := client.AddLabel("CLITEST-1", label)
	if err != nil {
		t.Fatalf("AddLabel: %v", err)
	}
	t.Logf("Label '%s' added", label)
}

// === Poller Tests ===

func TestIntegration_PollerSinglePoll(t *testing.T) {
	client := getIntegrationClient(t)

	var detected []string
	poller := NewPoller(PollerConfig{
		Client:   client,
		BoardID:  3,
		Interval: 30 * time.Second,
		OnNewTask: func(issue Issue) {
			detected = append(detected, issue.Key)
		},
	})

	// Reset to detect all as new
	poller.lastIssueSet = make(map[string]bool)

	ctx := t.Context()
	if err := poller.Poll(ctx); err != nil {
		t.Fatalf("Poll: %v", err)
	}

	t.Logf("Detected %d tasks: %v", len(detected), detected)
	if len(detected) == 0 {
		t.Error("expected to detect tasks")
	}
}

func TestIntegration_PollerNoNewTasks(t *testing.T) {
	client := getIntegrationClient(t)

	var detected []string
	poller := NewPoller(PollerConfig{
		Client:   client,
		BoardID:  3,
		Interval: 30 * time.Second,
		OnNewTask: func(issue Issue) {
			detected = append(detected, issue.Key)
		},
	})

	ctx := t.Context()

	// First poll - detect all
	if err := poller.Poll(ctx); err != nil {
		t.Fatalf("First poll: %v", err)
	}
	firstCount := len(detected)

	// Second poll - should detect none
	detected = nil
	if err := poller.Poll(ctx); err != nil {
		t.Fatalf("Second poll: %v", err)
	}

	t.Logf("First poll: %d, Second poll: %d", firstCount, len(detected))
	if len(detected) != 0 {
		t.Error("second poll should not detect new tasks")
	}
}

// === Confluence Link Extraction ===

func TestIntegration_ExtractConfluenceLinks(t *testing.T) {
	client := getIntegrationClient(t)

	links, err := client.GetRemoteLinks("CLITEST-1")
	if err != nil {
		t.Fatalf("GetRemoteLinks: %v", err)
	}

	confLinks := ExtractConfluenceLinks(links)
	t.Logf("Found %d Confluence links", len(confLinks))
	for _, cl := range confLinks {
		t.Logf("  Page %s: %s (%s)", cl.PageID, cl.Title, cl.SpaceKey)
	}
}
