package serverdb

const ServerSchema = `
CREATE TABLE IF NOT EXISTS workstations (
    id TEXT PRIMARY KEY,
    hostname TEXT NOT NULL,
    user_name TEXT NOT NULL,
    api_key_hash TEXT NOT NULL,
    agent_name TEXT,
    agent_type TEXT DEFAULT 'claude_code',
    capabilities TEXT[],
    team TEXT,
    last_seen_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE IF NOT EXISTS runs (
    id TEXT PRIMARY KEY,
    workstation_id TEXT REFERENCES workstations(id),
    jira_issue_key TEXT,
    jira_sprint_id TEXT,
    agent_name TEXT NOT NULL,
    agent_type TEXT NOT NULL DEFAULT 'claude_code',
    user_name TEXT NOT NULL,
    cwd TEXT,
    git_remote TEXT,
    git_head_before TEXT,
    git_head_after TEXT,
    command TEXT,
    started_at TIMESTAMPTZ NOT NULL,
    ended_at TIMESTAMPTZ,
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
    created_at TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE IF NOT EXISTS events (
    id BIGSERIAL PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES runs(id),
    layer INTEGER NOT NULL,
    event_type TEXT NOT NULL,
    data JSONB,
    ts TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE IF NOT EXISTS jira_tasks (
    issue_key TEXT PRIMARY KEY,
    summary TEXT,
    description TEXT,
    issue_type TEXT,
    priority TEXT,
    status TEXT,
    sprint_id TEXT,
    sprint_name TEXT,
    epic_key TEXT,
    story_points REAL,
    assignee TEXT,
    agent_name TEXT,
    confluence_links JSONB,
    detected_at TIMESTAMPTZ,
    assigned_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT now(),
    updated_at TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE IF NOT EXISTS sprint_state (
    sprint_id TEXT PRIMARY KEY,
    board_id INTEGER NOT NULL,
    sprint_name TEXT,
    issue_keys TEXT[] NOT NULL,
    polled_at TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE IF NOT EXISTS audit_log (
    id BIGSERIAL PRIMARY KEY,
    prev_hash TEXT,
    curr_hash TEXT,
    actor TEXT NOT NULL,
    action TEXT NOT NULL,
    entity_type TEXT,
    entity_id TEXT,
    details JSONB,
    ts TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_runs_jira ON runs(jira_issue_key);
CREATE INDEX IF NOT EXISTS idx_runs_status ON runs(status);
CREATE INDEX IF NOT EXISTS idx_runs_started ON runs(started_at);
CREATE INDEX IF NOT EXISTS idx_runs_agent ON runs(agent_name);
CREATE INDEX IF NOT EXISTS idx_events_run ON events(run_id);
CREATE INDEX IF NOT EXISTS idx_jira_tasks_sprint ON jira_tasks(sprint_id);
CREATE INDEX IF NOT EXISTS idx_jira_tasks_agent ON jira_tasks(agent_name);
`
