package db

import (
	"testing"
	"time"
)

func TestLegacyBucketReworkReason_TableDriven(t *testing.T) {
	cases := []struct {
		in   string
		want RunOutcomeReason
	}{
		{"test failure", ReasonTestFail},
		{"TEST FAILURE", ReasonTestFail},
		{"lint violation", ReasonLintFail},
		{"human reject", ReasonHumanReject},
		{"rejected by reviewer", ReasonHumanReject},
		{"timeout", ReasonTimeout},
		{"timed out after 30m", ReasonTimeout},
		{"policy violation", ReasonPolicyViolation},
		{"agent_finished", ReasonAgentFinished},
		{"user_interrupted", ReasonUserInterrupted},
		{"error", ReasonError},
		{"", ReasonOther},
		{"some unrelated string", ReasonOther},
	}
	for _, c := range cases {
		got := LegacyBucketReworkReason(c.in)
		if got != c.want {
			t.Errorf("LegacyBucketReworkReason(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestClassifyRunOutcome_PrefersEventsOverSessionEnd(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := d.Exec(`
		INSERT INTO runs (id, jira_issue_key, agent_name, agent_type, user, workstation_id,
			cwd, started_at, status, session_end_reason)
		VALUES ('r-test', 'P-1', 'a', 'cc', 'u', 'ws-1', '/tmp', ?, 'error', 'agent_finished')
	`, now); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	if _, err := d.Exec(`INSERT INTO events (run_id, layer, event_type, ts) VALUES ('r-test', 1, 'test.fail', ?)`, now); err != nil {
		t.Fatalf("seed event: %v", err)
	}
	got := d.ClassifyRunOutcome("r-test")
	if got != ReasonTestFail {
		t.Errorf("event signal should win: got %q, want %q", got, ReasonTestFail)
	}
}

func TestClassifyRunOutcome_FallsBackToSessionEnd(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := d.Exec(`
		INSERT INTO runs (id, jira_issue_key, agent_name, agent_type, user, workstation_id,
			cwd, started_at, status, session_end_reason)
		VALUES ('r-fb', 'P-1', 'a', 'cc', 'u', 'ws-1', '/tmp', ?, 'done', 'user_interrupted')
	`, now); err != nil {
		t.Fatalf("seed: %v", err)
	}
	got := d.ClassifyRunOutcome("r-fb")
	if got != ReasonUserInterrupted {
		t.Errorf("got %q, want %q", got, ReasonUserInterrupted)
	}
}

func TestClassifyRunOutcome_UnknownReturnsOther(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	got := d.ClassifyRunOutcome("nonexistent-run")
	if got != ReasonOther {
		t.Errorf("missing run should classify as Other, got %q", got)
	}
}
