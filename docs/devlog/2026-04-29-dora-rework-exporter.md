# 2026-04-29 — DORA + Rework Rate Exporter (G6)

Ship `dandori metric export` — 5 engineering metrics from Jira + local SQLite, in 3 wire formats (Faros / Oobeya / raw).

## Architectural decisions

- **Q1 (incident source)**: Jira-only. No webhook/CI ingest — keeps SoT single, dogfoods the human-update assumption.
- **Q2 (deploy signal)**: Jira status transition into a release status. JQL `status was in (...) DURING (start, end)` narrows the candidate set; full filtering done per-ticket via changelog inspection (JQL function granularity is unreliable across Cloud/DC).
- **Q3 (rework threshold)**: 10% over 28d, **strict `>`**. Cancelled runs kept in denominator (you intended the work). Threshold version stamped (`v1-2026Q2`) for dashboard reconciliation.

## Implementation

6 phases on branch `feat/dora-rework-exporter`:

| Phase | Adds |
|---|---|
| 01 | SQLite migration v3→v4 + `metric_snapshots` table |
| 02 | Rework Rate calculator from Layer-3 `task.iteration.start` events |
| 03 | Deploy frequency + Lead Time from Jira changelog (`In Progress → Released`) |
| 04 | CFR + MTTR from Jira incident query (issuetype OR labels) |
| 05 | `dandori metric export` Cobra subcommand + 3-format orchestrator |
| 06 | E2E httptest mock + dogfood live validation |

## Key code shapes

- `internal/metric/{deployment,cfr,mttr,rework,export,format}.go` — narrow `jiraDeploySource` / `jiraIncidentSource` interfaces; orchestrator composes with `ExportSources{Jira, Rework}`.
- `internal/jira/{changelog,incidents}.go` — `GetIssueChangelog(key)` returns chronologically-sorted `[]StatusChange`; `SearchIncidents(jql, max)` decodes only `summary/issuetype/labels/created/resolutiondate` to keep hot path lean.
- `Run()` orchestrator: sequential by choice — bottleneck is per-issue changelog fetch (deploy/lead share); parallelizing would only save rework + cfr/mttr legs (~30%) at the cost of error-handling complexity. Phase 06+ benchmarks; revisit then.

## Insufficient data semantics

When CFR/MTTR/Rework can't run, formatters emit `"value": null` (not `0`). Dashboards that gate on `null` show "N/A" instead of charting a misleading zero. Three triggers:
- Empty incident config → CFR + MTTR skipped, warning logged
- No deploys in window → `insufficient_data: ["change_failure_rate"]`
- `src.Rework == nil` → `insufficient_data: ["rework_rate"]`

## Live test (dogfood)

Real Jira (`fooknt.atlassian.net`, project `CLITEST2`), 28d window:

```
deploys=42 (1.5/day) · lead p50=310s · CFR=0.19 (8/42) · MTTR p50=130min ongoing=4 · rework=0.7% (1/137)
```

`tickets_without_in_progress=9` correctly flagged. CFR/MTTR/Rework all populated; no `insufficient_data` entries when incident config is set.

## Edge cases live-tested

- Invalid format (`--format zzz`) → exit 1, single error line (no double-print after `SilenceUsage`/`SilenceErrors`)
- Team filter with no matches → `rework_rate: 0/0` cleanly
- Absolute date (`--since 2026-04-15`) → window flag accepts `Nd` / `YYYY-MM-DD` / RFC3339 / `now`
- `--until before --since` → rejected with explicit error

## Tests

41 unit + 4 E2E tests (httptest mock Jira + temp SQLite). E2E covers: 20-deploy/3-incident happy path with exact-value asserts (CFR=0.15, rework boundary 0.10 strict-`>`), all-empty (faros body must contain `"value": null`), Jira 500 propagation.

## Output files

- `internal/metric/` (8 files, ~1100 LOC)
- `internal/jira/changelog.go`, `internal/jira/incidents.go`
- `cmd/metric.go`
- `internal/config/config.go` (added `Metric MetricConfig`)
- `docs/metric-export.md` (user guide)
