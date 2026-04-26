package jira

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestSearchBugs_ReturnsLinks asserts SearchBugs requests issuelinks +
// description fields from Jira and parses them into Issue.Links.
//
// We diverge from SearchIssues because SearchIssues defaults to a
// minimal field set without issuelinks — bug-link detection needs them.
func TestSearchBugs_ReturnsLinks(t *testing.T) {
	var capturedBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/search/jql") {
			http.NotFound(w, r)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &capturedBody)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"issues": [{
				"key": "BUG-1",
				"fields": {
					"summary": "crash on save",
					"description": "see caused_by: e1777abcdef9",
					"issuetype": {"name": "Bug"},
					"status": {"name": "Open", "statusCategory": {"key": "new"}},
					"issuelinks": [
						{"type": {"name": "is caused by"}, "inwardIssue": {"key": "TASK-9"}},
						{"type": {"name": "blocks"}, "outwardIssue": {"key": "TASK-10"}}
					]
				}
			}]
		}`))
	}))
	defer srv.Close()

	c := NewClient(ClientConfig{BaseURL: srv.URL, User: "u", Token: "t", IsCloud: true})
	bugs, err := c.SearchBugs("issuetype=Bug", 50)
	if err != nil {
		t.Fatalf("SearchBugs: %v", err)
	}
	if len(bugs) != 1 {
		t.Fatalf("got %d bugs, want 1", len(bugs))
	}

	got := bugs[0]
	if got.Key != "BUG-1" || got.Summary != "crash on save" {
		t.Errorf("issue parse mismatch: %+v", got)
	}
	if !strings.Contains(got.Description, "caused_by: e1777abcdef9") {
		t.Errorf("description missing caused_by tag: %q", got.Description)
	}
	if len(got.Links) != 2 {
		t.Fatalf("got %d links, want 2", len(got.Links))
	}
	if got.Links[0].Type != "is caused by" || got.Links[0].InwardKey != "TASK-9" {
		t.Errorf("link[0]=%+v", got.Links[0])
	}
	if got.Links[1].Type != "blocks" || got.Links[1].OutwardKey != "TASK-10" {
		t.Errorf("link[1]=%+v", got.Links[1])
	}

	fields, _ := capturedBody["fields"].([]any)
	wantFields := map[string]bool{"summary": true, "description": true, "issuetype": true, "status": true, "issuelinks": true}
	for k := range wantFields {
		found := false
		for _, f := range fields {
			if s, _ := f.(string); s == k {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("fields request missing %q (got %v)", k, fields)
		}
	}
}
