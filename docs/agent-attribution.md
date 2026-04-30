# Agent Contribution Attribution (G7)

Answers the question: **how much of a Jira task did the AI agent actually contribute, vs. the human?** Persisted as a row in the `task_attribution` table the moment a Jira ticket transitions to Done, then surfaced through `dandori metric export --include-attribution`.

## Definitions

- **Agent session** — one `dandori run` invocation. Bounded by `git_head_before` (HEAD when the wrapper started) and `git_head_after` (HEAD when it exited). Stored in `runs`.
- **Intervention** — a human text message ≥30 characters sent **after** the agent has used at least one tool in the session. Heuristic proxy for "the human had to step in to redirect the agent."
- **Approval** — a human text message <30 characters sent after the agent has used at least one tool. Heuristic proxy for "looks good", "proceed", etc.
- **Lines attributed agent** — at the final HEAD when the ticket went Done, lines whose introducing commit (`git blame`) is reachable from any session's `HeadBefore..HeadAfter`.
- **Lines attributed human** — lines whose introducing commit is reachable from any session's `HeadAfter..finalHead` range. Pre-session baseline lines are excluded from totals.
- **Intervention rate** — `interventions / (interventions + approvals)` over the task. Zero denominator (no human messages after agent tool use, e.g. one-shot `claude -p`) → rate = 0.
- **Agent autonomy rate** — fraction of tasks whose intervention rate is strictly **<0.2**. Tasks at exactly 0.2 do not count as autonomous.

## Output schema

`dandori metric export --include-attribution` adds a `task_attribution` block:

```json
{
  "task_attribution": {
    "tasks_total": 12,
    "tasks_with_session": 10,
    "agent_autonomy_rate": 0.6,
    "agent_code_retention_p50": 0.78,
    "agent_code_retention_p90": 0.95,
    "intervention_rate_p50": 0.15,
    "iterations_p50": 1,
    "iterations_p90": 3,
    "cost_per_retained_line_usd_p50": 0.012,
    "session_outcomes": { "agent_finished": 9, "user_interrupted": 1 }
  }
}
```

In `oobeya` the block is nested under `layers.productivity.task_attribution`. In `raw` it sits at the top level alongside the other metric blocks.

Without the flag, output is byte-for-byte identical to v0.5.0.

## Limitations (be honest)

1. **Format reflow can mask retained lines.** `gofmt` / `prettier` runs after a session rewrite every line and reattribute them to the formatter's commit. Workarounds: run formatters in the same session as the agent edits, or land formatter passes in the sessions that originated the code.
2. **Cross-repo work isn't attributed.** A session that only edits a sibling repo (e.g. infra) records `git_head_*` from the wrapper's CWD repo and won't trace blame across repos.
3. **The 30-character intervention threshold is a heuristic, not ground truth.** A short corrective ("no, use POST not GET") is technically an intervention but counts as approval. A long approval ("looks great, please also write the tests when you have time") counts as intervention. Aggregate trend is informative; per-task interpretation needs a human read of the transcript.
4. **Cross-repo / orphan-sha sessions are silently skipped.** The wrapper records `git_head_*` from the session's CWD repo. If those shas aren't reachable from the repo `ComputeAndPersist` is invoked against (sibling repo, pruned branch, another machine's workspace), that session contributes nothing to the row — but the row is still written for any sessions that *did* land here. Errors don't crash the Jira flow.
5. **Zero-signal tasks don't inflate autonomy.** A task with no classified human messages (one-shot `claude -p`, or runs predating G7's classifier) is excluded from both the autonomy numerator and denominator. The aggregate flips to `insufficient_data` if *every* row in the window is zero-signal — preventing a misleading "100% autonomy, 0% retention" report when the real answer is "we don't have data."

## Six questions this answers

| Question | How |
|---|---|
| What share of the codebase did the agent contribute over the last 28d? | `agent_code_retention_p50` × `tasks_with_session` |
| Which tasks did the agent finish without human course-correction? | `intervention_rate < 0.2`, count via `agent_autonomy_rate` |
| What's the cost-per-retained-line trend? | `cost_per_retained_line_usd_p50` over rolling windows |
| How often does an agent need >1 iteration? | `iterations_p50` / `iterations_p90` |
| What fraction of sessions ended via user interrupt vs agent finish? | `session_outcomes` histogram |
| For a specific task: lines kept vs replaced? | `SELECT lines_attributed_agent, lines_attributed_human FROM task_attribution WHERE jira_issue_key='X'` |

## How to use

```bash
# 28-day rolling agent-vs-human picture
dandori metric export --since 28d --include-attribution --format faros

# raw block for a one-off task
sqlite3 ~/.dandori/local.db \
  "SELECT * FROM task_attribution WHERE jira_issue_key='CLITEST2-42';"
```

Attribution is computed automatically by `dandori task run` and `dandori task done` BEFORE the Jira transition, so by the time a ticket lands in Done, the row is already written. Compute failures are non-fatal — observability must never block the Jira move.
