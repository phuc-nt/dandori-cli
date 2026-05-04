package db

import (
	"testing"
)

func TestAlertsAck_AckAndQuery(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	key := ComputeAlertKey("cost_multiple", "claude-code: cost 4× baseline")

	acked, err := d.IsAlertAcked(key)
	if err != nil {
		t.Fatalf("IsAlertAcked: %v", err)
	}
	if acked {
		t.Fatal("fresh key should not be acked")
	}

	if err := d.AckAlert(key, "phuc"); err != nil {
		t.Fatalf("AckAlert: %v", err)
	}

	acked, err = d.IsAlertAcked(key)
	if err != nil {
		t.Fatalf("IsAlertAcked after ack: %v", err)
	}
	if !acked {
		t.Fatal("key should be acked after AckAlert")
	}

	keys, err := d.AckedAlertKeys()
	if err != nil {
		t.Fatalf("AckedAlertKeys: %v", err)
	}
	if !keys[key] {
		t.Errorf("AckedAlertKeys missing %q, got %v", key, keys)
	}
}

func TestAlertsAck_Idempotent(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	key := ComputeAlertKey("ac_dip", "phuc: AC completion below baseline")

	if err := d.AckAlert(key, "u1"); err != nil {
		t.Fatalf("first ack: %v", err)
	}
	if err := d.AckAlert(key, "u2"); err != nil {
		t.Fatalf("second ack should be idempotent: %v", err)
	}

	var n int
	if err := d.QueryRow(
		`SELECT COUNT(*) FROM alerts_acked WHERE alert_key=?`, key,
	).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 row after re-ack, got %d", n)
	}
}

func TestAlertsAck_ExpiredFiltered(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	key := ComputeAlertKey("cost_multiple", "expired-test")
	if _, err := d.Exec(
		`INSERT INTO alerts_acked (alert_key, acked_by, expires_at)
		 VALUES (?, ?, datetime('now', '-1 hour'))`,
		key, "phuc",
	); err != nil {
		t.Fatalf("insert expired: %v", err)
	}

	acked, err := d.IsAlertAcked(key)
	if err != nil {
		t.Fatalf("IsAlertAcked: %v", err)
	}
	if acked {
		t.Error("expired ack should not count as acked")
	}

	keys, err := d.AckedAlertKeys()
	if err != nil {
		t.Fatalf("AckedAlertKeys: %v", err)
	}
	if keys[key] {
		t.Error("expired ack should not appear in AckedAlertKeys")
	}
}

func TestComputeAlertKey_Stable(t *testing.T) {
	a := ComputeAlertKey("cost_multiple", "x: cost 3× baseline")
	b := ComputeAlertKey("cost_multiple", "x: cost 3× baseline")
	if a != b {
		t.Errorf("ComputeAlertKey not deterministic: %q vs %q", a, b)
	}
	if len(a) != 12 {
		t.Errorf("key length = %d, want 12", len(a))
	}

	c := ComputeAlertKey("ac_dip", "x: cost 3× baseline")
	if a == c {
		t.Error("different kind should produce different key")
	}
}
