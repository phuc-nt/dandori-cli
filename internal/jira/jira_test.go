package jira

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestExtractJiraKeyFromBranch(t *testing.T) {
	tests := []struct {
		url       string
		wantID    string
		wantSpace string
	}{
		{
			url:       "https://confluence.example.com/pages/viewpage.action?pageId=12345",
			wantID:    "12345",
			wantSpace: "",
		},
		{
			url:       "https://confluence.example.com/wiki/spaces/PROJ/pages/67890",
			wantID:    "",
			wantSpace: "PROJ",
		},
		{
			url:       "https://example.atlassian.net/wiki/spaces/DEV/pages/123/My+Page",
			wantID:    "",
			wantSpace: "DEV",
		},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			pageID, spaceKey := parseConfluenceURL(tt.url)
			if pageID != tt.wantID {
				t.Errorf("pageID = %q, want %q", pageID, tt.wantID)
			}
			if spaceKey != tt.wantSpace {
				t.Errorf("spaceKey = %q, want %q", spaceKey, tt.wantSpace)
			}
		})
	}
}

func TestIsConfluenceURL(t *testing.T) {
	tests := []struct {
		url  string
		want bool
	}{
		{"https://confluence.example.com/pages/123", true},
		{"https://example.atlassian.net/wiki/spaces/PROJ", true},
		{"https://jira.example.com/browse/PROJ-123", false},
		{"https://github.com/org/repo", false},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			if got := isConfluenceURL(tt.url); got != tt.want {
				t.Errorf("isConfluenceURL(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

func TestIssueHasLabel(t *testing.T) {
	issue := Issue{
		Labels: []string{"backend", "urgent", "dandori-tracked"},
	}

	if !issue.HasLabel("backend") {
		t.Error("should have label backend")
	}
	if !issue.HasLabel("BACKEND") {
		t.Error("label check should be case-insensitive")
	}
	if !issue.IsTracked() {
		t.Error("should be tracked")
	}
	if issue.HasLabel("frontend") {
		t.Error("should not have label frontend")
	}
}

func TestIssueIsAssigned(t *testing.T) {
	issue1 := Issue{AgentName: ""}
	issue2 := Issue{AgentName: "alpha"}

	if issue1.IsAssigned() {
		t.Error("empty agent should not be assigned")
	}
	if !issue2.IsAssigned() {
		t.Error("agent alpha should be assigned")
	}
}

func TestRenderComments(t *testing.T) {
	data := CommentData{
		IssueType:    "Story",
		Labels:       "backend, api",
		AgentName:    "alpha",
		Capabilities: "go, typescript",
		Score:        85,
		AgentList:    "alpha, beta, gamma",
	}

	suggestion, err := RenderSuggestion(data)
	if err != nil {
		t.Fatalf("render suggestion: %v", err)
	}
	if suggestion == "" {
		t.Error("suggestion should not be empty")
	}
	if !contains(suggestion, "alpha") {
		t.Error("suggestion should contain agent name")
	}
	if !contains(suggestion, "85%") {
		t.Error("suggestion should contain score")
	}

	runData := CommentData{
		AgentName:     "beta",
		RunID:         "run-123",
		WorkstationID: "ws-001",
		StartedAt:     "2026-04-18T10:00:00Z",
	}

	started, err := RenderRunStarted(runData)
	if err != nil {
		t.Fatalf("render run started: %v", err)
	}
	if !contains(started, "run-123") {
		t.Error("run started should contain run ID")
	}

	completedData := CommentData{
		AgentName:     "beta",
		Duration:      "5m30s",
		InputTokens:   10000,
		OutputTokens:  5000,
		CostUSD:       0.15,
		ExitCode:      0,
		GitHeadBefore: "abc123",
		GitHeadAfter:  "def456",
	}

	completed, err := RenderRunCompleted(completedData)
	if err != nil {
		t.Fatalf("render run completed: %v", err)
	}
	if !contains(completed, "$0.1500") {
		t.Error("completed should contain cost")
	}

	failedData := CommentData{
		AgentName: "gamma",
		Duration:  "1m",
		ExitCode:  1,
		LastError: "compilation failed",
	}

	failed, err := RenderRunFailed(failedData)
	if err != nil {
		t.Fatalf("render run failed: %v", err)
	}
	if !contains(failed, "compilation failed") {
		t.Error("failed should contain error")
	}
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || contains(s[1:], substr)))
}

func TestExtractConfluenceLinks(t *testing.T) {
	links := []RemoteLink{
		{
			Object: struct {
				URL   string `json:"url"`
				Title string `json:"title"`
				Icon  struct {
					URL16x16 string `json:"url16x16"`
				} `json:"icon"`
			}{
				URL:   "https://confluence.example.com/wiki/spaces/PROJ/pages/123",
				Title: "Project Requirements",
			},
		},
		{
			Object: struct {
				URL   string `json:"url"`
				Title string `json:"title"`
				Icon  struct {
					URL16x16 string `json:"url16x16"`
				} `json:"icon"`
			}{
				URL:   "https://github.com/org/repo",
				Title: "GitHub Repo",
			},
		},
	}

	result := ExtractConfluenceLinks(links)
	if len(result) != 1 {
		t.Errorf("expected 1 confluence link, got %d", len(result))
	}
	if result[0].SpaceKey != "PROJ" {
		t.Errorf("expected space key PROJ, got %s", result[0].SpaceKey)
	}
}

func TestNewClient(t *testing.T) {
	cfg := ClientConfig{
		BaseURL: "https://jira.example.com/",
		User:    "user@example.com",
		Token:   "test-token",
		IsCloud: true,
		Timeout: 10 * time.Second,
	}

	client := NewClient(cfg)

	if client.baseURL != "https://jira.example.com" {
		t.Errorf("baseURL should strip trailing slash: %s", client.baseURL)
	}
	if client.user != "user@example.com" {
		t.Error("user not set")
	}
	if !client.isCloud {
		t.Error("isCloud should be true")
	}
}

func TestClientGetIssue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/2/issue/PROJ-123" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") == "" {
			t.Error("missing authorization header")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"key": "PROJ-123",
			"fields": {
				"summary": "Test issue",
				"description": "Test description",
				"issuetype": {"name": "Story"},
				"priority": {"name": "High"},
				"status": {"name": "To Do"},
				"labels": ["backend"],
				"assignee": {"displayName": "John Doe"}
			}
		}`))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		BaseURL: server.URL,
		User:    "user@example.com",
		Token:   "test-token",
		IsCloud: true,
	})

	issue, err := client.GetIssue("PROJ-123")
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}

	if issue.Key != "PROJ-123" {
		t.Errorf("Key = %s, want PROJ-123", issue.Key)
	}
	if issue.Summary != "Test issue" {
		t.Errorf("Summary = %s, want Test issue", issue.Summary)
	}
	if issue.IssueType != "Story" {
		t.Errorf("IssueType = %s, want Story", issue.IssueType)
	}
}

func TestClientGetActiveSprint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"values": [{
				"id": 42,
				"name": "Sprint 5",
				"state": "active"
			}]
		}`))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		BaseURL: server.URL,
		User:    "user",
		Token:   "token",
		IsCloud: true,
	})

	sprint, err := client.GetActiveSprint(1)
	if err != nil {
		t.Fatalf("GetActiveSprint failed: %v", err)
	}

	if sprint == nil {
		t.Fatal("sprint should not be nil")
	}
	if sprint.ID != 42 {
		t.Errorf("Sprint ID = %d, want 42", sprint.ID)
	}
	if sprint.Name != "Sprint 5" {
		t.Errorf("Sprint Name = %s, want Sprint 5", sprint.Name)
	}
}

func TestClientGetActiveSprintEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"values": []}`))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		BaseURL: server.URL,
		User:    "user",
		Token:   "token",
		IsCloud: true,
	})

	sprint, err := client.GetActiveSprint(1)
	if err != nil {
		t.Fatalf("GetActiveSprint failed: %v", err)
	}

	if sprint != nil {
		t.Error("sprint should be nil when no active sprint")
	}
}

func TestClientAddComment(t *testing.T) {
	var receivedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		receivedBody = string(buf[:n])

		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		BaseURL: server.URL,
		User:    "user",
		Token:   "token",
		IsCloud: true,
	})

	err := client.AddComment("PROJ-123", "Test comment")
	if err != nil {
		t.Fatalf("AddComment failed: %v", err)
	}

	if !contains(receivedBody, "Test comment") {
		t.Error("body should contain comment")
	}
}

func TestClientTransitions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{
				"transitions": [
					{"id": "11", "name": "Start Progress", "to": {"name": "In Progress"}},
					{"id": "21", "name": "Done", "to": {"name": "Done"}}
				]
			}`))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		BaseURL: server.URL,
		User:    "user",
		Token:   "token",
		IsCloud: true,
	})

	transitions, err := client.GetTransitions("PROJ-123")
	if err != nil {
		t.Fatalf("GetTransitions failed: %v", err)
	}

	if len(transitions) != 2 {
		t.Errorf("expected 2 transitions, got %d", len(transitions))
	}

	err = client.TransitionTo("PROJ-123", "In Progress")
	if err != nil {
		t.Fatalf("TransitionTo failed: %v", err)
	}
}

func TestClientAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"errorMessages": ["Issue not found"]}`))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		BaseURL: server.URL,
		User:    "user",
		Token:   "token",
		IsCloud: true,
	})

	_, err := client.GetIssue("NONEXISTENT-999")
	if err == nil {
		t.Error("expected error for 404")
	}
	if !contains(err.Error(), "404") {
		t.Errorf("error should mention status code: %v", err)
	}
}

func TestDefaultStatusMapping(t *testing.T) {
	mapping := DefaultStatusMapping

	if mapping.Running != "In Progress" {
		t.Errorf("Running = %s, want In Progress", mapping.Running)
	}
	if mapping.Done != "Done" {
		t.Errorf("Done = %s, want Done", mapping.Done)
	}
	if mapping.Error != "To Do" {
		t.Errorf("Error = %s, want To Do", mapping.Error)
	}
}

func TestPollerNewPoller(t *testing.T) {
	client := NewClient(ClientConfig{
		BaseURL: "https://jira.example.com",
		User:    "user",
		Token:   "token",
	})

	poller := NewPoller(PollerConfig{
		Client:   client,
		BoardID:  42,
		Interval: 60 * time.Second,
	})

	if len(poller.boardIDs) != 1 || poller.boardIDs[0] != 42 {
		t.Errorf("boardIDs = %v, want [42]", poller.boardIDs)
	}
	if poller.interval != 60*time.Second {
		t.Errorf("interval = %v, want 60s", poller.interval)
	}
	if poller.lastIssueSet == nil {
		t.Error("lastIssueSet should be initialized")
	}
}

func TestNewPoller_MergesBoardIDAndBoardIDs(t *testing.T) {
	// Single + list should dedupe and put the legacy single field first.
	poller := NewPoller(PollerConfig{
		BoardID:  3,
		BoardIDs: []int{4, 3, 5},
	})
	want := []int{3, 4, 5}
	if len(poller.boardIDs) != len(want) {
		t.Fatalf("boardIDs = %v, want %v", poller.boardIDs, want)
	}
	for i, v := range want {
		if poller.boardIDs[i] != v {
			t.Errorf("boardIDs[%d] = %d, want %d", i, poller.boardIDs[i], v)
		}
	}
}

func TestNewPoller_BoardIDsOnly(t *testing.T) {
	poller := NewPoller(PollerConfig{BoardIDs: []int{7, 8}})
	if len(poller.boardIDs) != 2 || poller.boardIDs[0] != 7 || poller.boardIDs[1] != 8 {
		t.Errorf("boardIDs = %v, want [7 8]", poller.boardIDs)
	}
}

func TestPollerDefaultInterval(t *testing.T) {
	poller := NewPoller(PollerConfig{
		Client:  nil,
		BoardID: 1,
	})

	if poller.interval != 30*time.Second {
		t.Errorf("default interval = %v, want 30s", poller.interval)
	}
}
