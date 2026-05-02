# dandori-cli FAQ & Troubleshooting

## Common Issues

### Token/Cost Shows $0.00

**Symptom:** Run completes but tokens and cost show as 0.

**Causes:**
1. Session log not found (symlink issue on macOS)
2. Wrong working directory
3. Session file not yet created when tailer reads

**Fix 1: Symlink issue (macOS)**

On macOS, `/tmp` is a symlink to `/private/tmp`. If you run from `/tmp/project`:
```bash
# Claude stores session at:
~/.claude/projects/-private-tmp-project/

# But dandori looks for:
~/.claude/projects/-tmp-project/  # Wrong!
```

**Solution:** Use real path or work from non-symlinked directory:
```bash
cd /private/tmp/project  # Use real path
# OR
cd ~/projects/myproject  # Use home directory
```

**Fix 2: Check session directory exists**
```bash
# Get the expected directory name
echo "-$(pwd | tr '/' '-')"

# Check if it exists
ls ~/.claude/projects/-$(pwd | tr '/' '-')/
```

**Fix 3: Use watch to capture orphan sessions**
```bash
dandori watch --once
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

### Shell Alias Not Working

**Symptom:** Typing `claude "..."` runs bare Claude without wrapping.

**Fix:**
1. Restart shell or `source ~/.zshrc`
2. Check alias is installed: `grep "dandori aliases" ~/.zshrc`
3. If missing, run `dandori init --shell`
4. Verify alias expands: `type claude` → should show `claude is an alias for 'dandori run -- claude'`

### Watch Daemon Misses Runs

**Symptom:** `dandori watch` doesn't pick up recent `\claude` invocations.

**Fix:**
1. Verify root is correct: `dandori watch --once --root ~/.claude/projects`
2. Check session file exists: `ls ~/.claude/projects/*/\*.jsonl | tail -3`
3. Orphan runs use `agent_name='orphan'` — filter analytics: `sqlite3 ~/.dandori/local.db "SELECT * FROM runs WHERE agent_name='orphan'"`

### Uninstall Shell Aliases

```bash
# Manually edit the rc file and remove block between markers:
#   # >>> dandori aliases (managed) >>>
#   ...
#   # <<< dandori aliases (managed) <<<
# Or use the markers as grep anchors:
sed -i '' '/>>> dandori aliases/,/<<< dandori aliases/d' ~/.zshrc
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
| `dandori init` | Create config + database + shell aliases |
| `dandori task run KEY` | **Recommended**: Run agent with full Jira+Confluence context |
| `dandori run` | Execute agent with tracking (explicit) |
| `dandori watch` | Background capture of orphan runs |
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
