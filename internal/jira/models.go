package jira

import (
	"strings"
	"time"
)

// JiraTime handles Jira's time format which may include timezone offset without colon
type JiraTime struct {
	time.Time
}

func (jt *JiraTime) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		return nil
	}

	// Try multiple formats Jira might use
	formats := []string{
		"2006-01-02T15:04:05.000-0700",
		"2006-01-02T15:04:05.000Z0700",
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05-0700",
		"2006-01-02T15:04:05Z",
		time.RFC3339,
		time.RFC3339Nano,
	}

	var err error
	for _, fmt := range formats {
		jt.Time, err = time.Parse(fmt, s)
		if err == nil {
			return nil
		}
	}
	return err
}

type Board struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Location struct {
		ProjectKey string `json:"projectKey"`
	} `json:"location"`
}

type Sprint struct {
	ID        int      `json:"id"`
	Name      string   `json:"name"`
	State     string   `json:"state"`
	StartDate JiraTime `json:"startDate"`
	EndDate   JiraTime `json:"endDate"`
	Goal      string   `json:"goal"`
}

type Issue struct {
	Key               string
	Summary           string
	Description       string
	IssueType         string
	Priority          string
	Status            string
	StatusCategoryKey string // Jira statusCategory.key: done|indeterminate|new
	SprintID          int
	SprintName        string
	Assignee          string
	Labels            []string
	StoryPoints       float64
	AgentName         string
	EpicKey           string
	CreatedAt         time.Time
	UpdatedAt         time.Time

	ConfluenceLinks []ConfluenceLink

	// Links is populated only by SearchBugs (it requests the issuelinks
	// field). GetSprintIssues / GetIssue leave it empty to avoid extra
	// payload weight on the hot path.
	Links []IssueLink
}

type ConfluenceLink struct {
	PageID   string
	Title    string
	URL      string
	SpaceKey string
}

type RemoteLink struct {
	ID     int `json:"id"`
	Object struct {
		URL   string `json:"url"`
		Title string `json:"title"`
		Icon  struct {
			URL16x16 string `json:"url16x16"`
		} `json:"icon"`
	} `json:"object"`
}

type Transition struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	To   struct {
		Name string `json:"name"`
	} `json:"to"`
}

type boardsResponse struct {
	Values []Board `json:"values"`
}

type sprintsResponse struct {
	Values []Sprint `json:"values"`
}

type issuesResponse struct {
	Issues []issueResponse `json:"issues"`
}

type issueResponse struct {
	Key    string `json:"key"`
	Fields struct {
		Summary     string `json:"summary"`
		Description any    `json:"description"` // Can be string or object
		IssueType   struct {
			Name string `json:"name"`
		} `json:"issuetype"`
		Priority struct {
			Name string `json:"name"`
		} `json:"priority"`
		Status struct {
			Name           string `json:"name"`
			StatusCategory struct {
				Key string `json:"key"`
			} `json:"statusCategory"`
		} `json:"status"`
		Labels   []string `json:"labels"`
		Assignee struct {
			DisplayName string `json:"displayName"`
		} `json:"assignee"`
		Sprint struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"sprint"`
		Epic struct {
			Key string `json:"key"`
		} `json:"epic"`
		StoryPoints any                 `json:"customfield_10020"` // Can be float64, array, or null
		AgentName   string              `json:"customfield_10100"`
		Created     JiraTime            `json:"created"`
		Updated     JiraTime            `json:"updated"`
		IssueLinks  []issueLinkResponse `json:"issuelinks"`
	} `json:"fields"`
}

// issueLinkResponse mirrors one entry of Jira's "issuelinks" array. Each
// row carries a type plus exactly one of inwardIssue/outwardIssue
// depending on which side of the link the parent issue sits.
type issueLinkResponse struct {
	Type struct {
		Name string `json:"name"`
	} `json:"type"`
	InwardIssue *struct {
		Key string `json:"key"`
	} `json:"inwardIssue,omitempty"`
	OutwardIssue *struct {
		Key string `json:"key"`
	} `json:"outwardIssue,omitempty"`
}

func parseIssue(resp *issueResponse) *Issue {
	return &Issue{
		Key:               resp.Key,
		Summary:           resp.Fields.Summary,
		Description:       parseDescription(resp.Fields.Description),
		IssueType:         resp.Fields.IssueType.Name,
		Priority:          resp.Fields.Priority.Name,
		Status:            resp.Fields.Status.Name,
		StatusCategoryKey: resp.Fields.Status.StatusCategory.Key,
		SprintID:          resp.Fields.Sprint.ID,
		SprintName:        resp.Fields.Sprint.Name,
		Assignee:          resp.Fields.Assignee.DisplayName,
		Labels:            resp.Fields.Labels,
		StoryPoints:       parseStoryPoints(resp.Fields.StoryPoints),
		AgentName:         resp.Fields.AgentName,
		EpicKey:           resp.Fields.Epic.Key,
		CreatedAt:         resp.Fields.Created.Time,
		UpdatedAt:         resp.Fields.Updated.Time,
		Links:             parseIssueLinks(resp.Fields.IssueLinks),
	}
}

func parseIssueLinks(rows []issueLinkResponse) []IssueLink {
	if len(rows) == 0 {
		return nil
	}
	out := make([]IssueLink, 0, len(rows))
	for _, r := range rows {
		link := IssueLink{Type: r.Type.Name}
		if r.InwardIssue != nil {
			link.InwardKey = r.InwardIssue.Key
		}
		if r.OutwardIssue != nil {
			link.OutwardKey = r.OutwardIssue.Key
		}
		out = append(out, link)
	}
	return out
}

func parseDescription(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	// Jira v3 uses ADF (Atlassian Document Format) - extract text content
	if m, ok := v.(map[string]any); ok {
		return extractTextFromADF(m)
	}
	return ""
}

// extractTextFromADF flattens an Atlassian Document Format doc into plain text.
// Top-level nodes (paragraphs, headings) are separated by "\n" so adjacent
// blocks don't bleed into each other — this matters for downstream regex
// matchers like the bug-link `caused_by:<hex>` parser, which would otherwise
// greedily consume a hex char from the next paragraph (Bug #5).
func extractTextFromADF(doc map[string]any) string {
	content, ok := doc["content"].([]any)
	if !ok {
		return ""
	}
	var result strings.Builder
	for i, node := range content {
		if i > 0 {
			result.WriteByte('\n')
		}
		if m, ok := node.(map[string]any); ok {
			if text, ok := m["text"].(string); ok {
				result.WriteString(text)
			}
			if nested, ok := m["content"].([]any); ok {
				for _, n := range nested {
					if nm, ok := n.(map[string]any); ok {
						if text, ok := nm["text"].(string); ok {
							result.WriteString(text)
						}
					}
				}
			}
		}
	}
	return result.String()
}

func parseStoryPoints(v any) float64 {
	if v == nil {
		return 0
	}
	if f, ok := v.(float64); ok {
		return f
	}
	// Sometimes returned as array with objects containing value
	if arr, ok := v.([]any); ok {
		for _, item := range arr {
			if m, ok := item.(map[string]any); ok {
				if val, ok := m["value"].(float64); ok {
					return val
				}
			}
		}
	}
	return 0
}

func (i *Issue) HasLabel(label string) bool {
	for _, l := range i.Labels {
		if strings.EqualFold(l, label) {
			return true
		}
	}
	return false
}

func (i *Issue) IsTracked() bool {
	return i.HasLabel("dandori-tracked")
}

func (i *Issue) IsAssigned() bool {
	return i.AgentName != ""
}

func ExtractConfluenceLinks(links []RemoteLink) []ConfluenceLink {
	var result []ConfluenceLink
	for _, link := range links {
		if isConfluenceURL(link.Object.URL) {
			cl := ConfluenceLink{
				URL:   link.Object.URL,
				Title: link.Object.Title,
			}
			cl.PageID, cl.SpaceKey = parseConfluenceURL(link.Object.URL)
			result = append(result, cl)
		}
	}
	return result
}

func isConfluenceURL(url string) bool {
	return strings.Contains(url, "/wiki/") ||
		strings.Contains(url, "confluence") ||
		strings.Contains(url, "/pages/")
}

func parseConfluenceURL(url string) (pageID, spaceKey string) {
	if idx := strings.Index(url, "/pages/viewpage.action?pageId="); idx != -1 {
		pageID = url[idx+len("/pages/viewpage.action?pageId="):]
		if ampIdx := strings.Index(pageID, "&"); ampIdx != -1 {
			pageID = pageID[:ampIdx]
		}
	}

	if idx := strings.Index(url, "/spaces/"); idx != -1 {
		rest := url[idx+len("/spaces/"):]
		if slashIdx := strings.Index(rest, "/"); slashIdx != -1 {
			spaceKey = rest[:slashIdx]
		} else {
			spaceKey = rest
		}
	}

	return
}
