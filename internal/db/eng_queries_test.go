package db

import (
	"testing"
	"time"
)

func seedEngRun(t *testing.T, d *LocalDB, id, agent, eng, model, sessionEnd, ws, repo string,
	started time.Time, cost, dur float64, input, cacheRead int, approvals int, status string) {
	t.Helper()
	_, err := d.Exec(`
		INSERT INTO runs (id, jira_issue_key, agent_name, agent_type,
			user, workstation_id, cwd, git_remote, started_at, duration_sec, status,
			cost_usd, engineer_name, department,
			input_tokens, cache_read_tokens, model, session_end_reason, human_approval_count)
		VALUES (?, 'PROJ-1', ?, 'claude_code', 'u', ?, '/tmp/repo', ?, ?, ?, ?, ?, ?, 'eng',
			?, ?, ?, ?, ?)
	`, id, agent, ws, repo, started.Format(time.RFC3339), dur, status,
		cost, eng, input, cacheRead, model, sessionEnd, approvals)
	if err != nil {
		t.Fatalf("seed run %s: %v", id, err)
	}
}

func TestAgentMetrics_AggregatesAcrossRuns(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now().UTC()
	seedEngRun(t, d, "r1", "alpha", "alice", "claude-sonnet-4", "stop", "ws1", "git@x:repo1.git",
		now.Add(-2*time.Hour), 1.0, 600, 1000, 500, 0, "done")
	seedEngRun(t, d, "r2", "alpha", "alice", "claude-sonnet-4", "stop", "ws1", "git@x:repo1.git",
		now.Add(-1*time.Hour), 2.0, 1200, 2000, 1500, 1, "done")
	seedEngRun(t, d, "r3", "beta", "bob", "claude-haiku", "stop", "ws2", "git@x:repo2.git",
		now, 0.5, 300, 500, 100, 2, "failed")

	pack, err := d.AgentMetrics("alpha")
	if err != nil {
		t.Fatalf("AgentMetrics: %v", err)
	}
	if pack.Runs != 2 {
		t.Errorf("Runs = %d, want 2", pack.Runs)
	}
	if pack.TotalCost != 3.0 {
		t.Errorf("TotalCost = %v, want 3.0", pack.TotalCost)
	}
	if pack.SuccessRate != 100 {
		t.Errorf("SuccessRate = %v, want 100", pack.SuccessRate)
	}
	// Cache eff: cacheRead=2000, total=cacheRead+input=2000+3000=5000 → 40%.
	if pack.CacheEffPct < 39.9 || pack.CacheEffPct > 40.1 {
		t.Errorf("CacheEffPct = %v, want ~40", pack.CacheEffPct)
	}
}

func TestAgentMetrics_EmptyAgentReturnsZero(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pack, err := d.AgentMetrics("nonexistent")
	if err != nil {
		t.Fatalf("AgentMetrics: %v", err)
	}
	if pack.Runs != 0 || pack.TotalCost != 0 {
		t.Errorf("expected zeros, got %+v", pack)
	}
}

func TestAutonomyTimeline_GroupsByDay(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	base := time.Now().UTC().Add(-24 * time.Hour)
	// 2 runs day 0, 1 run day 1.
	seedEngRun(t, d, "a1", "alpha", "alice", "m", "stop", "ws", "r", base, 1.0, 600, 100, 50, 0, "done")
	seedEngRun(t, d, "a2", "alpha", "alice", "m", "stop", "ws", "r", base.Add(time.Hour), 1.0, 600, 100, 50, 1, "done")
	seedEngRun(t, d, "a3", "alpha", "alice", "m", "stop", "ws", "r", base.AddDate(0, 0, 1), 1.0, 600, 100, 50, 0, "done")

	got, err := d.AutonomyTimeline("alice", 7)
	if err != nil {
		t.Fatalf("AutonomyTimeline: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 days, got %d", len(got))
	}
}

func TestModelMix_GroupsByModel(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now().UTC()
	seedEngRun(t, d, "m1", "alpha", "a", "claude-sonnet-4", "stop", "ws", "r", now, 1.0, 60, 100, 0, 0, "done")
	seedEngRun(t, d, "m2", "alpha", "a", "claude-sonnet-4", "stop", "ws", "r", now, 2.0, 60, 100, 0, 0, "done")
	seedEngRun(t, d, "m3", "alpha", "a", "claude-haiku", "stop", "ws", "r", now, 0.5, 60, 100, 0, 0, "done")

	got, err := d.ModelMix(28)
	if err != nil {
		t.Fatalf("ModelMix: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 models, got %d", len(got))
	}
	// First entry should be highest cost (sonnet, 3.0).
	if got[0].Model != "claude-sonnet-4" || got[0].Runs != 2 || got[0].Cost != 3.0 {
		t.Errorf("top row = %+v, want sonnet/runs=2/cost=3.0", got[0])
	}
}

func TestDurationHistogram_BucketsRuns(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now().UTC()
	durations := []float64{60, 120, 300, 600, 1200, 3600}
	for i, dur := range durations {
		seedEngRun(t, d, "d"+string(rune('a'+i)), "alpha", "a", "m", "stop", "ws", "r",
			now.Add(time.Duration(i)*time.Minute), 0.1, dur, 100, 0, 0, "done")
	}
	got, err := d.DurationHistogram(28)
	if err != nil {
		t.Fatalf("DurationHistogram: %v", err)
	}
	if len(got) != 6 {
		t.Fatalf("want 6 buckets, got %d", len(got))
	}
	total := 0
	for _, b := range got {
		total += b.Count
	}
	if total != len(durations) {
		t.Errorf("histogram total = %d, want %d", total, len(durations))
	}
}

func TestWorkstationMatrix_GroupsByPair(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now().UTC()
	seedEngRun(t, d, "w1", "alpha", "alice", "m", "stop", "ws-1", "r", now, 0.1, 60, 0, 0, 0, "done")
	seedEngRun(t, d, "w2", "alpha", "alice", "m", "stop", "ws-1", "r", now.Add(time.Hour), 0.1, 60, 0, 0, 0, "done")
	seedEngRun(t, d, "w3", "alpha", "bob", "m", "stop", "ws-2", "r", now, 0.1, 60, 0, 0, 0, "done")

	got, err := d.WorkstationMatrix(28)
	if err != nil {
		t.Fatalf("WorkstationMatrix: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 ws/eng pairs, got %d (%+v)", len(got), got)
	}
}

func TestRepoLeaderboard_RanksByCost(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now().UTC()
	seedEngRun(t, d, "r1", "alpha", "a", "m", "stop", "ws", "git@x:hi.git", now, 5.0, 60, 0, 0, 0, "done")
	seedEngRun(t, d, "r2", "alpha", "a", "m", "stop", "ws", "git@x:lo.git", now, 1.0, 60, 0, 0, 0, "done")

	got, err := d.RepoLeaderboard(28)
	if err != nil {
		t.Fatalf("RepoLeaderboard: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 repos, got %d", len(got))
	}
	if got[0].Cost < got[1].Cost {
		t.Errorf("not sorted by cost desc: %v", got)
	}
	if len(got[0].Spark) != 14 {
		t.Errorf("spark len = %d, want 14", len(got[0].Spark))
	}
}
