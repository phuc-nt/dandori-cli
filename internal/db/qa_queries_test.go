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

func TestBugHotspots_RegressionProxy(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now().UTC()
	// q1 has lint regression → counted; q2 has tests regression → counted; q3 clean → not counted.
	seedQARun(t, d, "q1", "P-1", "a", now, 3, 0, 50, 50, 0)
	seedQARun(t, d, "q2", "P-1", "a", now, 0, -2, 50, 50, 0)
	seedQARun(t, d, "q3", "P-1", "a", now, 0, 0, 50, 50, 0)

	cells, err := d.BugHotspots(8)
	if err != nil {
		t.Fatalf("BugHotspots: %v", err)
	}
	total := 0
	for _, c := range cells {
		total += c.Count
	}
	if total != 2 {
		t.Errorf("regression count = %d, want 2", total)
	}
}

func TestReworkCauses_BucketsJSONReasons(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	_, err := d.Exec(`
		INSERT INTO task_attribution (jira_issue_key, session_count, total_lines_final,
			lines_attributed_agent, lines_attributed_human, session_outcomes, jira_done_at)
		VALUES ('P-1', 3, 100, 80, 20,
			'[{"run_id":"r1","outcome":"failed","reason":"test failure"},{"run_id":"r2","outcome":"failed","reason":"lint violation"},{"run_id":"r3","outcome":"failed","reason":"timeout"}]',
			datetime('now'))
	`)
	if err != nil {
		t.Fatalf("seed task_attribution: %v", err)
	}
	got, err := d.ReworkCauses()
	if err != nil {
		t.Fatalf("ReworkCauses: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("want 5 buckets, got %d", len(got))
	}
	byCause := map[string]int{}
	for _, c := range got {
		byCause[c.Cause] = c.Count
	}
	if byCause["test_fail"] != 1 || byCause["lint_fail"] != 1 || byCause["timeout"] != 1 {
		t.Errorf("buckets wrong: %+v", byCause)
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
