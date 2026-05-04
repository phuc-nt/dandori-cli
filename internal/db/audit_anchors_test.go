package db

import (
	"strings"
	"testing"
	"time"
)

// freshAuditDB returns a fully-migrated DB with three audit_log rows so
// the chain has something for the anchor tests to bite into.
func freshAuditDB(t *testing.T) *LocalDB {
	t.Helper()
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for i := 0; i < 3; i++ {
		ts := time.Date(2026, 5, 1, 12, i, 0, 0, time.UTC).Format(time.RFC3339)
		if err := d.AppendAuditEntry("alice", "create", "run", "r"+string(rune('A'+i)), "details", ts); err != nil {
			t.Fatalf("append entry %d: %v", i, err)
		}
	}
	return d
}

func TestLatestAuditTip_EmptyDB(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	id, hash, err := d.LatestAuditTip()
	if err != nil {
		t.Fatalf("LatestAuditTip: %v", err)
	}
	if id != 0 || hash != "" {
		t.Errorf("expected (0,\"\") for empty audit_log, got (%d,%q)", id, hash)
	}
}

func TestLatestAuditTip_ReturnsLastRow(t *testing.T) {
	d := freshAuditDB(t)
	id, hash, err := d.LatestAuditTip()
	if err != nil {
		t.Fatalf("LatestAuditTip: %v", err)
	}
	if id != 3 {
		t.Errorf("want id=3, got %d", id)
	}
	if hash == "" {
		t.Errorf("want non-empty hash")
	}
}

func TestVerifyChainWithAnchors_ValidWhenChainAndAnchorsAgree(t *testing.T) {
	d := freshAuditDB(t)
	id, hash, _ := d.LatestAuditTip()
	if _, err := d.InsertAuditAnchor(id, hash, "", 0, "local-only"); err != nil {
		t.Fatalf("insert anchor: %v", err)
	}
	res, err := d.VerifyAuditChainWithAnchors(0)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !res.Valid {
		t.Errorf("expected valid, got reason=%q", res.Reason)
	}
}

func TestVerifyChainWithAnchors_FailsWhenTipHashRewritten(t *testing.T) {
	d := freshAuditDB(t)
	id, hash, _ := d.LatestAuditTip()
	if _, err := d.InsertAuditAnchor(id, hash, "", 0, "local-only"); err != nil {
		t.Fatalf("insert anchor: %v", err)
	}
	// Simulate tampering: rewrite the tip's curr_hash directly. Internal
	// verify will still fail (it recomputes), so we have to also fudge
	// the recomputable inputs to make the chain *look* internally valid
	// — easiest path is to overwrite curr_hash AND the anchor's
	// last_curr_hash differently. The internal chain check then passes
	// (only if we recompute properly), so for this test we bypass the
	// internal check by making the chain self-consistent but the anchor
	// stale. We do that by inserting a *new* audit_log row, then
	// pretending the anchor referred to that new id with a stale hash.
	if err := d.AppendAuditEntry("alice", "create", "run", "rD", "details",
		time.Date(2026, 5, 1, 12, 9, 0, 0, time.UTC).Format(time.RFC3339)); err != nil {
		t.Fatalf("append: %v", err)
	}
	newID, _, _ := d.LatestAuditTip()
	if _, err := d.Exec(
		`INSERT INTO audit_anchors (last_audit_id, last_curr_hash, status) VALUES (?, ?, 'local-only')`,
		newID, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"); err != nil {
		t.Fatalf("insert stale anchor: %v", err)
	}
	res, err := d.VerifyAuditChainWithAnchors(0)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if res.Valid {
		t.Errorf("expected INVALID with stale anchor")
	}
	if !strings.Contains(res.Reason, "anchor mismatch") {
		t.Errorf("expected reason to mention anchor mismatch, got %q", res.Reason)
	}
}

func TestVerifyChainWithAnchors_FailsWhenAnchoredRowMissing(t *testing.T) {
	d := freshAuditDB(t)
	// Anchor a non-existent id directly via raw insert (bypasses LatestAuditTip).
	if _, err := d.Exec(
		`INSERT INTO audit_anchors (last_audit_id, last_curr_hash, status) VALUES (9999, 'cafebabe', 'local-only')`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	res, err := d.VerifyAuditChainWithAnchors(0)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if res.Valid {
		t.Errorf("expected INVALID, anchored row missing from chain")
	}
	if !strings.Contains(res.Reason, "missing") {
		t.Errorf("reason should mention missing row, got %q", res.Reason)
	}
}

func TestInsertAuditAnchor_RecordsConfluenceMetadata(t *testing.T) {
	d := freshAuditDB(t)
	id, hash, _ := d.LatestAuditTip()
	if _, err := d.InsertAuditAnchor(id, hash, "PAGE-99", 7, "anchored"); err != nil {
		t.Fatalf("insert: %v", err)
	}
	a, err := d.LatestAuditAnchor()
	if err != nil || a == nil {
		t.Fatalf("LatestAuditAnchor: %v anchor=%v", err, a)
	}
	if a.ConfluencePageID != "PAGE-99" || a.ConfluenceVersion != 7 || a.Status != "anchored" {
		t.Errorf("metadata not stored, got %+v", a)
	}
}
