package jira

import (
	"testing"
	"time"
)

// IterationInput bundles the inputs DetectIteration needs without forcing a DB
// dependency on the pure function. The poller assembles these from db queries.
type iterationFixture struct {
	issue          *Issue
	lastRun        *PriorRun
	existingEvents []IterationEvent
}

func mkIssue(key, status, statusCategory string) *Issue {
	return &Issue{
		Key:               key,
		Status:            status,
		StatusCategoryKey: statusCategory,
		UpdatedAt:         time.Date(2026, 4, 25, 10, 0, 0, 0, time.UTC),
	}
}

func mkPriorRun(runID, prevStatus, prevCategory string, endedAt time.Time) *PriorRun {
	return &PriorRun{
		RunID:                    runID,
		Status:                   "done",
		JiraStatusAtCompletion:   prevStatus,
		JiraCategoryAtCompletion: prevCategory,
		EndedAt:                  endedAt,
	}
}

func TestDetectIteration_FirstRunSkipped(t *testing.T) {
	// No prior run — task is brand new, not an iteration.
	issue := mkIssue("KEY-1", "In Progress", "indeterminate")
	evt, err := DetectIteration(issue, nil, nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if evt != nil {
		t.Errorf("got %+v, want nil", evt)
	}
}

func TestDetectIteration_StillDone(t *testing.T) {
	// Last run finished done, status today still done — no transition.
	issue := mkIssue("KEY-1", "Done", "done")
	prior := mkPriorRun("run-A", "Done", "done", time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC))
	evt, err := DetectIteration(issue, prior, nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if evt != nil {
		t.Errorf("got %+v, want nil", evt)
	}
}

func TestDetectIteration_DoneToActive_EmitsEvent(t *testing.T) {
	// Last run finished done, status now back to In Progress → iteration round 2.
	issue := mkIssue("KEY-1", "In Progress", "indeterminate")
	prior := mkPriorRun("run-A", "Done", "done", time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC))
	evt, err := DetectIteration(issue, prior, nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if evt == nil {
		t.Fatal("expected event, got nil")
	}
	if evt.Round != 2 {
		t.Errorf("round=%d, want 2", evt.Round)
	}
	if evt.PrevRunID != "run-A" {
		t.Errorf("prev_run_id=%q, want run-A", evt.PrevRunID)
	}
	if evt.PrevStatus != "Done" {
		t.Errorf("prev_status=%q, want Done", evt.PrevStatus)
	}
	if evt.NewStatus != "In Progress" {
		t.Errorf("new_status=%q, want In Progress", evt.NewStatus)
	}
	if evt.IssueKey != "KEY-1" {
		t.Errorf("issue_key=%q, want KEY-1", evt.IssueKey)
	}
}

func TestDetectIteration_ThirdRound(t *testing.T) {
	// One prior iteration event exists — next transition is round 3.
	issue := mkIssue("KEY-1", "In Progress", "indeterminate")
	prior := mkPriorRun("run-B", "Done", "done", time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC))
	existing := []IterationEvent{
		{Round: 2, IssueKey: "KEY-1", TransitionedAt: time.Date(2026, 4, 23, 9, 0, 0, 0, time.UTC)},
	}
	evt, err := DetectIteration(issue, prior, existing)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if evt == nil {
		t.Fatal("expected event, got nil")
	}
	if evt.Round != 3 {
		t.Errorf("round=%d, want 3", evt.Round)
	}
}

func TestDetectIteration_AlreadyEmitted_Dedupe(t *testing.T) {
	// Same transition timestamp already in events — skip to avoid double emit.
	transitionTime := time.Date(2026, 4, 25, 10, 0, 0, 0, time.UTC)
	issue := mkIssue("KEY-1", "In Progress", "indeterminate")
	issue.UpdatedAt = transitionTime
	prior := mkPriorRun("run-A", "Done", "done", time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC))
	existing := []IterationEvent{
		{Round: 2, IssueKey: "KEY-1", TransitionedAt: transitionTime},
	}
	evt, err := DetectIteration(issue, prior, existing)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if evt != nil {
		t.Errorf("got %+v, want nil (dedupe)", evt)
	}
}

func TestDetectIteration_LastRunNotDone(t *testing.T) {
	// Edge case: last run failed/cancelled (category != done) — not an iteration.
	issue := mkIssue("KEY-1", "In Progress", "indeterminate")
	prior := mkPriorRun("run-A", "In Progress", "indeterminate", time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC))
	evt, err := DetectIteration(issue, prior, nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if evt != nil {
		t.Errorf("got %+v, want nil", evt)
	}
}
