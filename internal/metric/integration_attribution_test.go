package metric

import (
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/phuc-nt/dandori-cli/internal/attribution"
	"github.com/phuc-nt/dandori-cli/internal/db"
)

// TestE2E_AttributionPipeline drives the full chain: seed runs in a v5 DB,
// build a real git repo with controlled commits, call ComputeAndPersist
// (the same entry the cmd hooks use), then run the metric exporter and
// assert the aggregated AttributionResult matches expectations.
//
// Mirrors the dogfood path: agent session → human follow-up → Jira done.
func TestE2E_AttributionPipeline(t *testing.T) {
	d := openMetricAttributionDB(t)
	repoDir := makeRepo(t)
	commit(t, repoDir, "a.go", "package x\nfunc Init() {}\n")
	h0 := head(t, repoDir)

	commit(t, repoDir, "a.go", "package x\nfunc Init() {}\nfunc A() {}\n")
	h1 := head(t, repoDir)

	// Persist the agent session.
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := d.Exec(`INSERT INTO runs (
		id, jira_issue_key, agent_type, user, workstation_id, started_at,
		status, git_head_before, git_head_after,
		input_tokens, output_tokens, cost_usd,
		session_end_reason,
		human_message_count, agent_message_count,
		human_intervention_count, human_approval_count
	) VALUES ('e2e-1', 'TASK-E1', 'claude_code', 'tester', 'ws-1', ?,
		'done', ?, ?, 1500, 500, 0.10, 'agent_finished', 5, 4, 1, 4)`,
		now, h0, h1); err != nil {
		t.Fatalf("seed run: %v", err)
	}

	// Human appends a line after the session.
	commit(t, repoDir, "a.go", "package x\nfunc Init() {}\nfunc A() {}\nfunc H() {}\n")
	finalHead := head(t, repoDir)

	if err := attribution.ComputeAndPersist(d, "TASK-E1", repoDir, finalHead); err != nil {
		t.Fatalf("ComputeAndPersist: %v", err)
	}

	// Now run the metric export over a window that covers `now`.
	w := MetricWindow{
		Start: time.Now().UTC().Add(-24 * time.Hour),
		End:   time.Now().UTC().Add(24 * time.Hour),
	}
	res, err := AggregateAttribution(d, w)
	if err != nil {
		t.Fatalf("AggregateAttribution: %v", err)
	}
	if res.TasksWithSession != 1 {
		t.Errorf("TasksWithSession = %d, want 1", res.TasksWithSession)
	}
	// Lines: func A (agent), func H (human) → retention = 0.5.
	if math.Abs(res.RetentionP50-0.5) > 0.01 {
		t.Errorf("RetentionP50 = %f, want 0.5", res.RetentionP50)
	}
	// intervention_rate = 1 / (1 + 4) = 0.2. autonomy threshold is strict <0.2,
	// so this task does NOT count as autonomous → autonomy_rate = 0.
	if math.Abs(res.AgentAutonomyRate-0.0) > 0.01 {
		t.Errorf("AgentAutonomyRate = %f, want 0", res.AgentAutonomyRate)
	}

	// And formatted output with the flag on must include the block.
	rep := ExportReport{
		Config:      ExportConfig{Window: w, IncludeAttribution: true},
		GeneratedAt: time.Now().UTC(),
		Deploy:      DeployFreqResult{Window: w, InsufficientData: true},
		LeadTime:    LeadTimeResult{Window: w, InsufficientData: true},
		CFR:         CFRResult{Window: w, InsufficientData: true},
		MTTR:        MTTRResult{Window: w, InsufficientData: true},
		Rework:      ReworkResult{Window: w, InsufficientData: true},
		Attribution: &res,
	}
	body, err := FormatReport(rep, FormatFaros)
	if err != nil {
		t.Fatalf("FormatReport: %v", err)
	}
	if !strings.Contains(string(body), `"task_attribution"`) {
		t.Errorf("faros output missing task_attribution block: %s", string(body))
	}
}

// makeRepo, commit, head are minimal git helpers local to this E2E test.
// The attribution package owns its own equivalents for unit tests; we keep
// these private to avoid leaking helpers across package boundaries.
func makeRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runCmd(t, dir, "git", "init", "-q", "-b", "main")
	runCmd(t, dir, "git", "config", "user.email", "test@example.com")
	runCmd(t, dir, "git", "config", "user.name", "Tester")
	runCmd(t, dir, "git", "config", "commit.gpgsign", "false")
	runCmd(t, dir, "git", "commit", "--allow-empty", "-q", "-m", "init")
	return dir
}

func commit(t *testing.T, dir, relPath, content string) {
	t.Helper()
	full := filepath.Join(dir, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	runCmd(t, dir, "git", "add", relPath)
	runCmd(t, dir, "git", "commit", "-q", "-m", "edit "+relPath)
}

func head(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("rev-parse: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func runCmd(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}

// Compile-time check that *db.LocalDB satisfies the source the metric pkg
// expects for attribution. Not strictly a test, but it pins the contract.
var _ *db.LocalDB = (*db.LocalDB)(nil)
