# DORA + Rework Rate Export

`dandori metric export` computes 5 engineering metrics for a window and emits Faros / Oobeya / raw JSON. Source of truth = Jira (status transitions for deploys, issuetype/labels for incidents) + local SQLite (Layer-3 task iteration events for rework).

## What it measures

| Metric | Source | Definition |
|---|---|---|
| Deployment Frequency | Jira status transition into a release status | Distinct tickets entering a release status during window ÷ days |
| Lead Time for Changes | Jira changelog | Time from `In Progress → Released`, p50/p75/p90 (NIST linear interpolation) |
| Change Failure Rate | Jira incident query ÷ deploys | `incidents created in window ÷ deploys in window` (aggregate ratio, no per-deploy linkage) |
| Time to Restore Service | Jira incident `created → resolutiondate` | p50/p90; ongoing incidents reported separately, not in samples |
| Rework Rate | Local SQLite Layer-3 events | `runs with task.iteration.start (round ≥ 2) ÷ total runs`, threshold-flagged at 10% (strict `>`) |

## Quick start

```bash
# default: faros format, last 28d
dandori metric export

# explicit window + format
dandori metric export --format faros --since 28d
dandori metric export --format oobeya --since 2026-04-01
dandori metric export --format raw --output report.json

# scoped to a team
dandori metric export --team payments --since 7d
```

## Configuration

CLI flags drive window/team/format only. Status names + incident match come from `~/.dandori/config.yaml`:

```yaml
metric:
    release_status_names:    [Released, Deployed, Live, Done]   # ticket entered any of these = a deploy
    in_progress_status_names: ["In Progress"]                    # used as lead-time start
    incident_issue_types:    [Bug, Incident]                     # JQL: issuetype IN (...)
    incident_labels:         [prod-bug, incident]                # JQL: labels IN (...)
    jql_extra:               'AND project = PAY'                 # appended to deploy + incident queries
```

If `release_status_names` / `in_progress_status_names` are empty, defaults `{Released, Deployed, Live, Done}` and `{In Progress}` are used. **Incident config has no default** — both `incident_issue_types` and `incident_labels` empty → CFR + MTTR are skipped (logged in `data_quality.warnings`) so operators don't get false zeros.

## Output formats

- **`faros`** (default) — flat DORA schema with `metric_set: dora`, `period`, `metrics.{deployment_frequency,lead_time_for_changes,change_failure_rate,time_to_restore_service,rework_rate}`, `data_quality`. Insufficient data → `value: null` (NOT `0`), so dashboards can show "N/A" instead of charting a misleading zero.
- **`oobeya`** — same numbers regrouped under 6 layers (`productivity`, `delivery`, `quality`, `reliability`, `adoption`, `roi`). Adoption + ROI are placeholders, populated by future phases.
- **`raw`** — full report including `jira_config` echo (status names + incident filters + `jql_extra`) for reproducibility.

## Window flags

| Form | Meaning |
|---|---|
| `28d` (default) | last 28 days from now |
| `2026-04-01` | from that calendar day, 00:00 UTC |
| RFC3339 | exact instant, e.g. `2026-04-01T09:00:00Z` |
| `--until` | end of window (default `now`); same forms |

## Human Jira update assumption

The metrics rely on **humans transitioning Jira status promptly**. If a ticket is deployed to prod but never moved to `Released`, it won't count. The `data_quality` block reports two indicators:

- `tickets_without_in_progress` — deployed tickets with no `In Progress` transition (skipped from lead time)
- `insufficient_data` — list of metric IDs that lacked data (e.g. `change_failure_rate` if no deploys in window)

This is by design (single source of truth = Jira), but consumers of the export should treat anomalous numbers as a process signal first, a metric signal second.

## Threshold (Rework Rate)

The 10% threshold uses strict `>` (10 of 100 = NOT exceeding). `threshold_version: v1-2026Q2` lets dashboards detect threshold updates without re-keying.

## `--include-attribution` (G7, opt-in)

Adds a `task_attribution` block to all 3 formats. Aggregates over `task_attribution` rows whose `jira_done_at` lands in the export window.

```bash
dandori metric export --format faros --since 28d --include-attribution
```

Default OFF — without the flag the output is byte-for-byte identical to v0.5.0 dashboards.

| Field | Meaning |
|---|---|
| `tasks_total` | rows in window |
| `tasks_with_session` | tasks with at least one tracked agent session |
| `agent_autonomy_rate` | share of tasks with `intervention_rate < 0.2` |
| `agent_code_retention_p50` / `p90` | percentile of `lines_attributed_agent / (lines_attributed_agent + lines_attributed_human)` per task |
| `intervention_rate_p50` | percentile of per-task `human_intervention / (intervention + approval)` |
| `iterations_p50` / `p90` | per-task `task.iteration.start` count |
| `cost_per_retained_line_usd_p50` | `total_agent_cost_usd / lines_attributed_agent` (excludes tasks with 0 retained agent lines) |
| `session_outcomes` | merged histogram of `session_end_reason` (`agent_finished`, `user_interrupted`, `error`) |

Insufficient data semantics match v0.5.0: zero rows in window → block is `null` and `task_attribution` is added to `data_quality.insufficient_data`. In `oobeya`, the block is nested under `layers.productivity.task_attribution`.
