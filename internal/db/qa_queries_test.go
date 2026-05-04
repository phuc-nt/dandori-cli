package db

import (
	"testing"
	"time"
)

func seedQARun(t *testing.T, d *LocalDB, id, jira, eng string, started time.Time, lintDelta, testsDelta int, msgQ float64, qScore float64, cost float64) {
	t.Helper()
	_, err := d.Exec(`
		INSERT INTO runs (id, jira_issue_key, agent_name, agent_type, user, workstation_id,
			cwd, git_remote, started_at, duration_sec, status, cost_usd, engineer_name, department,
			human_intervention_count)
		VALUES (?, ?, 'a', 'claude_code', 'u', 'ws-1', '/tmp/r', 'git@x:r.git', ?, 60, 'done', ?, ?, 'eng', 1)
	`, id, jira, started.Format(time.RFC3339), cost, eng)
	if err != nil {
		t.Fatalf("seed run: %v", err)
	}
	_, err = d.Exec(`
		INSERT INTO quality_metrics (run_id, lint_delta, tests_delta, commit_msg_quality, quality_score)
		VALUES (?, ?, ?, ?, ?)
	`, id, lintDelta, testsDelta, msgQ, qScore)
	if err != nil {
		t.Fatalf("seed quality_metrics: %v", err)
	}
}

func TestQualityTimeline_AggregatesPerWeek(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now().UTC()
	seedQARun(t, d, "q1", "ALPHA-1", "alice", now, -2, 5, 80, 90, 1.0)
	seedQARun(t, d, "q2", "BETA-2", "bob", now, 1, -3, 50, 60, 0.5)

	pts, err := d.QualityTimeline("", 12)
	if err != nil {
		t.Fatalf("QualityTimeline: %v", err)
	}
	if len(pts) < 2 {
		t.Fatalf("want ≥2 points, got %d (%+v)", len(pts), pts)
	}
}

func TestQualityTimeline_FilterByProject(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now().UTC()
	seedQARun(t, d, "q1", "ALPHA-1", "alice", now, -2, 5, 80, 90, 1.0)
	seedQARun(t, d, "q2", "OTHER-1", "bob", now, 1, -3, 50, 60, 0.5)

	pts, err := d.QualityTimeline("ALPHA", 12)
	if err != nil {
		t.Fatalf("QualityTimeline: %v", err)
	}
	if len(pts) != 1 {
		t.Errorf("want 1 (filtered), got %d", len(pts))
	}
}

func TestCostQualityScatter_ReturnsPoints(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now().UTC()
	seedQARun(t, d, "q1", "PROJ-1", "alice", now, 0, 0, 70, 85, 2.0)
	seedQARun(t, d, "q2", "PROJ-2", "bob", now, 0, 0, 70, 65, 0.5)

	pts, err := d.CostQualityScatter(100)
	if err != nil {
		t.Fatalf("CostQualityScatter: %v", err)
	}
	if len(pts) != 2 {
		t.Errorf("want 2 points, got %d", len(pts))
	}
}

func TestCommitMsgDistribution_AlwaysFourBuckets(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now().UTC()
	seedQARun(t, d, "q1", "P-1", "a", now, 0, 0, 10, 50, 0)
	seedQARun(t, d, "q2", "P-1", "a", now, 0, 0, 30, 50, 0)
	seedQARun(t, d, "q3", "P-1", "a", now, 0, 0, 80, 50, 0)

	got, err := d.CommitMsgDistribution()
	if err != nil {
		t.Fatalf("CommitMsgDistribution: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("want 4 buckets, got %d", len(got))
	}
	wantOrder := []string{"0-25", "25-50", "50-75", "75-100"}
	for i, b := range got {
		if b.Bucket != wantOrder[i] {
			t.Errorf("bucket[%d]=%q want %q", i, b.Bucket, wantOrder[i])
		}
	}
}

func TestBugHotspots_CountsBuglinkRowsByRepoWeek(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now().UTC()
	// Three runs in the same week + repo. Lint/test deltas are irrelevant
	// post-v10 — only buglinks rows feed the count.
	seedQARun(t, d, "r1", "P-1", "a", now, 0, 0, 50, 50, 0)
	seedQARun(t, d, "r2", "P-1", "a", now, 0, 0, 50, 50, 0)
	seedQARun(t, d, "r3", "P-1", "a", now, 0, 0, 50, 50, 0)

	// r1 → linked from BUG-1
	if err := d.InsertBuglink("BUG-1", "r1", "test", "test"); err != nil {
		t.Fatalf("insert buglink BUG-1: %v", err)
	}
	// r2 → linked from BUG-2 AND BUG-3 (two distinct bugs, same offending run)
	if err := d.InsertBuglink("BUG-2", "r2", "test", "test"); err != nil {
		t.Fatalf("insert buglink BUG-2: %v", err)
	}
	if err := d.InsertBuglink("BUG-3", "r2", "test", "test"); err != nil {
		t.Fatalf("insert buglink BUG-3: %v", err)
	}
	// r3 → no buglink → must NOT show up.
	// BUG-1 also linked twice to r1 (idempotent — INSERT OR IGNORE drops dup)
	if err := d.InsertBuglink("BUG-1", "r1", "dup", "test"); err != nil {
		t.Fatalf("insert dup buglink: %v", err)
	}

	cells, err := d.BugHotspots(8)
	if err != nil {
		t.Fatalf("BugHotspots: %v", err)
	}
	total := 0
	for _, c := range cells {
		total += c.Count
	}
	// 3 distinct bug keys (BUG-1, BUG-2, BUG-3); r3 contributes 0; dup ignored.
	if total != 3 {
		t.Errorf("hotspot count = %d, want 3 (cells=%+v)", total, cells)
	}
}

func TestReworkCauses_GroupsByEnumKeys(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// Post v8→v9: session_outcomes is map[RunOutcomeReason]int.
	_, err := d.Exec(`
		INSERT INTO task_attribution (jira_issue_key, session_count, total_lines_final,
			lines_attributed_agent, lines_attributed_human, session_outcomes, jira_done_at)
		VALUES ('P-1', 4, 100, 80, 20,
			'{"test_fail":2,"lint_fail":1,"timeout":1}',
			datetime('now'))
	`)
	if err != nil {
		t.Fatalf("seed task_attribution: %v", err)
	}
	got, err := d.ReworkCauses()
	if err != nil {
		t.Fatalf("ReworkCauses: %v", err)
	}
	// Always 9 buckets, in canonical ReasonOrder.
	if len(got) != len(ReasonOrder) {
		t.Fatalf("want %d buckets, got %d", len(ReasonOrder), len(got))
	}
	byCause := map[string]int{}
	for _, c := range got {
		byCause[c.Cause] = c.Count
	}
	if byCause["test_fail"] != 2 || byCause["lint_fail"] != 1 || byCause["timeout"] != 1 {
		t.Errorf("buckets wrong: %+v", byCause)
	}
}

func TestReworkCauses_UnknownKeysFallToOther(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	_, err := d.Exec(`
		INSERT INTO task_attribution (jira_issue_key, session_count, total_lines_final,
			lines_attributed_agent, lines_attributed_human, session_outcomes, jira_done_at)
		VALUES ('P-2', 3, 50, 50, 0,
			'{"made_up_reason":2,"another_one":1}',
			datetime('now'))
	`)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	got, err := d.ReworkCauses()
	if err != nil {
		t.Fatalf("ReworkCauses: %v", err)
	}
	byCause := map[string]int{}
	for _, c := range got {
		byCause[c.Cause] = c.Count
	}
	if byCause["other"] != 3 {
		t.Errorf("unknown keys should fold to 'other': got %+v", byCause)
	}
}

func TestInterventionHeatmap_GroupsByEngineerAndHour(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now().UTC()
	// seedQARun seeds human_intervention_count=1 per row.
	seedQARun(t, d, "i1", "P-1", "alice", now, 0, 0, 50, 50, 0)
	seedQARun(t, d, "i2", "P-1", "alice", now.Add(-time.Hour), 0, 0, 50, 50, 0)
	seedQARun(t, d, "i3", "P-1", "bob", now, 0, 0, 50, 50, 0)

	cells, err := d.InterventionHeatmap(28)
	if err != nil {
		t.Fatalf("InterventionHeatmap: %v", err)
	}
	if len(cells) < 2 {
		t.Errorf("want ≥2 cells, got %d (%+v)", len(cells), cells)
	}
}
