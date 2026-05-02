# dandori-cli Setup Guide

## Prerequisites

- Go 1.21+
- Claude Code CLI (`claude`)
- Jira Cloud account with API token
- Confluence Cloud account (same Atlassian instance)

## Quick Start

```bash
# 1. Install
go install github.com/phuc-nt/dandori-cli@latest
# or: brew install phuc-nt/dandori/dandori

# 2. Full wizard (Jira email + token + Confluence space, test connection)
dandori init

# → Config + database created, ready to use immediately
```

The wizard prompts for:
- Agent name
- Jira email + API token
- Confluence space key
- Test connection (validates credentials live)

After init, you can start using dandori right away. No manual config editing needed.

## Configuration

### Config File (`~/.dandori/config.yaml`)

```yaml
agent:
  name: "alpha"
  type: "claude_code"

jira:
  base_url: "https://YOUR-DOMAIN.atlassian.net"
  user: "your-email@example.com"
  token: "YOUR_API_TOKEN"
  project_key: "PROJ"
  cloud: true

confluence:
  base_url: "https://YOUR-DOMAIN.atlassian.net/wiki"
  space_key: "PROJ"
  cloud: true
```

### Get Jira API Token

1. Go to https://id.atlassian.com/manage-profile/security/api-tokens
2. Click "Create API token"
3. Copy token to `config.yaml`

### Get Confluence Space Key

1. Open your Confluence space
2. Space key is in URL: `/wiki/spaces/SPACEKEY/...`

## Verify Setup

The init wizard tests both Jira and Confluence connections automatically. If either fails, the wizard shows the error and prompts to retry.

To manually verify after init:

```bash
# Test Jira connection
dandori task info PROJ-1

# Test Confluence connection
dandori conf-write --task PROJ-1 --dry-run
```

## Basic Workflow

### Recommended: Task Run with Context

```bash
# Single command: fetch context → run agent → sync results
dandori task run PROJ-123

# This automatically:
# 1. Fetches Jira issue + linked Confluence docs
# 2. Generates context file for agent
# 3. Transitions Jira to In Progress
# 4. Runs Claude with context injected
# 5. Tracks tokens, cost, duration
# 6. Transitions Jira to Done + posts detailed comment
```

### Alternative: Manual Steps

```bash
# 1. Start a task
dandori task start PROJ-123

# 2. Run agent — either transparent (via alias) or explicit
claude "implement feature X"                          # via shell alias
# OR: dandori run --task PROJ-123 -- claude "..."     # explicit

# 3. Sync status back to Jira
dandori jira-sync

# 4. Write report to Confluence
dandori conf-write --task PROJ-123

# 5. View analytics
dandori dashboard
```

## Background Capture (Optional)

If you sometimes run `claude` directly without `dandori` context, the watcher can capture those "orphan" runs:

```bash
# Manual single pass (good for cron / launchd / systemd)
dandori watch --once

# Continuous foreground (Ctrl-C to stop)
dandori watch

# Auto-start on macOS
dandori watch enable

# Auto-start on Linux (systemd-user)
dandori watch enable

# Check status
dandori watch status

# Stop auto-start
dandori watch disable
```

The watcher polls `~/.claude/projects/*/*.jsonl` and inserts orphan runs with `agent_name='orphan'`.

**Note:** On Windows, use manual cron-like scheduling. The `dandori watch enable` command will show guidance if unsupported.

## Server Setup (Optional)

For team-wide analytics, run the monitoring server:

```bash
# Start PostgreSQL
docker-compose up -d postgres

# Run server
DANDORI_DB_HOST=localhost ./bin/dandori-server

# Sync local data to server
./bin/dandori sync --daemon
```

## Directory Structure

```
~/.dandori/
├── config.yaml      # Configuration
├── local.db         # SQLite database (runs, events)
└── cache/           # Confluence page cache
```

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `DANDORI_CONFIG` | Config file path | `~/.dandori/config.yaml` |
| `DANDORI_DB_PATH` | SQLite database path | `~/.dandori/local.db` |
| `DANDORI_VERBOSE` | Enable debug logging | `false` |

## Next Steps

- Read [User Guide](02-user-guide.md) for step-by-step use cases
- Read [FAQ](03-faq.md) for common issues
- Check `dandori --help` for all commands
- See [devlog/](devlog/) for implementation details
