# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.6.0] — 2026-04-30

Agent contribution attribution (G7).

### Added
- **`dandori metric export --include-attribution`** — per-task accounting of agent vs human code contribution, plus aggregate intervention/iteration/cost percentiles:
  - **Line-level attribution** via `git blame` at the final HEAD when Jira moved to Done. Each line's introducing commit is membership-tested against the union of session-reachable commits (`rev-list HeadBefore..HeadAfter`); pre-session baseline lines are excluded from totals
  - **Intervention classifier** (v1 heuristic): human text ≥30 chars after agent tool use = intervention, <30 = approval. Documented as a proxy in [`docs/agent-attribution.md`](docs/agent-attribution.md)
  - **Computed BEFORE Jira transition** — `dandori task run` (auto-flow) and `dandori task done` (manual) both write the `task_attribution` row before calling `TransitionToDone`. Failure is non-fatal so observability never blocks the Jira move
  - 6 fields surfaced in the export block: `agent_autonomy_rate` (share of tasks with `intervention_rate < 0.2`), `agent_code_retention_p50/p90`, `intervention_rate_p50`, `iterations_p50/p90`, `cost_per_retained_line_usd_p50`, `session_outcomes` (merged histogram of `agent_finished` / `user_interrupted` / `error`)
  - Insufficient-data semantics match v0.5.0: zero rows in window → block is `null` and `task_attribution` is added to `data_quality.insufficient_data`
  - Backwards-compat: without the flag, output is byte-for-byte identical to v0.5.0 dashboards
- **SQLite migration v4 → v5**: `task_attribution` table + 5 new `runs` columns (`session_end_reason`, `human_message_count`, `agent_message_count`, `human_intervention_count`, `human_approval_count`)
- See [`docs/agent-attribution.md`](docs/agent-attribution.md) for definitions, output schema, three named limitations (format reflow, cross-repo, heuristic threshold), and 6 example questions.

## [0.5.0] — 2026-04-29

DORA + Rework Rate exporter (G6).

### Added
- **`dandori metric export`** — 5 engineering metrics (deployment frequency, lead time for changes, change failure rate, time to restore service, rework rate) over a configurable window:
  - Source of truth = Jira (status transitions for deploys, issuetype/labels for incidents) + local SQLite (Layer-3 `task.iteration.start` events for rework)
  - 3 wire formats: `faros` (DORA schema), `oobeya` (6-layer mapping), `raw` (full report with `jira_config` echo)
  - Insufficient-data semantics: emits `"value": null` (not `0`) so dashboards show "N/A" instead of charting misleading zeros
  - Window flags accept `Nd` / `YYYY-MM-DD` / RFC3339 / `now`; team filter scopes rework leg
  - Configurable via `metric:` block in `~/.dandori/config.yaml` — release statuses, in-progress statuses, incident issue types, incident labels, JQL extension
  - Incident config has no default; CFR + MTTR are skipped cleanly when not opted in
  - Lead time uses NIST linear-interpolation percentiles (p50/p75/p90); MTTR reports p50/p90 + ongoing-incident count
  - Rework Rate uses 10% threshold with strict `>` (10/100 = NOT exceeding); threshold version stamped (`v1-2026Q2`)
  - Reports `tickets_without_in_progress` count in `data_quality` so process gaps surface
- See [`docs/metric-export.md`](docs/metric-export.md) for command reference + config schema.

## [0.4.0] — 2026-04-28

Pre-sync verify gate, Layer-3 tracking, dogfooding bug-fix sweep.

### Added
- **Pre-sync verify gate** (`internal/verify/`) — blocks fake-completion before Jira transitions to Done:
  - Path-match semantic check: extracts file/path tokens from the task spec, flags when the diff misses them
  - Quality gate: fails the gate when the post-run lint/test snapshot reports failures
  - Workspace-scoped matching for the `demo-workspace/{date}-{TASK-ID}/` dogfooding convention
  - Doc-only diffs (`.md`/`.txt`/`.rst`) skip the quality gate; semantic check still runs
  - Inconclusive specs (no extractable paths) flag for review instead of silently passing
  - Warn-mode by default: gate failure leaves the ticket In Progress with a Jira comment, never blocks the exit code
  - Skip via Jira label (`verify.skip_label`, default `skip-verify`) for PO override
  - `dandori task run --no-verify` flag for emergency PO override (audit trail preserved)
  - Config keys: `verify.semantic_check`, `verify.quality_gate`, `verify.skip_label`
- **Layer-3 tracking** (tools, context, iterations, bug links):
  - `wrapper`: emit Layer-3 tool/skill events from the session JSONL
  - `taskcontext`: record `confluence.read` events per page fetched
  - `jira`: detect task iteration via statusCategory regression
  - `jira`: bug-link detection (parsers + `DetectBugLinks` for `caused_by:` description tags and inward/outward link types)
  - `jira-poll` daemon: wire bug-link cycle into poller + analytics
  - Analytics queries for tools, context, iterations on Layer-3 events
- **Composite quality KPIs** (`dandori analytics`):
  - Regression rate, bug rate, quality-adjusted cost — CLI + `analytics-all`
  - Dashboard quality KPI section with 3 dimensions per metric
- **Multi-board Jira polling**: `jira.board_ids` list (legacy `board_id` still honored)
- **Logging**: `LogLevel` config field with `DANDORI_LOG_LEVEL` env override
- **Demo**: `HandleHealthz` endpoint with httptest coverage

### Fixed
- **task-run** (Bug #1): auto `--add-dir` for the context tempDir when wrapping `claude`, so the agent can read the injected context file under `acceptEdits` allowlist
- **db** (Bug #2): tolerate `NULL` `agent_name` in scan paths (older runs)
- **jira** (Bug #5): separate ADF paragraphs with newline so completion comments render correctly
- **wrapper**: populate `runs.engineer_name` from Jira assignee
- **jira**: match canonical link-type Name "Caused" alongside inward/outward forms
- **confluence**: include time-of-day in report title to avoid duplicate-title rejections
- **wrapper**: resolve symlinks for session directory detection
- **quality** (Bug #4): isolate snapshot subprocess (process-group + `WaitDelay`) to prevent post-exit hang when `go test` grandchildren keep stdout pipe open

### Technical
- `internal/verify/semantic_check.go` — path-match extraction + workspace-scoped matching
- `internal/verify/gate.go` — combined gate orchestrator (semantic + quality + skip-label)
- `internal/wrapper/wrapper.go` — `Result.QualityAfter` exposes post-run snapshot to callers
- `internal/quality/collector.go` — `spawnCollectorCmd` helper with `Setpgid` + group-targeted SIGKILL `Cancel` + `WaitDelay`; `DANDORI_QUALITY_RUNNING` env recursion guard
- `internal/jira/buglink.go` — bug-link parsers + detector
- `cmd/version.go` — `formatVersion` extracted; `ParseSemver` helper added

## [0.3.0] — 2026-04-19

Agent Quality Comparison: Measure and compare code quality across agents.

### Added
- **Quality Metrics Tracking** (`internal/quality/`):
  - Lint errors/warnings delta (before/after run)
  - Test pass/fail delta (before/after run)
  - Git diff stats (lines added/removed, files changed)
  - Commit count and message quality scoring
  - Composite quality score (0-100)
- **Quality Analytics** (`dandori analytics quality`):
  - Agent comparison table with quality metrics
  - `--compare alpha,beta` flag for specific agents
  - `--format json` for export
- **Commit Scorer**: Conventional commit adherence scoring (0-1)
- **Git Analyzer**: Diff stats between commits
- **Schema v2**: `quality_metrics` table for storing metrics
- **Config**: `quality.enabled`, `quality.lint_command`, `quality.test_command`

### Technical
- `internal/quality/collector.go` — Lint/test snapshot collection
- `internal/quality/git_analyzer.go` — Git diff analysis
- `internal/quality/commit_scorer.go` — Commit message scoring
- `internal/db/quality.go` — Quality metrics storage/queries

## [0.2.0] — 2026-04-19

Major feature release: Context injection and enhanced tracking.

### Added
- **Task Run with Context** (`dandori task run KEY`):
  - Auto-fetch Jira issue (summary, description, AC)
  - Extract Confluence links from description
  - Fetch linked Confluence page content
  - Generate markdown context file for agent
  - Auto-sync results to Jira on completion
- **Enhanced Jira Completion Comment**:
  - Run statistics (agent, duration, cost, tokens, model)
  - Git HEAD before/after comparison
  - Files changed during the run
  - Acceptance Criteria extracted from task
  - Output location with report command
- **Cost Calculation** with model-specific pricing:
  - Sonnet 4.6: $3/$15 per 1M tokens (in/out)
  - Opus 4.5/4.6: $15/$75 per 1M tokens
  - Haiku 4.5: $0.80/$4 per 1M tokens
- Shell alias transparency via `dandori init`
- Watch daemon: `dandori watch [--once]`
- goreleaser for multi-platform releases
- GitHub Actions release workflow

### Fixed
- Tailer race condition: use channel for synchronization
- Context injection: prepend to user's -p prompt
- Claude session directory detection

### Tests
- 66 E2E tests across 15 groups (A-O)
- Real Jira + Confluence + Claude Code integration

## [0.1.0] — 2026-04-18 (Unreleased preparation)

### Added
- Shell alias transparency
- Watch daemon for orphan runs
- `internal/shellrc/` and `internal/watcher/` packages
- User guide documentation

## [0.1.0] — 2026-04-18

Initial release — all 8 implementation phases complete.

### Added
- **Phase 01** — Foundation: Go module, Cobra CLI, SQLite, config, hash chain
- **Phase 02** — 3-layer agent wrapper (fork/exec, tailer, semantic events), cost calculation
- **Phase 03** — Jira integration: client, poller, transitions, comments
- **Phase 04** — Confluence integration: client, storage↔markdown converter, reader/writer
- **Phase 05** — Monitoring server: PostgreSQL, REST API, SSE, dashboard
- **Phase 06** — Agent assignment: 4-component scorer, engine, REST API
- **Phase 07** — Analytics: 8 query types, CSV/JSON export, CLI commands
- **Phase 08** — E2E flow: Docker Compose, mock APIs, integration tests

### Commands
- `init`, `version`, `status`
- `run`, `event`, `sync`
- `task {start,done,info}`, `jira-sync`
- `conf-write`
- `analytics {runs,agents,cost,sprint}`
- `dashboard`
- `assign {suggest,set,list}`
