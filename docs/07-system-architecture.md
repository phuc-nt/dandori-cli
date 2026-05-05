# System Architecture — dandori-cli

> Tổng quan kiến trúc dandori-cli. Mục tiêu: đủ để onboard 1 maintainer mới hiểu hệ thống trong 15 phút. Chi tiết per-feature xem [reference/](reference/).

---

## 1. Overview

```
       External:  Jira · Confluence · Claude Code
                          ▲
                          │
   ┌──────────────────────┴──────────────────────┐
   │              CLI (engineer)                 │
   │                                             │
   │   cmd/  ──▶  Instrumentation (3 layers)     │
   │              │                              │
   │              ▼                              │
   │           SQLite local                      │
   │              │                              │
   │      ┌───────┼───────┬─────────┐            │
   │      ▼       ▼       ▼         ▼            │
   │   intent  attribution  metric  verify       │
   │              │                              │
   │              ▼                              │
   │            sync ───────┐                    │
   └────────────────────────┼────────────────────┘
                            │ batched events
                            ▼
   ┌─────────────────────────────────────────────┐
   │            Monitoring server                │
   │                                             │
   │   ingest + jira poller                      │
   │              │                              │
   │              ▼                              │
   │         Postgres (aggregate)                │
   │              │                              │
   │              ▼                              │
   │   Dashboard  (PO · QA · Eng · Audit)        │
   └─────────────────────────────────────────────┘
```

- **CLI** wrap Claude Code trên máy engineer, ghi mọi run vào SQLite local — hoạt động offline.
- **Server** nhận events từ CLI (`dandori sync`), poll Jira sprint, aggregate Postgres, render dashboard.
- **Jira IS task board · Confluence IS knowledge store** — dandori không duplicate, chỉ link bằng `jira_issue_key`.

Hai phần kế trình bày chi tiết từng nửa của diagram.

---

## 2. CLI internals

### 2.1 Three-layer instrumentation

Mọi `dandori run -- claude ...` đều đi qua 3 lớp song song. Layer 1 không bao giờ tắt — kể cả Layer 2/3 fail, run vẫn được track đủ cost-attribution + audit.

| Layer | Package | Bắt cái gì | Tắt được? |
|---|---|---|---|
| 1. **Wrapper** | `internal/wrapper/` | fork/exec, exit code, duration, cwd, git head before/after | Không |
| 2. **Tailer** | `internal/watcher/` | parse Claude session JSONL → tokens, cost, model, session_id | Tự động (best-effort) |
| 3. **Skill events** | `internal/event/` | semantic events agent emit (`intent.extracted`, `decision.point`, ...) | Có (agent cooperation) |

### 2.2 Package map

```
cmd/                  Cobra entrypoints (run · dashboard · analytics · audit · init · watch · ...)
internal/
  wrapper/  watcher/  event/      ← 3-layer instrumentation (xem 2.1)
  db/                              ← SQLite schema + queries
  intent/  attribution/  metric/   ← derived analytics (decision capture · line-blame · DORA)
  verify/                          ← quality gate (opt-in)
  jira/  confluence/               ← external clients
  analytics/  insights/             ← cost · alerts · bug hotspots
  assignment/                      ← suggest agent for Jira task
  watchctl/                        ← launchd / systemd-user orchestration
```

---

## 3. Server internals

| Component | Package | Vai trò |
|---|---|---|
| Event ingest API | `internal/server/` | nhận batched events từ `dandori sync` |
| Jira poller | `internal/jira/` (server mode) | sprint detection, task fetch |
| Aggregate store | `internal/serverdb/` | Postgres schema cross-engineer |
| Dashboard | `internal/server/` + `cmd/web/dashboard/` | htmx + ES6 modules, 5 persona views |

**Build constraint**: `dandori-server` binary tách bằng `-tags=server`. Cùng repo, khác entrypoint.

---

## 4. Data model

Bảng chính trong SQLite local (schema versioned, forward-only migrations qua `internal/db/`):

| Bảng | Vai trò |
|---|---|
| `runs` | 1 run = 1 lần wrap claude. Link `jira_issue_key`, cost, tokens, exit_code, git_head_before/after |
| `events` | Layer 3 semantic events (intent, decision, tool use) |
| `audit_log` | append-only, hash-chain (`prev_hash` / `curr_hash`) |
| `audit_anchors` | external anchor tới Confluence |
| `buglinks` | task→bug link, feed bug-hotspots widget |
| `task_attribution` | agent vs human line contribution per Jira task |
| `quality_metrics` | rework rate, autonomy rate per run |
| `metric_snapshots` | DORA cache (deploy freq, lead time, MTTR, change fail) |
| `assignments` | agent suggestion per Jira task |
| `jira_tasks` · `sprint_state` | Jira poller cache |
| `agent_configs` · `workstations` | engineer setup |
| `alerts_acked` | dashboard alert acknowledge state |
| `runs_v` | view: runs joined với latest metrics |

Server-side Postgres (`internal/serverdb/`) phản chiếu schema cho aggregation cross-engineer.

---

## 5. Data flow — 1 run điển hình

```
engineer: dandori task run CLITEST-12
    │
    ▼
[jira] fetch issue + linked Confluence pages → context bundle
    │
    ▼
[wrapper] fork claude, capture stdout, time exec
    │   ├── [tailer] parse ~/.claude/sessions/<id>.jsonl  → cost/tokens
    │   └── [event ] read stdin events from agent         → intent/decision
    ▼
[db] insert into runs + events + audit_log (hash-chained)
    │
    ▼
[verify] quality gate (semantic check, opt-in)
    │
    ▼
[attribution] git diff → line-blame agent vs human
    │
    ▼
[sync] batched POST → server → Postgres
    │
    ▼
[dashboard] PO/QA/Engineering/Admin/Audit views (htmx + ES6 modules)
```

---

## 6. Dashboard frontend

ES6 modules theo persona, no SPA bundler:

```
cmd/web/dashboard/
  ├── index.html       htmx + Chart.js UMD + module bootstrap
  ├── app.js           shell (URL state, persona switch, route handlers)
  └── widgets/
      ├── shared.js    helpers (escapeHtml, chartColors, ...)
      ├── qa.js        QA view renderers
      ├── audit.js     audit log + chain verify
      └── ...          po · tasks · engineering · admin (đang split dần)
```

Server dùng `//go:embed all:web/dashboard` — không cần build step.

---

## 7. Design principles (recap)

1. **Wrapper is non-negotiable** — Layer 1 luôn chạy
2. **CLI-heavy, server light** — server chỉ aggregate + dashboard
3. **Jira IS task board · Confluence IS knowledge store** — không duplicate
4. **Cloud-first** (Atlassian Cloud) — DC trên branch riêng
5. **Single binary** — pure Go, no CGO, cross-compile
6. **Offline-capable** — local SQLite hoạt động không cần server
7. **Append-only audit** — hash-chain, có external anchor option

---

## See also

- [plan.md](../../plans/260418-1301-dandori-cli/plan.md) — implementation phases gốc
- [reference/01-metric-export.md](reference/01-metric-export.md) — DORA + Rework Rate exporter
- [reference/02-agent-attribution.md](reference/02-agent-attribution.md) — agent vs human contribution
- [reference/03-intent-preservation.md](reference/03-intent-preservation.md) — intent extraction · incident-report
- [06-vision-and-roadmap.md](06-vision-and-roadmap.md) — vision + gap còn lại
