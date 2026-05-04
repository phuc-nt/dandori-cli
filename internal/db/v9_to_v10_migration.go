package db

// MigrationV9ToV10 introduces the buglinks table — a first-class record of
// "this run authored code that was later flagged as the cause of a bug".
//
// Before v10, the QA "Bug Hotspots" widget approximated this via a
// regression proxy (runs whose lint/tests degraded). That proxy is noisy
// because it conflates in-flight work with shipped regressions. The
// buglinks table is fed by the task-done hook (cmd/task.go) which scans
// the bug's Jira "is caused by" / "causes" links, resolves each to the
// run that originally produced the offending code, and inserts one row
// per (bug, run) pair.
//
// Schema:
//   - id            : surrogate key
//   - jira_bug_key  : the bug ticket key (e.g. CLITEST1-42)
//   - run_id        : FK to runs.id — the run blamed by the link
//   - reason        : optional free text from the link / hook
//   - linked_at     : when the hook recorded the link
//   - linked_by     : actor (defaults to 'task-done-hook')
//   - UNIQUE(jira_bug_key, run_id) — one row per bug↔run pair (idempotent
//     hook re-runs are no-ops via INSERT OR IGNORE)
const MigrationV9ToV10 = `
CREATE TABLE IF NOT EXISTS buglinks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    jira_bug_key TEXT NOT NULL,
    run_id TEXT NOT NULL REFERENCES runs(id),
    reason TEXT,
    linked_at TEXT NOT NULL DEFAULT (datetime('now')),
    linked_by TEXT NOT NULL DEFAULT 'task-done-hook',
    UNIQUE(jira_bug_key, run_id)
);

CREATE INDEX IF NOT EXISTS idx_buglinks_run    ON buglinks(run_id);
CREATE INDEX IF NOT EXISTS idx_buglinks_linked ON buglinks(linked_at);
CREATE INDEX IF NOT EXISTS idx_buglinks_bug    ON buglinks(jira_bug_key);

INSERT OR REPLACE INTO schema_version (version) VALUES (10);
`
