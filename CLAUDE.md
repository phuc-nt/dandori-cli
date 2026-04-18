# CLAUDE.md — dandori-cli Agent Instructions

> You are an implementation agent for the **dandori-cli** project. Read this file first, then follow the reading order below.

## What This Project Is

dandori-cli is a lightweight Go CLI that wraps AI coding agents (Claude Code first) with tracking, analytics, and Jira/Confluence integration. It proves that a software project can be operated by agent developers managed by human PO/PDM + QA.

**It is NOT the full Dandori platform** (Node.js/Express/SQLite at `dandori/`). This is a separate, focused tool — Go binary + small monitoring server.

## Reading Order

Read in this order. Total ~30 min.

| # | File | Time | What you get |
|---|---|---|---|
| 1 | **This file** (`CLAUDE.md`) | 5 min | Dev workflow, standards, project structure |
| 2 | **[plan.md](../plans/260418-1301-dandori-cli/plan.md)** | 10 min | Architecture, data model, tech stack, phase overview |
| 3 | **Phase file for your current task** | 10 min | Detailed implementation steps, file structure, success criteria |
| 4 | **[cli-pilot-proposal.md](../dandori-pitch/cli-pilot-proposal.md)** | 5 min | Original concept — 3-layer instrumentation, data flow, data governance |

**Reference (read when needed, not upfront):**
- [outer-harness.md](../dandori-pitch/outer-harness.md) — the "why": inner vs outer harness concept, 5 pillars
- [setup-jira-confluence-cloud.md](../plans/260418-1301-dandori-cli/setup-jira-confluence-cloud.md) — test environment setup checklist
- [dandori-overview.md](../dandori-pitch/dandori-overview.md) — full 13-feature vision (dandori-cli implements a subset)

## Phase Files (implementation plans)

All at `plans/260418-1301-dandori-cli/`:

| Phase | File | Priority | Depends on |
|---|---|---|---|
| 01 | `phase-01-foundation.md` | P0 | — |
| 02 | `phase-02-agent-wrapper.md` | P0 | 01 |
| 03 | `phase-03-jira-integration.md` | P0 | 01 |
| 04 | `phase-04-confluence-integration.md` | P1 | 03 |
| 05 | `phase-05-monitoring-server.md` | P0 | 02 |
| 06 | `phase-06-agent-assignment.md` | P1 | 03, 05 |
| 07 | `phase-07-analytics.md` | P1 | 05 |
| 08 | `phase-08-e2e-flow.md` | P2 | All |

**Start with Phase 01.** Then Phase 02 and 03 can run in parallel. Each phase file has: file structure, implementation steps, todo checklist, success criteria, risks.

## Tech Stack

| Component | Choice | Docs |
|---|---|---|
| Language | Go 1.22+ | https://go.dev/doc/ |
| CLI framework | Cobra | https://github.com/spf13/cobra |
| Local DB | SQLite via `modernc.org/sqlite` | https://pkg.go.dev/modernc.org/sqlite |
| Server DB | PostgreSQL via `pgx` | https://github.com/jackc/pgx |
| HTTP router | Chi | https://github.com/go-chi/chi |
| Config | `gopkg.in/yaml.v3` | https://pkg.go.dev/gopkg.in/yaml.v3 |
| Dashboard | `html/template` + HTMX | https://htmx.org/docs/ |

## Project Structure (target)

```
dandori-cli/
├── cmd/
│   ├── root.go                # Cobra root, global flags
│   ├── init.go                # dandori init
│   ├── run.go                 # dandori run -- <agent>
│   ├── event.go               # dandori event (Layer 3)
│   ├── sync.go                # dandori sync
│   ├── status.go              # dandori status
│   ├── conf_write.go          # dandori conf-write
│   ├── version.go             # dandori version
│   └── server/
│       └── main.go            # Server entrypoint
├── internal/
│   ├── config/                # YAML config + env overrides
│   ├── db/                    # Local SQLite schema + helpers
│   ├── model/                 # Shared Go structs (Run, Event, etc.)
│   ├── wrapper/               # Fork/exec, signal forwarding, git capture
│   ├── tailer/                # Claude Code JSONL parser, cost computation
│   ├── event/                 # Event recorder (local.db writes)
│   ├── jira/                  # Jira REST client, poller, comments
│   ├── confluence/            # Confluence REST client, converter, reader/writer
│   ├── context/               # Context assembler (Confluence → agent context file)
│   ├── assignment/            # Scoring engine, history, suggestions
│   ├── analytics/             # Queries, aggregator, export
│   ├── sync/                  # Event batch uploader (CLI → server)
│   ├── server/                # HTTP server, routes, SSE, middleware
│   ├── serverdb/              # PostgreSQL connection, queries
│   └── util/                  # SHA-256 hash chain, helpers
├── dashboard/
│   ├── templates/             # HTML templates
│   └── static/                # CSS, HTMX
├── test/
│   ├── docker-compose.yml     # Postgres for integration tests
│   ├── e2e_test.sh
│   └── fixtures/
├── go.mod
├── go.sum
├── Makefile
├── README.md
└── CLAUDE.md                  # This file
```

## Coding Standards

- **Go conventions**: `gofmt`, `golangci-lint`, standard project layout
- **File naming**: `snake_case.go` (Go convention)
- **Error handling**: wrap errors with `fmt.Errorf("context: %w", err)`, never ignore errors
- **Testing**: table-driven tests, `testify` for assertions if needed
- **No CGO**: use pure Go libraries only (cross-compile friendly)
- **Packages**: one concern per package, no circular imports
- **Config**: all external values from config file or env vars, never hardcoded
- **Logging**: `slog` (stdlib), structured JSON in production, text in dev
- **Comments**: godoc format on exported functions, skip obvious ones

## Development Workflow

```bash
# Build
make build           # → ./bin/dandori

# Test
make test            # go test ./...
make lint            # golangci-lint run

# Run CLI
./bin/dandori init
./bin/dandori run -- echo "test"
./bin/dandori status

# Run server
./bin/dandori-server  # or: go run cmd/server/main.go

# Integration test (requires Docker)
make test-e2e        # docker-compose up + e2e_test.sh
```

## Key Design Principles

1. **Wrapper is non-negotiable** — Layer 1 (fork/exec) captures every run, even if Layer 2/3 fail
2. **CLI-heavy** — CLI does the work, server only aggregates and dashboards
3. **Jira IS the task board** — don't duplicate Jira features
4. **Confluence IS the knowledge store** — don't build a document system
5. **Cloud-first** — build for Atlassian Cloud API, Data Center support later
6. **Single binary** — `go build` produces one file, no runtime dependencies
7. **Offline-capable** — local SQLite works without server; sync when available

## When Task is Complete

1. `go test ./...` passes
2. `golangci-lint run` clean
3. Manual test: run the relevant `dandori` command and verify output
4. Check off items in the phase's todo list
5. Commit with conventional message: `feat:`, `fix:`, `refactor:`, `test:`, `docs:`
