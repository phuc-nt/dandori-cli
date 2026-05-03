# dandori-cli

[![Go](https://img.shields.io/badge/go-1.26%2B-blue)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Tests](https://img.shields.io/badge/tests-906%20runs%20%2B%20E2E-brightgreen)](docs/devlog/)

Lightweight CLI outer harness for managing AI agent dev teams. Wraps agent execution, tracks runs, integrates with Jira/Confluence, and provides analytics for PO/PDM and QA.

## Concept

Prove that a software project can be operated by **agent developers** managed by **human PO/PDM + QA** — using Jira for PBIs, Confluence for project documents, and dandori-cli for agent orchestration, tracking, and analytics.

See [Outer Harness](https://phuc-nt.github.io/dandori-pitch/outer-harness.html) for the vision behind this tool.

## Features

- **Context injection** — `dandori task run KEY` auto-fetches Jira issue + linked Confluence docs
- **Explicit subcommands** — `dandori claude` for ad-hoc tracking, `dandori task run` for Jira-driven work
- **Background watcher** — `dandori watch` catches runs even when the wrapper is bypassed
- **3-layer instrumentation** — fork/exec + session log tailer + semantic events
- **Real-time cost tracking** — token counts × model price table (Claude Sonnet/Opus/Haiku)
- **Jira integration** — task start/done, status transitions, completion comments
- **Confluence integration** — auto-post run reports with metadata, files changed, git diff
- **G9 analytics dashboard** — 3-level surface (engineer · project · org), CWD-aware landing, DORA scorecard, attribution composite, intent feed, insight engine, drilldowns, mobile-responsive
- **Agent assignment** — scoring algorithm (capability 40% + type 30% + history 20% + load 10%)
- **Analytics CLI** — agent stats, cost breakdown, sprint summary

## Install

Choose one:

**Go (needs Go 1.26+ in PATH):**
```bash
go install github.com/phuc-nt/dandori-cli@latest
```

**Homebrew (macOS / Linux):**
```bash
brew install phuc-nt/dandori/dandori
```

**Prebuilt binary (no dependencies):**
```bash
# Replace with your platform: darwin_amd64, darwin_arm64, linux_amd64, linux_arm64, windows_amd64
curl -L https://github.com/phuc-nt/dandori-cli/releases/latest/download/dandori_darwin_arm64.tar.gz | tar xz
sudo mv dandori /usr/local/bin/
```

## Quick Start

```bash
# Setup (full wizard: Jira + Confluence + test connection)
dandori init

# Option 1: Run agent with full Jira+Confluence context (recommended)
dandori task run PROJ-123
# → Fetches issue + linked docs → injects context → runs agent → syncs results

# Option 2: Ad-hoc tracking without Jira
dandori claude "fix the auth bug"

# View analytics
dandori dashboard
```

See [User Guide](docs/02-user-guide.md) for step-by-step use cases.

## Commands

| Command | Purpose |
|---------|---------|
| `dandori init` | Full wizard: Jira + Confluence + test connection |
| `dandori task run KEY` | Run with full Jira+Confluence context |
| `dandori claude "..."` | Ad-hoc tracked run (no Jira context) |
| `dandori watch enable/disable/status` | Background daemon (macOS/Linux) |
| `dandori task start/done/info` | Manual Jira task lifecycle |
| `dandori run --task KEY -- <cmd>` | Low-level explicit wrapper (scripts) |
| `dandori watch [--once]` | Single/continuous orphan capture |
| `dandori jira-sync` | Transition Jira + add comments |
| `dandori conf-write --task KEY` | Confluence report |
| `dandori analytics {runs\|agents\|cost}` | Terminal analytics |
| `dandori analytics all --since 30` | 4-block snapshot (cost · leaderboard · quality · alerts) |
| `dandori analytics cost --by {engineer\|department}` | Group cost by engineer or department |
| `dandori dashboard` | Web UI (localhost:8088) |
| `dandori assign suggest/set` | Agent assignment |
| `dandori status` | Recent runs summary |
| `dandori sync` | Push to central server (optional) |
| `dandori demo --reset --seed --use\|--restore` | Seed/teardown demo DB (blog scenario) |

## Architecture

```
  Jira (sprint board)         Confluence (docs)
        │                           │
        └─────────┬─────────────────┘
                  │
         ┌────────▼────────┐
         │  dandori-cli    │
         │  ┌───────────┐  │
         │  │ wrapper   │──┼── 3-layer instrumentation
         │  │ tailer    │  │
         │  │ watcher   │  │
         │  └───────────┘  │
         │                 │
         │  SQLite local.db│
         └────────┬────────┘
                  │ events (optional)
                  ▼
         ┌─────────────────┐
         │ Monitoring Srv  │  PostgreSQL + Dashboard
         │   (optional)    │
         └─────────────────┘
                  │
          Claude Code (unchanged)
```

## Scope

**In scope:**
- Tracking & Audit (3-layer instrumentation)
- Analytics (multi-dimensional cost/time queries)
- Jira integration (lifecycle, status sync)
- Confluence integration (reports, context)
- Agent assignment (hybrid suggest + PO confirm)
- Explicit subcommands (`dandori claude`, `dandori watch enable/disable/status`)
- Background capture (watch daemon)

**Out of scope:**
- Knowledge marketplace (Phase 2)
- Quality gates (use CI/CD)
- Approval workflows (use Jira native)
- Multi-runtime beyond Claude Code (Phase 2)

## Tech Stack

| Component | Choice |
|---------|---------|
| CLI + Server | Go 1.26+ |
| CLI local DB | SQLite (modernc.org/sqlite — pure Go, no CGO) |
| Server DB | PostgreSQL (optional) |
| CLI framework | Cobra |
| Dashboard | Embedded HTML + Chart.js |

## Documentation

- [User Guide](docs/02-user-guide.md) — Step-by-step use cases
- [Setup Guide](docs/01-setup-guide.md) — Config and first run
- [FAQ](docs/03-faq.md) — Troubleshooting
- [Devlog](docs/devlog/) — Development history
- [Changelog](CHANGELOG.md) — Version history

## Development

```bash
make build      # → bin/dandori
make test       # go test ./...
make test-e2e   # comprehensive E2E suite (requires Jira/Confluence creds)
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for contribution guidelines.

## Implementation Status

All 8 phases complete. See [implementation plan](../plans/260418-1301-dandori-cli/) (workspace-level).

| Phase | Status |
|-------|--------|
| 01 Foundation | ✅ |
| 02 Agent Wrapper | ✅ |
| 03 Jira Integration | ✅ |
| 04 Confluence Integration | ✅ |
| 05 Monitoring Server | ✅ |
| 06 Agent Assignment | ✅ |
| 07 Analytics | ✅ |
| 08 E2E Flow | ✅ |

**Vision-aligned additions** (post Phase 08):
- Explicit subcommands (`dandori claude`, `dandori watch enable/disable/status`) — no shell rc mutation
- Watch daemon (background tracking via launchd/systemd-user)

## License

MIT — see [LICENSE](LICENSE).
