package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/phuc-nt/dandori-cli/internal/db"
	"github.com/phuc-nt/dandori-cli/internal/server"
)

func newAuditMux(t *testing.T) (*http.ServeMux, *db.LocalDB) {
	t.Helper()
	tmp := t.TempDir()
	store, err := db.Open(filepath.Join(tmp, "audit.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := store.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	mux := http.NewServeMux()
	server.RegisterAuditRoutes(mux, store)
	return mux, store
}

func seedAuditEvent(t *testing.T, d *db.LocalDB, runID, eventType string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	_, _ = d.Exec(`INSERT INTO runs (id, jira_issue_key, agent_name, agent_type, user, workstation_id, cwd, started_at, status)
		VALUES (?, 'P-1', 'a', 'cc', 'u', 'ws-1', '/tmp', ?, 'done') ON CONFLICT(id) DO NOTHING`, runID, now)
	if _, err := d.Exec(`INSERT INTO events (run_id, layer, event_type, data, ts) VALUES (?, 1, ?, '{}', ?)`,
		runID, eventType, now); err != nil {
		t.Fatalf("seed event: %v", err)
	}
}

func TestAudit_EventStream_FilterByRunAndType(t *testing.T) {
	mux, store := newAuditMux(t)
	seedAuditEvent(t, store, "r1", "approval.granted")
	seedAuditEvent(t, store, "r1", "tool.use")
	seedAuditEvent(t, store, "r2", "approval.denied")

	w := doGET(t, mux, "/api/events?run=r1")
	if w.Code != 200 {
		t.Fatalf("status=%d", w.Code)
	}
	var got []db.EventStreamRow
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if len(got) != 2 {
		t.Errorf("filter run=r1: want 2, got %d", len(got))
	}

	w2 := doGET(t, mux, "/api/events?type=approval")
	var got2 []db.EventStreamRow
	_ = json.Unmarshal(w2.Body.Bytes(), &got2)
	if len(got2) != 2 {
		t.Errorf("filter type=approval: want 2, got %d", len(got2))
	}
}

func TestAudit_AuditLog_Empty_ReturnsArray(t *testing.T) {
	mux, _ := newAuditMux(t)
	w := doGET(t, mux, "/api/audit-log")
	if w.Code != 200 {
		t.Fatalf("status=%d", w.Code)
	}
	if w.Body.String() == "null\n" {
		t.Errorf("want [], got null")
	}
}

func TestAudit_VerifyChain_ValidThenTampered(t *testing.T) {
	mux, store := newAuditMux(t)
	now := time.Now().UTC().Format(time.RFC3339)
	if err := store.AppendAuditEntry("alice", "create", "task", "P-1", "{}", now); err != nil {
		t.Fatalf("append1: %v", err)
	}
	if err := store.AppendAuditEntry("bob", "approve", "task", "P-1", "{}", now); err != nil {
		t.Fatalf("append2: %v", err)
	}

	// Verify clean chain.
	req, _ := http.NewRequest("POST", "/api/audit-log/verify", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var ok db.AuditVerifyResult
	if err := json.Unmarshal(w.Body.Bytes(), &ok); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !ok.Valid || ok.Entries != 2 {
		t.Errorf("clean chain: want valid=true entries=2, got %+v", ok)
	}

	// Tamper.
	if _, err := store.Exec(`UPDATE audit_log SET details='HACKED' WHERE id = 2`); err != nil {
		t.Fatalf("tamper: %v", err)
	}

	req2, _ := http.NewRequest("POST", "/api/audit-log/verify", nil)
	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, req2)
	var bad db.AuditVerifyResult
	_ = json.Unmarshal(w2.Body.Bytes(), &bad)
	if bad.Valid {
		t.Errorf("tampered chain: want valid=false, got valid=true (%+v)", bad)
	}
	if bad.BrokenAt != 2 {
		t.Errorf("want broken_at=2, got %d", bad.BrokenAt)
	}
}
