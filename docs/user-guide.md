# dandori-cli User Guide

Step-by-step guide organized by use case.

---

## Install

```bash
go install github.com/phuc-nt/dandori-cli@latest
dandori init         # installs shell aliases by default
source ~/.zshrc      # or restart your shell
```

From now on, `claude "..."` is automatically tracked. To bypass per-invocation: `\claude "..."`.

---

## Use Case 1 — Solo Engineer: Track My Agent Runs

**Goal:** Every `claude` invocation is recorded. You can review cost and time spent.

```bash
# One-time setup
dandori init

# Use Claude normally — wrapper transparently tracks via alias
claude "refactor auth module"

# View last 10 runs
dandori analytics runs

# Open visual dashboard
dandori dashboard
```

**What's recorded:** run_id, cwd, git HEAD, duration, exit code, input/output tokens, cache tokens, model, cost.

---

## Use Case 2 — Run Agent with Full Task Context (Recommended)

**Goal:** Agent automatically receives full context from Jira issue + linked Confluence docs. No manual copy-paste.

```bash
# Configure Jira + Confluence (first time only)
vim ~/.dandori/config.yaml

# Run agent with auto-context injection
dandori task run PROJ-123

# What happens:
# 1. Fetches Jira issue (summary, description, acceptance criteria)
# 2. Extracts Confluence links from description
# 3. Fetches linked Confluence page content
# 4. Writes context to temp file
# 5. Runs agent with context
# 6. Tracks run (tokens, cost, duration)
# 7. On success: transitions Jira to Done + adds completion comment
```

**Preview context without running:**
```bash
dandori task run PROJ-123 --dry-run
```

**Config snippet:**
```yaml
jira:
  base_url: "https://YOUR-DOMAIN.atlassian.net"
  user: "you@example.com"
  token: "YOUR_API_TOKEN"
  project_key: "PROJ"
  cloud: true
confluence:
  base_url: "https://YOUR-DOMAIN.atlassian.net/wiki"
  space_key: "PROJ"
  cloud: true
```

---

## Use Case 3 — Manual Task Lifecycle (Alternative)

**Goal:** More control over task lifecycle — start, run, and sync separately.

```bash
# Start a task (transitions Jira to In Progress + adds comment)
dandori task start PROJ-123

# Run agent tied to the task (manual prompt)
dandori run --task PROJ-123 -- claude "implement feature X"

# Sync completed runs to Jira (transitions to Done + posts completion comment)
dandori jira-sync
```

---

## Use Case 4 — Post Reports to Confluence

**Goal:** After each run, a Confluence page is created with run metadata, files changed, cost, and git diff.

```bash
# Preview the report first
dandori conf-write --task PROJ-123 --dry-run

# Create the page
dandori conf-write --task PROJ-123

# Output: Created report: PROJ-123 — Run abc123 — 2026-04-18
#         Page ID: 66045
#         URL: https://YOUR-DOMAIN.atlassian.net/wiki/pages/66045
```

**Config snippet:**
```yaml
confluence:
  base_url: "https://YOUR-DOMAIN.atlassian.net/wiki"
  space_key: "PROJ"
  cloud: true
```

---

## Use Case 5 — Capture Runs Even Without the Wrapper

**Goal:** You (or a teammate) ran `\claude` to bypass the wrapper, or forgot the alias. Catch those runs after the fact.

```bash
# Single pass (good for cron, launchd, systemd timers)
dandori watch --once

# Long-running foreground (Ctrl-C to stop)
dandori watch

# Custom cadence
dandori watch --interval 30
```

The watcher scans `~/.claude/projects/*/*.jsonl`, finds sessions with no matching DB run, and inserts them as `agent_name='orphan'` with tokens + cost extracted from the session.

**Auto-start on login (macOS):**
```bash
launchctl submit -l com.phuc.dandori-watch -- /usr/local/bin/dandori watch
```

---

## Use Case 6 — View Team Analytics

**Goal:** See which agents are used most, cost trends, success rates.

```bash
# CLI tables
dandori analytics runs          # recent runs
dandori analytics agents        # stats per agent
dandori analytics cost          # cost breakdown

# Web dashboard with charts
dandori dashboard               # opens browser to http://localhost:8088
```

The dashboard has:
- Total cost / runs / tokens overview
- Agent leaderboard
- Per-task cost breakdown (Jira links clickable)
- Recent runs timeline

---

## Use Case 7 — PO/PDM: Assign Agent to a New Task

**Goal:** Sprint poller suggests an agent when new tasks appear; PO confirms in Jira.

```bash
# Get suggestion with scores
dandori assign suggest PROJ-123

# Example output:
#   alpha  85%  (capability match: backend, go)
#   beta   60%  (backup — capability: frontend)

# Accept a suggestion
dandori assign set PROJ-123 alpha
# → posts confirmation comment on Jira
```

Scoring: capability 40%, issue type 30%, history 20%, load balance 10%.

---

## Use Case 8 — Bypass the Wrapper Once

```bash
\claude "..."                   # leading backslash bypasses the alias
```

The run is NOT tracked. Use sparingly; `dandori watch` can catch it later.

---

## Use Case 9 — Multi-workstation: Same Engineer, Different Machines

Each machine has its own `~/.dandori/local.db`. For now, analytics are per-workstation. Cross-workstation aggregation uses the optional monitoring server (`dandori sync` → PostgreSQL).

---

## Common Commands Reference

| Command | Purpose |
|---------|---------|
| `dandori init` | Config + DB + shell aliases |
| `dandori task run KEY` | Run agent with full Jira+Confluence context |
| `dandori task start/done/info KEY` | Manual Jira task lifecycle |
| `dandori run --task KEY -- <cmd>` | Explicit wrapper (for cron/scripts) |
| `dandori watch [--once]` | Catch orphan runs |
| `dandori jira-sync` | Push run status to Jira |
| `dandori conf-write --task KEY` | Confluence report |
| `dandori analytics {runs\|agents\|cost}` | Terminal analytics |
| `dandori dashboard` | Web UI |
| `dandori status` | Recent runs summary |
| `dandori assign suggest/set` | Agent assignment |
| `dandori sync` | Push events to server (optional) |

---

## Cost Calculation

Dandori tracks token usage and calculates cost using model-specific pricing:

| Model | Input | Output | Cache Write | Cache Read |
|-------|-------|--------|-------------|------------|
| Sonnet 4.6 | $3.00 | $15.00 | $3.75 | $0.30 |
| Opus 4.5/4.6 | $15.00 | $75.00 | $18.75 | $1.50 |
| Haiku 4.5 | $0.80 | $4.00 | $1.00 | $0.08 |

*Prices per 1M tokens*

Formula: `cost = (input × price) + (output × price) + (cache_write × price) + (cache_read × price)`

---

## Jira Completion Comment Format

When a task completes, dandori posts a structured comment:

```
✅ Agent Run Completed

h3. Run Statistics
||Agent||alpha||
||Duration||45s||
||Cost||$0.42||
||Tokens||1234 in / 567 out||
||Model||claude-sonnet-4-6||
||Git||abc123 → def456||

h3. Files Changed
  src/auth/token.go
  src/auth/session.go

h3. Acceptance Criteria (from task)
* (?) Token refresh works silently
* (?) No 401 during valid session

h3. Output Location
* Code: See commits above
* Report: {{dandori conf-write --run abc123}}
```

---

## Troubleshooting

See [FAQ](faq.md) for known issues and fixes.
