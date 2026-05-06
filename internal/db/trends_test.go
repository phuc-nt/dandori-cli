package db

import (
	"testing"
	"time"
)

// insertTrendRun inserts a minimal run for trend tests.
func insertTrendRun(t *testing.T, d *LocalDB, id string, exitCode int, costUSD float64, when time.Time, jiraKey string) {
	t.Helper()
	_, err := d.Exec(`
		INSERT INTO runs (id, agent_name, jira_issue_key, exit_code, cost_usd, user, workstation_id, started_at, status)
		VALUES (?, 'alpha', ?, ?, ?, 'tester', 'ws1', ?, 'done')
	`, id, jiraKey, exitCode, costUSD, when.Format(time.RFC3339))
	if err != nil {
		t.Fatalf("insertTrendRun %s: %v", id, err)
	}
}

// ---------- GetTrend / success-rate ----------

func TestGetTrend_EmptyDB_SuccessRate(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	pts, err := d.GetTrend("success-rate", time.Now().AddDate(0, 0, -90), 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// All points should be gaps.
	for _, p := range pts {
		if p.HasData {
			t.Errorf("expected no data, got HasData=true for week %s", p.WeekStart)
		}
	}
}

func TestGetTrend_UnknownMetric(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	_, err := d.GetTrend("invalid-metric", time.Now().AddDate(0, 0, -28), 7)
	if err == nil {
		t.Fatal("expected error for unknown metric, got nil")
	}
}

func TestGetTrend_SuccessRate_SingleWeek(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	now := time.Now().UTC()
	// 2 success + 2 fail → 50%
	insertTrendRun(t, d, "r1", 0, 1.0, now, "FEAT-1")
	insertTrendRun(t, d, "r2", 0, 1.0, now, "FEAT-2")
	insertTrendRun(t, d, "r3", 1, 1.0, now, "FEAT-3")
	insertTrendRun(t, d, "r4", 1, 1.0, now, "FEAT-4")

	pts, err := d.GetTrend("success-rate", now.AddDate(0, 0, -7), 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Find the data point (current week).
	var found *TrendPoint
	for i := range pts {
		if pts[i].HasData {
			found = &pts[i]
			break
		}
	}
	if found == nil {
		t.Fatal("no data point found in results")
	}
	if found.RunCount != 4 {
		t.Errorf("run_count = %d, want 4", found.RunCount)
	}
	if found.Value != 50.0 {
		t.Errorf("value = %.1f, want 50.0", found.Value)
	}
}

func TestGetTrend_GapFilling(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	now := time.Now().UTC()
	// Run only in current week; all prior weeks should be gaps.
	insertTrendRun(t, d, "r1", 0, 1.0, now, "FEAT-1")

	pts, err := d.GetTrend("success-rate", now.AddDate(0, 0, -28), 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pts) == 0 {
		t.Fatal("expected at least 1 point")
	}

	dataCount := 0
	for _, p := range pts {
		if p.HasData {
			dataCount++
		}
	}
	// Only current week has data; prior weeks are gaps.
	if dataCount < 1 {
		t.Error("expected at least 1 HasData=true point")
	}
	if dataCount == len(pts) {
		t.Error("expected some gap (HasData=false) points in a 28-day window with only 1 recent run")
	}
}

// ---------- GetTrend / cost ----------

func TestGetTrend_Cost(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	now := time.Now().UTC()
	insertTrendRun(t, d, "r1", 0, 2.0, now, "FEAT-1")
	insertTrendRun(t, d, "r2", 0, 4.0, now, "FEAT-2")

	pts, err := d.GetTrend("cost", now.AddDate(0, 0, -7), 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var found *TrendPoint
	for i := range pts {
		if pts[i].HasData {
			found = &pts[i]
			break
		}
	}
	if found == nil {
		t.Fatal("no data point found")
	}
	// avg(2, 4) = 3.0
	if found.Value != 3.0 {
		t.Errorf("cost value = %.2f, want 3.0", found.Value)
	}
}

// ---------- Slope ----------

func TestSlope_ImprovingTrend(t *testing.T) {
	pts := []TrendPoint{
		{Value: 70, HasData: true},
		{Value: 75, HasData: true},
		{Value: 80, HasData: true},
		{Value: 85, HasData: true},
	}
	s := Slope(pts)
	if s <= 0 {
		t.Errorf("slope = %.2f, want > 0 for improving trend", s)
	}
}

func TestSlope_DecliningTrend(t *testing.T) {
	pts := []TrendPoint{
		{Value: 90, HasData: true},
		{Value: 80, HasData: true},
		{Value: 70, HasData: true},
	}
	s := Slope(pts)
	if s >= 0 {
		t.Errorf("slope = %.2f, want < 0 for declining trend", s)
	}
}

func TestSlope_FlatTrend(t *testing.T) {
	pts := []TrendPoint{
		{Value: 80, HasData: true},
		{Value: 80, HasData: true},
		{Value: 80, HasData: true},
	}
	s := Slope(pts)
	if s != 0 {
		t.Errorf("slope = %.2f, want 0 for flat trend", s)
	}
}

func TestSlope_SkipsGaps(t *testing.T) {
	pts := []TrendPoint{
		{Value: 70, HasData: true},
		{Value: 0, HasData: false}, // gap — must be skipped
		{Value: 80, HasData: true},
		{Value: 90, HasData: true},
	}
	s := Slope(pts)
	if s <= 0 {
		t.Errorf("slope = %.2f, want > 0 (gap should be skipped)", s)
	}
}

func TestSlope_InsufficientData(t *testing.T) {
	pts := []TrendPoint{{Value: 80, HasData: true}}
	s := Slope(pts)
	if s != 0 {
		t.Errorf("slope with 1 point = %.2f, want 0", s)
	}
}

func TestSlopeLabel_Thresholds(t *testing.T) {
	cases := []struct {
		slope  float64
		points int
		want   string
	}{
		{0.3, 5, "flat"},
		{-0.3, 5, "flat"},
		{1.5, 5, "improving 1.5 pp/week"},
		{-2.0, 5, "declining 2.0 pp/week"},
		{1.0, 2, "insufficient data"},
	}
	for _, tc := range cases {
		got := SlopeLabel(tc.slope, tc.points)
		if got != tc.want {
			t.Errorf("SlopeLabel(%.1f, %d) = %q, want %q", tc.slope, tc.points, got, tc.want)
		}
	}
}
