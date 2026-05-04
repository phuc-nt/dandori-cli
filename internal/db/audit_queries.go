// Package db — audit_queries.go: query helpers for Phase 04 Audit View.
//
// Tables used:
//   - events       (id, run_id, layer, event_type, data, ts)
//   - audit_log    (id, prev_hash, curr_hash, actor, action, entity_type, entity_id, details, ts)
//
// Hash chain:
//
//	curr_hash = sha256(prev_hash || actor || action || entity_type || entity_id || details || ts)
//
// VerifyAuditChain replays the chain, returning the index of the first mismatch.
package db

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// EventStreamRow is one row in the event-stream table.
type EventStreamRow struct {
	ID        int64  `json:"id"`
	RunID     string `json:"run_id"`
	Layer     int    `json:"layer"`
	EventType string `json:"event_type"`
	Data      string `json:"data"`
	Timestamp string `json:"ts"`
}

// EventStream lists events filtered by run_id (optional), event_type prefix
// (optional), with limit/offset pagination. Latest first.
func (l *LocalDB) EventStream(runID, typeFilter string, limit, offset int) ([]EventStreamRow, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	q := `SELECT id, run_id, layer, event_type, COALESCE(data, ''), ts FROM events WHERE 1=1`
	args := []any{}
	if runID != "" {
		q += " AND run_id = ?"
		args = append(args, runID)
	}
	if typeFilter != "" {
		q += " AND event_type LIKE ?"
		args = append(args, typeFilter+"%")
	}
	q += " ORDER BY id DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := l.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EventStreamRow{}
	for rows.Next() {
		var r EventStreamRow
		if err := rows.Scan(&r.ID, &r.RunID, &r.Layer, &r.EventType, &r.Data, &r.Timestamp); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AuditLogRow is one row in the audit_log table.
type AuditLogRow struct {
	ID         int64  `json:"id"`
	PrevHash   string `json:"prev_hash"`
	CurrHash   string `json:"curr_hash"`
	Actor      string `json:"actor"`
	Action     string `json:"action"`
	EntityType string `json:"entity_type"`
	EntityID   string `json:"entity_id"`
	Details    string `json:"details"`
	Timestamp  string `json:"ts"`
}

// AuditLog lists audit_log entries, optionally filtered by entity_type or
// date range. Oldest first (chain order).
func (l *LocalDB) AuditLog(entity, from, to string, limit, offset int) ([]AuditLogRow, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	q := `SELECT id, COALESCE(prev_hash,''), COALESCE(curr_hash,''),
		actor, action, COALESCE(entity_type,''), COALESCE(entity_id,''),
		COALESCE(details,''), ts
		FROM audit_log WHERE 1=1`
	args := []any{}
	if entity != "" {
		q += " AND entity_type = ?"
		args = append(args, entity)
	}
	if from != "" {
		q += " AND ts >= ?"
		args = append(args, from)
	}
	if to != "" {
		q += " AND ts <= ?"
		args = append(args, to)
	}
	q += " ORDER BY id ASC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := l.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditLogRow{}
	for rows.Next() {
		var r AuditLogRow
		if err := rows.Scan(&r.ID, &r.PrevHash, &r.CurrHash, &r.Actor, &r.Action,
			&r.EntityType, &r.EntityID, &r.Details, &r.Timestamp); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AuditVerifyResult is the response of VerifyAuditChain.
type AuditVerifyResult struct {
	Valid       bool   `json:"valid"`
	Entries     int    `json:"entries"`
	BrokenAt    int64  `json:"broken_at,omitempty"`    // audit_log.id of first mismatch
	BrokenIndex int    `json:"broken_index,omitempty"` // 0-based position in scanned slice
	Reason      string `json:"reason,omitempty"`
}

// VerifyAuditChain replays sha256(prev_hash || actor || action || entity_type
// || entity_id || details || ts) and compares to stored curr_hash. Scans up to
// `limit` rows oldest-first; pass 0 for default 1000.
func (l *LocalDB) VerifyAuditChain(limit int) (*AuditVerifyResult, error) {
	if limit <= 0 {
		limit = 1000
	}
	rows, err := l.Query(`
		SELECT id, COALESCE(prev_hash,''), COALESCE(curr_hash,''),
			actor, action, COALESCE(entity_type,''), COALESCE(entity_id,''),
			COALESCE(details,''), ts
		FROM audit_log
		ORDER BY id ASC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	res := &AuditVerifyResult{Valid: true}
	prevHash := ""
	idx := 0
	for rows.Next() {
		var r AuditLogRow
		if err := rows.Scan(&r.ID, &r.PrevHash, &r.CurrHash, &r.Actor, &r.Action,
			&r.EntityType, &r.EntityID, &r.Details, &r.Timestamp); err != nil {
			return nil, err
		}
		// First entry: prev_hash should be empty.
		if idx == 0 && r.PrevHash != "" {
			res.Valid = false
			res.BrokenAt = r.ID
			res.BrokenIndex = idx
			res.Reason = "first entry has non-empty prev_hash"
			return res, rows.Err()
		}
		// prev_hash continuity.
		if idx > 0 && r.PrevHash != prevHash {
			res.Valid = false
			res.BrokenAt = r.ID
			res.BrokenIndex = idx
			res.Reason = "prev_hash does not match previous curr_hash"
			return res, rows.Err()
		}
		// Recompute curr_hash.
		expected := computeAuditHash(r.PrevHash, r.Actor, r.Action, r.EntityType, r.EntityID, r.Details, r.Timestamp)
		if !strings.EqualFold(strings.TrimSpace(r.CurrHash), expected) {
			res.Valid = false
			res.BrokenAt = r.ID
			res.BrokenIndex = idx
			res.Reason = "curr_hash does not match recomputed hash"
			return res, rows.Err()
		}
		prevHash = r.CurrHash
		idx++
	}
	res.Entries = idx
	return res, rows.Err()
}

// computeAuditHash returns hex sha256 of the canonical concatenation.
func computeAuditHash(prev, actor, action, entityType, entityID, details, ts string) string {
	h := sha256.New()
	h.Write([]byte(prev))
	h.Write([]byte("|"))
	h.Write([]byte(actor))
	h.Write([]byte("|"))
	h.Write([]byte(action))
	h.Write([]byte("|"))
	h.Write([]byte(entityType))
	h.Write([]byte("|"))
	h.Write([]byte(entityID))
	h.Write([]byte("|"))
	h.Write([]byte(details))
	h.Write([]byte("|"))
	h.Write([]byte(ts))
	return hex.EncodeToString(h.Sum(nil))
}

// AppendAuditEntry inserts a new audit_log row computing the proper hash
// linkage. Useful for tests + admin actions.
func (l *LocalDB) AppendAuditEntry(actor, action, entityType, entityID, details, ts string) error {
	var prev string
	row := l.QueryRow(`SELECT COALESCE(curr_hash,'') FROM audit_log ORDER BY id DESC LIMIT 1`)
	_ = row.Scan(&prev)
	curr := computeAuditHash(prev, actor, action, entityType, entityID, details, ts)
	_, err := l.Exec(`
		INSERT INTO audit_log (prev_hash, curr_hash, actor, action, entity_type, entity_id, details, ts)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, prev, curr, actor, action, entityType, entityID, details, ts)
	return err
}
