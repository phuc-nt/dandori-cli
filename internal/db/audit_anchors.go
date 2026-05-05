// Package db — audit_anchors.go: store helpers for the audit_anchors table
// introduced in v11. Each row pins a tip of the audit_log hash chain to a
// point in time, optionally accompanied by an external Confluence record.
package db

import (
	"database/sql"
	"errors"
	"fmt"
)

// AuditAnchor is one row in audit_anchors.
type AuditAnchor struct {
	ID                int64  `json:"id"`
	AnchoredAt        string `json:"anchored_at"`
	LastAuditID       int64  `json:"last_audit_id"`
	LastCurrHash      string `json:"last_curr_hash"`
	ConfluencePageID  string `json:"confluence_page_id"`
	ConfluenceVersion int    `json:"confluence_version"`
	Status            string `json:"status"` // 'anchored' | 'local-only'
}

// LatestAuditTip returns the (id, curr_hash) of the most recent audit_log
// row, or (0, "") if the audit_log is empty. Caller is expected to treat
// the empty-log case as "nothing to anchor yet" rather than an error.
func (l *LocalDB) LatestAuditTip() (int64, string, error) {
	var id int64
	var hash string
	err := l.QueryRow(`
		SELECT id, COALESCE(curr_hash, '')
		FROM audit_log
		ORDER BY id DESC
		LIMIT 1
	`).Scan(&id, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", nil
	}
	if err != nil {
		return 0, "", fmt.Errorf("latest audit tip: %w", err)
	}
	return id, hash, nil
}

// LatestAuditAnchor returns the most recently inserted anchor, or nil if
// no anchor has ever been recorded.
func (l *LocalDB) LatestAuditAnchor() (*AuditAnchor, error) {
	var a AuditAnchor
	err := l.QueryRow(`
		SELECT id, anchored_at, last_audit_id, last_curr_hash,
		       confluence_page_id, confluence_version, status
		FROM audit_anchors
		ORDER BY id DESC
		LIMIT 1
	`).Scan(&a.ID, &a.AnchoredAt, &a.LastAuditID, &a.LastCurrHash,
		&a.ConfluencePageID, &a.ConfluenceVersion, &a.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("latest audit anchor: %w", err)
	}
	return &a, nil
}

// ListAuditAnchors returns up to `limit` anchors oldest-first (chain order)
// for verification or display. limit<=0 means all.
func (l *LocalDB) ListAuditAnchors(limit int) ([]AuditAnchor, error) {
	q := `SELECT id, anchored_at, last_audit_id, last_curr_hash,
	             confluence_page_id, confluence_version, status
	      FROM audit_anchors ORDER BY id ASC`
	args := []any{}
	if limit > 0 {
		q += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := l.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("list audit anchors: %w", err)
	}
	defer rows.Close()
	out := []AuditAnchor{}
	for rows.Next() {
		var a AuditAnchor
		if err := rows.Scan(&a.ID, &a.AnchoredAt, &a.LastAuditID, &a.LastCurrHash,
			&a.ConfluencePageID, &a.ConfluenceVersion, &a.Status); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// InsertAuditAnchor records a new anchor. Idempotent on (last_audit_id):
// if a row already exists for the same last_audit_id, its confluence_page_id,
// confluence_version, and status are updated — this upgrades a local-only row
// to anchored when a Confluence write succeeds after a prior local-only record.
// confluencePageID is empty when status='local-only' (no external record was made).
func (l *LocalDB) InsertAuditAnchor(lastAuditID int64, lastCurrHash, confluencePageID string, confluenceVersion int, status string) (int64, error) {
	if status == "" {
		status = "local-only"
	}
	res, err := l.Exec(`
		INSERT INTO audit_anchors
		    (last_audit_id, last_curr_hash, confluence_page_id, confluence_version, status)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(last_audit_id) DO UPDATE SET
		    confluence_page_id  = excluded.confluence_page_id,
		    confluence_version  = excluded.confluence_version,
		    status              = excluded.status
	`, lastAuditID, lastCurrHash, confluencePageID, confluenceVersion, status)
	if err != nil {
		return 0, fmt.Errorf("insert audit anchor: %w", err)
	}
	id, _ := res.LastInsertId()
	return id, nil
}
