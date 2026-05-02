package db

// MigrationV6ToV7 adds an index on runs.started_at, which is the primary
// time-range filter in all analytics and dashboard queries. Without it SQLite
// performs a full-table scan on every dashboard page load.
//
// IF NOT EXISTS ensures the migration is safe to apply against DBs that were
// created from the v7 SchemaSQL (which already includes the index).
const MigrationV6ToV7 = `
CREATE INDEX IF NOT EXISTS idx_runs_started_at ON runs(started_at);

INSERT OR REPLACE INTO schema_version (version) VALUES (7);
`
