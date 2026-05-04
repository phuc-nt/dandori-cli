// Package db — alerts_ack.go: persistence for the dashboard Alert Center.
//
// Alerts themselves are recomputed every request from analytics (see
// internal/analytics/alerts.go). We only persist *which alert keys the user
// has dismissed* so the same alert doesn't keep reappearing after a refresh.
//
// AlertKey is a deterministic 12-char hash of (kind + message). It is computed
// at the API boundary (server/alerts.go), not stored on Alert itself, so the
// existing analytics.Alert struct stays unchanged.
package db

import (
	"crypto/sha256"
	"encoding/hex"
)

// AckedAlert is a row from alerts_acked.
type AckedAlert struct {
	AlertKey  string
	AckedBy   string
	AckedAt   string
	ExpiresAt string
}

// AckAlert records that AlertKey has been dismissed.
// Idempotent: re-acking the same key updates acked_by/acked_at.
func (l *LocalDB) AckAlert(alertKey, ackedBy string) error {
	_, err := l.Exec(`
		INSERT INTO alerts_acked (alert_key, acked_by) VALUES (?, ?)
		ON CONFLICT(alert_key) DO UPDATE SET
			acked_by = excluded.acked_by,
			acked_at = datetime('now')
	`, alertKey, ackedBy)
	return err
}

// IsAlertAcked returns true if alertKey is in alerts_acked and not expired.
func (l *LocalDB) IsAlertAcked(alertKey string) (bool, error) {
	var n int
	err := l.QueryRow(`
		SELECT COUNT(*) FROM alerts_acked
		WHERE alert_key = ?
		  AND (expires_at IS NULL OR expires_at > datetime('now'))
	`, alertKey).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// AckedAlertKeys returns the set of currently-acked, non-expired alert keys.
// Used to filter the live alert list before returning to the dashboard.
func (l *LocalDB) AckedAlertKeys() (map[string]bool, error) {
	rows, err := l.Query(`
		SELECT alert_key FROM alerts_acked
		WHERE expires_at IS NULL OR expires_at > datetime('now')
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]bool{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out[k] = true
	}
	return out, rows.Err()
}

// ComputeAlertKey returns a stable 12-char key for an alert identified by
// (kind, message). Kind+message together are stable across reloads since the
// detector is deterministic on the same data window.
func ComputeAlertKey(kind, message string) string {
	sum := sha256.Sum256([]byte(kind + "|" + message))
	return hex.EncodeToString(sum[:6]) // 12 hex chars
}
