package db

// MigrationV5ToV6 backfills task_attribution.jira_done_at to UTC Z form.
//
// Bug (CLITEST2-14): pre-v6, ComputeAndPersist stored the timestamp verbatim
// from runs.ended_at, which carries the local offset (e.g. +07:00).
// AggregateAttribution then bound window bounds as Z and SQLite-string-compared
// the two formats, silently dropping rows whose offset wasn't UTC. Per-row
// data is intact; only window membership was wrong.
//
// strftime('%Y-%m-%dT%H:%M:%SZ', datetime(x)) parses x's offset, converts to
// UTC, and reformats with Z. No-op for already-Z rows.
const MigrationV5ToV6 = `
UPDATE task_attribution
SET jira_done_at = strftime('%Y-%m-%dT%H:%M:%SZ', datetime(jira_done_at))
WHERE jira_done_at NOT LIKE '%Z' AND datetime(jira_done_at) IS NOT NULL;

INSERT OR REPLACE INTO schema_version (version) VALUES (6);
`
