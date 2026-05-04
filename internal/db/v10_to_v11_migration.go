package db

// MigrationV10ToV11 introduces the audit_anchors table — periodic snapshots
// of the audit_log hash chain's tip, optionally pushed to an external
// system (Confluence) so that a later VerifyAuditChain --with-anchor run
// can prove the local chain hasn't been silently rewritten.
//
// Without anchors, VerifyAuditChain only confirms that the local rows are
// internally consistent: anyone with write access could rebuild the entire
// chain from scratch and produce a valid-looking log. Anchors break that
// attack: each row pins (last_audit_id, last_curr_hash, anchored_at), and
// the external copy on Confluence is the witness. If today's chain doesn't
// reproduce yesterday's anchored hash for the same last_audit_id, the
// chain has been tampered with between then and now.
//
// Schema:
//   - id                  : surrogate key
//   - anchored_at         : when the anchor was taken (RFC3339)
//   - last_audit_id       : audit_log.id at the tip when anchored
//   - last_curr_hash      : audit_log.curr_hash at that tip
//   - confluence_page_id  : '' if no external anchor was made (offline)
//   - confluence_version  : Confluence page version after upsert (0 if local-only)
//   - status              : 'anchored' (pushed) or 'local-only' (no Confluence)
//   - UNIQUE(last_audit_id) — one anchor per audit tip; idempotent re-runs
const MigrationV10ToV11 = `
CREATE TABLE IF NOT EXISTS audit_anchors (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    anchored_at TEXT NOT NULL DEFAULT (datetime('now')),
    last_audit_id INTEGER NOT NULL,
    last_curr_hash TEXT NOT NULL,
    confluence_page_id TEXT NOT NULL DEFAULT '',
    confluence_version INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'local-only',
    UNIQUE(last_audit_id)
);

CREATE INDEX IF NOT EXISTS idx_audit_anchors_anchored ON audit_anchors(anchored_at);
CREATE INDEX IF NOT EXISTS idx_audit_anchors_last_id  ON audit_anchors(last_audit_id);

INSERT OR REPLACE INTO schema_version (version) VALUES (11);
`
