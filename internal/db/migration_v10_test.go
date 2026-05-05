package db

import (
	"testing"
)

// Brings a DB up to v9, then verifies the v9→v10 branch creates the
// buglinks table. Mirrors newV8DBWithLegacyAttribution but stops at v9.
func newV9DB(t *testing.T) *LocalDB {
	t.Helper()
	d := newEmptyLocalDB(t)
	if _, err := d.Exec(SchemaSQL); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	// Drop buglinks (added at v10) and roll schema_version back to 9.
	if _, err := d.Exec(`DROP TABLE IF EXISTS buglinks`); err != nil {
		t.Fatalf("drop buglinks: %v", err)
	}
	if _, err := d.Exec(`DELETE FROM schema_version`); err != nil {
		t.Fatalf("reset version: %v", err)
	}
	if _, err := d.Exec(`INSERT INTO schema_version (version) VALUES (9)`); err != nil {
		t.Fatalf("set version 9: %v", err)
	}
	return d
}

func TestMigrationV10_CreatesBuglinksTable(t *testing.T) {
	d := newV9DB(t)
	// Sanity: pre-migrate the table really doesn't exist.
	var n int
	_ = d.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='buglinks'`).Scan(&n)
	if n != 0 {
		t.Fatalf("buglinks should not exist pre-migrate (got %d)", n)
	}

	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if err := d.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='buglinks'`).Scan(&n); err != nil {
		t.Fatalf("query: %v", err)
	}
	if n != 1 {
		t.Errorf("buglinks table missing after migrate")
	}
}

func TestMigrationV10_IndexesPresent(t *testing.T) {
	d := newV9DB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	want := []string{"idx_buglinks_run", "idx_buglinks_linked", "idx_buglinks_bug"}
	for _, name := range want {
		var got int
		if err := d.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?`, name).Scan(&got); err != nil {
			t.Fatalf("query %s: %v", name, err)
		}
		if got != 1 {
			t.Errorf("index %s missing", name)
		}
	}
}

func TestMigrationV10_InsertBuglinkUniqueConstraint(t *testing.T) {
	d := newV9DB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// Need a runs row to satisfy the FK.
	if _, err := d.Exec(`
		INSERT INTO runs (id, user, workstation_id, started_at, status)
		VALUES ('run-x', 'u', 'ws', datetime('now'), 'done')
	`); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	if _, err := d.InsertBuglink("BUG-1", "run-x", "first", "test"); err != nil {
		t.Fatalf("insert 1: %v", err)
	}
	// Second insert with same (bug_key, run_id) is silently ignored.
	if _, err := d.InsertBuglink("BUG-1", "run-x", "second", "test"); err != nil {
		t.Fatalf("insert 2: %v", err)
	}
	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM buglinks WHERE jira_bug_key='BUG-1'`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("idempotent insert produced %d rows, want 1", n)
	}
}

func TestMigrationV10_Idempotent(t *testing.T) {
	d := newV9DB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate first: %v", err)
	}
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate second (no-op): %v", err)
	}
}
