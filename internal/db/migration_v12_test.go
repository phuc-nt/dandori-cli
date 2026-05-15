package db

import "testing"

// newV11DB brings a fresh DB up to v11 by applying the full schema then
// dropping the v12 tables and rolling schema_version back. Mirrors
// newV10DB one step further along.
func newV11DB(t *testing.T) *LocalDB {
	t.Helper()
	d := newEmptyLocalDB(t)
	if _, err := d.Exec(SchemaSQL); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	for _, stmt := range []string{
		`DROP TABLE IF EXISTS pr_events`,
		`DROP TABLE IF EXISTS sync_state`,
	} {
		if _, err := d.Exec(stmt); err != nil {
			t.Fatalf("drop v12 table (%s): %v", stmt, err)
		}
	}
	if _, err := d.Exec(`DELETE FROM schema_version`); err != nil {
		t.Fatalf("reset version: %v", err)
	}
	if _, err := d.Exec(`INSERT INTO schema_version (version) VALUES (11)`); err != nil {
		t.Fatalf("set version 11: %v", err)
	}
	return d
}

func TestMigrationV12_CreatesPREventsTable(t *testing.T) {
	d := newV11DB(t)
	var n int
	_ = d.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='pr_events'`).Scan(&n)
	if n != 0 {
		t.Fatalf("pr_events should not exist pre-migrate (got %d)", n)
	}
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	_ = d.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='pr_events'`).Scan(&n)
	if n != 1 {
		t.Errorf("pr_events table missing after migrate")
	}
}

func TestMigrationV12_CreatesSyncStateTable(t *testing.T) {
	d := newV11DB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	var n int
	_ = d.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='sync_state'`).Scan(&n)
	if n != 1 {
		t.Errorf("sync_state table missing after migrate")
	}
}

func TestMigrationV12_IndexesPresent(t *testing.T) {
	d := newV11DB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	want := []string{"idx_pr_events_merged_at", "idx_pr_events_repo_state", "idx_pr_events_title"}
	for _, idx := range want {
		var n int
		_ = d.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?`, idx).Scan(&n)
		if n != 1 {
			t.Errorf("missing index %s", idx)
		}
	}
}

func TestMigrationV12_PREventsUniqueConstraint(t *testing.T) {
	d := newV11DB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	insert := `INSERT INTO pr_events (repo, pr_number, title, state) VALUES (?, ?, ?, ?)`
	if _, err := d.Exec(insert, "o/r", 42, "feat: x", "open"); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if _, err := d.Exec(insert, "o/r", 42, "feat: x", "closed"); err == nil {
		t.Error("expected UNIQUE(repo, pr_number) violation, got nil")
	}
}

func TestMigrationV12_Idempotent(t *testing.T) {
	d := newV11DB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if err := d.Migrate(); err != nil {
		t.Errorf("second migrate not idempotent: %v", err)
	}
}

func TestMigrationV12_SchemaVersionBumped(t *testing.T) {
	d := newV11DB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	v, err := d.getSchemaVersion()
	if err != nil {
		t.Fatalf("schema version: %v", err)
	}
	// Migrate runs all incremental steps to SchemaVersion. As of v0.14 a v11
	// DB ends up at v13 because v12→v13 is the latest step. The check below
	// just confirms we got past v12; the v13-specific column assertions
	// live in migration_v13_test.go.
	if v < 12 {
		t.Errorf("schema_version = %d, want ≥ 12", v)
	}
}
