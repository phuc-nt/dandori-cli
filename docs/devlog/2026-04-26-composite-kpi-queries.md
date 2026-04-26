# 2026-04-26 — Composite KPI queries (Phase 01)

## Summary

Added three composite quality KPIs joining Layer-3 events (`task.iteration.start`, `bug.filed`) with run cost data. Exposed via new `dandori analytics kpi` subcommand and a `[5] QUALITY KPI` block in `dandori analytics all`.

## What shipped

### New files
- `internal/db/quality_kpi.go` — three query funcs + row types:
  - `RegressionRate(groupBy, sinceDays)` → `[]RegressionRow`
  - `BugRate(groupBy, sinceDays)` → `[]BugRateRow`
  - `QualityAdjustedCost(groupBy, sinceDays, top)` → `[]TaskCostRow`
  - `resolveGroupCol` helper (DRY dimension switch, mirrors `IterationStats`)
- `internal/db/quality_kpi_test.go` — 13 unit cases (9 dim×KPI + 4 edge cases)
- `cmd/analytics_quality.go` — `analytics kpi` subcommand with `--kpi regression|bugs|cost`, `--by agent|engineer|sprint`, `--since`, `--top`, `--format table|json`

### Modified files
- `internal/analytics/all.go` — `QualityKPIBlock` struct, `Snapshot.QualityKPI` field, `BuildSnapshot` calls all 3 funcs (agent-only, top 10), `FormatTable` renders `[5] QUALITY KPI` section
- `internal/analytics/all_test.go` — 2 new integration tests (`TestBuildSnapshot_IncludesQualityKPI`, `TestFormatTable_QualityKPIBlock`)

## Notes

- Command uses `analytics kpi` (not `analytics quality`) to avoid conflict with the pre-existing `analytics quality` code-quality subcommand in `analytics.go`.
- `analytics all` block is agent-only per Q1 decision (keeps snapshot scannable). Full 3-dim matrix available via `analytics kpi --by engineer|sprint`.
- LOC: ~440 production + ~378 test = ~818 total new. Exceeds plan's 550 budget (budget likely targeted production code only; test coverage for 13 cases is non-negotiable).

## Tests

- 13 unit tests in `quality_kpi_test.go` — all green
- 2 integration tests in `all_test.go` — all green
- Full `go test ./...` — clean

## Plan

Plan: `plans/260426-1354-composite-kpi-queries/`
Phase: `phase-01-quality-kpi-queries-and-cli.md`
