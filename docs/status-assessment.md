# dandori-cli Status Assessment

> Last updated: 2026-04-30 (v0.6.0)

## Vision vs Reality

| Vision (từ outer-harness.md) | Hiện trạng |
|------------------------------|------------|
| **Cost Attribution** — $180K bill, CFO asks "where did money go?" | ✅ **Done** — tokens, cost per run/agent/task/project |
| **Audit Trail** — agent committed bad code, who responsible? | ✅ **Done** — run logs, git HEAD before/after, Jira sync |
| **Task Tracking** — Jira IS the task board | ✅ **Done** — task run, jira-sync, status transitions |
| **Quality Gates** — lint, test before return | ✅ **Done** — pre-sync verify gate (v0.4.0), quality metrics, lint/test delta |
| **Multi-layer Knowledge Flow** — context/skills inheritance | ⚠️ **Partial** — Confluence read only, no skill library |
| **Enterprise Measurement** — DORA, attribution | ✅ **Done (v0.5.0)** — DORA + Rework Rate exporter, agent contribution attribution |

## Phase Completion

| Phase | Name | Status |
|-------|------|--------|
| 01 | Foundation (Go, CLI, SQLite, config) | ✅ Done |
| 02 | Agent Wrapper (3-layer instrumentation) | ✅ Done |
| 03 | Jira Integration (poller, transitions, comments) | ✅ Done |
| 04 | Confluence Integration (read/write) | ✅ Done |
| 05 | Monitoring Server (PostgreSQL, REST, dashboard) | ✅ Done |
| 06 | Agent Assignment (scorer, suggest, confirm) | ✅ Done |
| 07 | Analytics (8 query types, export) | ✅ Done |
| 08 | E2E Flow (integration tests) | ✅ Done |

**Bonus:**
- Quality Comparison (v0.3.0)
- Pre-sync verify gate + Layer-3 tracking + composite quality KPIs (v0.4.0)
- DORA + Rework Rate exporter (G6, v0.5.0)
- Agent contribution attribution (G7, v0.5.0)
- `dandori clean` for `go-build*` temp-dir remediation (v0.5.0)
- Intent preservation (G8, v0.6.0) — 3 event types + `dandori incident-report` + Jira G8 sections

## 5 Pillars Assessment

```
┌─────────────────────────────────────────────────────────┐
│  OUTER HARNESS PILLARS               Current State      │
├─────────────────────────────────────────────────────────┤
│  1. Cost Attribution                 ███████████ 100%   │
│     - Per-run, per-agent, per-task, per-project        │
│     - Model-specific pricing                            │
│     - Dashboard visualization                           │
│                                                         │
│  2. Multi-layer Knowledge Flow       ██████░░░░░  55%   │
│     ✅ Confluence read (context injection)              │
│     ✅ Confluence write (reports)                       │
│     ❌ Skill library (not in scope)                     │
│     ❌ Context inheritance (org → project → team)       │
│                                                         │
│  3. Task Tracking                    ███████████ 100%   │
│     - Jira polling, transitions, comments               │
│     - task run with full context                        │
│     - Comprehensive completion comments                 │
│                                                         │
│  4. Quality Gates                    ████████░░░  75%   │
│     ✅ Lint delta tracking                              │
│     ✅ Test delta tracking                              │
│     ✅ Commit quality scoring                           │
│     ❌ Pre-commit blocking (agent-side)                 │
│                                                         │
│  5. Audit & Analytics                ███████████ 100%   │
│     - Hash chain audit log                              │
│     - 8+ analytics queries                              │
│     - Dashboard with real-time data                     │
│     - Export CSV/JSON                                   │
└─────────────────────────────────────────────────────────┘
```

## Business Questions

| Question | Answerable? | Command |
|----------|-------------|---------|
| Tháng này team tốn bao nhiêu tiền API, chia theo project? | ✅ | `dandori analytics cost` |
| Agent commit code vi phạm security — ai chịu trách nhiệm? | ✅ | Jira comments + git audit |
| Migration làm sập staging — ai approve? | ✅ | Run logs + Jira history |
| Team A viết code tốt hơn hay tệ hơn Team B? | ✅ | `dandori analytics quality --compare` |
| Senior dev nghỉ — kinh nghiệm prompt ở đâu? | ⚠️ | Confluence reports only |

## Release History

| Version | Date | Highlights |
|---------|------|------------|
| v0.1.0 | 2026-04-18 | Initial release, 8 phases complete |
| v0.2.0 | 2026-04-19 | Context injection, enhanced Jira comments, cost calculation |
| v0.3.0 | 2026-04-19 | Quality metrics, agent comparison, lint/test delta |
| v0.4.0 | 2026-04-28 | Pre-sync verify gate, Layer-3 tracking, composite KPIs |
| v0.5.0 | 2026-04-30 | DORA + Rework Rate exporter (G6), agent contribution attribution (G7), `go-build*` leak hotfix |
| v0.6.0 | 2026-04-30 | Intent preservation (G8) — `intent.extracted`, `decision.point`, `agent.reasoning` events + `dandori incident-report` command + Jira G8 comment sections |

## Known Gaps

| Gap | Priority | Effort | Notes |
|-----|----------|--------|-------|
| Session detection timing | ✅ resolved | — | Symlink (2026-04-19) + post-exit drain + sandbox pre-existence (2026-04-25) |
| DORA + Rework Rate | ✅ resolved | — | Shipped v0.5.0 (G6) — 5 metrics, 3 wire formats |
| Agent contribution attribution | ✅ resolved | — | Shipped v0.5.0 (G7) — line-level blame + intervention classifier |
| Multi-agent orchestration (G1) | P1 | 16h | Codex/Copilot: pricing + session parser + side-by-side compare |
| Context inheritance (G2) | P2 | 8h | parent_run_id + per-ticket aggregation |
| Skill library (G3) | P3 | 24h | Layer-3 already tracks tools; need registry + sharing convention |
| Homebrew tap (G4) | ✅ resolved | — | PAT + `phuc-nt/homebrew-dandori` tap bootstrapped 2026-04-30 |
| Jira/Confluence DC (G5) | P2 | — | Tùng owns branch `feat/jira-confluence-datacenter` |
| Intent preservation (G8) | ✅ resolved | — | Shipped v0.6.0 — 3 event types, incident-report cmd, Jira G8 sections |
| ~~Spec-Driven Development (G7-new)~~ | ❌ dropped | — | Dropped 2026-04-30: human PO defines spec, not tool's job |

## Summary

**dandori-cli hoàn thành ~99% vision** của outer harness cho scope CLI pilot, cộng enterprise measurement layer (G6+G7 v0.5.0) và intent preservation (G8 v0.6.0).

- ✅ All 8 phases done + verify gate + DORA + attribution + intent preservation
- ✅ 5/5 business questions answerable + DORA-grounded leadership questions
- ✅ v0.6.0 published — full session intent + RCA via `dandori incident-report`
- ⚠️ Còn 3 gap active (G1 multi-agent, G2 context inheritance, G3 skill) + 1 Tùng-owned (G5 DC). G7-new SDD dropped — backlog trong [`plans/260429-0000-future-roadmap`](../../plans/260429-0000-future-roadmap/plan.md)

## Architecture

```
                    PO / QA
                      │
          Jira Sprint Board + Confluence docs
                      │
    ┌─────────────────▼──────────────────────┐
    │           MONITORING SERVER             │
    │  Event Ingest API  ←── batched events   │
    │  Aggregate DB (PostgreSQL)              │
    │  Dashboard UI (real-time + historical)  │
    │  Jira Poller (sprint detection)         │
    │  Assignment Engine (suggest agent)      │
    └─────────────────┬──────────────────────┘
                      │
    ┌─────────────────▼──────────────────────┐
    │        ENGINEER WORKSTATION             │
    │  dandori CLI (Go binary)                │
    │    ├── dandori task run KEY             │
    │    ├── dandori run -- claude ...        │
    │    ├── dandori jira-sync                │
    │    ├── dandori conf-write               │
    │    ├── dandori analytics {runs,cost,all}│
    │    ├── dandori demo (seed/restore)      │
    │    └── dandori dashboard                │
    │                                         │
    │  ~/.dandori/                            │
    │    ├── config.yaml                      │
    │    └── local.db (SQLite)                │
    └─────────────────────────────────────────┘
```

## Next Steps

1. ✅ **Token capture** — resolved 2026-04-25.
2. ✅ **Pre-sync verify gate** — shipped v0.4.0 (semantic + quality check warn-mode).
3. ✅ **DORA + Rework Rate exporter (G6)** — shipped v0.5.0.
4. ✅ **Agent contribution attribution (G7)** — shipped v0.5.0.
5. ✅ **Intent preservation (G8)** — shipped v0.6.0.
6. **G7 v1 fixes validation** — 1-week-out check on UTC normalize + no-commit warning ([`plans/260430-1149-g7-followup-validation`](../../plans/260430-1149-g7-followup-validation/plan.md)).
7. **Pick next gap** from backlog (G1 multi-agent, G2 context inheritance, G3 skill).
