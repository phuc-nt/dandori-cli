package cmd

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/phuc-nt/dandori-cli/internal/db"
	"github.com/phuc-nt/dandori-cli/internal/metric"
)

// TestCacheMetricSnapshot_StoresFarosShapeWithJSONFormat verifies that
// `dandori metric export` populates metric_snapshots with format="json"
// and a faros-shaped payload — the form the G9 DORA handler reads.
func TestCacheMetricSnapshot_StoresFarosShapeWithJSONFormat(t *testing.T) {
	store := openTestDB(t)
	defer store.Close()

	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 4, 28, 0, 0, 0, 0, time.UTC)
	rep := metric.ExportReport{
		GeneratedAt: time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC),
		Config: metric.ExportConfig{
			Window: metric.MetricWindow{Start: start, End: end},
			Team:   "platform",
		},
		Deploy:   metric.DeployFreqResult{PerDay: 4.2, Count: 117},
		LeadTime: metric.LeadTimeResult{P50Seconds: 64800, SamplesUsed: 90},
	}
	cfg := rep.Config

	if err := cacheMetricSnapshot(store, rep, cfg); err != nil {
		t.Fatalf("cacheMetricSnapshot: %v", err)
	}

	snap, err := store.LatestSnapshot("platform", "json")
	if err != nil {
		t.Fatalf("LatestSnapshot: %v", err)
	}
	if snap == nil {
		t.Fatal("expected snapshot, got nil — DORA panel will be empty")
	}
	if snap.Format != "json" {
		t.Errorf("Format = %q; want \"json\" (handler queries on this)", snap.Format)
	}

	// Payload must be valid JSON in faros shape so normalizeDoraPayload picks
	// up deployment_frequency / lead_time_for_changes.
	var raw map[string]any
	if err := json.Unmarshal([]byte(snap.Payload), &raw); err != nil {
		t.Fatalf("payload not JSON: %v", err)
	}
	metrics, ok := raw["metrics"].(map[string]any)
	if !ok {
		t.Fatalf("payload missing metrics key; got: %s", snap.Payload[:min(200, len(snap.Payload))])
	}
	if _, ok := metrics["deployment_frequency"]; !ok {
		t.Error("payload.metrics missing deployment_frequency (faros shape expected)")
	}
	if _, ok := metrics["lead_time_for_changes"]; !ok {
		t.Error("payload.metrics missing lead_time_for_changes")
	}
}

// TestCacheMetricSnapshot_NoTeam_StoresEmptyTeam verifies the empty-team path.
// The G9 handler queries LatestSnapshot("", "json") for the org-wide view.
func TestCacheMetricSnapshot_NoTeam_StoresEmptyTeam(t *testing.T) {
	store := openTestDB(t)
	defer store.Close()

	rep := metric.ExportReport{
		Config: metric.ExportConfig{
			Window: metric.MetricWindow{
				Start: time.Now().Add(-28 * 24 * time.Hour),
				End:   time.Now(),
			},
		},
	}
	if err := cacheMetricSnapshot(store, rep, rep.Config); err != nil {
		t.Fatalf("cacheMetricSnapshot: %v", err)
	}

	snap, err := store.LatestSnapshot("", "json")
	if err != nil {
		t.Fatalf("LatestSnapshot: %v", err)
	}
	if snap == nil {
		t.Fatal("expected snapshot for empty team, got nil")
	}
	if snap.Team != "" {
		t.Errorf("Team = %q; want empty", snap.Team)
	}
}

// TestNewSnapshotID_Unique verifies IDs do not collide across rapid calls
// (used as primary key in metric_snapshots).
func TestNewSnapshotID_Unique(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 50; i++ {
		id := newSnapshotID()
		if !strings.HasPrefix(id, "snap-") {
			t.Errorf("id %q missing snap- prefix", id)
		}
		if seen[id] {
			t.Errorf("duplicate id %q", id)
		}
		seen[id] = true
	}
}

func openTestDB(t *testing.T) *db.LocalDB {
	t.Helper()
	dir := t.TempDir()
	store, err := db.Open(dir + "/test.db")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	if err := store.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return store
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
