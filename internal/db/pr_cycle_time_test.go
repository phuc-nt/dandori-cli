package db

import (
	"math"
	"testing"
	"time"
)

// seedMergedPRWithApproval inserts a merged PR whose first approval came
// `approvalDelay` after submission.
func seedMergedPRWithApproval(t *testing.T, d *LocalDB, num int, mergedAt time.Time, approvalDelay time.Duration) {
	t.Helper()
	submitted := mergedAt.Add(-approvalDelay - time.Hour) // submit before approve
	approval := submitted.Add(approvalDelay)
	subStr := submitted.UTC().Format(time.RFC3339)
	mergedStr := mergedAt.UTC().Format(time.RFC3339)
	approvalStr := approval.UTC().Format(time.RFC3339)
	if err := d.UpsertPR(PREvent{
		Repo: "o/r", PRNumber: num, Title: "feat: x", State: "merged",
		CreatedAt: subStr, SubmittedAt: subStr,
		MergedAt: &mergedStr, ClosedAt: &mergedStr,
		FirstApprovalAt: &approvalStr,
	}); err != nil {
		t.Fatalf("upsert pr#%d: %v", num, err)
	}
}

// seedMergedPRNoApproval inserts a merged PR with no approving review
// (e.g. self-merge). Such PRs should count toward MergedTotal but NOT
// WithApproval.
func seedMergedPRNoApproval(t *testing.T, d *LocalDB, num int, mergedAt time.Time) {
	t.Helper()
	ts := mergedAt.UTC().Format(time.RFC3339)
	if err := d.UpsertPR(PREvent{
		Repo: "o/r", PRNumber: num, Title: "feat: y", State: "merged",
		CreatedAt: ts, SubmittedAt: ts,
		MergedAt: &ts, ClosedAt: &ts,
	}); err != nil {
		t.Fatalf("upsert pr#%d: %v", num, err)
	}
}

func TestGetPRReviewCycleTime_EmptyWindow(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatal(err)
	}
	res, err := d.GetPRReviewCycleTime(28)
	if err != nil {
		t.Fatal(err)
	}
	if res.HasData {
		t.Error("HasData should be false on empty pr_events")
	}
	if res.MergedTotal != 0 || res.WithApproval != 0 {
		t.Errorf("counts non-zero: %+v", res)
	}
}

func TestGetPRReviewCycleTime_SinglePR(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	seedMergedPRWithApproval(t, d, 1, now.Add(-24*time.Hour), 4*time.Hour)
	res, err := d.GetPRReviewCycleTime(28)
	if err != nil {
		t.Fatal(err)
	}
	if !res.HasData {
		t.Fatal("HasData=false")
	}
	if res.MergedTotal != 1 || res.WithApproval != 1 {
		t.Errorf("counts wrong: %+v", res)
	}
	if math.Abs(res.MedianHours-4.0) > 0.1 {
		t.Errorf("median = %.2fh, want ~4h", res.MedianHours)
	}
}

func TestGetPRReviewCycleTime_OddCountMedian(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	// 3 PRs with delays 2h, 4h, 8h → median = 4h.
	seedMergedPRWithApproval(t, d, 1, now.Add(-3*24*time.Hour), 2*time.Hour)
	seedMergedPRWithApproval(t, d, 2, now.Add(-2*24*time.Hour), 4*time.Hour)
	seedMergedPRWithApproval(t, d, 3, now.Add(-1*24*time.Hour), 8*time.Hour)

	res, _ := d.GetPRReviewCycleTime(28)
	if math.Abs(res.MedianHours-4.0) > 0.1 {
		t.Errorf("median = %.2fh, want ~4h", res.MedianHours)
	}
}

func TestGetPRReviewCycleTime_EvenCountMedianInterpolates(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	// 4 PRs with delays 1h, 3h, 5h, 7h → p50 between 3 and 5 → 4h.
	seedMergedPRWithApproval(t, d, 1, now.Add(-4*24*time.Hour), 1*time.Hour)
	seedMergedPRWithApproval(t, d, 2, now.Add(-3*24*time.Hour), 3*time.Hour)
	seedMergedPRWithApproval(t, d, 3, now.Add(-2*24*time.Hour), 5*time.Hour)
	seedMergedPRWithApproval(t, d, 4, now.Add(-1*24*time.Hour), 7*time.Hour)

	res, _ := d.GetPRReviewCycleTime(28)
	if math.Abs(res.MedianHours-4.0) > 0.5 {
		t.Errorf("median = %.2fh, want ~4h (interpolated)", res.MedianHours)
	}
	// p75 of [1,3,5,7] → pos = 2.25 → 5 + 0.25*(7-5) = 5.5
	if math.Abs(res.P75Hours-5.5) > 0.5 {
		t.Errorf("p75 = %.2fh, want ~5.5h", res.P75Hours)
	}
}

func TestGetPRReviewCycleTime_NoApprovalsHasDataFalse(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	// 2 merged PRs but no reviews → MergedTotal=2, WithApproval=0,
	// HasData=false (solo-engineer edge case).
	seedMergedPRNoApproval(t, d, 1, now.Add(-2*24*time.Hour))
	seedMergedPRNoApproval(t, d, 2, now.Add(-1*24*time.Hour))

	res, _ := d.GetPRReviewCycleTime(28)
	if res.HasData {
		t.Error("HasData should be false when no approvals exist")
	}
	if res.MergedTotal != 2 {
		t.Errorf("MergedTotal = %d, want 2", res.MergedTotal)
	}
	if res.WithApproval != 0 {
		t.Errorf("WithApproval = %d, want 0", res.WithApproval)
	}
}

func TestGetPRReviewCycleTime_MixedApprovalCoverage(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	seedMergedPRWithApproval(t, d, 1, now.Add(-2*24*time.Hour), 6*time.Hour)
	seedMergedPRNoApproval(t, d, 2, now.Add(-1*24*time.Hour))

	res, _ := d.GetPRReviewCycleTime(28)
	if !res.HasData {
		t.Fatal("HasData=false despite 1 approved PR")
	}
	if res.MergedTotal != 2 || res.WithApproval != 1 {
		t.Errorf("counts wrong: total=%d with=%d", res.MergedTotal, res.WithApproval)
	}
	if math.Abs(res.MedianHours-6.0) > 0.5 {
		t.Errorf("median = %.2fh, want ~6h", res.MedianHours)
	}
}

func TestGetPRReviewCycleTime_WindowExcludesOldPRs(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	seedMergedPRWithApproval(t, d, 1, now.Add(-100*24*time.Hour), 2*time.Hour) // OUT of window
	seedMergedPRWithApproval(t, d, 2, now.Add(-1*24*time.Hour), 10*time.Hour)  // IN window

	res, _ := d.GetPRReviewCycleTime(28)
	if res.MergedTotal != 1 {
		t.Errorf("MergedTotal = %d, want 1 (older PR excluded)", res.MergedTotal)
	}
	if math.Abs(res.MedianHours-10.0) > 0.5 {
		t.Errorf("median = %.2fh, want ~10h", res.MedianHours)
	}
}

func TestPercentile_EdgeCases(t *testing.T) {
	if percentileFloat(nil, 0.5) != 0 {
		t.Error("nil → 0")
	}
	if percentileFloat([]float64{42}, 0.5) != 42 {
		t.Error("single elem → that elem")
	}
	if percentileFloat([]float64{1, 2, 3}, 0) != 1 {
		t.Error("p=0 → min")
	}
	if percentileFloat([]float64{1, 2, 3}, 1) != 3 {
		t.Error("p=1 → max")
	}
}
