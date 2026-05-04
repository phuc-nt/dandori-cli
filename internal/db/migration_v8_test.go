package db

import (
	"testing"
)

// TestMigration_V8_AddsAlertsAckedAndCompositeIndexes verifies the v7→v8
// migration creates the alerts_acked table and the 3 composite indexes
// powering Dashboard v2 cross-project / cross-sprint queries.
func TestMigration_V8_AddsAlertsAckedAndCompositeIndexes(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// alerts_acked table exists and accepts the documented shape
	if _, err := d.Exec(
		`INSERT INTO alerts_acked (alert_key, acked_by) VALUES (?, ?)`,
		"orphan-cost-run-123", "phuc",
	); err != nil {
		t.Fatalf("insert alerts_acked: %v", err)
	}

	var got string
	if err := d.QueryRow(
		`SELECT alert_key FROM alerts_acked WHERE alert_key=?`,
		"orphan-cost-run-123",
	).Scan(&got); err != nil {
		t.Fatalf("scan alerts_acked: %v", err)
	}
	if got != "orphan-cost-run-123" {
		t.Errorf("alert_key = %q, want %q", got, "orphan-cost-run-123")
	}

	// Composite indexes exist
	wantIndexes := []string{
		"idx_runs_sprint_started",
		"idx_runs_dept_started",
		"idx_runs_remote_started",
	}
	for _, name := range wantIndexes {
		var found int
		if err := d.QueryRow(
			`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?`,
			name,
		).Scan(&found); err != nil {
			t.Fatalf("query index %s: %v", name, err)
		}
		if found != 1 {
			t.Errorf("index %s not found", name)
		}
	}

	// Migration is idempotent (re-running schema doesn't error)
	if _, err := d.Exec(MigrationV7ToV8); err != nil {
		t.Errorf("re-applying v7→v8 should be idempotent: %v", err)
	}
}

// TestMigration_V8_FreshInstallSchemaVersion confirms a fresh install lands
// directly at v8 (SchemaSQL contains all v8 additions).
func TestMigration_V8_FreshInstallSchemaVersion(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	v, err := d.getSchemaVersion()
	if err != nil {
		t.Fatalf("get version: %v", err)
	}
	if v != SchemaVersion {
		t.Errorf("schema version = %d, want %d", v, SchemaVersion)
	}
	if v != 8 {
		t.Errorf("schema version = %d, want 8 (Phase 01 baseline)", v)
	}

	// alerts_acked is in fresh-install SchemaSQL (not just migration path)
	var found int
	if err := d.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='alerts_acked'`,
	).Scan(&found); err != nil {
		t.Fatalf("query table: %v", err)
	}
	if found != 1 {
		t.Error("alerts_acked missing from fresh-install SchemaSQL")
	}
}
