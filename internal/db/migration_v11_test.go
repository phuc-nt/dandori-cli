package db

import (
	"testing"
)

// Brings a DB up to v10, then exercises the v10→v11 branch. Mirrors
// newV9DB, just a step further along.
func newV10DB(t *testing.T) *LocalDB {
	t.Helper()
	d := newEmptyLocalDB(t)
	if _, err := d.Exec(SchemaSQL); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	// Drop audit_anchors (added at v11) and roll version back to 10.
	if _, err := d.Exec(`DROP TABLE IF EXISTS audit_anchors`); err != nil {
		t.Fatalf("drop audit_anchors: %v", err)
	}
	if _, err := d.Exec(`DELETE FROM schema_version`); err != nil {
		t.Fatalf("reset version: %v", err)
	}
	if _, err := d.Exec(`INSERT INTO schema_version (version) VALUES (10)`); err != nil {
		t.Fatalf("set version 10: %v", err)
	}
	return d
}

func TestMigrationV11_CreatesAuditAnchorsTable(t *testing.T) {
	d := newV10DB(t)
	var n int
	_ = d.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='audit_anchors'`).Scan(&n)
	if n != 0 {
		t.Fatalf("audit_anchors should not exist pre-migrate (got %d)", n)
	}
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := d.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='audit_anchors'`).Scan(&n); err != nil {
		t.Fatalf("query: %v", err)
	}
	if n != 1 {
		t.Errorf("audit_anchors table missing after migrate")
	}
}

func TestMigrationV11_IndexesPresent(t *testing.T) {
	d := newV10DB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	want := []string{"idx_audit_anchors_anchored", "idx_audit_anchors_last_id"}
	for _, idx := range want {
		var n int
		_ = d.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?`, idx).Scan(&n)
		if n != 1 {
			t.Errorf("missing index %s", idx)
		}
	}
}

func TestMigrationV11_InsertAuditAnchorUniqueConstraint(t *testing.T) {
	d := newV10DB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := d.InsertAuditAnchor(42, "deadbeef", "PAGE-1", 1, "anchored"); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	// Re-anchor same tip — INSERT OR IGNORE means no error, no second row.
	if _, err := d.InsertAuditAnchor(42, "deadbeef", "PAGE-1", 2, "anchored"); err != nil {
		t.Fatalf("dup insert: %v", err)
	}
	var n int
	_ = d.QueryRow(`SELECT COUNT(*) FROM audit_anchors WHERE last_audit_id = 42`).Scan(&n)
	if n != 1 {
		t.Errorf("expected 1 row after dup insert, got %d", n)
	}
}

func TestMigrationV11_Idempotent(t *testing.T) {
	d := newV10DB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if err := d.Migrate(); err != nil {
		t.Errorf("second migrate not idempotent: %v", err)
	}
}
