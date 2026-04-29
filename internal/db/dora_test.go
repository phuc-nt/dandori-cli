package db

import (
	"testing"
	"time"
)

func TestMigration_V4_CreatesMetricSnapshots(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var version int
	if err := d.QueryRow(`SELECT MAX(version) FROM schema_version`).Scan(&version); err != nil {
		t.Fatalf("read schema_version: %v", err)
	}
	if version != SchemaVersion {
		t.Fatalf("schema_version=%d want %d", version, SchemaVersion)
	}

	var n int
	err := d.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='metric_snapshots'`).Scan(&n)
	if err != nil {
		t.Fatalf("query master: %v", err)
	}
	if n != 1 {
		t.Fatal("metric_snapshots table missing after migrate")
	}

	if err := d.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_events_type_run'`).Scan(&n); err != nil {
		t.Fatalf("query idx: %v", err)
	}
	if n != 1 {
		t.Fatal("idx_events_type_run missing")
	}
}

func TestMetricSnapshot_RoundTrip(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 28)

	snap := MetricSnapshot{
		ID:          "01HZZTEAM",
		Team:        "payments",
		Format:      "faros",
		WindowStart: start,
		WindowEnd:   end,
		Payload:     `{"deployment_frequency":3.5}`,
	}
	if err := d.InsertSnapshot(snap); err != nil {
		t.Fatalf("insert: %v", err)
	}

	got, err := d.LatestSnapshot("payments", "faros")
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if got == nil {
		t.Fatal("latest returned nil")
	}
	if got.ID != snap.ID || got.Team != "payments" || got.Payload != snap.Payload {
		t.Errorf("snapshot mismatch: got %+v", got)
	}
	if !got.WindowStart.Equal(start) || !got.WindowEnd.Equal(end) {
		t.Errorf("window mismatch: start=%s end=%s", got.WindowStart, got.WindowEnd)
	}
}

func TestMetricSnapshot_LatestPicksMostRecent(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	base := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	for i, id := range []string{"old", "newer", "newest"} {
		s := MetricSnapshot{
			ID:          id,
			Team:        "infra",
			Format:      "raw",
			WindowStart: base,
			WindowEnd:   base.AddDate(0, 0, 28),
			Payload:     `{}`,
		}
		if err := d.InsertSnapshot(s); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
		// SQLite default created_at has 1-second resolution; sleep to ensure ordering.
		time.Sleep(1100 * time.Millisecond)
	}

	got, err := d.LatestSnapshot("infra", "raw")
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if got == nil || got.ID != "newest" {
		t.Errorf("expected newest, got %+v", got)
	}
}

func TestMetricSnapshot_NoTeamFilter(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	now := time.Now().UTC()
	if err := d.InsertSnapshot(MetricSnapshot{
		ID:          "global",
		Team:        "",
		Format:      "raw",
		WindowStart: now.Add(-24 * time.Hour),
		WindowEnd:   now,
		Payload:     `{}`,
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	got, err := d.LatestSnapshot("", "raw")
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if got == nil || got.ID != "global" {
		t.Errorf("expected global snapshot, got %+v", got)
	}

	miss, err := d.LatestSnapshot("payments", "raw")
	if err != nil {
		t.Fatalf("latest miss: %v", err)
	}
	if miss != nil {
		t.Errorf("expected nil for unmatched team, got %+v", miss)
	}
}

func TestMetricSnapshot_ListFilter(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	now := time.Now().UTC()
	for i, team := range []string{"a", "b", "a"} {
		if err := d.InsertSnapshot(MetricSnapshot{
			ID:          "s" + string(rune('0'+i)),
			Team:        team,
			Format:      "faros",
			WindowStart: now.Add(-24 * time.Hour),
			WindowEnd:   now,
			Payload:     `{}`,
		}); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	teamA, err := d.ListSnapshots("a", "faros", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(teamA) != 2 {
		t.Errorf("team a count=%d want 2", len(teamA))
	}

	all, err := d.ListSnapshots("", "", 10)
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("all count=%d want 3", len(all))
	}
}
