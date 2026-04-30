//go:build integration

package confluence

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func getIntegrationClient(t *testing.T) *Client {
	baseURL := os.Getenv("DANDORI_CONFLUENCE_URL")
	if baseURL == "" {
		baseURL = os.Getenv("DANDORI_JIRA_URL")
		if baseURL != "" {
			baseURL += "/wiki"
		}
	}
	user := os.Getenv("DANDORI_JIRA_USER")
	token := os.Getenv("DANDORI_JIRA_TOKEN")

	if baseURL == "" || user == "" || token == "" {
		t.Skip("DANDORI_CONFLUENCE_URL/DANDORI_JIRA_URL, DANDORI_JIRA_USER, DANDORI_JIRA_TOKEN required")
	}

	return NewClient(ClientConfig{
		BaseURL: baseURL,
		User:    user,
		Token:   token,
		IsCloud: true,
	})
}

// === Search Tests ===

func TestIntegration_SearchPages(t *testing.T) {
	client := getIntegrationClient(t)
	ctx := context.Background()

	pages, err := client.SearchPages(ctx, "CLITEST", "")
	if err != nil {
		t.Fatalf("SearchPages: %v", err)
	}

	t.Logf("Found %d pages in CLITEST space", len(pages))
	for _, p := range pages {
		t.Logf("  %s - %s", p.ID, p.Title)
	}

	if len(pages) == 0 {
		t.Error("expected pages in CLITEST space")
	}
}

func TestIntegration_SearchPagesWithTitle(t *testing.T) {
	client := getIntegrationClient(t)
	ctx := context.Background()

	pages, err := client.SearchPages(ctx, "CLITEST", "Requirements")
	if err != nil {
		t.Fatalf("SearchPages: %v", err)
	}

	t.Logf("Found %d pages matching 'Requirements'", len(pages))
	for _, p := range pages {
		if !strings.Contains(strings.ToLower(p.Title), "requirement") {
			t.Logf("  Warning: %s doesn't contain 'requirement'", p.Title)
		}
	}
}

func TestIntegration_SearchPagesInvalidSpace(t *testing.T) {
	client := getIntegrationClient(t)
	ctx := context.Background()

	pages, err := client.SearchPages(ctx, "NONEXISTENT", "")
	if err != nil {
		t.Logf("Error (may be expected): %v", err)
		return
	}

	if len(pages) != 0 {
		t.Error("should return empty for non-existent space")
	}
}

// === Page CRUD Tests ===

func TestIntegration_CreateAndGetPage(t *testing.T) {
	client := getIntegrationClient(t)
	ctx := context.Background()

	testTitle := "Integration Test Page - " + time.Now().Format("20060102-150405")
	page, err := client.CreatePage(ctx, CreatePageRequest{
		SpaceKey: "CLITEST",
		Title:    testTitle,
		Body:     "<h1>Test Page</h1><p>Created by dandori-cli integration test.</p>",
	})
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	t.Logf("Created page: %s (ID: %s)", page.Title, page.ID)

	// Get the page back
	fetched, err := client.GetPage(ctx, page.ID)
	if err != nil {
		t.Fatalf("GetPage: %v", err)
	}

	if fetched.Title != testTitle {
		t.Errorf("title = %s, want %s", fetched.Title, testTitle)
	}
	if fetched.Body.Storage.Value == "" {
		t.Error("body should not be empty")
	}
	t.Logf("Fetched page body length: %d chars", len(fetched.Body.Storage.Value))
}

func TestIntegration_CreatePageWithParent(t *testing.T) {
	client := getIntegrationClient(t)
	ctx := context.Background()

	// First find a parent page
	pages, err := client.SearchPages(ctx, "CLITEST", "")
	if err != nil || len(pages) == 0 {
		t.Skip("No pages to use as parent")
	}

	parentID := pages[0].ID
	testTitle := "Child Page - " + time.Now().Format("20060102-150405")

	page, err := client.CreatePage(ctx, CreatePageRequest{
		SpaceKey: "CLITEST",
		Title:    testTitle,
		Body:     "<p>This is a child page.</p>",
		ParentID: parentID,
	})
	if err != nil {
		t.Fatalf("CreatePage with parent: %v", err)
	}

	t.Logf("Created child page: %s under parent %s", page.ID, parentID)
}

func TestIntegration_CreatePageWithRichContent(t *testing.T) {
	client := getIntegrationClient(t)
	ctx := context.Background()

	testTitle := "Rich Content Page - " + time.Now().Format("20060102-150405")
	body := `<h1>Rich Content Test</h1>
<p>This page tests various Confluence storage format elements.</p>

<h2>Lists</h2>
<ul>
<li>Item 1</li>
<li>Item 2</li>
<li>Item 3</li>
</ul>

<h2>Table</h2>
<table>
<tr><th>Header 1</th><th>Header 2</th></tr>
<tr><td>Cell 1</td><td>Cell 2</td></tr>
</table>

<h2>Code Block</h2>
<ac:structured-macro ac:name="code">
<ac:parameter ac:name="language">go</ac:parameter>
<ac:plain-text-body><![CDATA[func main() {
    fmt.Println("Hello")
}]]></ac:plain-text-body>
</ac:structured-macro>

<h2>Info Panel</h2>
<ac:structured-macro ac:name="info">
<ac:rich-text-body>
<p>This is an info panel.</p>
</ac:rich-text-body>
</ac:structured-macro>`

	page, err := client.CreatePage(ctx, CreatePageRequest{
		SpaceKey: "CLITEST",
		Title:    testTitle,
		Body:     body,
	})
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	t.Logf("Created rich content page: %s", page.ID)
}

func TestIntegration_GetPageNotFound(t *testing.T) {
	client := getIntegrationClient(t)
	ctx := context.Background()

	_, err := client.GetPage(ctx, "99999999")
	if err == nil {
		t.Error("should return error for non-existent page")
	}
	t.Logf("Error (expected): %v", err)
}

func TestIntegration_UpdatePage(t *testing.T) {
	client := getIntegrationClient(t)
	ctx := context.Background()

	// First create a page
	testTitle := "Update Test Page - " + time.Now().Format("20060102-150405")
	page, err := client.CreatePage(ctx, CreatePageRequest{
		SpaceKey: "CLITEST",
		Title:    testTitle,
		Body:     "<p>Original content</p>",
	})
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	// Update it
	updatedPage, err := client.UpdatePage(ctx, page.ID, UpdatePageRequest{
		Title:   testTitle + " (Updated)",
		Body:    "<p>Updated content</p>",
		Version: PageVersion{Number: page.Version.Number + 1},
	})
	if err != nil {
		t.Fatalf("UpdatePage: %v", err)
	}

	if !strings.Contains(updatedPage.Title, "Updated") {
		t.Error("title should contain 'Updated'")
	}
	t.Logf("Updated page: %s (version %d)", updatedPage.ID, updatedPage.Version.Number)
}

// === Report Writer Tests ===

func TestIntegration_CreateReport(t *testing.T) {
	client := getIntegrationClient(t)
	ctx := context.Background()

	writer := NewWriter(WriterConfig{
		Client:   client,
		SpaceKey: "CLITEST",
	})

	runID := fmt.Sprintf("r%d", time.Now().UnixNano())
	report := RunReport{
		RunID:        runID,
		IssueKey:     "CLITEST-1",
		AgentName:    "integration-test",
		Status:       "done",
		Duration:     90 * time.Second,
		CostUSD:      1.25,
		InputTokens:  5000,
		OutputTokens: 2000,
		Model:        "claude-sonnet-4-5-20250514",
		FilesChanged: []string{"main.go", "config.go"},
		GitDiff:      "+func hello() {}\n-func old() {}",
		StartedAt:    time.Now().Add(-90 * time.Second),
		EndedAt:      time.Now(),
	}

	page, err := writer.CreateReport(ctx, report)
	if err != nil {
		t.Fatalf("CreateReport: %v", err)
	}

	t.Logf("Created report page: %s (ID: %s)", page.Title, page.ID)

	// Verify content
	fetched, err := client.GetPage(ctx, page.ID)
	if err != nil {
		t.Fatalf("GetPage: %v", err)
	}

	body := fetched.Body.Storage.Value
	if !strings.Contains(body, "CLITEST-1") {
		t.Error("report should contain issue key")
	}
	if !strings.Contains(body, "integration-test") {
		t.Error("report should contain agent name")
	}
	if !strings.Contains(body, "1.25") {
		t.Error("report should contain cost")
	}
}

func TestIntegration_CreateReportWithDecisions(t *testing.T) {
	client := getIntegrationClient(t)
	ctx := context.Background()

	writer := NewWriter(WriterConfig{
		Client:   client,
		SpaceKey: "CLITEST",
	})

	runID := fmt.Sprintf("d%d", time.Now().UnixNano())
	report := RunReport{
		RunID:     runID,
		IssueKey:  "CLITEST-2",
		AgentName: "alpha",
		Status:    "done",
		Duration:  120 * time.Second,
		CostUSD:   2.50,
		Decisions: []string{
			"Used dependency injection for testability",
			"Chose PostgreSQL over MySQL for JSON support",
			"Implemented retry logic with exponential backoff",
		},
		FilesChanged: []string{"db.go", "repository.go", "retry.go"},
		Summary:      "Refactored database layer with improved error handling",
		StartedAt:    time.Now().Add(-120 * time.Second),
		EndedAt:      time.Now(),
	}

	page, err := writer.CreateReport(ctx, report)
	if err != nil {
		t.Fatalf("CreateReport: %v", err)
	}

	t.Logf("Created report with decisions: %s", page.ID)
}

func TestIntegration_CreateReportWithLargeDiff(t *testing.T) {
	client := getIntegrationClient(t)
	ctx := context.Background()

	writer := NewWriter(WriterConfig{
		Client:   client,
		SpaceKey: "CLITEST",
	})

	// Generate large diff
	var diffBuilder strings.Builder
	for i := 0; i < 100; i++ {
		diffBuilder.WriteString("+// Line added " + string(rune('0'+i%10)) + "\n")
	}

	runID := fmt.Sprintf("l%d", time.Now().UnixNano())
	report := RunReport{
		RunID:        runID,
		IssueKey:     "CLITEST-3",
		AgentName:    "beta",
		Status:       "done",
		Duration:     300 * time.Second,
		GitDiff:      diffBuilder.String(),
		FilesChanged: []string{"large_file.go"},
		StartedAt:    time.Now().Add(-300 * time.Second),
		EndedAt:      time.Now(),
	}

	page, err := writer.CreateReport(ctx, report)
	if err != nil {
		t.Fatalf("CreateReport: %v", err)
	}

	t.Logf("Created report with large diff: %s", page.ID)
}

// === Reader Cache Tests ===

func TestIntegration_ReaderCache(t *testing.T) {
	client := getIntegrationClient(t)
	ctx := context.Background()

	cacheDir := t.TempDir()
	reader := NewReader(ReaderConfig{
		Client:   client,
		CacheDir: cacheDir,
		TTL:      time.Hour,
	})

	pages, err := client.SearchPages(ctx, "CLITEST", "")
	if err != nil {
		t.Fatalf("SearchPages: %v", err)
	}
	if len(pages) == 0 {
		t.Skip("No pages in CLITEST space")
	}

	pageID := pages[0].ID
	t.Logf("Testing cache with page ID: %s", pageID)

	// Fetch and cache
	cachePath, err := reader.FetchAndCache(ctx, pageID)
	if err != nil {
		t.Fatalf("FetchAndCache: %v", err)
	}
	t.Logf("Cached to: %s", cachePath)

	// Check cache hit
	if !reader.IsCached(pageID) {
		t.Error("page should be cached")
	}

	// Read cached content
	content, err := reader.GetCachedMarkdown(pageID)
	if err != nil {
		t.Fatalf("GetCachedMarkdown: %v", err)
	}
	t.Logf("Cached content length: %d chars", len(content))
}

func TestIntegration_ReaderCacheMultiplePages(t *testing.T) {
	client := getIntegrationClient(t)
	ctx := context.Background()

	cacheDir := t.TempDir()
	reader := NewReader(ReaderConfig{
		Client:   client,
		CacheDir: cacheDir,
		TTL:      time.Hour,
	})

	pages, err := client.SearchPages(ctx, "CLITEST", "")
	if err != nil {
		t.Fatalf("SearchPages: %v", err)
	}

	// Cache multiple pages
	cached := 0
	for _, page := range pages[:min(5, len(pages))] {
		_, err := reader.FetchAndCache(ctx, page.ID)
		if err != nil {
			t.Logf("Failed to cache %s: %v", page.ID, err)
			continue
		}
		cached++
	}

	t.Logf("Cached %d pages", cached)
	if cached == 0 {
		t.Error("should cache at least one page")
	}
}

func TestIntegration_ReaderCacheInvalidation(t *testing.T) {
	client := getIntegrationClient(t)
	ctx := context.Background()

	cacheDir := t.TempDir()
	reader := NewReader(ReaderConfig{
		Client:   client,
		CacheDir: cacheDir,
		TTL:      time.Hour,
	})

	pages, err := client.SearchPages(ctx, "CLITEST", "")
	if err != nil || len(pages) == 0 {
		t.Skip("No pages")
	}

	pageID := pages[0].ID

	// Cache it
	_, err = reader.FetchAndCache(ctx, pageID)
	if err != nil {
		t.Fatalf("FetchAndCache: %v", err)
	}

	// Verify cached
	if !reader.IsCached(pageID) {
		t.Fatal("should be cached")
	}

	// Invalidate
	if err := reader.InvalidateCache(pageID); err != nil {
		t.Fatalf("InvalidateCache: %v", err)
	}

	// Should not be cached now
	if reader.IsCached(pageID) {
		t.Error("should not be cached after invalidation")
	}
}

// === Context Assembler Tests ===

func TestIntegration_ContextAssembler(t *testing.T) {
	client := getIntegrationClient(t)
	ctx := context.Background()

	cacheDir := t.TempDir()
	reader := NewReader(ReaderConfig{
		Client:   client,
		CacheDir: cacheDir,
		TTL:      time.Hour,
	})

	contextDir := t.TempDir()
	assembler := NewContextAssembler(ContextAssemblerConfig{
		Reader:     reader,
		ContextDir: contextDir,
	})

	pages, err := client.SearchPages(ctx, "CLITEST", "")
	if err != nil || len(pages) < 2 {
		t.Skip("Need at least 2 pages")
	}

	pageIDs := []string{pages[0].ID, pages[1].ID}
	contextPath, err := assembler.AssembleContext(ctx, "CLITEST-1", pageIDs, "Test task summary")
	if err != nil {
		t.Fatalf("AssembleContext: %v", err)
	}

	content, err := os.ReadFile(contextPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	t.Logf("Context file: %s (%d bytes)", contextPath, len(content))

	// Verify structure
	contentStr := string(content)
	if !strings.Contains(contentStr, "CLITEST-1") {
		t.Error("should contain issue key")
	}
	if !strings.Contains(contentStr, "Test task summary") {
		t.Error("should contain summary")
	}
}

func TestIntegration_ContextAssemblerWithErrors(t *testing.T) {
	client := getIntegrationClient(t)
	ctx := context.Background()

	cacheDir := t.TempDir()
	reader := NewReader(ReaderConfig{
		Client:   client,
		CacheDir: cacheDir,
		TTL:      time.Hour,
	})

	contextDir := t.TempDir()
	assembler := NewContextAssembler(ContextAssemblerConfig{
		Reader:     reader,
		ContextDir: contextDir,
	})

	// Mix valid and invalid page IDs
	pageIDs := []string{"99999999", "88888888"}
	contextPath, err := assembler.AssembleContext(ctx, "CLITEST-1", pageIDs, "Test with errors")
	if err != nil {
		t.Fatalf("AssembleContext: %v", err)
	}

	content, _ := os.ReadFile(contextPath)
	contentStr := string(content)

	// Should contain error messages
	if !strings.Contains(contentStr, "Error") {
		t.Log("Content:", contentStr)
	}
	t.Logf("Context with errors: %d bytes", len(content))
}

// === Converter Tests ===

func TestIntegration_StorageToMarkdownRealPage(t *testing.T) {
	client := getIntegrationClient(t)
	ctx := context.Background()

	pages, err := client.SearchPages(ctx, "CLITEST", "")
	if err != nil || len(pages) == 0 {
		t.Skip("No pages")
	}

	page, err := client.GetPage(ctx, pages[0].ID)
	if err != nil {
		t.Fatalf("GetPage: %v", err)
	}

	storage := page.Body.Storage.Value
	md := StorageToMarkdown(storage)

	t.Logf("Storage length: %d, Markdown length: %d", len(storage), len(md))
	t.Logf("Markdown preview:\n%s", truncate(md, 500))

	if len(md) == 0 && len(storage) > 0 {
		t.Error("markdown should not be empty for non-empty storage")
	}
}

func TestIntegration_RoundTripConversion(t *testing.T) {
	client := getIntegrationClient(t)
	ctx := context.Background()

	// Create page with known markdown
	originalMD := "# Test Heading\n\nThis is a paragraph.\n\n- Item 1\n- Item 2\n\n```go\nfunc main() {}\n```"
	storage := MarkdownToStorage(originalMD)

	testTitle := "RoundTrip Test - " + time.Now().Format("20060102-150405")
	page, err := client.CreatePage(ctx, CreatePageRequest{
		SpaceKey: "CLITEST",
		Title:    testTitle,
		Body:     storage,
	})
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	// Fetch and convert back
	fetched, err := client.GetPage(ctx, page.ID)
	if err != nil {
		t.Fatalf("GetPage: %v", err)
	}

	convertedMD := StorageToMarkdown(fetched.Body.Storage.Value)

	t.Logf("Original:\n%s", originalMD)
	t.Logf("Converted:\n%s", convertedMD)

	// Check key elements preserved
	if !strings.Contains(convertedMD, "Test Heading") {
		t.Error("heading should be preserved")
	}
	if !strings.Contains(convertedMD, "paragraph") {
		t.Error("paragraph should be preserved")
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
