package jira

import (
	"reflect"
	"sort"
	"testing"
)

func TestParseDescriptionTags_Found(t *testing.T) {
	got := ParseDescriptionTags("Steps to reproduce... caused_by: e1777abcdef9 happened during run.")
	want := []string{"e1777abcdef9"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseDescriptionTags_NoTag(t *testing.T) {
	got := ParseDescriptionTags("just a regular bug report, nothing to see")
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

func TestParseDescriptionTags_Multiple(t *testing.T) {
	got := ParseDescriptionTags(`First seen in caused_by: aaaaaaaaaaaa
Reproduced from caused_by: bbbbbbbbbbbb later.`)
	sort.Strings(got)
	want := []string{"aaaaaaaaaaaa", "bbbbbbbbbbbb"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseDescriptionTags_RejectsShortPrefix(t *testing.T) {
	// 8-char prefix is too risky for collisions; require ≥12 hex.
	got := ParseDescriptionTags("caused_by: abcd1234")
	if len(got) != 0 {
		t.Errorf("got %v, want empty (too short)", got)
	}
}

// Regression for Bug #5 — multi-paragraph ADF descriptions used to be joined
// without a separator, so a hex-letter word in the next paragraph (e.g. "Fix:")
// leaked into the runID match. parseDescription must keep paragraphs apart.
func TestParseDescription_MultiParagraphADF_PreservesRunIDBoundary(t *testing.T) {
	adf := map[string]any{
		"type":    "doc",
		"version": 1,
		"content": []any{
			map[string]any{
				"type":    "paragraph",
				"content": []any{map[string]any{"type": "text", "text": "caused_by: a2669999463cdf04"}},
			},
			map[string]any{
				"type":    "paragraph",
				"content": []any{map[string]any{"type": "text", "text": "Fix: do the thing"}},
			},
		},
	}
	flat := parseDescription(adf)
	tags := ParseDescriptionTags(flat)
	want := []string{"a2669999463cdf04"}
	if !reflect.DeepEqual(tags, want) {
		t.Errorf("tags = %v, want %v (flattened: %q)", tags, want, flat)
	}
}

func TestParseLinkCandidates_StructuredCausedBy(t *testing.T) {
	bug := &BugIssue{
		Key:     "BUG-1",
		Summary: "crash",
		Links: []IssueLink{
			{Type: "is caused by", InwardKey: "TASK-9"},
			{Type: "blocks", InwardKey: "TASK-10"},
		},
	}
	got := ParseLinkCandidates(bug)
	want := []string{"TASK-9"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseLinkCandidates_OutwardOnly(t *testing.T) {
	bug := &BugIssue{
		Key: "BUG-2",
		Links: []IssueLink{
			{Type: "caused by", OutwardKey: "TASK-7"},
		},
	}
	got := ParseLinkCandidates(bug)
	if !reflect.DeepEqual(got, []string{"TASK-7"}) {
		t.Errorf("got %v, want [TASK-7]", got)
	}
}

// TestParseLinkCandidates_RealJiraShape locks in the canonical link-type
// Name that real Jira sends ("Caused"), distinct from the inward/outward
// description forms. Regression: live verify on fooknt failed because the
// matcher only accepted the inward description.
func TestParseLinkCandidates_RealJiraShape(t *testing.T) {
	bug := &BugIssue{
		Key: "BUG-3",
		Links: []IssueLink{
			{Type: "Caused", OutwardKey: "TASK-1"},
		},
	}
	got := ParseLinkCandidates(bug)
	if !reflect.DeepEqual(got, []string{"TASK-1"}) {
		t.Errorf("got %v, want [TASK-1]", got)
	}
}

func TestParseLinkCandidates_OnlyUnrelated(t *testing.T) {
	bug := &BugIssue{
		Links: []IssueLink{
			{Type: "blocks", InwardKey: "TASK-1"},
			{Type: "relates to", InwardKey: "TASK-2"},
		},
	}
	got := ParseLinkCandidates(bug)
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

// fakeResolver is the test double for BugLinkResolver. Tracks calls so tests
// can assert that DetectBugLinks short-circuits on dedupe.
type fakeResolver struct {
	bugExists       map[string]bool
	runByIssue      map[string]string
	runByPrefix     map[string]string
	prefixErr       map[string]error
	bugExistsCalls  int
	latestRunCalls  int
	findPrefixCalls int
}

func (f *fakeResolver) LatestRunForIssue(k string) (string, error) {
	f.latestRunCalls++
	return f.runByIssue[k], nil
}
func (f *fakeResolver) FindRunByPrefix(p string) (string, error) {
	f.findPrefixCalls++
	if err, ok := f.prefixErr[p]; ok {
		return "", err
	}
	return f.runByPrefix[p], nil
}
func (f *fakeResolver) BugEventExists(k string) (bool, error) {
	f.bugExistsCalls++
	return f.bugExists[k], nil
}

func TestDetectBugLinks_StructuredLink(t *testing.T) {
	bug := &BugIssue{
		Key:     "BUG-1",
		Summary: "crash",
		Links:   []IssueLink{{Type: "is caused by", InwardKey: "TASK-9"}},
	}
	r := &fakeResolver{
		runByIssue: map[string]string{"TASK-9": "run-aaa"},
	}
	events, err := DetectBugLinks(bug, r)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].RunID != "run-aaa" {
		t.Errorf("run=%q, want run-aaa", events[0].RunID)
	}
	if events[0].Payload["link_type"] != "jira_link" {
		t.Errorf("link_type=%v", events[0].Payload["link_type"])
	}
	if events[0].Payload["bug_key"] != "BUG-1" {
		t.Errorf("bug_key=%v", events[0].Payload["bug_key"])
	}
}

func TestDetectBugLinks_DescriptionTag(t *testing.T) {
	bug := &BugIssue{
		Key:         "BUG-2",
		Description: "see caused_by: e1777abcdef9",
	}
	r := &fakeResolver{
		runByPrefix: map[string]string{"e1777abcdef9": "run-bbb"},
	}
	events, err := DetectBugLinks(bug, r)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(events) != 1 || events[0].RunID != "run-bbb" {
		t.Fatalf("got %+v", events)
	}
	if events[0].Payload["link_type"] != "description_tag" {
		t.Errorf("link_type=%v", events[0].Payload["link_type"])
	}
}

func TestDetectBugLinks_AlreadyRecorded_Dedupe(t *testing.T) {
	bug := &BugIssue{
		Key:   "BUG-3",
		Links: []IssueLink{{Type: "is caused by", InwardKey: "TASK-1"}},
	}
	r := &fakeResolver{
		bugExists:  map[string]bool{"BUG-3": true},
		runByIssue: map[string]string{"TASK-1": "run-ccc"},
	}
	events, err := DetectBugLinks(bug, r)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("got %d, want 0 (dedupe)", len(events))
	}
	if r.latestRunCalls != 0 {
		t.Errorf("expected no resolve calls when bug already exists, got %d", r.latestRunCalls)
	}
}

func TestDetectBugLinks_BothMethodsSameRun_NoDuplicate(t *testing.T) {
	bug := &BugIssue{
		Key:         "BUG-4",
		Description: "caused_by: aaaaaaaaaaaa",
		Links:       []IssueLink{{Type: "is caused by", InwardKey: "TASK-9"}},
	}
	r := &fakeResolver{
		runByIssue:  map[string]string{"TASK-9": "run-shared"},
		runByPrefix: map[string]string{"aaaaaaaaaaaa": "run-shared"},
	}
	events, err := DetectBugLinks(bug, r)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("got %d events, want 1 (same run not double-emitted)", len(events))
	}
}

func TestDetectBugLinks_NoMatches(t *testing.T) {
	bug := &BugIssue{Key: "BUG-5", Description: "nothing here"}
	r := &fakeResolver{}
	events, err := DetectBugLinks(bug, r)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("got %d, want 0", len(events))
	}
}

func TestParseLinkCandidates_CaseInsensitive(t *testing.T) {
	bug := &BugIssue{
		Links: []IssueLink{
			{Type: "Is Caused By", InwardKey: "TASK-1"},
		},
	}
	got := ParseLinkCandidates(bug)
	if !reflect.DeepEqual(got, []string{"TASK-1"}) {
		t.Errorf("got %v, want [TASK-1]", got)
	}
}
