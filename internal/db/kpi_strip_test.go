package db

import (
	"testing"
	"time"
)

func TestGetKPIDailyStats_PadsEmptyDays(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	got, err := d.GetKPIDailyStats(14)
	if err != nil {
		t.Fatalf("GetKPIDailyStats: %v", err)
	}
	if len(got) != 14 {
		t.Errorf("length = %d, want 14 (padded)", len(got))
	}
	for _, row := range got {
		if row.Runs != 0 || row.Cost != 0 || row.Tokens != 0 {
			t.Errorf("empty DB should produce zero rows, got %+v", row)
		}
		if row.Day == "" {
			t.Error("Day must be set for every row")
		}
	}

	// Last row is today (UTC), first row is 13 days ago.
	wantLast := time.Now().UTC().Format("2006-01-02")
	if got[len(got)-1].Day != wantLast {
		t.Errorf("last day = %q, want %q", got[len(got)-1].Day, wantLast)
	}
}

func TestGetKPIDailyStats_AggregatesRunsByDay(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	now := time.Now().UTC()
	insertRun := func(id string, when time.Time, cost float64, in, out int) {
		_, err := d.Exec(`
			INSERT INTO runs (id, user, workstation_id, started_at, status, cost_usd, input_tokens, output_tokens)
			VALUES (?, 'phuc', 'ws1', ?, 'success', ?, ?, ?)
		`, id, when.Format(time.RFC3339), cost, in, out)
		if err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}

	// Today + yesterday + 5 days ago.
	insertRun("r1", now, 1.0, 100, 50)
	insertRun("r2", now, 2.5, 200, 100)
	insertRun("r3", now.AddDate(0, 0, -1), 0.5, 50, 25)
	insertRun("r4", now.AddDate(0, 0, -5), 4.0, 400, 200)

	got, err := d.GetKPIDailyStats(14)
	if err != nil {
		t.Fatalf("GetKPIDailyStats: %v", err)
	}
	if len(got) != 14 {
		t.Fatalf("length = %d, want 14", len(got))
	}

	today := now.Format("2006-01-02")
	yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")
	fiveDaysAgo := now.AddDate(0, 0, -5).Format("2006-01-02")

	byDay := map[string]KPIDay{}
	for _, row := range got {
		byDay[row.Day] = row
	}

	if byDay[today].Runs != 2 || byDay[today].Cost != 3.5 || byDay[today].Tokens != 450 {
		t.Errorf("today = %+v, want runs=2 cost=3.5 tokens=450", byDay[today])
	}
	if byDay[yesterday].Runs != 1 || byDay[yesterday].Cost != 0.5 {
		t.Errorf("yesterday = %+v, want runs=1 cost=0.5", byDay[yesterday])
	}
	if byDay[fiveDaysAgo].Runs != 1 || byDay[fiveDaysAgo].Cost != 4.0 {
		t.Errorf("d-5 = %+v, want runs=1 cost=4.0", byDay[fiveDaysAgo])
	}
}
