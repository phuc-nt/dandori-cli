# dandori-cli User Guide

Step-by-step guide organized by use case.

---

## Install

```bash
go install github.com/phuc-nt/dandori-cli@latest
dandori init         # full wizard
```

---

## Use Case 1 — Solo Engineer: Ad-hoc Tracking

**Goal:** Run Claude with tracking when you don't have a Jira task.

```bash
# One-time setup
dandori init

# Run with tracking (no Jira context)
dandori claude "refactor auth module"

# View last 10 runs
dandori analytics runs

# Open visual dashboard
dandori dashboard
```

**What's recorded:** run_id, cwd, git HEAD, duration, exit code, input/output tokens, cache tokens, model, cost.

**Quiet mode:** Suppress run summary and keep logs concise:
```bash
dandori claude -q "refactor auth module"
# Errors/warnings still print to stderr; info logs suppressed
```

---

## Use Case 2 — Run Agent with Full Task Context (Recommended)

**Goal:** Agent automatically receives full context from Jira issue + linked Confluence docs. All activities tracked and synced to Jira.

```bash
# Setup done via init wizard — just run:
dandori task run PROJ-123

# What happens:
# 1. Fetches Jira issue (summary, description, acceptance criteria)
# 2. Extracts Confluence links from description
# 3. Fetches linked Confluence page content
# 4. Writes context to temp file
# 5. Transitions Jira to "In Progress" + adds start comment
# 6. Runs agent with context injected into prompt
# 7. Captures tokens, cost, duration from session log
# 8. Captures git changes (files, commits)
# 9. On success: transitions Jira to "Done" + adds completion comment with:
#    - Run statistics (agent, duration, cost, tokens, model)
#    - Git HEAD before → after
#    - Files changed
#    - Commits made
#    - Acceptance criteria for verification
```

**Preview context without running:**
```bash
dandori task run PROJ-123 --dry-run
```

**Important:** Always use `dandori task run` (not `dandori run`) when working on Jira tasks. This ensures:
- Full context injection from Jira + Confluence
- Automatic Jira status transitions
- Comprehensive completion comments with all activity details
- Proper token/cost tracking

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
dandori analytics runs                       # recent runs
dandori analytics agents                     # stats per agent
dandori analytics cost                       # cost breakdown (per project)
dandori analytics cost --by engineer         # group by engineer
dandori analytics cost --by department       # group by department
dandori analytics all --since 30             # 4-block snapshot: cost · leaderboard · quality · alerts

# Layer-3 instrumentation analytics (tools, context, iterations)
dandori analytics tools                      # top tools used by agents (with success%)
dandori analytics tools --top 10 --since 30
dandori analytics context                    # top Confluence pages read as task context
dandori analytics iterations                 # avg/max feedback rounds per agent
dandori analytics iterations --by engineer   # group by engineer
dandori analytics iterations --by sprint     # group by sprint
dandori analytics bugs                       # bugs caused per agent (from Jira Bug tickets)
dandori analytics bugs --by task             # bugs per Jira task
dandori analytics bugs --since 30 --format json

# Composite quality KPIs (regression rate, bug rate, quality-adjusted cost)
dandori analytics kpi                                    # default: regression by agent
dandori analytics kpi --kpi regression --by engineer     # regression rate per engineer
dandori analytics kpi --kpi bugs --by sprint             # bugs / runs per sprint
dandori analytics kpi --kpi cost --by agent --top 10     # quality-adjusted cost per task
# Also surfaced as a [5] QUALITY KPI block in: dandori analytics all
# All accept --format json for piping

# Jira poller daemon (sprint cycle + bug-link cycle)
dandori jira-poll                            # foreground daemon (Ctrl-C to stop)
dandori jira-poll --once                     # single pass — useful for cron / launchd / systemd
dandori jira-poll --interval 60 --bug-interval 1800
dandori jira-poll --skip-bugs                # only run sprint cycle (skip bug-link search)

# Web dashboard with charts
dandori dashboard               # opens browser to http://localhost:8088
```

The dashboard has:
- Total cost / runs / tokens overview
- Agent leaderboard
- Per-task cost breakdown (Jira links clickable)
- Recent runs timeline
- Quality KPI section: regression rate, bug rate, quality-adjusted cost — each with `--by agent | engineer | sprint` dropdown

### `dandori analytics kpi` — Composite Quality KPIs

Flags: `--kpi {regression|bugs|cost}` selects the metric (regression rate, bug rate, quality-adjusted cost); `--by {agent|engineer|sprint}` groups results; `--top N` limits rows; `--since N` restricts to last N days; `--format json` for piping.

Example output (`dandori analytics kpi --kpi regression --by agent`):
```
AGENT   RUNS  REGRESSIONS  RATE
alpha   42    3            7.1%
beta    18    4            22.2%
```

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

## Use Case 10 — Compare Agent Code Quality

**Goal:** Answer "Does Team A's agent write better code than Team B?"

```bash
# View quality comparison by agent
dandori analytics quality

# Compare specific agents
dandori analytics quality --compare alpha,beta

# Export as JSON for external analysis
dandori analytics quality --format json
```

**Output:**
```
=== Agent Quality Comparison ===
Lint Δ: negative = fewer errors | Tests Δ: positive = more passing

AGENT  RUNS  LINT Δ  TESTS Δ  LINES  COMMITS  MSG QUAL  IMPROVED
-----  ----  ------  -------  -----  -------  --------  --------
alpha  10    -2.3    +15.0    450    12       85%       80%
beta   8     +1.0    +5.0     820    8        60%       50%
```

**Metrics tracked per run:**
- **Lint delta**: Change in lint errors (negative = improvement)
- **Tests delta**: Change in passing tests (positive = improvement)
- **Lines changed**: Lines added + removed
- **Commits**: Number of commits made during run
- **Msg Quality**: Conventional commit adherence (0-100%)
- **Improved**: % of runs that improved code quality

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
| `dandori analytics {runs\|agents\|cost\|quality\|all}` | Terminal analytics |
| `dandori analytics cost --by {engineer\|department}` | Group cost by engineer or department |
| `dandori analytics all --since 30` | 4-block snapshot (cost · leaderboard · quality · alerts) |
| `dandori analytics {tools\|context\|iterations\|bugs}` | Layer-3 instrumentation analytics |
| `dandori analytics iterations --by {agent\|engineer\|sprint}` | Group iteration rounds |
| `dandori analytics bugs --by {agent\|task}` | Group bug.filed events |
| `dandori analytics kpi --kpi {regression\|bugs\|cost} --by {agent\|engineer\|sprint}` | Composite quality KPIs |
| `dandori jira-poll [--once\|--bug-interval N\|--skip-bugs]` | Sprint + bug-link poller daemon |
| `dandori dashboard` | Web UI |
| `dandori demo --reset --seed --use\|--restore` | Demo DB sandbox (blog scenario) |
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

## Best Practices

### Choose Your Command Path

| Command | Use When |
|---------|----------|
| `dandori task run KEY` | Working on Jira tasks (recommended) |
| `dandori claude "..."` | Ad-hoc work, no Jira context, just tracking |
| `dandori run --task KEY -- <any-cmd>` | Power user / scripting; wrap any agent CLI |

**Why choose each:**
- **`task run`** — Auto-fetches context from Jira + Confluence, comprehensive Jira sync, best for daily workflow
- **`claude`** — Fast, tracked, no Jira overhead, good for quick fixes and exploration
- **`run`** — Low-level scripting, when you need to wrap non-Claude tools or batch processes

### Ensure Agent Commits Changes

For git changes to appear in Jira comments, the agent must commit:

```bash
# Good: Agent commits changes
dandori task run PROJ-123 -- claude -p "Add login feature and commit"

# Git changes in Jira comment:
#   Files Changed: src/auth/login.go
#   Commits: abc123 feat: add login
```

### Verify Token Capture

If tokens show as 0, check:
1. Config has correct session directory
2. Run `dandori watch --once` to capture from session files

## Troubleshooting

See [FAQ](03-faq.md) for known issues and fixes.
