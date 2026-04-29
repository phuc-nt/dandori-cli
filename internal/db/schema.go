package db

const SchemaVersion = 5

const SchemaSQL = `
CREATE TABLE IF NOT EXISTS schema_version (
    version INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS runs (
    id TEXT PRIMARY KEY,
    jira_issue_key TEXT,
    jira_sprint_id TEXT,
    agent_name TEXT,
    agent_type TEXT NOT NULL DEFAULT 'claude_code',
    user TEXT NOT NULL,
    workstation_id TEXT NOT NULL,
    cwd TEXT,
    git_remote TEXT,
    git_head_before TEXT,
    git_head_after TEXT,
    command TEXT,
    started_at TEXT NOT NULL,
    ended_at TEXT,
    duration_sec REAL,
    exit_code INTEGER,
    status TEXT NOT NULL DEFAULT 'running',
    session_id TEXT,
    input_tokens INTEGER DEFAULT 0,
    output_tokens INTEGER DEFAULT 0,
    cache_read_tokens INTEGER DEFAULT 0,
    cache_write_tokens INTEGER DEFAULT 0,
    model TEXT,
    cost_usd REAL DEFAULT 0,
    engineer_name TEXT,
    department TEXT,
    session_end_reason TEXT,
    human_message_count INTEGER DEFAULT 0,
    agent_message_count INTEGER DEFAULT 0,
    human_intervention_count INTEGER DEFAULT 0,
    human_approval_count INTEGER DEFAULT 0,
    synced INTEGER DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL REFERENCES runs(id),
    layer INTEGER NOT NULL,
    event_type TEXT NOT NULL,
    data TEXT,
    ts TEXT NOT NULL DEFAULT (datetime('now')),
    synced INTEGER DEFAULT 0
);

CREATE TABLE IF NOT EXISTS audit_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    prev_hash TEXT,
    curr_hash TEXT,
    actor TEXT NOT NULL,
    action TEXT NOT NULL,
    entity_type TEXT,
    entity_id TEXT,
    details TEXT,
    ts TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS quality_metrics (
    run_id TEXT PRIMARY KEY REFERENCES runs(id),
    lint_errors_before INTEGER DEFAULT 0,
    lint_errors_after INTEGER DEFAULT 0,
    lint_warnings_before INTEGER DEFAULT 0,
    lint_warnings_after INTEGER DEFAULT 0,
    tests_total_before INTEGER DEFAULT 0,
    tests_passed_before INTEGER DEFAULT 0,
    tests_failed_before INTEGER DEFAULT 0,
    tests_total_after INTEGER DEFAULT 0,
    tests_passed_after INTEGER DEFAULT 0,
    tests_failed_after INTEGER DEFAULT 0,
    lint_delta INTEGER DEFAULT 0,
    tests_delta INTEGER DEFAULT 0,
    lines_added INTEGER DEFAULT 0,
    lines_removed INTEGER DEFAULT 0,
    files_changed INTEGER DEFAULT 0,
    commit_count INTEGER DEFAULT 0,
    commit_msg_quality REAL DEFAULT 0,
    quality_score REAL,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_runs_jira ON runs(jira_issue_key);
CREATE INDEX IF NOT EXISTS idx_runs_synced ON runs(synced) WHERE synced = 0;
CREATE INDEX IF NOT EXISTS idx_runs_status ON runs(status);
CREATE INDEX IF NOT EXISTS idx_runs_engineer ON runs(engineer_name);
CREATE INDEX IF NOT EXISTS idx_runs_department ON runs(department);
CREATE INDEX IF NOT EXISTS idx_events_run_id ON events(run_id);
CREATE INDEX IF NOT EXISTS idx_events_synced ON events(synced) WHERE synced = 0;
CREATE INDEX IF NOT EXISTS idx_audit_log_entity ON audit_log(entity_type, entity_id);
CREATE INDEX IF NOT EXISTS idx_quality_run ON quality_metrics(run_id);

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

CREATE TABLE IF NOT EXISTS task_attribution (
    jira_issue_key TEXT PRIMARY KEY,
    session_count INTEGER NOT NULL,
    total_lines_final INTEGER NOT NULL,
    lines_attributed_agent INTEGER NOT NULL,
    lines_attributed_human INTEGER NOT NULL,
    total_agent_tokens INTEGER DEFAULT 0,
    total_agent_cost_usd REAL DEFAULT 0,
    total_iterations INTEGER DEFAULT 0,
    total_human_messages INTEGER DEFAULT 0,
    total_intervention_count INTEGER DEFAULT 0,
    intervention_rate REAL DEFAULT 0,
    session_outcomes TEXT,
    git_head_at_jira_done TEXT,
    jira_done_at TEXT NOT NULL,
    computed_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_attribution_done_at ON task_attribution(jira_done_at);
`

// Migration from v1 to v2: add quality_metrics table
const MigrationV1ToV2 = `
CREATE TABLE IF NOT EXISTS quality_metrics (
    run_id TEXT PRIMARY KEY REFERENCES runs(id),
    lint_errors_before INTEGER DEFAULT 0,
    lint_errors_after INTEGER DEFAULT 0,
    lint_warnings_before INTEGER DEFAULT 0,
    lint_warnings_after INTEGER DEFAULT 0,
    tests_total_before INTEGER DEFAULT 0,
    tests_passed_before INTEGER DEFAULT 0,
    tests_failed_before INTEGER DEFAULT 0,
    tests_total_after INTEGER DEFAULT 0,
    tests_passed_after INTEGER DEFAULT 0,
    tests_failed_after INTEGER DEFAULT 0,
    lint_delta INTEGER DEFAULT 0,
    tests_delta INTEGER DEFAULT 0,
    lines_added INTEGER DEFAULT 0,
    lines_removed INTEGER DEFAULT 0,
    files_changed INTEGER DEFAULT 0,
    commit_count INTEGER DEFAULT 0,
    commit_msg_quality REAL DEFAULT 0,
    quality_score REAL,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_quality_run ON quality_metrics(run_id);

INSERT OR REPLACE INTO schema_version (version) VALUES (2);
`

// Migration v2→v3:
//   - add engineer_name + department columns
//   - relax runs.agent_name from NOT NULL → NULL (blog leaderboard needs
//     human-only rows where agent_name IS NULL)
//
// SQLite cannot ALTER COLUMN, so we rebuild the table. All other columns
// are preserved exactly.
const MigrationV2ToV3 = `
ALTER TABLE runs ADD COLUMN engineer_name TEXT;
ALTER TABLE runs ADD COLUMN department TEXT;
CREATE INDEX IF NOT EXISTS idx_runs_engineer ON runs(engineer_name);
CREATE INDEX IF NOT EXISTS idx_runs_department ON runs(department);

CREATE TABLE IF NOT EXISTS runs_v3 (
    id TEXT PRIMARY KEY,
    jira_issue_key TEXT,
    jira_sprint_id TEXT,
    agent_name TEXT,
    agent_type TEXT NOT NULL DEFAULT 'claude_code',
    user TEXT NOT NULL,
    workstation_id TEXT NOT NULL,
    cwd TEXT,
    git_remote TEXT,
    git_head_before TEXT,
    git_head_after TEXT,
    command TEXT,
    started_at TEXT NOT NULL,
    ended_at TEXT,
    duration_sec REAL,
    exit_code INTEGER,
    status TEXT NOT NULL DEFAULT 'running',
    session_id TEXT,
    input_tokens INTEGER DEFAULT 0,
    output_tokens INTEGER DEFAULT 0,
    cache_read_tokens INTEGER DEFAULT 0,
    cache_write_tokens INTEGER DEFAULT 0,
    model TEXT,
    cost_usd REAL DEFAULT 0,
    engineer_name TEXT,
    department TEXT,
    synced INTEGER DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

INSERT INTO runs_v3 SELECT
    id, jira_issue_key, jira_sprint_id, agent_name, agent_type, user,
    workstation_id, cwd, git_remote, git_head_before, git_head_after,
    command, started_at, ended_at, duration_sec, exit_code, status,
    session_id, input_tokens, output_tokens, cache_read_tokens,
    cache_write_tokens, model, cost_usd, engineer_name, department,
    synced, created_at
FROM runs;

DROP TABLE runs;
ALTER TABLE runs_v3 RENAME TO runs;

CREATE INDEX IF NOT EXISTS idx_runs_jira ON runs(jira_issue_key);
CREATE INDEX IF NOT EXISTS idx_runs_synced ON runs(synced) WHERE synced = 0;
CREATE INDEX IF NOT EXISTS idx_runs_status ON runs(status);
CREATE INDEX IF NOT EXISTS idx_runs_engineer ON runs(engineer_name);
CREATE INDEX IF NOT EXISTS idx_runs_department ON runs(department);

INSERT OR REPLACE INTO schema_version (version) VALUES (3);
`
