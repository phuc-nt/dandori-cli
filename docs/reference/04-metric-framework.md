# AI-Agent Productivity Metric Framework

> Defensible metric set + formulas for the outer-harness model (agents code, humans review). Anchored in DORA Four Keys, SPACE, and DevEx; extended with AI-specific measures (acceptance rate, rework, cost-per-task).

**Audience**: solo engineer using dandori today, stakeholders auditing at enterprise scale tomorrow.
**Source data**: everything below uses fields already captured by dandori-cli (runs, Jira lifecycle, line-blame, cost). Gaps explicitly flagged in §6.

## 1. The 12-metric set

Grouped by 4 dimensions. Each row: **formula → why this one → target type (KR vs diagnostic) → framework**.

### Delivery throughput

| Metric | Formula | Why | Target type | Source |
|---|---|---|---|---|
| Deployment Frequency | `deploys_in_window / window_days` | Validates iteration speed; hard to game | **KR** | DORA |
| Lead Time for Changes | `median(done_date − created_date)` | Agility signal; lower compounds | Diagnostic | DORA |
| Completed Tasks/Week | `Σ(tasks.status=Done) / 7` | Raw capacity, pair with cost | **KR** | DORA Activity |

### Delivery stability

| Metric | Formula | Why | Target type | Source |
|---|---|---|---|---|
| AI Change Failure Rate (AI-CFR) | `(reopened_within_7d + reverted_post_deploy) / merged_PRs` | Quality gate; classic DORA redefined for AI workflows | **KR (careful)** | DORA, modified |
| Rework Rate | `lines_modified_within_7d_of_merge / merged_lines` | Catches failures masked by rapid follow-ups | Diagnostic, alarm > 25% | AI-CodeGen |
| Recovery Time | `median(incident_created → resolved)` hours | Operational resilience | Diagnostic | DORA |

### Efficiency & flow

| Metric | Formula | Why | Target type | Source |
|---|---|---|---|---|
| Cost per Accepted Task | `(Σ tokens × model_rate) / tasks_accepted` | Productivity-per-$, not just velocity | **KR** | Custom |
| Human Intervention Rate | `(human_edits + rejections) / runs` | Reveals prompt quality; > 30% = poor agent fit | Diagnostic | Custom |
| PR Review Cycle Time | `median(PR_submitted → first_approval)` hours | Feedback-loop speed (SPACE/DevEx) | Diagnostic | SPACE |

### Code quality & attribution

| Metric | Formula | Why | Target type | Source |
|---|---|---|---|---|
| Code Acceptance Rate | `lines_retained / lines_generated` | Copilot research's best productivity proxy (ρ=0.24) | **KR** | AI-CodeGen |
| Test Coverage (AI lines) | `lines_tested / lines_generated` | Track, never target — Goodhart magnet | Diagnostic | SPACE |
| Line-Blame Rework % | `blame_human ÷ blame_agent` on lines reworked within 30d | Sustainability signal for agent code | Diagnostic (limit caveat in §5) | AI-Attribution |

## 2. KR vs diagnostic — and why

**KRs** (4 of them): process measures that improve only through real wins. Hard to game at solo scale.

- Deployment Frequency, Cost per Accepted Task, Code Acceptance Rate, AI-CFR.

**Diagnostics** (8 of them): signal but should not be targeted directly. Targeting invites:

- *Test Coverage* → meaningless tests.
- *Human Intervention* → insufficient review.
- *Rework Rate* → "let it pass broken, fix fast" culture.
- *Lead Time / Recovery* → external factors (infra, deps) dominate.

Goodhart's Law summary: when a measure becomes a target, it ceases to be a good measure. LOC, commits/day, story points are forbidden as KRs in AI workflows — they invite quantity-over-quality gaming.

## 3. Trust Index — composite KR

A single weighted composite to drive the biggest decision: **how much autonomy to grant the agent**.

```
Trust = (Code_Acceptance × 0.40)
      + ((1 − AI_CFR)    × 0.35)
      + ((1 − Intervention_Rate) × 0.25)

Worked example:
  Acceptance 78%, CFR 15%, Intervention 25%
  → 0.78·0.4 + 0.85·0.35 + 0.75·0.25 = 0.312 + 0.298 + 0.188 = 79.8/100
```

Decision bands:

| Trust | Posture |
|---|---|
| ≥ 80 | Agent owns complex features; human reviews at PR stage only |
| 60–79 | Co-own; pair design review; human validates approach before code |
| < 60 | Human leads; agent assists (Copilot mode); investigate failure modes first |

Why composite: one metric hides trade-offs. A "fast but fragile" agent (high deploy, high rework) and a "reliable but slow" one (low deploy, low rework) look identical on most single dimensions. Trust Index separates them.

## 4. Effective Velocity — secondary composite

Business value per dollar; resists LOC gaming.

```
Effective Velocity = Σ story_points_accepted / total_cost_$
```

Industry range for AI-assisted teams: **0.08–0.20 points/$**. Reasonable goal once baselined: > 0.15.

## 5. Goal-setting cadence (solo engineer)

Suggested targets — calibrate baseline from your own first sprint of data.

| Metric | Week 1 baseline | Week 4 | Week 8 | Why this gradient |
|---|---|---|---|---|
| Deployment Frequency | 2/week | 3/week | 5/week | Process improvement; CI/CD tuning |
| Code Acceptance Rate | 60% | 65% | 72% | Prompt refinement from rejection learnings |
| Cost per Accepted Task | $15 | $14.25 | $13.50 | Token efficiency (better context = fewer tokens) |
| AI-CFR (reopened within 7d) | 25% | 20% | 17% | Test/lint rigor; agent learning from failures |
| **Trust Index** | 62 | 67 | 73 | Composite; tracks autonomy unlock |

Diagnostics to monitor (not target): Human Intervention 35% → 28%, Rework Rate < 25% as alarm.

## 6. Data dandori-cli already has vs gaps

### Already captured

- Per-run: agent name, task type, duration, cost, success/fail, failure root cause, human intervention count, prompt/completion tokens — `internal/runner/`, `internal/store/`.
- Per-task: Jira lifecycle events, reopened-after-merge — `internal/jira/`.
- Line-blame (G7): agent vs human authorship per line — `internal/attribution/`.
- DORA exports (G6): deployment freq, lead time, CFR, MTTR, rework rate — `internal/metric/`.
- RCA breakdown (G8): intent preservation, incident-report — `internal/intent/`.
- Weekly aggregates: success rate, rework rate, cost/run, slope.

### Gaps, ranked by ROI

| Gap | Impact | Effort | Notes |
|---|---|---|---|
| PR review latency (`review_requested_at → first_approval_at`) | High | Trivial (~1 line at PR webhook) | Unlocks PR Review Cycle Time + correlates with lead time |
| Code Acceptance Rate (lines retained / generated) | High | Medium (extend G7 line-blame for delta on merge) | Unlocks single most-cited AI productivity metric |
| Trust Index composite endpoint | High | Low (pure analytics on existing fields) | Unlocks composite KR + dashboard tile |
| Intervention classification (rejection vs minor-fix vs design-question) | Medium | Low (extend `failure_root_cause` enum) | Sharpens prompt-quality signal |
| Per-agent calibration (success rate by agent × task type) | Medium | Low (data exists, add query) | Useful when 2nd agent joins |
| Semantic complexity proxy (cyclomatic, nesting depth) | Medium | Medium (lint pass at merge) | Defer until 8 weeks of data |
| Production incident attribution | Medium | High (needs incident pipeline) | Enterprise concern, not solo |

## 7. Open debates (field hasn't agreed)

1. **Line-blame fairness** — Mesa AgentBlame, Cursor Blame show line-level attribution is feasible. But penalising agents for bugs human reviewers missed is unfair. → Track for RCA only; never use for evaluation.

2. **CFR redefinition for AI** — DORA's original "production incident" is too late for AI workflows; rework happens in hours, not days. → Define as *merged-but-reverted within 7 days* and label it **AI-CFR** to avoid confusion with DORA stock.

3. **Token-count predictiveness** — Copilot research finds acceptance rate (ρ=0.24) is the productivity proxy; tokens don't show up. → Collect tokens/task as drift signal ("token inflation = prompt drift"), don't optimise directly.

4. **Multi-agent credit split** — Unsolved. For solo, credit the run's agent and move on. Revisit when 2nd agent joins.

5. **Trust Index portability across domains** — Hypothesis: weights may differ for frontend vs backend vs infra tasks. Validate when you have ≥ 3 task categories of data.

## 8. Validation matrix — what the metric story means

Use this when reading the dashboard to interpret combinations:

| Symptom | Diagnosis |
|---|---|
| High deploy freq + low acceptance | Prompt is off-target; agent ships the shape, not the quality |
| Low deploy freq + high acceptance | Over-engineering or ambiguous context; agent plays it safe |
| High AI-CFR + high rework | Tests/lint inadequate; not an agent problem |
| Rising cost/task + flat Effective Velocity | Token inflation or task drift; audit prompts |
| High Trust Index + low Deploy Freq | Bottleneck is process (CI, review), not agent capability |

## 9. Implementation phasing for dandori-cli

| Phase | Metric | Status | Module |
|---|---|---|---|
| v0.12 (next) | Code Acceptance Rate | ❌ → add | extend `internal/attribution/` (uses existing `task_attribution.jira_done_at`) |
| v0.12 (next) | **Trust Index composite** | ❌ → add | new `internal/analytics/trust.go` + dashboard tile |
| v0.13 | PR Review Cycle Time | ❌ → add | Jira/GitHub webhook capture |
| v0.13 | Intervention classification | ❌ → add | extend `failure_root_cause` enum |
| v0.13 | AI-CFR (true "reverted within 7d") | ⚠️ proxy now | upgrade once PR/deploy events captured |
| Existing | Deployment Frequency, Rework Rate, MTTR, Cost/Run | ✅ | `internal/metric/` G6 |
| Existing | AI-CFR (proxy: `total_iterations>1`) | ⚠️ partial | `task_attribution.total_iterations` from G7 — see note below |
| Existing | Human Intervention Rate | ✅ | G7 `internal/attribution/` |
| Existing | RCA breakdown | ✅ | G8 `internal/intent/` |

### Note on AI-CFR proxy (v0.12 interim)

The "true" AI-CFR per §1 is `(reopened_within_7d + reverted_post_deploy) / merged_PRs`. dandori-cli today captures **iteration count per task** (`task_attribution.total_iterations`) but not PR-level reverts or post-deploy events. For v0.12, Trust Index uses the **proxy** `SUM(total_iterations>1) / COUNT(tasks)` over the window. This:

- ✅ Catches tasks that needed rework before being marked Done.
- ❌ Misses bugs that escape to production and get reverted after Done.

The proxy understates AI-CFR (false-negative bias). When `internal/jira/` + a future PR webhook capture revert events (v0.13), the formula upgrades to the §1 definition without changing the Trust Index weights.

## Sources

1. [DORA Four Keys Metrics Guide](https://dora.dev/guides/dora-metrics-four-keys/)
2. [DORA Thresholds: Elite/High/Medium/Low (DX)](https://getdx.com/blog/dora-metrics/)
3. [SPACE Framework: Five Dimensions](https://space-framework.com/)
4. [DevEx: What Actually Drives Productivity (ACM Queue)](https://queue.acm.org/detail.cfm?id=3595878)
5. [Measuring GitHub Copilot's Impact (CACM)](https://cacm.acm.org/research/measuring-github-copilots-impact-on-productivity/)
6. [Measuring AI Code Assistants and Agents (DX Research)](https://getdx.com/research/measuring-ai-code-assistants-and-agents/)
7. [Goodhart's Law in Software Engineering (Jellyfish)](https://jellyfish.co/blog/goodharts-law-in-software-engineering-and-how-to-avoid-gaming-your-metrics/)
8. [AgentBlame: Line-Level AI Attribution (Mesa)](https://www.mesa.dev/blog/agentblame-deep-dive)
9. [Cost Per Story Point Antipatterns (Mountain Goat)](https://www.mountaingoatsoftware.com/blog/is-it-dangerous-to-calculate-the-cost-per-point)

## Related

- [01-metric-export.md](01-metric-export.md) — G6 DORA exporter (provides Deployment Freq, AI-CFR, Rework, MTTR raw)
- [02-agent-attribution.md](02-agent-attribution.md) — G7 line-blame (provides Human Intervention Rate; will provide Code Acceptance Rate in v0.12)
- [03-intent-preservation.md](03-intent-preservation.md) — G8 incident-report (provides RCA breakdown feeding diagnostic dashboard)
