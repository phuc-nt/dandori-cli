package db

import (
	"testing"
	"time"
)

func newV12DB(t *testing.T) *LocalDB {
	t.Helper()
	d := newEmptyLocalDB(t)
	if _, err := d.Exec(SchemaSQL); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	return d
}

func strPtr(s string) *string { return &s }
func intPtr(i int) *int       { return &i }

func TestUpsertPR_InsertAndIdempotent(t *testing.T) {
	d := newV12DB(t)
	pr := PREvent{
		Repo:      "o/r",
		PRNumber:  1,
		Title:     "feat: hello",
		State:     "open",
		Author:    "phuc",
		CreatedAt: "2026-05-01T00:00:00Z",
	}
	if err := d.UpsertPR(pr); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	if err := d.UpsertPR(pr); err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	n, err := d.CountPRs("o/r")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("want 1 row after dup upsert, got %d", n)
	}
}

func TestUpsertPR_UpdatesMutableFields(t *testing.T) {
	d := newV12DB(t)
	pr := PREvent{Repo: "o/r", PRNumber: 1, Title: "feat: x", State: "open", CreatedAt: "2026-05-01T00:00:00Z"}
	if err := d.UpsertPR(pr); err != nil {
		t.Fatal(err)
	}
	// Now PR closes + merges.
	pr.State = "closed"
	pr.MergedAt = strPtr("2026-05-02T10:00:00Z")
	pr.MergeCommitSHA = "abc123"
	pr.FirstApprovalAt = strPtr("2026-05-02T09:00:00Z")
	if err := d.UpsertPR(pr); err != nil {
		t.Fatal(err)
	}
	got, err := d.GetPRByNumber("o/r", 1)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("row missing")
	}
	if got.State != "closed" {
		t.Errorf("state = %s, want closed", got.State)
	}
	if got.MergedAt == nil || *got.MergedAt != "2026-05-02T10:00:00Z" {
		t.Errorf("merged_at not updated: %v", got.MergedAt)
	}
	if got.MergeCommitSHA != "abc123" {
		t.Errorf("sha = %s, want abc123", got.MergeCommitSHA)
	}
	if got.FirstApprovalAt == nil || *got.FirstApprovalAt != "2026-05-02T09:00:00Z" {
		t.Errorf("first_approval_at not set: %v", got.FirstApprovalAt)
	}
}

func TestGetPRByTitle_FindsRecentMerge(t *testing.T) {
	d := newV12DB(t)
	mergedAt := time.Now().UTC().Add(-3 * 24 * time.Hour).Format(time.RFC3339)
	if err := d.UpsertPR(PREvent{
		Repo: "o/r", PRNumber: 5, Title: "feat: add login", State: "closed",
		CreatedAt: "2026-05-01T00:00:00Z", MergedAt: &mergedAt,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := d.GetPRByTitle("o/r", "feat: add login", 30)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.PRNumber != 5 {
		t.Fatalf("want PR#5, got %+v", got)
	}
}

func TestGetPRByTitle_IgnoresOldMerges(t *testing.T) {
	d := newV12DB(t)
	mergedAt := time.Now().UTC().Add(-60 * 24 * time.Hour).Format(time.RFC3339)
	if err := d.UpsertPR(PREvent{
		Repo: "o/r", PRNumber: 5, Title: "feat: ancient", State: "closed",
		CreatedAt: "2026-03-01T00:00:00Z", MergedAt: &mergedAt,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := d.GetPRByTitle("o/r", "feat: ancient", 30)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("want nil for >30d merge, got #%d", got.PRNumber)
	}
}

func TestGetPRByTitle_IgnoresUnmerged(t *testing.T) {
	d := newV12DB(t)
	// No merged_at → revert lookup must skip.
	if err := d.UpsertPR(PREvent{
		Repo: "o/r", PRNumber: 5, Title: "feat: pending", State: "open",
		CreatedAt: "2026-05-01T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	got, err := d.GetPRByTitle("o/r", "feat: pending", 30)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("want nil for unmerged PR, got #%d", got.PRNumber)
	}
}

func TestMarkReverted(t *testing.T) {
	d := newV12DB(t)
	if err := d.UpsertPR(PREvent{Repo: "o/r", PRNumber: 5, Title: "feat: x", State: "closed", CreatedAt: "2026-05-01T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	if err := d.MarkReverted("o/r", 5, 9); err != nil {
		t.Fatal(err)
	}
	got, _ := d.GetPRByNumber("o/r", 5)
	if !got.IsReverted {
		t.Error("is_reverted not flipped")
	}
	if got.RevertedByPR == nil || *got.RevertedByPR != 9 {
		t.Errorf("reverted_by_pr = %v, want 9", got.RevertedByPR)
	}
	if got.RevertedAt == nil {
		t.Error("reverted_at not set")
	}
	firstStamp := *got.RevertedAt
	// Re-mark must preserve the original reverted_at (COALESCE behaviour).
	if err := d.MarkReverted("o/r", 5, 9); err != nil {
		t.Fatal(err)
	}
	got2, _ := d.GetPRByNumber("o/r", 5)
	if got2.RevertedAt == nil || *got2.RevertedAt != firstStamp {
		t.Errorf("reverted_at changed on re-mark: %v → %v", firstStamp, got2.RevertedAt)
	}
}

func TestMarkReopened(t *testing.T) {
	d := newV12DB(t)
	if err := d.UpsertPR(PREvent{Repo: "o/r", PRNumber: 7, Title: "feat: y", State: "open", CreatedAt: "2026-05-01T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	if err := d.MarkReopened("o/r", 7, "2026-05-02T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	got, _ := d.GetPRByNumber("o/r", 7)
	if got.ReopenedAt == nil || *got.ReopenedAt != "2026-05-02T00:00:00Z" {
		t.Errorf("reopened_at = %v", got.ReopenedAt)
	}
	// Idempotent — second call with a later timestamp must NOT overwrite.
	if err := d.MarkReopened("o/r", 7, "2026-05-03T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	got2, _ := d.GetPRByNumber("o/r", 7)
	if *got2.ReopenedAt != "2026-05-02T00:00:00Z" {
		t.Errorf("reopened_at overwritten: %s", *got2.ReopenedAt)
	}
}

func TestSyncState_RoundTrip(t *testing.T) {
	d := newV12DB(t)
	_, ok, err := d.GetSyncState("github.last_pr_sync_at")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("expected missing key on fresh DB")
	}
	if err := d.SetSyncState("github.last_pr_sync_at", "2026-05-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	v, ok, err := d.GetSyncState("github.last_pr_sync_at")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || v != "2026-05-01T00:00:00Z" {
		t.Errorf("get returned ok=%v v=%q", ok, v)
	}
	// Upsert
	if err := d.SetSyncState("github.last_pr_sync_at", "2026-05-02T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	v2, _, _ := d.GetSyncState("github.last_pr_sync_at")
	if v2 != "2026-05-02T00:00:00Z" {
		t.Errorf("upsert value = %s", v2)
	}
}

func TestCountPRs_ScopedByRepo(t *testing.T) {
	d := newV12DB(t)
	for _, n := range []int{1, 2, 3} {
		_ = d.UpsertPR(PREvent{Repo: "o/a", PRNumber: n, State: "open", CreatedAt: "2026-05-01T00:00:00Z"})
	}
	_ = d.UpsertPR(PREvent{Repo: "o/b", PRNumber: 1, State: "open", CreatedAt: "2026-05-01T00:00:00Z"})
	a, _ := d.CountPRs("o/a")
	b, _ := d.CountPRs("o/b")
	if a != 3 || b != 1 {
		t.Errorf("counts wrong: o/a=%d o/b=%d", a, b)
	}
}

// Unused vars silence unused-import in some go toolchains.
var _ = intPtr
