package attribution

import (
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/phuc-nt/dandori-cli/internal/db"
)

// openAttributionDB opens a fresh local SQLite at v5 schema. Mirrors the
// helper pattern used in internal/wrapper tests.
func openAttributionDB(t *testing.T) *db.LocalDB {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return d
}

// insertRun inserts a runs row at status=done with the given jira key, head
// before/after, tokens, cost, and message counts. Returns the runID.
func insertRun(t *testing.T, d *db.LocalDB, jiraKey, headBefore, headAfter string, inTokens, outTokens int, cost float64, mc MessageCounts, iterations int) string {
	t.Helper()
	runID := jiraKey + "-" + headAfter[:6]
	_, err := d.Exec(`INSERT INTO runs (
		id, jira_issue_key, agent_type, user, workstation_id, started_at,
		status, git_head_before, git_head_after,
		input_tokens, output_tokens, cost_usd,
		session_end_reason,
		human_message_count, agent_message_count,
		human_intervention_count, human_approval_count
	) VALUES (?, ?, 'claude_code', 'tester', 'ws-1', ?,
		'done', ?, ?, ?, ?, ?, 'agent_finished', ?, ?, ?, ?)`,
		runID, jiraKey, time.Now().Format(time.RFC3339),
		headBefore, headAfter, inTokens, outTokens, cost,
		mc.HumanTotal, mc.AgentTotal, mc.Interventions, mc.Approvals,
	)
	if err != nil {
		t.Fatalf("insert run: %v", err)
	}
	// Iterations as task.iteration.start events on this run.
	for i := 0; i < iterations; i++ {
		_, err := d.Exec(`INSERT INTO events (run_id, layer, event_type, data, ts)
			VALUES (?, 4, 'task.iteration.start', ?, ?)`,
			runID, `{}`, time.Now().Format(time.RFC3339))
		if err != nil {
			t.Fatalf("insert iteration event: %v", err)
		}
	}
	return runID
}

// TestComputeAndPersist_HappyPath: 2 sessions on the same Jira key, agent-only
// diffs, no human follow-up. Verifies the aggregated row is written with
// correct totals.
func TestComputeAndPersist_HappyPath(t *testing.T) {
	d := openAttributionDB(t)
	repo := newTestRepo(t)
	repo.commit("a.go", "package x\n")
	h0 := repo.head()
	repo.commit("a.go", "package x\nfunc S1() {}\n")
	h1 := repo.head()
	repo.commit("a.go", "package x\nfunc S1() {}\nfunc S2() {}\n")
	h2 := repo.head()

	insertRun(t, d, "TASK-1", h0, h1, 800, 200, 0.05,
		MessageCounts{HumanTotal: 3, AgentTotal: 4, Interventions: 1, Approvals: 2}, 2)
	insertRun(t, d, "TASK-1", h1, h2, 400, 100, 0.025,
		MessageCounts{HumanTotal: 2, AgentTotal: 3, Interventions: 0, Approvals: 2}, 1)

	if err := ComputeAndPersist(d, "TASK-1", repo.path, h2); err != nil {
		t.Fatalf("ComputeAndPersist: %v", err)
	}

	var (
		sessionCount, totalAgentTokens, agentLines, humanLines, totalIters, totalHumanMsg, totalIntervention int
		cost, intRate                                                                                        float64
	)
	row := d.QueryRow(`SELECT session_count, total_agent_tokens, total_agent_cost_usd,
		lines_attributed_agent, lines_attributed_human,
		total_iterations, total_human_messages, total_intervention_count, intervention_rate
		FROM task_attribution WHERE jira_issue_key = ?`, "TASK-1")
	if err := row.Scan(&sessionCount, &totalAgentTokens, &cost, &agentLines, &humanLines, &totalIters, &totalHumanMsg, &totalIntervention, &intRate); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if sessionCount != 2 {
		t.Errorf("session_count = %d, want 2", sessionCount)
	}
	if totalAgentTokens != 1500 {
		t.Errorf("total_agent_tokens = %d, want 1500", totalAgentTokens)
	}
	if math.Abs(cost-0.075) > 0.001 {
		t.Errorf("total_agent_cost_usd = %f, want ~0.075", cost)
	}
	if agentLines != 2 {
		t.Errorf("lines_attributed_agent = %d, want 2", agentLines)
	}
	if humanLines != 0 {
		t.Errorf("lines_attributed_human = %d, want 0", humanLines)
	}
	if totalIters != 3 {
		t.Errorf("total_iterations = %d, want 3", totalIters)
	}
	if totalHumanMsg != 5 {
		t.Errorf("total_human_messages = %d, want 5", totalHumanMsg)
	}
	if totalIntervention != 1 {
		t.Errorf("total_intervention_count = %d, want 1", totalIntervention)
	}
	// intervention_rate = 1 / (1 + 4) = 0.2
	if math.Abs(intRate-0.2) > 0.01 {
		t.Errorf("intervention_rate = %f, want ~0.2", intRate)
	}
}

// TestComputeAndPersist_HumanFollowupAfterSession: agent session ends, then
// human commits more lines. lines_attributed_human > 0.
func TestComputeAndPersist_HumanFollowupAfterSession(t *testing.T) {
	d := openAttributionDB(t)
	repo := newTestRepo(t)
	repo.commit("f.go", "package x\n")
	h0 := repo.head()
	repo.commit("f.go", "package x\nfunc Agent() {}\n")
	h1 := repo.head()
	repo.commit("f.go", "package x\nfunc Agent() {}\nfunc Human() {}\n")
	finalHead := repo.head()

	insertRun(t, d, "TASK-2", h0, h1, 100, 50, 0.01,
		MessageCounts{HumanTotal: 1, Interventions: 0, Approvals: 1}, 1)

	if err := ComputeAndPersist(d, "TASK-2", repo.path, finalHead); err != nil {
		t.Fatalf("ComputeAndPersist: %v", err)
	}
	var agentLines, humanLines int
	if err := d.QueryRow(`SELECT lines_attributed_agent, lines_attributed_human
		FROM task_attribution WHERE jira_issue_key=?`, "TASK-2").
		Scan(&agentLines, &humanLines); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if agentLines != 1 || humanLines != 1 {
		t.Errorf("attribution = (%d agent, %d human), want (1, 1)", agentLines, humanLines)
	}
}

// TestComputeAndPersist_NoSessions_HumanOnlyTask: no runs exist for the key.
// Spec says: skip persist (keep table sparse). Function returns nil.
func TestComputeAndPersist_NoSessions_HumanOnlyTask(t *testing.T) {
	d := openAttributionDB(t)
	repo := newTestRepo(t)
	repo.commit("h.go", "package x\nfunc HumanOnly() {}\n")
	finalHead := repo.head()

	if err := ComputeAndPersist(d, "TASK-NONE", repo.path, finalHead); err != nil {
		t.Fatalf("ComputeAndPersist: %v", err)
	}
	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM task_attribution WHERE jira_issue_key=?`, "TASK-NONE").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("rows = %d, want 0 (skip when no sessions)", n)
	}
}

// TestComputeAndPersist_ReentrantUpsert: second call with same key updates
// the row in place rather than erroring on PRIMARY KEY.
func TestComputeAndPersist_ReentrantUpsert(t *testing.T) {
	d := openAttributionDB(t)
	repo := newTestRepo(t)
	repo.commit("u.go", "package x\n")
	h0 := repo.head()
	repo.commit("u.go", "package x\nfunc U() {}\n")
	h1 := repo.head()

	insertRun(t, d, "TASK-3", h0, h1, 100, 50, 0.01,
		MessageCounts{HumanTotal: 1, Approvals: 1}, 1)

	if err := ComputeAndPersist(d, "TASK-3", repo.path, h1); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if err := ComputeAndPersist(d, "TASK-3", repo.path, h1); err != nil {
		t.Fatalf("second call: %v", err)
	}
	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM task_attribution WHERE jira_issue_key=?`, "TASK-3").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("rows after upsert = %d, want 1", n)
	}
}

// TestComputeAndPersist_ZeroDenominatorInterventionRate: no human messages
// after agent tool_use → no interventions, no approvals → rate = 0 (and no
// crash). Spec calls this out explicitly.
func TestComputeAndPersist_ZeroDenominatorInterventionRate(t *testing.T) {
	d := openAttributionDB(t)
	repo := newTestRepo(t)
	repo.commit("z.go", "package x\n")
	h0 := repo.head()
	repo.commit("z.go", "package x\nfunc Z() {}\n")
	h1 := repo.head()

	insertRun(t, d, "TASK-Z", h0, h1, 100, 50, 0.01,
		MessageCounts{HumanTotal: 0, Interventions: 0, Approvals: 0}, 1)

	if err := ComputeAndPersist(d, "TASK-Z", repo.path, h1); err != nil {
		t.Fatalf("ComputeAndPersist: %v", err)
	}
	var rate float64
	if err := d.QueryRow(`SELECT intervention_rate FROM task_attribution WHERE jira_issue_key=?`, "TASK-Z").Scan(&rate); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if rate != 0 {
		t.Errorf("intervention_rate = %f, want 0", rate)
	}
}
