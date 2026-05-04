package jira

import (
	"errors"
	"testing"
)

// fakeFetcher returns a canned issue keyed by issueKey. Used to exercise
// the bug/non-bug branches of RecordOnTaskDone.
type fakeFetcher struct {
	byKey map[string]*Issue
	err   error
}

func (f *fakeFetcher) GetIssue(k string) (*Issue, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.byKey[k], nil
}

// fakeStore captures every InsertBuglink call so tests can assert the
// (bug, run) pairs and the linked_by tag.
type fakeStore struct {
	calls []struct{ bug, run, reason, by string }
	err   error
}

func (f *fakeStore) InsertBuglink(bug, run, reason, by string) error {
	if f.err != nil {
		return f.err
	}
	f.calls = append(f.calls, struct{ bug, run, reason, by string }{bug, run, reason, by})
	return nil
}

// fakeHookResolver: minimal BugLinkResolver — only the methods the hook
// actually calls are exercised here.
type fakeHookResolver struct {
	latest map[string]string // taskKey → runID
	prefix map[string]string // hex prefix → runID
}

func (r *fakeHookResolver) LatestRunForIssue(k string) (string, error) {
	return r.latest[k], nil
}
func (r *fakeHookResolver) FindRunByPrefix(p string) (string, error) {
	return r.prefix[p], nil
}
func (r *fakeHookResolver) BugEventExists(string) (bool, error) { return false, nil }

func TestRecordOnTaskDone_NonBugIsNoOp(t *testing.T) {
	fetcher := &fakeFetcher{byKey: map[string]*Issue{
		"TASK-1": {Key: "TASK-1", IssueType: "Story", Links: []IssueLink{{Type: "is caused by", InwardKey: "TASK-99"}}},
	}}
	store := &fakeStore{}
	resolver := &fakeHookResolver{latest: map[string]string{"TASK-99": "run-99"}}

	n, err := RecordOnTaskDone(fetcher, store, resolver, "TASK-1")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if n != 0 {
		t.Errorf("inserted=%d, want 0 for non-bug", n)
	}
	if len(store.calls) != 0 {
		t.Errorf("store should not be touched for non-bug, got %d calls", len(store.calls))
	}
}

func TestRecordOnTaskDone_BugWithLinkInsertsRow(t *testing.T) {
	fetcher := &fakeFetcher{byKey: map[string]*Issue{
		"BUG-7": {
			Key: "BUG-7", IssueType: "Bug",
			Links: []IssueLink{{Type: "is caused by", InwardKey: "TASK-2"}},
		},
	}}
	store := &fakeStore{}
	resolver := &fakeHookResolver{latest: map[string]string{"TASK-2": "run-22"}}

	n, err := RecordOnTaskDone(fetcher, store, resolver, "BUG-7")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if n != 1 || len(store.calls) != 1 {
		t.Fatalf("want 1 insert, got n=%d calls=%d", n, len(store.calls))
	}
	c := store.calls[0]
	if c.bug != "BUG-7" || c.run != "run-22" || c.by != "task-done-hook" {
		t.Errorf("call wrong: %+v", c)
	}
}

func TestRecordOnTaskDone_BugWithDescriptionTagInsertsRow(t *testing.T) {
	fetcher := &fakeFetcher{byKey: map[string]*Issue{
		"BUG-8": {
			Key:         "BUG-8",
			IssueType:   "Bug",
			Description: "Stack trace says caused_by: abc123def456 — please investigate",
		},
	}}
	store := &fakeStore{}
	resolver := &fakeHookResolver{prefix: map[string]string{"abc123def456": "run-abc"}}

	n, err := RecordOnTaskDone(fetcher, store, resolver, "BUG-8")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if n != 1 {
		t.Fatalf("want 1 insert, got %d", n)
	}
	if store.calls[0].run != "run-abc" || store.calls[0].bug != "BUG-8" {
		t.Errorf("call wrong: %+v", store.calls[0])
	}
}

func TestRecordOnTaskDone_BugWithUnresolvableLinkIsNoOp(t *testing.T) {
	fetcher := &fakeFetcher{byKey: map[string]*Issue{
		"BUG-9": {Key: "BUG-9", IssueType: "Bug", Links: []IssueLink{{Type: "is caused by", InwardKey: "TASK-X"}}},
	}}
	store := &fakeStore{}
	resolver := &fakeHookResolver{latest: map[string]string{}} // TASK-X has no run

	n, err := RecordOnTaskDone(fetcher, store, resolver, "BUG-9")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if n != 0 || len(store.calls) != 0 {
		t.Errorf("unresolvable link should produce 0 inserts, got n=%d", n)
	}
}

func TestRecordOnTaskDone_FetcherErrorBubbles(t *testing.T) {
	fetcher := &fakeFetcher{err: errors.New("boom")}
	n, err := RecordOnTaskDone(fetcher, &fakeStore{}, &fakeHookResolver{}, "BUG-X")
	if err == nil {
		t.Fatalf("expected fetcher error to bubble up")
	}
	if n != 0 {
		t.Errorf("n=%d, want 0", n)
	}
}

func TestRecordOnTaskDone_DefectAndCustomTypeStillCount(t *testing.T) {
	// Bug-type matching is case-insensitive substring on "bug" so custom
	// issue types like "Production Bug" still trigger the hook.
	cases := []string{"Bug", "bug", "Production Bug", "Customer Bug Report"}
	for _, ty := range cases {
		fetcher := &fakeFetcher{byKey: map[string]*Issue{
			"X-1": {Key: "X-1", IssueType: ty, Links: []IssueLink{{Type: "is caused by", InwardKey: "T-1"}}},
		}}
		store := &fakeStore{}
		resolver := &fakeHookResolver{latest: map[string]string{"T-1": "run-1"}}
		n, err := RecordOnTaskDone(fetcher, store, resolver, "X-1")
		if err != nil {
			t.Fatalf("type=%q err=%v", ty, err)
		}
		if n != 1 {
			t.Errorf("type=%q n=%d want 1", ty, n)
		}
	}
}

func TestRecordOnTaskDone_NonBugCustomTypeIsNoOp(t *testing.T) {
	cases := []string{"Story", "Task", "Epic", "Improvement", "Sub-task"}
	for _, ty := range cases {
		fetcher := &fakeFetcher{byKey: map[string]*Issue{
			"X-1": {Key: "X-1", IssueType: ty, Links: []IssueLink{{Type: "is caused by", InwardKey: "T-1"}}},
		}}
		store := &fakeStore{}
		resolver := &fakeHookResolver{latest: map[string]string{"T-1": "run-1"}}
		n, _ := RecordOnTaskDone(fetcher, store, resolver, "X-1")
		if n != 0 {
			t.Errorf("type=%q triggered hook (n=%d)", ty, n)
		}
	}
}
