# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
