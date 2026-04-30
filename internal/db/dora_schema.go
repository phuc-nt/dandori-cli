package db

// Migration v3→v4 (G6 — DORA + Rework Rate exporter):
//   - add metric_snapshots table for caching DORA metric exports
//   - add composite index on events(event_type, run_id) to speed up
//     Rework Rate queries (filter by event_type='task.iteration.start')
//
// ADD-only — no ALTER on existing tables. Rollback = DROP the new objects.
const MigrationV3ToV4 = `
CREATE TABLE IF NOT EXISTS metric_snapshots (
    id TEXT PRIMARY KEY,
    team TEXT,
    format TEXT NOT NULL,
    window_start TEXT NOT NULL,
    window_end TEXT NOT NULL,
    payload TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_snap_team_window ON metric_snapshots(team, window_end);
CREATE INDEX IF NOT EXISTS idx_events_type_run ON events(event_type, run_id);

INSERT OR REPLACE INTO schema_version (version) VALUES (4);
`
