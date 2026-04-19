package taskcontext

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/phuc-nt/dandori-cli/internal/confluence"
	"github.com/phuc-nt/dandori-cli/internal/jira"
)

// TaskContext holds all context fetched from Jira and Confluence for a task
type TaskContext struct {
	IssueKey    string
	Summary     string
	Description string
	IssueType   string
	Priority    string
	Status      string
	Labels      []string

	// Confluence docs linked in the issue
	LinkedDocs []LinkedDoc
}

// LinkedDoc represents a Confluence page linked from the Jira issue
type LinkedDoc struct {
	PageID  string
	Title   string
	URL     string
	Content string // Plain text content extracted from HTML
}

// Fetcher fetches task context from Jira and Confluence
type Fetcher struct {
	jiraClient *jira.Client
	confClient *confluence.Client
}

// NewFetcher creates a new context fetcher
func NewFetcher(jiraClient *jira.Client, confClient *confluence.Client) *Fetcher {
	return &Fetcher{
		jiraClient: jiraClient,
		confClient: confClient,
	}
}

// Fetch retrieves the full context for a Jira issue
func (f *Fetcher) Fetch(ctx context.Context, issueKey string) (*TaskContext, error) {
	// 1. Get Jira issue
	issue, err := f.jiraClient.GetIssue(issueKey)
	if err != nil {
		return nil, fmt.Errorf("get issue %s: %w", issueKey, err)
	}

	tc := &TaskContext{
		IssueKey:    issue.Key,
		Summary:     issue.Summary,
		Description: issue.Description,
		IssueType:   issue.IssueType,
		Priority:    issue.Priority,
		Status:      issue.Status,
		Labels:      issue.Labels,
	}

	// 2. Extract Confluence links from description
	confLinks := extractConfluenceLinks(issue.Description)

	// 3. Also check remote links
	remoteLinks, err := f.jiraClient.GetRemoteLinks(issueKey)
	if err == nil {
		for _, rl := range remoteLinks {
			if isConfluenceURL(rl.Object.URL) {
				pageID, _ := parseConfluenceURL(rl.Object.URL)
				if pageID != "" {
					confLinks = append(confLinks, ConfluenceRef{
						PageID: pageID,
						URL:    rl.Object.URL,
						Title:  rl.Object.Title,
					})
				}
			}
		}
	}

	// 4. Deduplicate by PageID
	seen := make(map[string]bool)
	var uniqueLinks []ConfluenceRef
	for _, cl := range confLinks {
		if cl.PageID != "" && !seen[cl.PageID] {
			seen[cl.PageID] = true
			uniqueLinks = append(uniqueLinks, cl)
		}
	}

	// 5. Fetch each Confluence page
	if f.confClient != nil {
		for _, cl := range uniqueLinks {
			page, err := f.confClient.GetPage(ctx, cl.PageID)
			if err != nil {
				continue // Skip pages we can't access
			}

			content := extractTextFromHTML(page.Body.Storage.Value)
			tc.LinkedDocs = append(tc.LinkedDocs, LinkedDoc{
				PageID:  cl.PageID,
				Title:   page.Title,
				URL:     cl.URL,
				Content: content,
			})
		}
	}

	return tc, nil
}

// ConfluenceRef is a reference to a Confluence page found in text
type ConfluenceRef struct {
	PageID string
	URL    string
	Title  string
}

// extractConfluenceLinks finds Confluence URLs in text
func extractConfluenceLinks(text string) []ConfluenceRef {
	var refs []ConfluenceRef

	// Pattern for Confluence URLs
	// Examples:
	// - https://domain.atlassian.net/wiki/pages/360635
	// - https://domain.atlassian.net/wiki/spaces/SPACE/pages/123456/Title
	// - https://domain.atlassian.net/wiki/pages/viewpage.action?pageId=123456
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`https?://[^/]+/wiki/pages/(\d+)`),
		regexp.MustCompile(`https?://[^/]+/wiki/spaces/[^/]+/pages/(\d+)(?:/[^\s]*)?`),
		regexp.MustCompile(`https?://[^/]+/wiki/pages/viewpage\.action\?pageId=(\d+)`),
	}

	for _, pattern := range patterns {
		matches := pattern.FindAllStringSubmatch(text, -1)
		for _, m := range matches {
			if len(m) >= 2 {
				refs = append(refs, ConfluenceRef{
					PageID: m[1],
					URL:    m[0],
				})
			}
		}
	}

	return refs
}

// isConfluenceURL checks if a URL is a Confluence URL
func isConfluenceURL(url string) bool {
	return strings.Contains(url, "/wiki/") ||
		strings.Contains(url, "confluence") ||
		strings.Contains(url, "/pages/")
}

// parseConfluenceURL extracts page ID from Confluence URL
func parseConfluenceURL(url string) (pageID, spaceKey string) {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`/pages/(\d+)`),
		regexp.MustCompile(`pageId=(\d+)`),
	}

	for _, pattern := range patterns {
		if m := pattern.FindStringSubmatch(url); len(m) >= 2 {
			pageID = m[1]
			break
		}
	}

	if idx := strings.Index(url, "/spaces/"); idx != -1 {
		rest := url[idx+len("/spaces/"):]
		if slashIdx := strings.Index(rest, "/"); slashIdx != -1 {
			spaceKey = rest[:slashIdx]
		}
	}

	return
}

// extractTextFromHTML converts HTML to plain text
func extractTextFromHTML(html string) string {
	// Remove script and style tags
	scriptRe := regexp.MustCompile(`(?i)<script[^>]*>[\s\S]*?</script>`)
	html = scriptRe.ReplaceAllString(html, "")

	styleRe := regexp.MustCompile(`(?i)<style[^>]*>[\s\S]*?</style>`)
	html = styleRe.ReplaceAllString(html, "")

	// Convert block elements to newlines
	blockRe := regexp.MustCompile(`(?i)</?(p|div|br|h[1-6]|li|tr)[^>]*>`)
	html = blockRe.ReplaceAllString(html, "\n")

	// Remove all remaining tags
	tagRe := regexp.MustCompile(`<[^>]+>`)
	text := tagRe.ReplaceAllString(html, "")

	// Decode common HTML entities
	text = strings.ReplaceAll(text, "&nbsp;", " ")
	text = strings.ReplaceAll(text, "&amp;", "&")
	text = strings.ReplaceAll(text, "&lt;", "<")
	text = strings.ReplaceAll(text, "&gt;", ">")
	text = strings.ReplaceAll(text, "&quot;", "\"")
	text = strings.ReplaceAll(text, "&#39;", "'")

	// Normalize whitespace
	spaceRe := regexp.MustCompile(`[ \t]+`)
	text = spaceRe.ReplaceAllString(text, " ")

	// Normalize newlines
	nlRe := regexp.MustCompile(`\n\s*\n+`)
	text = nlRe.ReplaceAllString(text, "\n\n")

	return strings.TrimSpace(text)
}

// ToMarkdown converts the task context to a markdown string for agent consumption
func (tc *TaskContext) ToMarkdown() string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# Task: %s\n\n", tc.IssueKey))
	sb.WriteString(fmt.Sprintf("**Summary:** %s\n\n", tc.Summary))
	sb.WriteString(fmt.Sprintf("**Type:** %s | **Priority:** %s | **Status:** %s\n\n", tc.IssueType, tc.Priority, tc.Status))

	if len(tc.Labels) > 0 {
		sb.WriteString(fmt.Sprintf("**Labels:** %s\n\n", strings.Join(tc.Labels, ", ")))
	}

	sb.WriteString("## Description\n\n")
	sb.WriteString(tc.Description)
	sb.WriteString("\n\n")

	if len(tc.LinkedDocs) > 0 {
		sb.WriteString("---\n\n")
		sb.WriteString("## Related Documentation\n\n")

		for _, doc := range tc.LinkedDocs {
			sb.WriteString(fmt.Sprintf("### %s\n\n", doc.Title))
			sb.WriteString(doc.Content)
			sb.WriteString("\n\n")
		}
	}

	return sb.String()
}
