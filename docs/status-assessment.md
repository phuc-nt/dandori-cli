# dandori-cli Status Assessment

> Last updated: 2026-04-20

## Vision vs Reality

| Vision (từ outer-harness.md) | Hiện trạng |
|------------------------------|------------|
| **Cost Attribution** — $180K bill, CFO asks "where did money go?" | ✅ **Done** — tokens, cost per run/agent/task/project |
| **Audit Trail** — agent committed bad code, who responsible? | ✅ **Done** — run logs, git HEAD before/after, Jira sync |
| **Task Tracking** — Jira IS the task board | ✅ **Done** — task run, jira-sync, status transitions |
| **Quality Gates** — lint, test before return | ✅ **Done** — quality metrics, lint/test delta |
| **Multi-layer Knowledge Flow** — context/skills inheritance | ⚠️ **Partial** — Confluence read only, no skill library |

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

**Bonus:** Quality Comparison (v0.3.0)

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

## Known Gaps

| Gap | Priority | Effort | Notes |
|-----|----------|--------|-------|
| Session detection timing | P1 | 4h | Symlink fixed, tailer timing remains |
| Multi-agent orchestration | P2 | 16h | Current: 1 agent per task |
| Context inheritance | P2 | 8h | org → project → team hierarchy |
| Skill library | P3 | 24h | Out of scope for CLI pilot |
| Homebrew tap | P3 | 2h | Missing homebrew-dandori repo |

## Summary

**dandori-cli hoàn thành ~88% vision** của outer harness cho scope CLI pilot.

- ✅ All 8 phases done
- ✅ 5/5 business questions answerable
- ✅ v0.3.0 published với quality metrics
- ⚠️ Skill library và context inheritance chưa có (out of scope)

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
    │    ├── dandori analytics                │
    │    └── dandori dashboard                │
    │                                         │
    │  ~/.dandori/                            │
    │    ├── config.yaml                      │
    │    └── local.db (SQLite)                │
    └─────────────────────────────────────────┘
```

## Next Steps

1. **Fix remaining tailer issues** — ensure 100% token capture
2. **Improve E2E test reliability** — currently 82.9% pass rate
3. **Consider multi-agent** — for complex tasks requiring parallel work
4. **Document real-world usage** — collect feedback from actual PO/QA usage
