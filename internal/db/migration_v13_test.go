package db

import "testing"

// newV12SchemaDB brings a fresh DB up to v12 by applying the full schema
// then recreating pr_events without the v13 additions/deletions columns
// and rolling schema_version back to 12. Mirrors newV11DB one step
// further along.
//
// Distinct from newV12DB in pr_events_test.go, which is misnamed — that
// helper applies the latest schema (currently v13) and is used for
// runtime CRUD tests, not migration tests.
func newV12SchemaDB(t *testing.T) *LocalDB {
	t.Helper()
	d := newEmptyLocalDB(t)
	if _, err := d.Exec(SchemaSQL); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	// SQLite has no DROP COLUMN before 3.35; recreate pr_events without
	// the v13 columns so the v12→v13 migration has work to do.
	for _, stmt := range []string{
		`DROP TABLE IF EXISTS pr_events`,
		`CREATE TABLE pr_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			repo TEXT NOT NULL,
			pr_number INTEGER NOT NULL,
			title TEXT NOT NULL,
			state TEXT NOT NULL,
			merged_at TIMESTAMP,
			closed_at TIMESTAMP,
			reopened_at TIMESTAMP,
			reverted_at TIMESTAMP,
			revert_pr_number INTEGER,
			submitted_at TIMESTAMP,
			first_approval_at TIMESTAMP,
			last_synced_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(repo, pr_number)
		)`,
	} {
		if _, err := d.Exec(stmt); err != nil {
			t.Fatalf("recreate pr_events at v12 (%s): %v", stmt, err)
		}
	}
	if _, err := d.Exec(`DELETE FROM schema_version`); err != nil {
		t.Fatalf("reset version: %v", err)
	}
	if _, err := d.Exec(`INSERT INTO schema_version (version) VALUES (12)`); err != nil {
		t.Fatalf("set version 12: %v", err)
	}
	return d
}

func TestMigrationV13_AddsAdditionsColumn(t *testing.T) {
	d := newV12SchemaDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	var n int
	_ = d.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('pr_events') WHERE name='additions'`).Scan(&n)
	if n != 1 {
		t.Errorf("additions column missing after migrate")
	}
}

func TestMigrationV13_AddsDeletionsColumn(t *testing.T) {
	d := newV12SchemaDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	var n int
	_ = d.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('pr_events') WHERE name='deletions'`).Scan(&n)
	if n != 1 {
		t.Errorf("deletions column missing after migrate")
	}
}

func TestMigrationV13_ColumnsNullable(t *testing.T) {
	d := newV12SchemaDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// Insert without additions/deletions — should succeed (NULL is the
	// "we haven't fetched detail yet" sentinel).
	if _, err := d.Exec(
		`INSERT INTO pr_events (repo, pr_number, title, state) VALUES (?, ?, ?, ?)`,
		"o/r", 1, "feat: x", "open",
	); err != nil {
		t.Fatalf("insert without size columns: %v", err)
	}
	var add, del *int
	if err := d.QueryRow(
		`SELECT additions, deletions FROM pr_events WHERE repo=? AND pr_number=?`,
		"o/r", 1,
	).Scan(&add, &del); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if add != nil || del != nil {
		t.Errorf("expected NULL additions/deletions, got %v/%v", add, del)
	}
}

func TestMigrationV13_Idempotent(t *testing.T) {
	d := newV12SchemaDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if err := d.Migrate(); err != nil {
		t.Errorf("second migrate not idempotent: %v", err)
	}
}

func TestMigrationV13_SchemaVersionBumped(t *testing.T) {
	d := newV12SchemaDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	v, err := d.getSchemaVersion()
	if err != nil {
		t.Fatalf("schema version: %v", err)
	}
	if v != 13 {
		t.Errorf("schema_version = %d, want 13", v)
	}
}
