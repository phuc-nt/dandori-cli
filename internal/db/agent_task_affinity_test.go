package db

import (
	"testing"
	"time"
)

// insertAffinityRun inserts a minimal run row for affinity tests.
func insertAffinityRun(t *testing.T, d *LocalDB, id, agent, issueKey string, exitCode int, when time.Time) {
	t.Helper()
	_, err := d.Exec(`
		INSERT INTO runs (id, agent_name, jira_issue_key, exit_code, user, workstation_id, started_at, status)
		VALUES (?, ?, ?, ?, 'tester', 'ws1', ?, 'done')
	`, id, agent, issueKey, exitCode, when.Format(time.RFC3339))
	if err != nil {
		t.Fatalf("insertAffinityRun %s: %v", id, err)
	}
}

func TestGetAgentTaskAffinity_EmptyDB(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	cells, err := d.GetAgentTaskAffinity(time.Now().AddDate(0, 0, -28))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cells) != 0 {
		t.Errorf("expected empty slice, got %d cells", len(cells))
	}
}

func TestGetAgentTaskAffinity_SingleAgentSingleType(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	now := time.Now().UTC()
	insertAffinityRun(t, d, "r1", "alpha", "FEAT-1", 0, now) // success
	insertAffinityRun(t, d, "r2", "alpha", "FEAT-2", 0, now) // success
	insertAffinityRun(t, d, "r3", "alpha", "FEAT-3", 1, now) // failure

	cells, err := d.GetAgentTaskAffinity(now.AddDate(0, 0, -1))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cells) != 1 {
		t.Fatalf("expected 1 cell, got %d: %+v", len(cells), cells)
	}
	c := cells[0]
	if c.Agent != "alpha" {
		t.Errorf("agent = %q, want alpha", c.Agent)
	}
	if c.TaskType != "Feat" {
		t.Errorf("task_type = %q, want Feat", c.TaskType)
	}
	if c.Runs != 3 {
		t.Errorf("runs = %d, want 3", c.Runs)
	}
	// 2/3 = 66.7%
	if c.SuccessRate < 66.0 || c.SuccessRate > 67.0 {
		t.Errorf("success_rate = %.1f, want ~66.7", c.SuccessRate)
	}
}

func TestGetAgentTaskAffinity_MultiAgentMatrix(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	now := time.Now().UTC()
	// alpha: FEAT → 2/2 = 100%, BUG → 1/2 = 50%
	insertAffinityRun(t, d, "r1", "alpha", "FEAT-1", 0, now)
	insertAffinityRun(t, d, "r2", "alpha", "FEAT-2", 0, now)
	insertAffinityRun(t, d, "r3", "alpha", "BUG-1", 0, now)
	insertAffinityRun(t, d, "r4", "alpha", "BUG-2", 1, now)
	// beta: FEAT → 0/1 = 0%
	insertAffinityRun(t, d, "r5", "beta", "FEAT-3", 1, now)

	cells, err := d.GetAgentTaskAffinity(now.AddDate(0, 0, -1))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Expect 3 cells: (alpha,Bug), (alpha,Feat), (beta,Feat)
	if len(cells) != 3 {
		t.Fatalf("expected 3 cells, got %d: %+v", len(cells), cells)
	}

	idx := map[string]AffinityCell{}
	for _, c := range cells {
		idx[c.Agent+"|"+c.TaskType] = c
	}

	if c, ok := idx["alpha|Feat"]; !ok {
		t.Error("missing (alpha, Feat)")
	} else if c.SuccessRate != 100.0 {
		t.Errorf("alpha/Feat success = %.1f, want 100", c.SuccessRate)
	}

	if c, ok := idx["alpha|Bug"]; !ok {
		t.Error("missing (alpha, Bug)")
	} else if c.SuccessRate != 50.0 {
		t.Errorf("alpha/Bug success = %.1f, want 50", c.SuccessRate)
	}

	if c, ok := idx["beta|Feat"]; !ok {
		t.Error("missing (beta, Feat)")
	} else if c.SuccessRate != 0.0 {
		t.Errorf("beta/Feat success = %.1f, want 0", c.SuccessRate)
	}
}

func TestGetAgentTaskAffinity_FallbackToUnknown(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	now := time.Now().UTC()
	// No jira key → unknown bucket
	insertAffinityRun(t, d, "r1", "alpha", "", 0, now)
	insertAffinityRun(t, d, "r2", "alpha", "", 1, now)

	cells, err := d.GetAgentTaskAffinity(now.AddDate(0, 0, -1))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cells) != 1 {
		t.Fatalf("expected 1 cell, got %d", len(cells))
	}
	if cells[0].TaskType != "(unknown)" {
		t.Errorf("task_type = %q, want (unknown)", cells[0].TaskType)
	}
}

func TestGetAgentTaskAffinity_SinceFilter(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	now := time.Now().UTC()
	// Within window
	insertAffinityRun(t, d, "r1", "alpha", "FEAT-1", 0, now)
	// Outside window (40 days ago)
	insertAffinityRun(t, d, "r2", "alpha", "BUG-1", 0, now.AddDate(0, 0, -40))

	cells, err := d.GetAgentTaskAffinity(now.AddDate(0, 0, -28))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cells) != 1 {
		t.Fatalf("expected 1 cell (FEAT only), got %d: %+v", len(cells), cells)
	}
	if cells[0].TaskType != "Feat" {
		t.Errorf("task_type = %q, want Feat", cells[0].TaskType)
	}
}

// TestResolveTaskType verifies the prefix-parse + unknown fallback logic directly.
func TestResolveTaskType(t *testing.T) {
	cache := map[string]string{}
	cases := []struct {
		key  string
		want string
	}{
		{"FEAT-1", "Feat"},
		{"BUG-99", "Bug"},
		{"CLITEST-123", "Clitest"},
		{"", "(unknown)"},
		{"nohyphen", "(unknown)"},
		{"123-bad", "(unknown)"}, // doesn't start with letter
	}
	for _, tc := range cases {
		got := resolveTaskType(tc.key, cache)
		if got != tc.want {
			t.Errorf("resolveTaskType(%q) = %q, want %q", tc.key, got, tc.want)
		}
	}
	// Cache hit: same key returns cached value
	cache["X-1"] = "Cached"
	if got := resolveTaskType("X-1", cache); got != "Cached" {
		t.Errorf("cache hit: got %q, want Cached", got)
	}
}
