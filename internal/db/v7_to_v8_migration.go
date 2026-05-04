package db

// MigrationV7ToV8 adds Dashboard v2 foundation primitives:
//
//   - alerts_acked: persists "dismissed" state for Alert Center cards so an
//     orphan-cost or stale-data alert doesn't reappear after page reload.
//   - 3 composite indexes covering cross-project / cross-sprint / cross-repo
//     queries introduced by Phase 02 PO View widgets (cost-by-department,
//     sprint burndown, repo leaderboard). Without these, dashboard queries
//     fall back to filtering an already-time-filtered set in memory.
const MigrationV7ToV8 = `
CREATE TABLE IF NOT EXISTS alerts_acked (
    alert_key TEXT PRIMARY KEY,
    acked_by TEXT,
    acked_at TEXT NOT NULL DEFAULT (datetime('now')),
    expires_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_runs_sprint_started ON runs(jira_sprint_id, started_at);
CREATE INDEX IF NOT EXISTS idx_runs_dept_started ON runs(department, started_at);
CREATE INDEX IF NOT EXISTS idx_runs_remote_started ON runs(git_remote, started_at);

INSERT OR REPLACE INTO schema_version (version) VALUES (8);
`
