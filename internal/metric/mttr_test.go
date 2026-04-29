package metric

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/phuc-nt/dandori-cli/internal/jira"
)

type fakeIncidentSource struct {
	incidents []jira.Incident
	err       error
}

func (f *fakeIncidentSource) SearchIncidents(_ string, _ int) ([]jira.Incident, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.incidents, nil
}

func resolved(t time.Time) *time.Time { return &t }

func TestComputeMTTR_Happy(t *testing.T) {
	w := defaultWindow()
	created := w.Start.Add(24 * time.Hour)
	src := &fakeIncidentSource{
		incidents: []jira.Incident{
			{Key: "I-1", CreatedAt: created, ResolvedAt: resolved(created.Add(1 * time.Hour))},
			{Key: "I-2", CreatedAt: created, ResolvedAt: resolved(created.Add(2 * time.Hour))},
			{Key: "I-3", CreatedAt: created, ResolvedAt: resolved(created.Add(4 * time.Hour))},
			{Key: "I-4", CreatedAt: created, ResolvedAt: resolved(created.Add(8 * time.Hour))},
		},
	}
	got, err := ComputeTimeToRestore(src, defaultIncidentQuery(w))
	if err != nil {
		t.Fatal(err)
	}
	if got.SamplesUsed != 4 {
		t.Errorf("samples=%d want 4", got.SamplesUsed)
	}
	// durations: 1h, 2h, 4h, 8h → p50 = (2+4)/2 = 3h = 10800s (linear interp)
	if math.Abs(got.P50Seconds-10800) > 1 {
		t.Errorf("p50=%v want 10800", got.P50Seconds)
	}
}

func TestComputeMTTR_OngoingExcluded(t *testing.T) {
	w := defaultWindow()
	created := w.Start.Add(24 * time.Hour)
	src := &fakeIncidentSource{
		incidents: []jira.Incident{
			{Key: "I-resolved", CreatedAt: created, ResolvedAt: resolved(created.Add(time.Hour))},
			{Key: "I-ongoing-1", CreatedAt: created},
			{Key: "I-ongoing-2", CreatedAt: created},
		},
	}
	got, _ := ComputeTimeToRestore(src, defaultIncidentQuery(w))
	if got.SamplesUsed != 1 {
		t.Errorf("samples=%d want 1", got.SamplesUsed)
	}
	if got.OngoingIncidents != 2 {
		t.Errorf("ongoing=%d want 2", got.OngoingIncidents)
	}
}

func TestComputeMTTR_BadDataSkipped(t *testing.T) {
	w := defaultWindow()
	created := w.Start.Add(24 * time.Hour)
	src := &fakeIncidentSource{
		incidents: []jira.Incident{
			{Key: "I-good", CreatedAt: created, ResolvedAt: resolved(created.Add(time.Hour))},
			{Key: "I-bad", CreatedAt: created, ResolvedAt: resolved(created.Add(-time.Hour))},
		},
	}
	got, _ := ComputeTimeToRestore(src, defaultIncidentQuery(w))
	if got.SamplesUsed != 1 || got.BadDataSkipped != 1 {
		t.Errorf("got samples=%d bad=%d want 1/1", got.SamplesUsed, got.BadDataSkipped)
	}
}

func TestComputeMTTR_InsufficientData(t *testing.T) {
	w := defaultWindow()
	src := &fakeIncidentSource{incidents: nil}
	got, _ := ComputeTimeToRestore(src, defaultIncidentQuery(w))
	if !got.InsufficientData {
		t.Error("want insufficient_data=true")
	}
}

func TestComputeMTTR_EmptyMatchConfigErrors(t *testing.T) {
	w := defaultWindow()
	bad := IncidentQuery{Window: w}
	_, err := ComputeTimeToRestore(&fakeIncidentSource{}, bad)
	if err == nil {
		t.Error("want err on empty match config")
	}
}

func TestComputeMTTR_FetchError(t *testing.T) {
	w := defaultWindow()
	src := &fakeIncidentSource{err: errors.New("boom")}
	_, err := ComputeTimeToRestore(src, defaultIncidentQuery(w))
	if err == nil {
		t.Error("want err propagated")
	}
}
