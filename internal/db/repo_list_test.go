package db

import (
	"testing"
	"time"
)

// seedMergedRepoPR upserts a merged PR for an arbitrary repo. Used to
// build multi-repo fixtures for the repo_list + per-repo trust tests.
func seedMergedRepoPR(t *testing.T, d *LocalDB, repo string, num int, mergedAt time.Time) {
	t.Helper()
	ts := mergedAt.UTC().Format(time.RFC3339)
	if err := d.UpsertPR(PREvent{
		Repo: repo, PRNumber: num, Title: "feat: pr", State: "merged",
		CreatedAt: ts, SubmittedAt: ts,
		MergedAt: &ts, ClosedAt: &ts,
	}); err != nil {
		t.Fatalf("upsert %s#%d: %v", repo, num, err)
	}
}

func TestListReposWithMergedPRs_OrdersByCountDesc(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	// repo-A: 3 merges, repo-B: 1, repo-C: 2
	seedMergedRepoPR(t, d, "o/a", 1, now.Add(-3*24*time.Hour))
	seedMergedRepoPR(t, d, "o/a", 2, now.Add(-2*24*time.Hour))
	seedMergedRepoPR(t, d, "o/a", 3, now.Add(-1*24*time.Hour))
	seedMergedRepoPR(t, d, "o/b", 4, now.Add(-1*24*time.Hour))
	seedMergedRepoPR(t, d, "o/c", 5, now.Add(-2*24*time.Hour))
	seedMergedRepoPR(t, d, "o/c", 6, now.Add(-1*24*time.Hour))

	out, err := d.ListReposWithMergedPRs(28)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 3 {
		t.Fatalf("got %d repos, want 3: %+v", len(out), out)
	}
	if out[0].Repo != "o/a" || out[0].MergedCount != 3 {
		t.Errorf("first = %+v, want {o/a, 3}", out[0])
	}
	if out[1].Repo != "o/c" || out[1].MergedCount != 2 {
		t.Errorf("second = %+v, want {o/c, 2}", out[1])
	}
	if out[2].Repo != "o/b" || out[2].MergedCount != 1 {
		t.Errorf("third = %+v, want {o/b, 1}", out[2])
	}
}

func TestListReposWithMergedPRs_ExcludesOutOfWindow(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	seedMergedRepoPR(t, d, "o/old", 1, now.Add(-100*24*time.Hour))
	seedMergedRepoPR(t, d, "o/new", 2, now.Add(-1*24*time.Hour))

	out, _ := d.ListReposWithMergedPRs(28)
	if len(out) != 1 || out[0].Repo != "o/new" {
		t.Errorf("expected only o/new, got %+v", out)
	}
}

func TestListReposWithMergedPRs_EmptyWindow(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatal(err)
	}
	out, err := d.ListReposWithMergedPRs(28)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Errorf("expected empty, got %+v", out)
	}
}

func TestGetTrustIndexByRepo_ScopesAICFR(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	// Seed minimal task_attribution + run so HasData=true.
	if _, err := d.Exec(`
		INSERT INTO task_attribution (jira_issue_key, session_count, total_lines_final,
			lines_attributed_agent, lines_attributed_human, session_outcomes, total_iterations,
			jira_done_at)
		VALUES ('T-1', 1, 100, 90, 10, '{}', 1, ?)
	`, now.AddDate(0, 0, -1).Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`
		INSERT INTO runs (id, user, workstation_id, session_id, command, status, started_at, ended_at, human_intervention_count)
		VALUES ('R-1', 'tester', 'ws-1', 'S-1', 'claude', 'completed', ?, ?, 0)
	`, now.AddDate(0, 0, -1).Format(time.RFC3339), now.AddDate(0, 0, -1).Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	// repo-A: 2 clean merges. repo-B: 1 clean + 1 revert → CFR = 0.5.
	seedMergedRepoPR(t, d, "o/a", 1, now.Add(-3*24*time.Hour))
	seedMergedRepoPR(t, d, "o/a", 2, now.Add(-2*24*time.Hour))
	seedMergedRepoPR(t, d, "o/b", 10, now.Add(-3*24*time.Hour))
	seedMergedRepoPR(t, d, "o/b", 11, now.Add(-2*24*time.Hour))
	if err := d.MarkReverted("o/b", 11, 12); err != nil {
		t.Fatal(err)
	}

	// Filter to o/a — zero failures, CFR = 0.
	resA, err := d.GetTrustIndexByRepo(28, "o/a")
	if err != nil {
		t.Fatal(err)
	}
	if resA.Components.AICFR != 0 {
		t.Errorf("o/a CFR = %.3f, want 0", resA.Components.AICFR)
	}
	if resA.Repo != "o/a" || resA.RepoScope != "cfr_only" {
		t.Errorf("o/a Repo=%q RepoScope=%q", resA.Repo, resA.RepoScope)
	}

	// Filter to o/b — 1/2 reverted, CFR = 0.5.
	resB, err := d.GetTrustIndexByRepo(28, "o/b")
	if err != nil {
		t.Fatal(err)
	}
	if resB.Components.AICFR != 0.5 {
		t.Errorf("o/b CFR = %.3f, want 0.5", resB.Components.AICFR)
	}

	// No filter — blended over 4 merges, 1 failure → 0.25.
	resAll, err := d.GetTrustIndexByRepo(28, "")
	if err != nil {
		t.Fatal(err)
	}
	if resAll.Components.AICFR != 0.25 {
		t.Errorf("blended CFR = %.3f, want 0.25", resAll.Components.AICFR)
	}
	if resAll.RepoScope != "all" {
		t.Errorf("unfiltered RepoScope = %q, want all", resAll.RepoScope)
	}
}

func TestGetPRReviewCycleTimeByRepo_ScopesToRepo(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	// Helper inlined: PRs in different repos with different approval delays.
	seed := func(repo string, num int, delay time.Duration) {
		mergedAt := now.Add(-time.Duration(num) * 24 * time.Hour)
		submitted := mergedAt.Add(-delay - time.Hour)
		approval := submitted.Add(delay)
		subStr := submitted.UTC().Format(time.RFC3339)
		mergedStr := mergedAt.UTC().Format(time.RFC3339)
		approvalStr := approval.UTC().Format(time.RFC3339)
		if err := d.UpsertPR(PREvent{
			Repo: repo, PRNumber: num, Title: "feat: x", State: "merged",
			CreatedAt: subStr, SubmittedAt: subStr,
			MergedAt: &mergedStr, ClosedAt: &mergedStr,
			FirstApprovalAt: &approvalStr,
		}); err != nil {
			t.Fatalf("upsert: %v", err)
		}
	}
	seed("o/a", 1, 2*time.Hour)
	seed("o/a", 2, 4*time.Hour)
	seed("o/b", 3, 20*time.Hour)
	seed("o/b", 4, 24*time.Hour)

	resA, _ := d.GetPRReviewCycleTimeByRepo(28, "o/a")
	if resA.MergedTotal != 2 {
		t.Errorf("o/a MergedTotal = %d, want 2", resA.MergedTotal)
	}
	if resA.Repo != "o/a" {
		t.Errorf("o/a Repo = %q", resA.Repo)
	}
	if resA.MedianHours > 5 {
		t.Errorf("o/a median = %.1f, want <5h (fast reviews)", resA.MedianHours)
	}

	resB, _ := d.GetPRReviewCycleTimeByRepo(28, "o/b")
	if resB.MedianHours < 15 {
		t.Errorf("o/b median = %.1f, want >15h (slow reviews)", resB.MedianHours)
	}
}
