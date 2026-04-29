package metric

import (
	"errors"
	"testing"
	"time"
)

type fakeSource struct {
	total  []string
	rework []string
	err    error
}

func (f *fakeSource) TotalRunIDs(_, _ time.Time, _ string) ([]string, error) {
	return f.total, f.err
}
func (f *fakeSource) ReworkRunIDs(_, _ time.Time, _ string) ([]string, error) {
	return f.rework, f.err
}

func windowFixture() MetricWindow {
	end := time.Date(2026, 4, 29, 0, 0, 0, 0, time.UTC)
	return MetricWindow{Start: end.AddDate(0, 0, -28), End: end}
}

func ids(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "run-" + time.Now().Add(time.Duration(i)*time.Nanosecond).Format("150405.000000000")
	}
	return out
}

func TestComputeReworkRate_HappyPath(t *testing.T) {
	src := &fakeSource{total: ids(10), rework: ids(2)}
	res, err := ComputeReworkRate(src, ReworkQuery{Window: windowFixture()})
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if res.Rate != 0.2 {
		t.Errorf("rate=%v want 0.2", res.Rate)
	}
	if !res.ExceedsThreshold {
		t.Error("0.2 > 0.10 should exceed threshold")
	}
	if res.InsufficientData {
		t.Error("10 runs is enough data")
	}
	if res.ThresholdVersion != ReworkThresholdTag {
		t.Errorf("threshold version=%q want %q", res.ThresholdVersion, ReworkThresholdTag)
	}
}

func TestComputeReworkRate_KeepsCancelled(t *testing.T) {
	// 5 done + 5 cancelled = 10 total; 1 rework. Rate must be 1/10, NOT 1/5.
	src := &fakeSource{total: ids(10), rework: ids(1)}
	res, _ := ComputeReworkRate(src, ReworkQuery{Window: windowFixture()})
	if res.Rate != 0.1 {
		t.Errorf("rate=%v want 0.1 (cancelled in denominator)", res.Rate)
	}
	if res.ExceedsThreshold {
		t.Error("0.10 == threshold should NOT exceed (strict >)")
	}
}

func TestComputeReworkRate_AtThresholdBoundary(t *testing.T) {
	// 11/100 = 0.11 > 0.10
	src := &fakeSource{total: ids(100), rework: ids(11)}
	res, _ := ComputeReworkRate(src, ReworkQuery{Window: windowFixture()})
	if !res.ExceedsThreshold {
		t.Errorf("0.11 should exceed 0.10, got rate=%v", res.Rate)
	}
}

func TestComputeReworkRate_EmptyWindow(t *testing.T) {
	src := &fakeSource{total: nil, rework: nil}
	res, err := ComputeReworkRate(src, ReworkQuery{Window: windowFixture()})
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if res.Rate != 0 || !res.InsufficientData {
		t.Errorf("expected rate=0 + insufficient_data=true, got %+v", res)
	}
	if res.ExceedsThreshold {
		t.Error("zero data must not flag threshold breach")
	}
}

func TestComputeReworkRate_InvalidWindow(t *testing.T) {
	now := time.Now().UTC()
	bad := MetricWindow{Start: now, End: now.Add(-time.Hour)}
	_, err := ComputeReworkRate(&fakeSource{}, ReworkQuery{Window: bad})
	if err == nil {
		t.Fatal("expected error for inverted window")
	}
}

func TestComputeReworkRate_DBErrorPropagates(t *testing.T) {
	src := &fakeSource{err: errors.New("boom")}
	_, err := ComputeReworkRate(src, ReworkQuery{Window: windowFixture()})
	if err == nil {
		t.Fatal("expected error when db fails")
	}
}

func TestComputeReworkRate_TeamCarriedThrough(t *testing.T) {
	src := &fakeSource{total: ids(4), rework: ids(1)}
	res, _ := ComputeReworkRate(src, ReworkQuery{
		Window: windowFixture(),
		Filter: TeamFilter{Team: "payments"},
	})
	if res.Team != "payments" {
		t.Errorf("team=%q want payments", res.Team)
	}
}

func TestDefaultWindow_28Days(t *testing.T) {
	now := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)
	w := DefaultWindow(now)
	if !w.End.Equal(now) {
		t.Errorf("end=%s want %s", w.End, now)
	}
	delta := w.End.Sub(w.Start).Hours() / 24
	if delta != 28 {
		t.Errorf("window span=%v days want 28", delta)
	}
}
