package db

// Migration v4→v5 (G7 — agent vs human contribution attribution):
//   - runs gains 5 columns of session-end metadata: a reason enum
//     (agent_finished | user_interrupted | error) plus four message
//     counters that drive the intervention-rate proxy.
//   - task_attribution holds the per-Jira-task summary computed at
//     transition-to-Done time (line retention via git blame, aggregated
//     session stats, intervention rate, outcome histogram).
//   - idx_attribution_done_at lets `metric export --include-attribution`
//     window-scan the table by jira_done_at without a full scan.
//
// ADD-only — no rebuild of runs, no data movement. Existing rows keep
// session_end_reason NULL until backfilled by a future session.
const MigrationV4ToV5 = `
ALTER TABLE runs ADD COLUMN session_end_reason TEXT;
ALTER TABLE runs ADD COLUMN human_message_count INTEGER DEFAULT 0;
ALTER TABLE runs ADD COLUMN agent_message_count INTEGER DEFAULT 0;
ALTER TABLE runs ADD COLUMN human_intervention_count INTEGER DEFAULT 0;
ALTER TABLE runs ADD COLUMN human_approval_count INTEGER DEFAULT 0;

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

INSERT OR REPLACE INTO schema_version (version) VALUES (5);
`
