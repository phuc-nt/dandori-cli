package db

// MigrationV11ToV12 introduces two tables for v0.13's GitHub-backed metrics:
//
//   - pr_events  — one row per (repo, pr_number). Holds the projected GitHub
//                  PR state the metric queries need: timestamps for created /
//                  merged / closed, the first_approval_at used by PR Review
//                  Cycle Time, plus revert + reopen flags used by True
//                  AI-CFR. UNIQUE(repo, pr_number) makes pulls idempotent
//                  via INSERT ... ON CONFLICT upserts.
//
//   - sync_state — generic key/value store. v0.13 uses one key,
//                  "github.last_pr_sync_at", to remember the watermark
//                  between incremental pulls. Future integrations can park
//                  their watermarks here without another migration.
//
// Pure additive — no existing column changes, no data backfill. v0.12
// binaries reading a v12 DB just ignore the new tables.
const MigrationV11ToV12 = `
CREATE TABLE IF NOT EXISTS pr_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    repo TEXT NOT NULL,
    pr_number INTEGER NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL DEFAULT '',
    author TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT '',
    submitted_at TEXT NOT NULL DEFAULT '',
    merged_at TEXT,
    closed_at TEXT,
    first_approval_at TEXT,
    merge_commit_sha TEXT NOT NULL DEFAULT '',
    is_reverted INTEGER NOT NULL DEFAULT 0,
    reverted_by_pr INTEGER,
    reverted_at TEXT,
    reopened_at TEXT,
    jira_issue_keys TEXT NOT NULL DEFAULT '',
    last_synced_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(repo, pr_number)
);

CREATE INDEX IF NOT EXISTS idx_pr_events_merged_at  ON pr_events(merged_at);
CREATE INDEX IF NOT EXISTS idx_pr_events_repo_state ON pr_events(repo, state);
CREATE INDEX IF NOT EXISTS idx_pr_events_title      ON pr_events(title);

CREATE TABLE IF NOT EXISTS sync_state (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

INSERT OR REPLACE INTO schema_version (version) VALUES (12);
`
