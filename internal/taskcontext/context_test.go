package taskcontext

import (
	"strings"
	"testing"
)

func TestExtractConfluenceLinks(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		wantIDs []string
	}{
		{
			name:    "simple page URL",
			text:    "See doc: https://fooknt.atlassian.net/wiki/pages/360635",
			wantIDs: []string{"360635"},
		},
		{
			name:    "page with space and title",
			text:    "Check https://fooknt.atlassian.net/wiki/spaces/CLITEST/pages/123456/My+Page+Title for details",
			wantIDs: []string{"123456"},
		},
		{
			name:    "viewpage action URL",
			text:    "Link: https://fooknt.atlassian.net/wiki/pages/viewpage.action?pageId=789012",
			wantIDs: []string{"789012"},
		},
		{
			name:    "multiple links",
			text:    "See https://x.atlassian.net/wiki/pages/111 and https://x.atlassian.net/wiki/pages/222",
			wantIDs: []string{"111", "222"},
		},
		{
			name:    "no links",
			text:    "This is plain text without any Confluence links",
			wantIDs: nil,
		},
		{
			name:    "mixed content",
			text:    "Check Jira PROJ-123 and Confluence https://x.atlassian.net/wiki/pages/456 for more",
			wantIDs: []string{"456"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			refs := extractConfluenceLinks(tt.text)

			if len(refs) != len(tt.wantIDs) {
				t.Errorf("got %d links, want %d", len(refs), len(tt.wantIDs))
				return
			}

			for i, ref := range refs {
				if ref.PageID != tt.wantIDs[i] {
					t.Errorf("link %d: got pageID %q, want %q", i, ref.PageID, tt.wantIDs[i])
				}
			}
		})
	}
}

func TestExtractTextFromHTML(t *testing.T) {
	tests := []struct {
		name string
		html string
		want string
	}{
		{
			name: "simple paragraph",
			html: "<p>Hello world</p>",
			want: "Hello world",
		},
		{
			name: "nested tags",
			html: "<div><p>First</p><p>Second</p></div>",
			want: "First\n\nSecond",
		},
		{
			name: "list items",
			html: "<ul><li>Item 1</li><li>Item 2</li></ul>",
			want: "Item 1\n\nItem 2",
		},
		{
			name: "html entities",
			html: "<p>Tom &amp; Jerry &lt;3 &quot;cheese&quot;</p>",
			want: "Tom & Jerry <3 \"cheese\"",
		},
		{
			name: "script tags removed",
			html: "<p>Text</p><script>alert('xss')</script><p>More</p>",
			want: "Text\n\nMore",
		},
		{
			name: "headings",
			html: "<h2>Title</h2><p>Content</p>",
			want: "Title\n\nContent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractTextFromHTML(tt.html)
			// Normalize for comparison
			got = strings.TrimSpace(got)
			want := strings.TrimSpace(tt.want)

			if got != want {
				t.Errorf("got:\n%s\n\nwant:\n%s", got, want)
			}
		})
	}
}

func TestTaskContextToMarkdown(t *testing.T) {
	tc := &TaskContext{
		IssueKey:    "PROJ-123",
		Summary:     "Fix authentication bug",
		Description: "Users report 401 errors during login",
		IssueType:   "Bug",
		Priority:    "High",
		Status:      "To Do",
		Labels:      []string{"backend", "auth"},
		LinkedDocs: []LinkedDoc{
			{
				PageID:  "456",
				Title:   "Auth Architecture",
				URL:     "https://example.com/wiki/pages/456",
				Content: "Token flow documentation here",
			},
		},
	}

	md := tc.ToMarkdown()

	// Check key elements are present
	checks := []string{
		"# Task: PROJ-123",
		"**Summary:** Fix authentication bug",
		"**Type:** Bug",
		"**Priority:** High",
		"**Labels:** backend, auth",
		"## Description",
		"Users report 401 errors",
		"## Related Documentation",
		"### Auth Architecture",
		"Token flow documentation",
	}

	for _, check := range checks {
		if !strings.Contains(md, check) {
			t.Errorf("markdown missing: %q", check)
		}
	}
}

func TestIsConfluenceURL(t *testing.T) {
	tests := []struct {
		url  string
		want bool
	}{
		{"https://x.atlassian.net/wiki/pages/123", true},
		{"https://confluence.example.com/display/SPACE", true},
		{"https://x.atlassian.net/pages/viewpage.action?pageId=123", true},
		{"https://jira.example.com/browse/PROJ-123", false},
		{"https://github.com/org/repo", false},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got := isConfluenceURL(tt.url)
			if got != tt.want {
				t.Errorf("isConfluenceURL(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

func TestParseConfluenceURL(t *testing.T) {
	tests := []struct {
		url        string
		wantPageID string
		wantSpace  string
	}{
		{
			url:        "https://x.atlassian.net/wiki/pages/360635",
			wantPageID: "360635",
			wantSpace:  "",
		},
		{
			url:        "https://x.atlassian.net/wiki/spaces/CLITEST/pages/123456/Title",
			wantPageID: "123456",
			wantSpace:  "CLITEST",
		},
		{
			url:        "https://x.atlassian.net/wiki/pages/viewpage.action?pageId=789",
			wantPageID: "789",
			wantSpace:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			pageID, space := parseConfluenceURL(tt.url)
			if pageID != tt.wantPageID {
				t.Errorf("pageID: got %q, want %q", pageID, tt.wantPageID)
			}
			if space != tt.wantSpace {
				t.Errorf("space: got %q, want %q", space, tt.wantSpace)
			}
		})
	}
}
