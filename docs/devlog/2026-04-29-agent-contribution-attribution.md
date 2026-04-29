# 2026-04-29 — Agent Contribution Attribution (G7)

Ship `dandori metric export --include-attribution` — per-task accounting of which lines came from the AI agent vs the human, plus aggregate intervention/iteration/cost percentiles.

## Why now

After v0.5.0 (DORA exporter) shipped, the missing question was "how much of this team's velocity is the agent vs the human?" Without it, a 5x throughput claim is unverifiable — so the value the platform promises (PO + agent + QA, no human devs) can't be justified to stakeholders. G7 closes that gap with hard numbers: lines kept, intervention rate, cost per retained line.

## Architectural decisions

- **Q1 (line-level attribution)**: `git blame` at finalHead. Each line's introducing commit is membership-tested against the union of session-reachable commits (`rev-list HeadBefore..HeadAfter`). Pre-session baseline lines are excluded — totals only count what changed during the task. **Why blame and not diff parsing**: blame already does the hard work of tracking which commit a line belongs to even after intermediate edits.
- **Q2 (intervention classifier)**: 30-character threshold heuristic ("≥30 chars after agent tool_use = intervention, <30 = approval"). Documented as v1 proxy in `docs/agent-attribution.md`. **Why heuristic v1**: ground-truth labelling needs an LLM-as-judge pass which is its own scope; the proxy is good enough to surface trends and cheap enough to ship now.
- **Q3 (when to compute)**: BEFORE the Jira `TransitionToDone` call in both `task run` (auto-flow) and `task done` (manual). Failure is non-fatal — observability must never block a Jira move. **Why before, not after**: snapshot reflects the tree state the human is signing off on; running it post-transition risks racing with subsequent edits.
- **Q4 (insufficient-data semantics)**: matches v0.5.0 — empty window → block is `null`, `task_attribution` listed in `data_quality.insufficient_data`. Backwards-compat: without the flag, output is byte-identical to v0.5.0.

## Implementation

5 phases on branch `feat/agent-contribution-attribution`:

| Phase | Adds |
|---|---|
| 01 | SQLite migration v4→v5: 5 new `runs` columns + `task_attribution` table + `sessionEndReason()` (exit code → reason) |
| 02 | Transcript message classifier (intervention vs approval) + per-run aggregator + persist to runs |
| 03 | `internal/attribution/{retention,compute}.go` — git-blame line attribution + `ComputeAndPersist` upsert; hooked into `cmd/task_run.go` + `cmd/task.go` BEFORE `TransitionToDone` |
| 04 | `internal/metric/attribution.go` — `AggregateAttribution` + `--include-attribution` flag injecting `task_attribution` block into faros/oobeya/raw |
| 05 | E2E integration test (5x flake-free), `docs/agent-attribution.md`, case-study template |

## Key code shapes

- **`internal/attribution/retention.go`** — `ComputeRetention(repoPath, sessions, finalHead) → RetentionResult`. For each file in the union of session-touched files: blame at finalHead → for each line, classify SHA into agent / human / pre-session-baseline. Pure function over git, no DB.
- **`internal/attribution/compute.go`** — `ComputeAndPersist(d, jiraKey, repoPath, finalHead)`. Loads done/error runs for the key, calls retention, aggregates session counters (tokens, cost, iterations, message counts, outcomes histogram), upserts `task_attribution`. UPSERT keeps re-runs idempotent.
- **`internal/wrapper/{intervention_classifier,message_counter}.go`** — stateful walk over the JSONL transcript: tracks `seenAgentTool` so initial framing isn't classified as intervention; only first text part of a user line counts (continuation parts are noise).
- **`internal/metric/attribution.go`** — query `task_attribution` rows where `jira_done_at` falls in window, compute percentiles via the existing `percentile()` helper, merge `session_outcomes` histograms.

## Insufficient-data + backcompat

| Trigger | Behavior |
|---|---|
| `--include-attribution` not passed | Block absent, output byte-identical to v0.5.0 |
| Flag passed, zero rows in window | Block is `null`, `task_attribution` added to `data_quality.insufficient_data` |
| Flag passed, rows present but every task lacks tracked lines (deleted files) | Percentiles emit 0 (existing `pctOrZero` semantics); aggregate counts still populate |

## Live test

E2E integration test (`internal/metric/integration_attribution_test.go`) drives the full chain on a temp git repo + temp SQLite at v5: agent commits `func A`, human appends `func H`, `ComputeAndPersist` writes the row, `AggregateAttribution` reads it back. Asserts retention=0.5 (one agent line, one human line) and autonomy=0 (intervention_rate exactly 0.2 fails the strict <0.2 threshold). 5x consecutive runs all green.

## Next

The 5-task case study against real CLITEST2 dogfood data is the manual gate before merging. Template in `plans/260429-1240-agent-contribution-attribution/case-study.md`. If 2+ tasks exceed 10% diff vs manual ground truth, the heuristic needs tightening before ship.
