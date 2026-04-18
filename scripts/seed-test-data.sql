-- Seed realistic test data for analytics testing
-- Based on integration tests with CLITEST project

-- Clear existing data
TRUNCATE runs, events CASCADE;

-- Insert agent runs simulating real work on CLITEST issues
INSERT INTO runs (
    id, jira_issue_key, jira_sprint_id, agent_name, agent_type,
    "user", workstation_id, cwd, git_remote, command,
    started_at, ended_at, duration_sec, exit_code, status,
    input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
    model, cost_usd
) VALUES
-- Sprint 4 runs - CLITEST-1 (Story: Add /hello endpoint)
('run-001', 'CLITEST-1', '4', 'beta', 'claude_code', 'phucnt', 'ws-01', '/workspace/api', 'github.com/test/api', 'implement hello endpoint',
 NOW() - INTERVAL '3 hours', NOW() - INTERVAL '2 hours 45 minutes', 900, 0, 'done',
 15000, 4500, 8000, 2000, 'claude-sonnet-4-5-20250514', 2.85),

('run-002', 'CLITEST-1', '4', 'beta', 'claude_code', 'phucnt', 'ws-01', '/workspace/api', 'github.com/test/api', 'add tests for hello',
 NOW() - INTERVAL '2 hours 30 minutes', NOW() - INTERVAL '2 hours 15 minutes', 900, 0, 'done',
 12000, 3800, 6000, 1500, 'claude-sonnet-4-5-20250514', 2.15),

-- CLITEST-2 (Bug: Fix null pointer)
('run-003', 'CLITEST-2', '4', 'alpha', 'claude_code', 'phucnt', 'ws-02', '/workspace/api', 'github.com/test/api', 'investigate null pointer',
 NOW() - INTERVAL '2 hours', NOW() - INTERVAL '1 hour 40 minutes', 1200, 0, 'done',
 18000, 5200, 10000, 2500, 'claude-sonnet-4-5-20250514', 3.45),

('run-004', 'CLITEST-2', '4', 'alpha', 'claude_code', 'phucnt', 'ws-02', '/workspace/api', 'github.com/test/api', 'fix null check',
 NOW() - INTERVAL '1 hour 30 minutes', NOW() - INTERVAL '1 hour 10 minutes', 1200, 0, 'done',
 14000, 4000, 7000, 1800, 'claude-sonnet-4-5-20250514', 2.65),

-- CLITEST-3 (Task: Write unit tests)
('run-005', 'CLITEST-3', '4', 'alpha', 'claude_code', 'phucnt', 'ws-02', '/workspace/api', 'github.com/test/api', 'write unit tests',
 NOW() - INTERVAL '1 hour', NOW() - INTERVAL '30 minutes', 1800, 0, 'done',
 22000, 6500, 12000, 3000, 'claude-sonnet-4-5-20250514', 4.25),

-- CLITEST-4 (Task: Refactor config) - failed then succeeded
('run-006', 'CLITEST-4', '4', 'gamma', 'claude_code', 'phucnt', 'ws-03', '/workspace/api', 'github.com/test/api', 'refactor config module',
 NOW() - INTERVAL '50 minutes', NOW() - INTERVAL '35 minutes', 900, 1, 'failed',
 10000, 2800, 5000, 1200, 'claude-sonnet-4-5-20250514', 1.85),

('run-007', 'CLITEST-4', '4', 'alpha', 'claude_code', 'phucnt', 'ws-02', '/workspace/api', 'github.com/test/api', 'retry config refactor',
 NOW() - INTERVAL '25 minutes', NOW() - INTERVAL '5 minutes', 1200, 0, 'done',
 16000, 4800, 8000, 2000, 'claude-sonnet-4-5-20250514', 3.05),

-- Historical runs from previous sprint (Sprint 3)
('run-008', 'CLITEST-101', '3', 'alpha', 'claude_code', 'phucnt', 'ws-02', '/workspace/api', 'github.com/test/api', 'setup project',
 NOW() - INTERVAL '2 days', NOW() - INTERVAL '2 days' + INTERVAL '30 minutes', 1800, 0, 'done',
 20000, 5500, 10000, 2500, 'claude-sonnet-4-5-20250514', 3.75),

('run-009', 'CLITEST-102', '3', 'beta', 'claude_code', 'phucnt', 'ws-01', '/workspace/api', 'github.com/test/api', 'implement auth',
 NOW() - INTERVAL '2 days' + INTERVAL '1 hour', NOW() - INTERVAL '2 days' + INTERVAL '2 hours', 3600, 0, 'done',
 35000, 9500, 18000, 4500, 'claude-sonnet-4-5-20250514', 6.85),

('run-010', 'CLITEST-103', '3', 'gamma', 'claude_code', 'phucnt', 'ws-03', '/workspace/api', 'github.com/test/api', 'add logging',
 NOW() - INTERVAL '1 day', NOW() - INTERVAL '1 day' + INTERVAL '45 minutes', 2700, 0, 'done',
 25000, 7000, 13000, 3200, 'claude-sonnet-4-5-20250514', 4.95);

-- Insert events for tracking
INSERT INTO events (run_id, layer, event_type, data, ts) VALUES
-- Layer 1 events (process lifecycle)
('run-001', 1, 'process_start', '{"pid": 12345, "command": "implement hello endpoint"}', NOW() - INTERVAL '3 hours'),
('run-001', 1, 'process_end', '{"exit_code": 0, "duration_sec": 900}', NOW() - INTERVAL '2 hours 45 minutes'),

-- Layer 2 events (output parsing)
('run-001', 2, 'file_edit', '{"path": "src/api/hello.go", "lines_added": 45, "lines_removed": 0}', NOW() - INTERVAL '2 hours 55 minutes'),
('run-001', 2, 'file_edit', '{"path": "src/api/hello_test.go", "lines_added": 120, "lines_removed": 0}', NOW() - INTERVAL '2 hours 50 minutes'),
('run-001', 2, 'cost_update', '{"input_tokens": 15000, "output_tokens": 4500, "cost_usd": 2.85}', NOW() - INTERVAL '2 hours 45 minutes'),

-- Layer 3 events (skill events)
('run-001', 3, 'skill_invoke', '{"skill": "ck:test", "args": "src/api/hello_test.go"}', NOW() - INTERVAL '2 hours 48 minutes'),
('run-001', 3, 'decision', '{"decision": "Use table-driven tests for handler", "rationale": "Better coverage and maintainability"}', NOW() - INTERVAL '2 hours 47 minutes'),

-- Events for bug fix run
('run-003', 1, 'process_start', '{"pid": 12346, "command": "investigate null pointer"}', NOW() - INTERVAL '2 hours'),
('run-003', 2, 'file_read', '{"path": "src/config/loader.go", "lines": 250}', NOW() - INTERVAL '1 hour 55 minutes'),
('run-003', 2, 'grep_search', '{"pattern": "nil check", "matches": 8}', NOW() - INTERVAL '1 hour 50 minutes'),
('run-003', 3, 'root_cause', '{"issue": "Missing nil check on config.Database", "file": "src/config/loader.go", "line": 45}', NOW() - INTERVAL '1 hour 45 minutes'),
('run-003', 1, 'process_end', '{"exit_code": 0, "duration_sec": 1200}', NOW() - INTERVAL '1 hour 40 minutes'),

-- Failed run events
('run-006', 1, 'process_start', '{"pid": 12347, "command": "refactor config module"}', NOW() - INTERVAL '50 minutes'),
('run-006', 2, 'error', '{"error": "Test failed: TestConfigLoad", "output": "expected 3 fields, got 2"}', NOW() - INTERVAL '38 minutes'),
('run-006', 1, 'process_end', '{"exit_code": 1, "duration_sec": 900}', NOW() - INTERVAL '35 minutes');

-- Verify counts
SELECT 'runs' as table_name, COUNT(*) as count FROM runs
UNION ALL
SELECT 'events', COUNT(*) FROM events;
