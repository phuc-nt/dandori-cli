# CLAUDE.md — dandori-cli

> Go CLI outer harness for managing AI agent dev teams. Wraps Claude Code, tracks runs, integrates Jira/Confluence, provides analytics for PO/PDM + QA.
>
> **Not** the full Dandori platform (`dandori/` — Node.js/Express). This is a separate, focused Go tool.

## Plan

All architecture, data model, phases, project structure, and tech stack are in:

**[`plans/260418-1301-dandori-cli/plan.md`](../plans/260418-1301-dandori-cli/plan.md)**

Phase files (01–08) in the same directory. Start with Phase 01. Each has implementation steps, file structure, todo checklist, success criteria.

Reference: [`dandori-pitch/cli-pilot-proposal.md`](../dandori-pitch/cli-pilot-proposal.md) for the original concept (3-layer instrumentation, data governance).

## Coding Standards

- **Go conventions**: `gofmt`, `golangci-lint`, standard project layout
- **File naming**: `snake_case.go`
- **Error handling**: `fmt.Errorf("context: %w", err)`, never ignore
- **Testing**: table-driven tests
- **No CGO**: pure Go only (cross-compile)
- **Logging**: `slog` (stdlib), structured JSON in prod
- **Config**: YAML + env overrides, never hardcode

## Dev Workflow

```bash
make build        # → ./bin/dandori
make test         # go test ./...
make lint         # golangci-lint run
make test-e2e     # docker-compose + integration test
```

## Design Principles

1. **Wrapper is non-negotiable** — Layer 1 captures every run even if Layer 2/3 fail
2. **CLI-heavy** — server only aggregates + dashboards
3. **Jira IS the task board** — don't duplicate
4. **Confluence IS the knowledge store** — don't duplicate
5. **Cloud-first** — Atlassian Cloud API first, Data Center later
6. **Single binary** — zero runtime dependencies
7. **Offline-capable** — local SQLite works without server

## When Done

1. `go test ./...` passes
2. `golangci-lint run` clean
3. Manual test the relevant `dandori` command
4. Check off items in the phase's todo list
5. Commit: `feat:`, `fix:`, `refactor:`, `test:`, `docs:`
