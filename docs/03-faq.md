# dandori-cli FAQ & Troubleshooting

## Common Issues

### Token/Cost Shows $0.00

**Symptom:** Run completes but tokens and cost show as 0.

**Causes:**
1. Wrong working directory
2. Session file not yet created when tailer reads

**Fix:**
```bash
# Capture orphan sessions manually
dandori watch --once
```

Or enable auto-capture:
```bash
dandori watch enable
```

### Jira Connection Failed

**Symptom:** `jira API error: 401 - Unauthorized`

**Fix:**
1. Verify API token is correct (not password)
2. Check `cloud: true` for Atlassian Cloud
3. Ensure user email matches Jira account

```bash
# Test connection
curl -u "email@example.com:API_TOKEN" \
  https://YOUR-DOMAIN.atlassian.net/rest/api/2/myself
```

### Confluence Write Failed

**Symptom:** `confluence API error: 404`

**Fix:**
1. Verify `space_key` exists
2. Check `reports_parent_page_id` if set
3. Ensure user has write permission

```yaml
confluence:
  space_key: "PROJ"  # Not "Project Name"
  # reports_parent_page_id: "12345"  # Optional
```

### Task Transition Failed

**Symptom:** `Warning: could not transition`

**Cause:** Issue already in target status or workflow doesn't allow transition.

**Fix:** Check issue status in Jira. Transitions are workflow-dependent.

### Database Locked

**Symptom:** `database is locked`

**Fix:** Only one dandori process should write at a time. Kill other processes:
```bash
pkill -f "dandori run"
```

### Using `dandori claude` Instead of Shell Alias

**v0.9.0 change:** Shell aliases have been removed in favor of explicit `dandori claude` subcommand.

**If you have an old shell alias block from v0.8.x:**
```bash
# Clean up legacy alias from ~/.zshrc or ~/.bashrc
dandori init --uninstall-shell

# Or manually remove the block:
sed -i '' '/>>> dandori aliases/,/<<< dandori aliases/d' ~/.zshrc
```

**Use the subcommand instead:**
```bash
# Instead of: claude "..."
dandori claude "fix the auth bug"

# Or run with Jira context:
dandori task run PROJ-123
```

### Watch Daemon Misses Runs

**Symptom:** `dandori watch` doesn't pick up recent direct `claude` invocations.

**Fix:**
```bash
# Enable auto-capture daemon
dandori watch enable

# Or run manually once
dandori watch --once

# Check status
dandori watch status
```

## Configuration Questions

### How to use multiple agents?

Change agent name per workstation:
```yaml
agent:
  name: "alpha"  # or "beta", "gamma"
```

### How to track without Jira?

Omit `--task` flag:
```bash
./bin/dandori run -- claude "do something"
```
Run is tracked locally but not linked to Jira.

### How to disable Confluence auto-post?

```yaml
confluence:
  auto_post: false
```

### Where is data stored?

| Data | Location |
|------|----------|
| Config | `~/.dandori/config.yaml` |
| Runs/Events | `~/.dandori/local.db` |
| Page cache | `~/.dandori/cache/` |

### Context Injection Not Working

**Symptom:** Agent runs but doesn't seem to have Jira/Confluence context.

**Fix:**
1. Check Confluence links in Jira description use full URLs
2. Verify `--dry-run` shows expected context: `dandori task run PROJ-1 --dry-run`
3. Ensure Confluence pages are accessible with current credentials

### No Git Changes Reported

**Symptom:** Jira completion comment shows "No code changes in this run"

**Cause:** Agent didn't create any commits, or working directory isn't a git repo.

**Fix:** Ensure agent commits changes during the run. Git HEAD is compared before/after.

### Verify Gate Fails on Every Task Run

**Symptom:** `dandori task run` prints "semantic check failed" or "quality gate failed" every time.

**Cause:** v0.9.0 default changed — `verify.semantic_check` and `verify.quality_gate` are now **off** by default.

**To enable (if you want stricter verification):**
```yaml
verify:
  semantic_check: true
  quality_gate: true
```

**Or use the flag to skip for one run:**
```bash
dandori task run PROJ-123 --no-verify
```

**To disable permanently** (recommended for most workflows):
Ensure your config has:
```yaml
verify:
  semantic_check: false
  quality_gate: false
```

### Quality Metrics Show Zero

**Symptom:** `dandori analytics quality` shows all zeros for lint/test.

**Cause:** Quality config not set, or lint/test commands failed.

**Fix:**
1. Ensure quality is enabled in `~/.dandori/config.yaml`:
   ```yaml
   quality:
     enabled: true
     lint_command: "golangci-lint run --json 2>/dev/null || true"
     test_command: "go test -json ./... 2>&1 || true"
     timeout: "30s"
   ```
2. Verify commands work in your project directory
3. For non-Go projects, customize lint/test commands

### Commit Quality Score Low

**Symptom:** MSG QUAL shows low percentage in quality analytics.

**Cause:** Commits don't follow conventional commit format.

**Best practice:** Use conventional commits:
- `feat: add user login`
- `fix(auth): resolve token expiration`
- `docs: update README`

## Command Reference

| Command | Purpose |
|---------|---------|
| `dandori init` | Interactive wizard: config + database + live healthcheck |
| `dandori doctor` | Health check: config + Jira + Confluence + DB + claude binary |
| `dandori claude "..."` | Ad-hoc agent run with tracking (no Jira context) |
| `dandori task run KEY` | **Recommended**: Run agent with full Jira+Confluence context |
| `dandori run` | Execute agent with tracking (explicit, power-user) |
| `dandori watch [enable\|disable\|status\|--once]` | Daemon: catches orphan claude runs |
| `dandori task start/done/info` | Manage Jira task lifecycle |
| `dandori jira-sync` | Sync run status to Jira |
| `dandori conf-write` | Write report to Confluence |
| `dandori analytics` | View local analytics |
| `dandori dashboard` | Open web dashboard |
| `dandori assign suggest/set` | Agent assignment |
| `dandori status` | Show recent runs |
| `dandori sync` | Upload to server (if configured) |

## Debug Mode

Enable verbose logging:
```bash
./bin/dandori -v run --task PROJ-1 -- claude "..."
```

Or set environment:
```bash
export DANDORI_VERBOSE=true
```

## Reset Local Data

```bash
rm ~/.dandori/local.db
./bin/dandori init
```

## Still Stuck?

1. Check `docs/devlog.md` for known issues
2. Run tests: `go test ./...`
3. Open issue: https://github.com/phuc-nt/dandori-cli/issues
