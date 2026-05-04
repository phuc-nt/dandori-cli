package demo

import (
	"path/filepath"
	"testing"

	"github.com/phuc-nt/dandori-cli/internal/db"
)

func newMigratedDB(t *testing.T) *db.LocalDB {
	t.Helper()
	p := filepath.Join(t.TempDir(), "seed.db")
	d, err := db.Open(p)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func countRuns(t *testing.T, d *db.LocalDB, where string, args ...any) int {
	t.Helper()
	q := "SELECT COUNT(*) FROM runs"
	if where != "" {
		q += " WHERE " + where
	}
	var c int
	if err := d.QueryRow(q, args...).Scan(&c); err != nil {
		t.Fatalf("count: %v", err)
	}
	return c
}

// Blog scenario:
//
//	Alice+alpha: 12 runs
//	Bob human-only: 9 rows (agent_name IS NULL)
//	Carol+beta: 7 runs
//
// Total = 28
func TestSeed_BlogScenario_InsertsExpectedRuns(t *testing.T) {
	d := newMigratedDB(t)

	if err := SeedBlogScenario(d); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if got := countRuns(t, d, "agent_name=? AND engineer_name=?", "alpha", "Alice"); got != 12 {
		t.Errorf("Alice+alpha: expected 12, got %d", got)
	}
	if got := countRuns(t, d, "engineer_name=? AND agent_name IS NULL", "Bob"); got != 9 {
		t.Errorf("Bob human-only: expected 9, got %d", got)
	}
	if got := countRuns(t, d, "agent_name=? AND engineer_name=?", "beta", "Carol"); got != 7 {
		t.Errorf("Carol+beta: expected 7, got %d", got)
	}
	if got := countRuns(t, d, ""); got != 28 {
		t.Errorf("total: expected 28, got %d", got)
	}
}

func TestSeed_Idempotent(t *testing.T) {
	d := newMigratedDB(t)

	if err := SeedBlogScenario(d); err != nil {
		t.Fatal(err)
	}
	if err := SeedBlogScenario(d); err != nil {
		t.Fatal(err)
	}

	if got := countRuns(t, d, ""); got != 28 {
		t.Errorf("expected 28 after 2x seed, got %d", got)
	}
}

func TestReset_ClearsAllRuns(t *testing.T) {
	d := newMigratedDB(t)

	if err := SeedBlogScenario(d); err != nil {
		t.Fatal(err)
	}
	if err := ResetDB(d); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if got := countRuns(t, d, ""); got != 0 {
		t.Errorf("expected 0 after reset, got %d", got)
	}
}

func TestSeed_CarolAC64Percent(t *testing.T) {
	d := newMigratedDB(t)
	if err := SeedBlogScenario(d); err != nil {
		t.Fatal(err)
	}

	// Carol seed should produce ~64% AC — we record quality_improved=1 for 4 or 5 of 7 runs.
	var improved int
	err := d.QueryRow(`
		SELECT COUNT(*) FROM runs r
		JOIN quality_metrics q ON q.run_id = r.id
		WHERE r.engineer_name = 'Carol' AND q.quality_score >= 0.8
	`).Scan(&improved)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if improved < 4 || improved > 5 {
		t.Errorf("Carol improved: expected 4-5 (≈64%% of 7), got %d", improved)
	}
}

func TestSeed_DepartmentsPopulated(t *testing.T) {
	d := newMigratedDB(t)
	if err := SeedBlogScenario(d); err != nil {
		t.Fatal(err)
	}

	// Seed must assign departments so `analytics cost --by department` has data.
	var depts int
	if err := d.QueryRow(`SELECT COUNT(DISTINCT department) FROM runs WHERE department IS NOT NULL`).Scan(&depts); err != nil {
		t.Fatal(err)
	}
	if depts < 2 {
		t.Errorf("expected ≥2 departments, got %d", depts)
	}
}

func TestSeedCrossProject_Counts(t *testing.T) {
	d := newMigratedDB(t)
	if err := SeedCrossProject(d); err != nil {
		t.Fatal(err)
	}

	// 3 projects × 3 sprints × 4 runs = 36.
	if got := countRuns(t, d, `command = ?`, seedTagCross); got != 36 {
		t.Errorf("cross-project runs = %d, want 36", got)
	}

	// Each project must contribute runs.
	for _, p := range []string{"CLITEST1", "CLITEST2", "CLITEST3"} {
		got := countRuns(t, d, `jira_issue_key LIKE ? AND command = ?`, p+"-%", seedTagCross)
		if got != 12 {
			t.Errorf("%s runs = %d, want 12", p, got)
		}
	}

	// 3 distinct sprints per project (suffix S1/S2/S3).
	var sprints int
	if err := d.QueryRow(`SELECT COUNT(DISTINCT jira_sprint_id) FROM runs WHERE command = ?`, seedTagCross).Scan(&sprints); err != nil {
		t.Fatal(err)
	}
	if sprints != 9 {
		t.Errorf("distinct sprints = %d, want 9 (3 projects × 3 sprints)", sprints)
	}
}

func TestSeedCrossProject_Idempotent(t *testing.T) {
	d := newMigratedDB(t)
	if err := SeedCrossProject(d); err != nil {
		t.Fatal(err)
	}
	if err := SeedCrossProject(d); err != nil {
		t.Fatal(err)
	}
	if got := countRuns(t, d, `command = ?`, seedTagCross); got != 36 {
		t.Errorf("after double-seed: %d runs, want 36 (idempotent)", got)
	}
}
