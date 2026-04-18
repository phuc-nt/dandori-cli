# dandori-cli

Lightweight CLI outer harness for managing AI agent dev teams. Wraps agent execution, tracks runs, integrates with Jira/Confluence, and provides analytics for PO/PDM and QA.

## Concept

Prove that a software project can be operated by **agent developers** managed by **human PO/PDM + QA** — using Jira for PBIs, Confluence for project documents, and dandori-cli for agent orchestration, tracking, and analytics.

See [Outer Harness](https://phuc-nt.github.io/dandori-pitch/outer-harness.html) for the concept behind this tool.

## Architecture

```
  Jira (sprint board)  ←──→  Monitoring Server  ←──→  Dashboard
  Confluence (docs)              │
                                 │ events (batched)
                                 │
                    CLI (workstation)  →  Claude Code (unchanged)
```

- **CLI**: Go binary on each engineer's workstation. Wraps agent commands, captures run data, syncs to server.
- **Server**: Aggregates events, hosts Jira poller, serves dashboard + analytics API.
- **Jira**: Task board — dandori-cli polls for sprint changes, suggests agents, tracks status.
- **Confluence**: Knowledge store — dandori-cli reads docs for context, writes agent reports back.

## Scope

**In scope**: Tracking & Audit, Analytics, Jira integration, Confluence integration (two-way), agent assignment (hybrid: suggest + PO confirm).

**Out of scope**: Knowledge marketplace, quality gates, approval workflows (use Jira native), evaluation suite.

**Agent runtime**: Claude Code first. Extensible to Codex, Copilot later.

## Quick Start

```bash
# Install
go install github.com/phuc-nt/dandori-cli@latest

# Setup
dandori init

# Run an agent
dandori run --task PROJ-123 -- claude "fix the auth bug"

# Check status
dandori status

# Sync to server
dandori sync
```

## Implementation Plan

See [`plans/260418-1301-dandori-cli/`](../plans/260418-1301-dandori-cli/) for the full 8-phase implementation plan:

| Phase | Name | Priority |
|---|---|---|
| 01 | Foundation (Go, CLI, SQLite, config) | P0 |
| 02 | Agent Wrapper (3-layer instrumentation) | P0 |
| 03 | Jira Integration (poller, task fetch, status sync) | P0 |
| 04 | Confluence Integration (read docs, write reports) | P1 |
| 05 | Monitoring Server (event ingest, dashboard, SSE) | P0 |
| 06 | Agent Assignment (hybrid suggest + confirm) | P1 |
| 07 | Analytics (multi-dimensional, materialized views) | P1 |
| 08 | E2E Flow (integration tests, demo script) | P2 |

## Tech Stack

| Component | Choice |
|---|---|
| CLI + Server | Go |
| CLI local DB | SQLite (pure Go) |
| Server DB | PostgreSQL |
| Dashboard | Server-rendered HTML + HTMX |
| CLI framework | Cobra |

## License

MIT
