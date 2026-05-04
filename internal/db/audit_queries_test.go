package db

import (
	"testing"
	"time"
)

func seedEvent(t *testing.T, d *LocalDB, runID, eventType, data string, ts time.Time) {
	t.Helper()
	_, err := d.Exec(`
		INSERT INTO runs (id, jira_issue_key, agent_name, agent_type, user, workstation_id, cwd, started_at, status)
		VALUES (?, 'P-1', 'a', 'cc', 'u', 'ws-1', '/tmp', ?, 'done')
		ON CONFLICT(id) DO NOTHING
	`, runID, ts.Format(time.RFC3339))
	if err != nil {
		t.Fatalf("seed run: %v", err)
	}
	_, err = d.Exec(`
		INSERT INTO events (run_id, layer, event_type, data, ts) VALUES (?, 1, ?, ?, ?)
	`, runID, eventType, data, ts.Format(time.RFC3339))
	if err != nil {
		t.Fatalf("seed event: %v", err)
	}
}

func TestEventStream_FiltersByRunAndType(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now().UTC()
	seedEvent(t, d, "r1", "approval.granted", `{"by":"alice"}`, now)
	seedEvent(t, d, "r1", "approval.requested", `{"tool":"bash"}`, now.Add(time.Second))
	seedEvent(t, d, "r2", "tool.use", `{"name":"Read"}`, now)

	all, err := d.EventStream("", "", 50, 0)
	if err != nil {
		t.Fatalf("EventStream: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("want 3 events, got %d", len(all))
	}
	r1, _ := d.EventStream("r1", "", 50, 0)
	if len(r1) != 2 {
		t.Errorf("filter run=r1: want 2, got %d", len(r1))
	}
	approvals, _ := d.EventStream("", "approval", 50, 0)
	if len(approvals) != 2 {
		t.Errorf("filter type=approval: want 2, got %d", len(approvals))
	}
}

func TestVerifyAuditChain_ValidChain(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if err := d.AppendAuditEntry("alice", "create", "task", "P-1", "{}", now); err != nil {
		t.Fatalf("append1: %v", err)
	}
	if err := d.AppendAuditEntry("bob", "approve", "task", "P-1", `{"reason":"ok"}`, now); err != nil {
		t.Fatalf("append2: %v", err)
	}
	res, err := d.VerifyAuditChain(0)
	if err != nil {
		t.Fatalf("VerifyAuditChain: %v", err)
	}
	if !res.Valid || res.Entries != 2 {
		t.Errorf("want valid=true entries=2, got %+v", res)
	}
}

func TestVerifyAuditChain_DetectsTampering(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_ = d.AppendAuditEntry("alice", "create", "task", "P-1", "{}", now)
	_ = d.AppendAuditEntry("bob", "approve", "task", "P-1", `{"reason":"ok"}`, now)

	// Tamper: rewrite details on entry 2.
	if _, err := d.Exec(`UPDATE audit_log SET details='{"reason":"HACKED"}' WHERE id = 2`); err != nil {
		t.Fatalf("tamper: %v", err)
	}

	res, err := d.VerifyAuditChain(0)
	if err != nil {
		t.Fatalf("VerifyAuditChain: %v", err)
	}
	if res.Valid {
		t.Errorf("want valid=false after tampering, got valid=true (%+v)", res)
	}
	if res.BrokenAt != 2 {
		t.Errorf("want broken_at=2, got %d", res.BrokenAt)
	}
}

func TestAuditLog_FilterByEntity(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_ = d.AppendAuditEntry("a", "create", "task", "P-1", "{}", now)
	_ = d.AppendAuditEntry("b", "create", "user", "u-1", "{}", now)

	all, err := d.AuditLog("", "", "", 100, 0)
	if err != nil {
		t.Fatalf("AuditLog: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("want 2, got %d", len(all))
	}
	tasks, _ := d.AuditLog("task", "", "", 100, 0)
	if len(tasks) != 1 {
		t.Errorf("entity=task: want 1, got %d", len(tasks))
	}
}
