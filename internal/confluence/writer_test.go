package confluence

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestWriterCreateReport(t *testing.T) {
	var createdPage CreatePageRequest
	mockClient := &mockConfluenceClient{
		pages: make(map[string]*Page),
	}

	// Override CreatePage to capture request
	writer := NewWriter(WriterConfig{
		Client:       mockClient,
		SpaceKey:     "TEST",
		ParentPageID: "parent123",
	})

	run := RunReport{
		RunID:         "run-abc",
		IssueKey:      "CLITEST-1",
		AgentName:     "alpha",
		Status:        "done",
		Duration:      120 * time.Second,
		CostUSD:       1.50,
		InputTokens:   5000,
		OutputTokens:  2000,
		Model:         "claude-sonnet-4-5-20250514",
		GitHeadBefore: "abc123",
		GitHeadAfter:  "def456",
		FilesChanged:  []string{"main.go", "config.go"},
		GitDiff:       "+func hello() {}\n-func old() {}",
		StartedAt:     time.Now().Add(-2 * time.Minute),
		EndedAt:       time.Now(),
	}

	page, err := writer.CreateReport(context.Background(), run)
	if err != nil {
		t.Fatalf("CreateReport failed: %v", err)
	}
	if page == nil {
		t.Fatal("page should not be nil")
	}

	_ = createdPage // Used by mock
}

func TestRenderReportTemplate(t *testing.T) {
	run := RunReport{
		RunID:        "run-123",
		IssueKey:     "PROJ-456",
		AgentName:    "beta",
		Status:       "done",
		Duration:     90 * time.Second,
		CostUSD:      2.50,
		InputTokens:  10000,
		OutputTokens: 5000,
		Model:        "claude-opus-4-5-20250514",
		FilesChanged: []string{"api.go", "handler.go"},
		GitDiff:      "+new line\n-old line",
	}

	body := RenderReportTemplate(run)

	if body == "" {
		t.Fatal("rendered body should not be empty")
	}
	if !strings.Contains(body, "PROJ-456") {
		t.Error("should contain issue key")
	}
	if !strings.Contains(body, "beta") {
		t.Error("should contain agent name")
	}
	if !strings.Contains(body, "2.50") || !strings.Contains(body, "$") {
		t.Error("should contain cost")
	}
	if !strings.Contains(body, "api.go") {
		t.Error("should contain changed files")
	}
	if !strings.Contains(body, "new line") {
		t.Error("should contain git diff")
	}
}

func TestRenderReportTemplateEmptyFields(t *testing.T) {
	run := RunReport{
		RunID:    "run-empty",
		IssueKey: "TEST-1",
		Status:   "error",
	}

	body := RenderReportTemplate(run)

	if body == "" {
		t.Fatal("should render even with empty fields")
	}
	if !strings.Contains(body, "TEST-1") {
		t.Error("should contain issue key")
	}
}

func TestRenderReportTemplateEscaping(t *testing.T) {
	run := RunReport{
		RunID:    "run-escape",
		IssueKey: "TEST-1",
		Summary:  "<script>alert('xss')</script>",
	}

	body := RenderReportTemplate(run)

	// Summary should be escaped (not in CDATA)
	if strings.Contains(body, "<script>alert") && !strings.Contains(body, "CDATA") {
		t.Error("should escape HTML in summary")
	}
	// The escaped version should exist
	if !strings.Contains(body, "&lt;script&gt;") && !strings.Contains(body, "CDATA") {
		t.Log("body:", body)
	}
}

func TestWriterPageTitle(t *testing.T) {
	run := RunReport{
		RunID:     "abc123",
		IssueKey:  "PROJ-789",
		StartedAt: time.Date(2026, 4, 18, 10, 30, 0, 0, time.UTC),
	}

	title := GenerateReportTitle(run)

	if !strings.Contains(title, "PROJ-789") {
		t.Errorf("title should contain issue key: %s", title)
	}
	if !strings.Contains(title, "abc123") {
		t.Error("title should contain run ID")
	}
}

func TestWriterPageTitle_NoIssueKey(t *testing.T) {
	run := RunReport{
		RunID:     "abc12345xyz",
		StartedAt: time.Date(2026, 4, 18, 10, 30, 0, 0, time.UTC),
	}

	title := GenerateReportTitle(run)

	if strings.HasPrefix(title, " — ") {
		t.Errorf("title should not have leading dash when issue key is empty: %q", title)
	}
	if !strings.Contains(title, "abc12345") {
		t.Errorf("title should contain run ID: %q", title)
	}
}

func TestRunReportValidation(t *testing.T) {
	tests := []struct {
		name    string
		run     RunReport
		wantErr bool
	}{
		{
			name:    "valid",
			run:     RunReport{RunID: "123", IssueKey: "TEST-1"},
			wantErr: false,
		},
		{
			name:    "missing run id",
			run:     RunReport{IssueKey: "TEST-1"},
			wantErr: true,
		},
		{
			name:    "missing issue key is allowed (ad-hoc run)",
			run:     RunReport{RunID: "123"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.run.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
