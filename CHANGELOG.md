# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.9.1] — 2026-05-03

Polish on top of v0.9.0 — adds `dandori doctor` for ongoing health checks and sweeps stale legacy mentions from docs.

### Added

- **`dandori doctor`** — health check: config completeness, `claude` binary in PATH, SQLite DB writability, Jira `/myself` reachable, Confluence space readable. Exit 0 if all green, 1 otherwise. Useful after token expiry, space rename, or before raising a support ticket. 11 unit tests cover each probe.

### Fixed

- **Goreleaser** — `dandori-server` build needs `-tags=server` (build constraint added in v0.8.1 perf split). Without it, `goreleaser release` failed at the build step.
- **README badges + Tech Stack table** — Go version `1.21+` → `1.26+` (matches `go.mod` directive added in v0.8.1).
- **Docs stale references** (sweep):
  - `01-setup-guide.md` Quick Start — removed "via shell alias" example (was already removed in init flow but doc still showed it)
  - `01-setup-guide.md` Background Capture — collapsed manual `dandori watch --once` + `launchctl submit` steps, promoted `dandori watch enable`
  - `01-setup-guide.md` Verify Setup — replaced manual `task info` / `conf-write --dry-run` probes with `dandori doctor`
  - `02-user-guide.md` Use Case 5 — removed legacy `launchctl submit -l com.phuc.dandori-watch` example (label was renamed to `com.dandori.watch` in v0.9.0)
  - `02-user-guide.md` Use Case 8 — `\claude` backslash-bypass now meaningless (no alias to bypass); replaced with plain `claude`
  - `02-user-guide.md` Common Commands table — `dandori init` mistakenly described as "Config + DB + shell aliases"; corrected
  - `03-faq.md` Command Reference — added `doctor`, `claude`, expanded `watch` row

## [0.9.0] — 2026-05-02

UX overhaul. Setup from 4-6 steps → 1 command. Removed shell alias override (footgun), added explicit subcommands. Verify gate now opt-in (was noisy by default).

### Added

- **`dandori init` full wizard** — 13 interactive prompts: agent name, Jira email + token, Confluence space key + derived URL, test connection live (masks token input), creates config + database, ready to use immediately
- **`dandori claude "..."` subcommand** — explicit pass-through to Claude binary, tracked like any run, replaces shell alias `claude='dandori run -- claude'` footgun
- **`dandori watch enable/disable/status`** — daemon orchestration: `launchd` on macOS, `systemd-user` on Linux, scheduled background capture. Windows shows guidance on error.
- **`dandori init --uninstall-shell`** — clean up legacy `claude` alias block from `~/.zshrc` / `~/.bashrc` for users migrating from v0.8.x
- **`-q / --quiet` global flag** — suppress run summary + info logs, keep errors/warnings to stderr, mutual exclusion with `-v`

### Changed

- **Default `verify.semantic_check` + `verify.quality_gate`: `true` → `false`** — User configs with explicit `true` values are honored; users without the field get `false` (behavior change by design to reduce friction)
- **`dandori init` wizard flow** — no longer writes shell aliases to rc files; config yaml fully populated by wizard so no manual editing needed
- **Init wizard mask token input** — uses `golang.org/x/term.ReadPassword` so credentials not echoed to screen

### Removed

- **Shell alias auto-install** — `InstallAliases` function and related rc-file write logic (`internal/shellrc/`) removed entirely; `dandori init --shell` / `--no-shell` flags removed
- **Symlink `/tmp` workaround documentation** — workaround was fixed in v0.4.0; stale FAQ entries removed from `docs/01-setup-guide.md`, `docs/02-user-guide.md`, `docs/03-faq.md`
- **Manual `launchctl submit` pattern** — replaced with `dandori watch enable` for consistency

### Fixed

- **`dandori claude` persistent flag propagation** — `-q/--quiet`, `-v/--verbose`, and `--config` were silently ignored when used with `dandori claude` because `DisableFlagParsing:true` (needed to forward arbitrary claude flags like `--dangerously-skip-permissions`) also blocked cobra from parsing dandori's own flags. `runClaude` now strips dandori-owned flags from args manually before forwarding the rest to claude. Patterns supported: `dandori -q claude …`, `dandori claude -q …`, `dandori claude … -q`, `--config path`, `--config=path`.

### Migration from v0.8.x

**For users with no existing config:**
- Just run `dandori init` once, follow the wizard, done. No manual editing, no rc-file sourcing.

**For users with v0.8.x config (`~/.dandori/config.yaml`):**
- Verify gate behavior: if yaml has explicit `verify.semantic_check: true` or `verify.quality_gate: true`, they stay enabled. If field missing, gate is OFF (new default).
- Example: to re-enable verification, add:
  ```yaml
  verify:
    semantic_check: true
    quality_gate: true
  ```
- Shell alias removal: if you have `claude=...` alias block in `~/.zshrc` / `~/.bashrc`, run `dandori init --uninstall-shell` to remove, then use `dandori claude "..."` directly.
- Watch daemon: if you had manual `launchctl submit ... dandori watch` or similar cron setup, unload it first (`launchctl remove com.phuc.dandori-watch` or equivalent), then run `dandori watch enable`.

**Migration checklist (if upgrading existing v0.8.x user):**
- [ ] Verify gate behavior: read config, decide if you want `verify: {semantic_check: true, quality_gate: true}` or accept new OFF default
- [ ] Shell alias: run `dandori init --uninstall-shell` OR manually `unalias claude`
- [ ] Watch daemon: disable old cron / launchd / systemd setup, then `dandori watch enable`

### Tests

- **Phase 01 (init wizard):** 20 tests — Jira healthcheck, Confluence healthcheck, wizard happy path (13 prompts), sad path (cred failure), `deriveConfluenceURL` from Jira base_url
- **Phase 02 (symlink resolution):** 3 regression tests — symlink resolve path, git.ResolveSymlink behavior, no false positives on regular paths
- **Phase 03 (claude subcommand + init cleanup):** 9 tests — `HasAliasBlock` scanner, `UninstallAliasBlock` remover, claude subcommand pass-through, mutual exclusion `-q` vs `-v`
- **Phase 04 (verify gate defaults):** 5 tests — default OFF, backcompat with explicit `true`, env override, yaml load behavior
- **Phase 05 (quiet flag):** 7 tests — quiet flag sets log level, errors still visible, conflict detection `-q -v`
- **Phase 06 (watch daemon):** 12 tests — watchctl darwin file generation, linux systemd-user unit, status check, enable/disable state transitions

**Total new tests:** ~42 top-level test functions across Phase 01–06 + claude flag-strip fix (subtests + table-driven cases bring effective coverage higher). Suite now: 789 top-level test funcs / ~906 test runs across 26 packages.

**E2E:**
- Manual sweep: `dandori init` wizard flow, `dandori claude`, `dandori watch enable`, verify gate opt-in behavior
- Browser: N/A (CLI release)

### Docs updated

- `README.md` — quick start (removed `source ~/.zshrc` step), commands table (added `dandori claude`, `dandori watch enable/disable/status`), feature bullet (changed alias transparency to explicit subcommands)
- `docs/01-setup-guide.md` — wizard walkthrough + test connection, dropped `vim config.yaml` step, `dandori watch enable` instead of manual launchctl
- `docs/02-user-guide.md` — Use Case 1 rewritten (ad-hoc `dandori claude`), Use Case 2 removed config step, "Best Practices" now has 3-path table (`task run` / `claude` / `run`), quiet flag example
- `docs/03-faq.md` — removed symlink workaround (fixed in v0.4.0), replaced alias Q&A with v0.9.0 guidance + `--uninstall-shell`, new Q about verify gate opt-in, watch daemon status commands
- `docs/devlog/2026-05-02-v0.9.0-release.md` — release notes, phase ship order, test posture, lessons learned
- `docs/devlog/README.md` — index updated with v0.9.0 entry

## [0.8.1] — 2026-05-02

Performance + DX patch. No new features, no breaking changes. Binary size 22MB → 14MB (-36%).

### Performance

- **Strip symbols**: `Makefile` adds `-ldflags="-s -w"` (-7MB).
- **pgx build-tag split**: `//go:build server` on 14 files in `internal/{analytics,assignment,server}` + `cmd/server`. CLI binary no longer links pgx (-1MB). Server build now requires `make build-server` (passes `-tags server`).
- **Regex package-level cache**: hoisted 7 regex in `internal/confluence/converter.go` and 1 in `internal/intent/decisions.go` out of hot loops — no per-call recompile.
- **SQLite tuning** (`internal/db/local.go`): `synchronous=NORMAL`, `mmap_size=128MB`, `temp_store=MEMORY`, `cache_size=64MB`. Connection pool serialized (`SetMaxOpenConns(1)`) to match WAL writer model.
- **JSONL streaming** (`internal/wrapper/message_counter.go`, `wrapper.go`): G7 path now uses `os.Open` + `bufio.Scanner` (4MB max line) instead of `os.ReadFile`. Memory peak stable for large sessions.
- **Index** (migration v6→v7): `idx_runs_started_at` for analytics range scans.

### Fixed

- **Latent deadlock** in `internal/insights/insights_cost.go` `wowCostSpike`: explicit `rows.Close()` before nested `topCostContributor` query — exposed by pool size 1.

### Tests

- New benchmark suite (was zero before): 4 files, 29 benchmarks across `internal/{intent,wrapper,confluence}`. Includes 1MB JSONL fixture for regression.
- Profile on 86MB session: parse 470ms, peak RAM 64.6MB, throughput 183 MB/sec.

### Docs

- Restructure `docs/` by audience (user · stakeholder · maintainer) with numeric prefix ordering.
- New: `docs/04-release-summary-v0.5.0-to-v0.8.0.md`, `docs/06-vision-and-roadmap.md` (enterprise-scale ROI + 6 gaps), `docs/reference/` (G6/G7/G8 deep-dive).
- Removed 16 stale files (12 pre-v0.5 micro-phase devlogs + status-assessment, hackday-demo-script, onboarding, ck-tools-usage).

## [0.8.0] — 2026-05-01

G10 dashboard expansion: closes 5 high-impact data gaps from the G9 GA audit (engineer KPI strip, org alerts banner, DORA history sparklines, mix leaderboard, rework rate) plus 1 P0 mislabel fix (Iteration Distribution rewire to actual round counts). No schema migration — all features additive on existing tables.

### Added — G10 dashboard

- **Engineer KPI strip** — 5 hero tiles on engineer view (cost / runs / interventions / autonomy / success, all 7-day window) with WoW delta arrow on cost. Backend: `/api/g9/engineer/{name}` returns `kpi_7d` block.
- **Org alerts banner** — persistent red banner above DORA scorecard surfacing `analytics.DetectAlerts` cost-multiple + AC-dip breaches. Each row has a `view` drilldown link (`?role=agent&id=<name>` for cost multiples, `?role=engineer&id=<name>` for AC dips). Hidden when no alerts. Backend: `/api/g9/alerts`.
- **DORA history sparklines** — 32px sparkline appended to each of the 4 DORA tiles (org + project), rendering last 12 `metric_snapshots` chronologically. Trend coloring respects metric direction (deploy_freq up = green, lead_time/CFR/MTTR down = green). Backend: `/api/g9/dora/history?scope=&id=&limit=`.
- **Mix leaderboard (engineer × agent)** — sortable table on org view backed by `db.GetMixLeaderboard` (28-day window, top-20). Click engineer name to drill into engineer view. Backend: `/api/g9/mix-leaderboard`.
- **Rework rate tile + WoW delta** — DORA-adjacent hero tile on org view showing `rework_runs / total_runs` over last 28 days, threshold flag at 0.10 (matches `metric.ReworkThresholdV1`), WoW arrow in percentage points (lower = better, green when down). Backend: `/api/g9/rework?scope=&id=&period=` honors Jira project-key prefix filter.

### Changed

- **Iteration Distribution chart** — was bucketing by *duration* (mislabel from G9). Now buckets by *round count* (1, 2, 3, 4, 5+) per task — definition matches `dandori analytics iterations`. Old `TestG9Iterations_ReturnsHistogramByDuration` removed (asserted the bug).
- **Engineer KPI strip CSS** — added `#engineer-kpi-grid` 5-column grid override (was inheriting 4-column `.project-hero-grid` and wrapping the 5th tile).
- **Mobile @ ≤768px** — `.dora-grid` now responsive (2 cols instead of fixed 4) with `min-width:0` on tiles so they shrink to fit. Mix leaderboard table gains `overflow-x:auto` wrapper. Closes a v0.7.0 regression where the DORA card overflowed inside its `overflow:hidden` parent at 375px viewport.

### Tests

- 23 new test cases across `internal/server/g9_*_test.go` (4 engineer KPI · 3 alerts · 6 DORA history · 4 mix leaderboard · 5 rework · 1 iteration rewire). All green.
- Full suite: 24 packages, all green; ~858 test runs total.
- Browser visual sweep (Playwright @1440 desktop + 375 mobile): 0 failures, 0 console errors across org/project/engineer scopes.

### Live-test cross-checks

| Phase | Endpoint | CLI source | Match |
|---|---|---|---|
| P1 | `/api/g9/engineer/Phúc Nguyễn` `kpi_7d` | `dandori analytics cost --by engineer` | $31.82 / 21 runs / 0 interv / 100% / 90% — exact |
| P2 | `/api/g9/alerts` | `dandori analytics all` alerts section | orphan agent at 1070.1× baseline — exact |
| P3 | `/api/g9/dora/history` | `metric_snapshots` payload (snap-20260501100043) | deploy_freq 1.6785714 / lead_time 0.00381 / CFR 0.19148 / mttr 2.17382 — byte-identical |
| P4 | `/api/g9/mix-leaderboard` | `dandori analytics mix --since 28` | orphan/109/$10217.94, Phúc/e2e-test-alpha/19/$31.82 — exact |
| P5 | `/api/g9/rework` | `dandori metric export` rework field | rate 0.007194, 1/139, threshold 0.1, exceeds=false — byte-identical |
| P6 | `/api/g9/iterations` buckets | `dandori analytics iterations` task counts | 17 tasks, avg 1.08 — internally consistent |

### Plan / devlog refs

- Plan: `plans/260501-1243-g10-dashboard-expansion/plan.md`
- Devlog: `docs/devlog/2026-05-01-v0.8.0-release.md`

## [0.7.0] — 2026-05-01

G9 dashboard redesign GA: 3-level analytics surface (engineer · project · org) replacing the single-page legacy dashboard. CWD-aware landing, role switcher, period selector, vs-prior comparison, insight engine, drilldowns.

### Added — G9 dashboard

- **3-level navigation** — bookmarkable `?role=org|project|engineer&id=&period=&compare=` URL state. Role switcher dropdown reads/writes URL.
- **CWD-aware landing** — `/api/g9/landing` resolves `cwd` git remote → project key (e.g. inside CLITEST repo lands on `?role=project&id=CLITEST`); falls back to `org` outside any repo.
- **Per-level hero tiles**
  - Org: total monthly · DORA composite · avg autonomy · interventions · active engineers
  - Project: cost · tasks completed · $/task · DORA 4-light mini · 3 sparklines
  - Engineer: today cost · success 7d · interventions 7d · autonomy % · retention % · 4-bucket weekly retention chart
- **DORA scorecard** (org + project) — surfaces latest `metric_snapshot` with Elite/High/Medium/Low ratings per DORA 2023 thresholds. Stale-banner when snapshot >24h old. Project scope honors both `?role=project&id=` and `?project=` query forms; falls back to org when project snapshot missing.
- **Attribution composite tile** — `AI Authored X% · Retained Y%` with 28-day sparkline (G7 surface).
- **Intent feed** — chronological layer-4 events (`intent.extracted`, `decision.point`), click-to-expand inline. Filterable by `?engineer=` and `?project=` (LIKE `<KEY>-%` against `runs.jira_issue_key`).
- **Insight engine** — `internal/insights/` ships 5 SQL heuristics (WoW spike, retention decay, intervention anomaly, cost outlier, DORA degradation) rendered as cards on org + project views with drilldown URLs.
- **Drilldowns** — run-row inline expand shows iterations + intent events; engineer name click → `/api/g9/engineer/{name}` with last 50 runs and weekly retention sparkline.
- **Mobile responsive** — verified at 375×812: header wraps, table x-scroll, single-column hero, no horizontal page overflow.

### Changed

- `dandori dashboard` no longer requires `--experimental`. The G9 surface is the only dashboard; flag and legacy `newExperimentalDashboardMux` removed. Legacy panels (Overview, Agents, Cost charts, Recent Runs, Quality KPI) remain mounted unchanged.
- Sidebar badge updated `⚗ Experimental` → `G9 Analytics`; page title `Dandori Analytics`.

### Fixed

- DORA scorecard now scopes by project (was always returning org snapshot).
- Project view intent feed now renders (was hidden via `applyRoleVisibility`); only the org-wide attribution-tile card hides at project scope.

### Tests

- 835 unit tests across 24 packages; all green.
- 4 new server tests for DORA/intent project scoping; 1 new DB test for `GetRecentIntentEvents` project filter.
- E2E (`-tags=e2e`) Phase 8 flow green (14.7s).
- Live test matrix: 30 cells × 3 levels + mobile 375 + bookmark restore + CWD landing all verified via Playwright.
- Cross-check vs `dandori analytics` CLI: dashboard `/api/overview` ($68.80, 36 runs, 24500 tokens) matches engineer sums; `/api/cost/agent` byte-identical to `analytics cost --format json`.

### Plan / devlog

- Plan: `plans/260430-2039-g9-dashboard-redesign/` (P1–P4)
- Devlog: `docs/devlog/2026-05-01-g9-dashboard-ga.md`

## [0.6.0] — 2026-04-30

Intent preservation: captures why an agent ran (G8). Sub-30-minute RCA without reading the full transcript.

### Added — Intent preservation (G8)

Three new Layer-4 semantic events written after every `dandori run` completes:

- **`intent.extracted`** — first human message, final agent summary, spec back-links (Jira key + Confluence URLs from cwd files). One event per run.
- **`decision.point`** — heuristic-detected design choices (`chosen`, `rejected[]`, `rationale`). Capped at 5/run; tagged advisory in all output surfaces.
- **`agent.reasoning`** — reasoning snippets (`thinking` blocks + narrative text before tool use). Capped at 10/run, 1 KB each.

Additional changes:

- **`dandori incident-report --run <id>`** — single-run markdown report with Intent, Key Decisions, Reasoning Trace, Diff Stats, Tool Usage, Quality sections.
- **`dandori incident-report --task <key>`** — multi-run aggregation across all runs for a Jira task: cross-run summary + per-run blocks.
- **Jira completion comment extension** (`jira-sync`) — when `intent.extracted` exists for a run, the comment gains `h3. Intent` and `h3. Key Decisions` sections. Falls back silently to pre-G8 format for legacy runs.
- **Env gate** `DANDORI_INTENT_DISABLED=1` — skips all extraction; no Layer-4 events written; Jira comment and incident report render without G8 sections.
- See [`docs/reference/03-intent-preservation.md`](docs/reference/03-intent-preservation.md) for event schema, heuristic limitations, privacy notes, and v2 roadmap.

### Fixed

- **Redact regex false-positives** — generic secret pattern previously matched prose like "password hashing" or "reset the user token" because it allowed any whitespace between keyword and value. Now requires explicit assignment delimiter (`=`, `:`) or quoted JSON form. Real assignments (`password=hunter2`, `{"token": "abc"}`) still redacted; documentation/spec text preserved.

## [0.5.0] — 2026-04-30

Enterprise measurement layer: DORA + Rework Rate exporter (G6) and agent contribution attribution (G7), plus a critical `go-build*` temp-dir leak hotfix.

### Added — DORA + Rework Rate exporter (G6)
- **`dandori metric export`** — 5 engineering metrics (deployment frequency, lead time for changes, change failure rate, time to restore service, rework rate) over a configurable window:
  - Source of truth = Jira (status transitions for deploys, issuetype/labels for incidents) + local SQLite (Layer-3 `task.iteration.start` events for rework)
  - 3 wire formats: `faros` (DORA schema), `oobeya` (6-layer mapping), `raw` (full report with `jira_config` echo)
  - Insufficient-data semantics: emits `"value": null` (not `0`) so dashboards show "N/A" instead of charting misleading zeros
  - Window flags accept `Nd` / `YYYY-MM-DD` / RFC3339 / `now`; team filter scopes rework leg
  - Configurable via `metric:` block in `~/.dandori/config.yaml` — release statuses, in-progress statuses, incident issue types, incident labels, JQL extension
  - Incident config has no default; CFR + MTTR are skipped cleanly when not opted in
  - Lead time uses NIST linear-interpolation percentiles (p50/p75/p90); MTTR reports p50/p90 + ongoing-incident count
  - Rework Rate uses 10% threshold with strict `>` (10/100 = NOT exceeding); threshold version stamped (`v1-2026Q2`)
  - Reports `tickets_without_in_progress` count in `data_quality` so process gaps surface
- See [`docs/reference/01-metric-export.md`](docs/reference/01-metric-export.md) for command reference + config schema.

### Added — Agent contribution attribution (G7)
- **`dandori metric export --include-attribution`** — per-task accounting of agent vs human code contribution, plus aggregate intervention/iteration/cost percentiles:
  - **Line-level attribution** via `git blame` at the final HEAD when Jira moved to Done. Each line's introducing commit is membership-tested against the union of session-reachable commits (`rev-list HeadBefore..HeadAfter`); pre-session baseline lines are excluded from totals
  - **Intervention classifier** (v1 heuristic): human text ≥30 chars after agent tool use = intervention, <30 = approval. Documented as a proxy in [`docs/reference/02-agent-attribution.md`](docs/reference/02-agent-attribution.md)
  - **Computed BEFORE Jira transition** — `dandori task run` (auto-flow) and `dandori task done` (manual) both write the `task_attribution` row before calling `TransitionToDone`. Failure is non-fatal so observability never blocks the Jira move
  - 6 fields surfaced in the export block: `agent_autonomy_rate` (share of tasks with `intervention_rate < 0.2`), `agent_code_retention_p50/p90`, `intervention_rate_p50`, `iterations_p50/p90`, `cost_per_retained_line_usd_p50`, `session_outcomes` (merged histogram of `agent_finished` / `user_interrupted` / `error`)
  - Insufficient-data semantics: zero rows in window → block is `null` and `task_attribution` is added to `data_quality.insufficient_data`
  - Backwards-compat: without the flag, output is byte-for-byte identical to G6 dashboards
- **SQLite migration v4 → v6**: `task_attribution` table + 5 new `runs` columns (`session_end_reason`, `human_message_count`, `agent_message_count`, `human_intervention_count`, `human_approval_count`); v5 → v6 backfills `jira_done_at` to UTC `Z` for window-scan correctness
- See [`docs/reference/02-agent-attribution.md`](docs/reference/02-agent-attribution.md) for definitions, output schema, three named limitations (format reflow, cross-repo, heuristic threshold), and 6 example questions.

### Fixed
- **`go-build*` temp-dir leak** (high severity): `dandori run` with `quality.enabled=true` (the previous default) spawned `go test` whose 30s SIGKILL timeout prevented the Go toolchain from cleaning up its scratch dirs. One user accumulated ~43k dirs / ~199 GB in `$TMPDIR`. Three-part fix:
  1. **Default `quality.enabled` flipped to `false`** (`internal/quality/collector.go`, `internal/config/config.go`). `dandori init` now prompts to opt in; existing configs are unchanged.
  2. **SIGTERM + 2s grace before SIGKILL** (`internal/quality/spawn_unix.go`): `cmd.Cancel` now sends SIGTERM to the process group so `go test` can run its deferred cleanup; `WaitDelay` gains a `gracePeriod` buffer before Go escalates to SIGKILL. Verified by `TestSpawnCollectorCmd_SIGTERM_AllowsCleanup`.
  3. **New `dandori clean` command** (`cmd/clean.go`): scans `$TMPDIR` for `go-build*` dirs older than 60 minutes (in-flight protection), reports reclaimable size, and deletes only with `--force`. Does **not** touch `GOCACHE` (long-lived cache).
- **attribution window scan** (CLITEST2-14): `AggregateAttribution` lexically string-compared `jira_done_at` against UTC-Z window bounds, silently dropping rows whose stored timestamp carried a non-UTC offset (e.g. `+07:00`). Per-row data was correct, only window membership was wrong. Fix: `compute.go` now normalizes `jira_done_at` to UTC `Z` before INSERT; v5→v6 migration backfills existing rows. Surfaced via 5/5 dogfood case study.
- **wrapper no-commit warning**: when an agent edits the working tree but never runs `git commit`, `task run` now logs a warning + prints a CLI hint. Attribution still reports zero agent lines for that run, but the user knows why instead of silently mis-attributing. New `Result.NoCommitDetected` field.

### Breaking
- `quality.enabled` default flipped from `true` → `false`. Existing configs are honored; users who had not customized must explicitly opt in via `dandori init` or set `quality.enabled: true` in `~/.dandori/config.yaml`. Rationale: prior default leaked `go-build*` scratch dirs on `go test` timeout (see Fixed). Users still wanting quality tracking should run `dandori init` once or edit config.

## [0.4.0] — 2026-04-28

Pre-sync verify gate, Layer-3 tracking, dogfooding bug-fix sweep.

### Added
- **Pre-sync verify gate** (`internal/verify/`) — blocks fake-completion before Jira transitions to Done:
  - Path-match semantic check: extracts file/path tokens from the task spec, flags when the diff misses them
  - Quality gate: fails the gate when the post-run lint/test snapshot reports failures
  - Workspace-scoped matching for the `demo-workspace/{date}-{TASK-ID}/` dogfooding convention
  - Doc-only diffs (`.md`/`.txt`/`.rst`) skip the quality gate; semantic check still runs
  - Inconclusive specs (no extractable paths) flag for review instead of silently passing
  - Warn-mode by default: gate failure leaves the ticket In Progress with a Jira comment, never blocks the exit code
  - Skip via Jira label (`verify.skip_label`, default `skip-verify`) for PO override
  - `dandori task run --no-verify` flag for emergency PO override (audit trail preserved)
  - Config keys: `verify.semantic_check`, `verify.quality_gate`, `verify.skip_label`
- **Layer-3 tracking** (tools, context, iterations, bug links):
  - `wrapper`: emit Layer-3 tool/skill events from the session JSONL
  - `taskcontext`: record `confluence.read` events per page fetched
  - `jira`: detect task iteration via statusCategory regression
  - `jira`: bug-link detection (parsers + `DetectBugLinks` for `caused_by:` description tags and inward/outward link types)
  - `jira-poll` daemon: wire bug-link cycle into poller + analytics
  - Analytics queries for tools, context, iterations on Layer-3 events
- **Composite quality KPIs** (`dandori analytics`):
  - Regression rate, bug rate, quality-adjusted cost — CLI + `analytics-all`
  - Dashboard quality KPI section with 3 dimensions per metric
- **Multi-board Jira polling**: `jira.board_ids` list (legacy `board_id` still honored)
- **Logging**: `LogLevel` config field with `DANDORI_LOG_LEVEL` env override
- **Demo**: `HandleHealthz` endpoint with httptest coverage

### Fixed
- **task-run** (Bug #1): auto `--add-dir` for the context tempDir when wrapping `claude`, so the agent can read the injected context file under `acceptEdits` allowlist
- **db** (Bug #2): tolerate `NULL` `agent_name` in scan paths (older runs)
- **jira** (Bug #5): separate ADF paragraphs with newline so completion comments render correctly
- **wrapper**: populate `runs.engineer_name` from Jira assignee
- **jira**: match canonical link-type Name "Caused" alongside inward/outward forms
- **confluence**: include time-of-day in report title to avoid duplicate-title rejections
- **wrapper**: resolve symlinks for session directory detection
- **quality** (Bug #4): isolate snapshot subprocess (process-group + `WaitDelay`) to prevent post-exit hang when `go test` grandchildren keep stdout pipe open

### Technical
- `internal/verify/semantic_check.go` — path-match extraction + workspace-scoped matching
- `internal/verify/gate.go` — combined gate orchestrator (semantic + quality + skip-label)
- `internal/wrapper/wrapper.go` — `Result.QualityAfter` exposes post-run snapshot to callers
- `internal/quality/collector.go` — `spawnCollectorCmd` helper with `Setpgid` + group-targeted SIGKILL `Cancel` + `WaitDelay`; `DANDORI_QUALITY_RUNNING` env recursion guard
- `internal/jira/buglink.go` — bug-link parsers + detector
- `cmd/version.go` — `formatVersion` extracted; `ParseSemver` helper added

## [0.3.0] — 2026-04-19

Agent Quality Comparison: Measure and compare code quality across agents.

### Added
- **Quality Metrics Tracking** (`internal/quality/`):
  - Lint errors/warnings delta (before/after run)
  - Test pass/fail delta (before/after run)
  - Git diff stats (lines added/removed, files changed)
  - Commit count and message quality scoring
  - Composite quality score (0-100)
- **Quality Analytics** (`dandori analytics quality`):
  - Agent comparison table with quality metrics
  - `--compare alpha,beta` flag for specific agents
  - `--format json` for export
- **Commit Scorer**: Conventional commit adherence scoring (0-1)
- **Git Analyzer**: Diff stats between commits
- **Schema v2**: `quality_metrics` table for storing metrics
- **Config**: `quality.enabled`, `quality.lint_command`, `quality.test_command`

### Technical
- `internal/quality/collector.go` — Lint/test snapshot collection
- `internal/quality/git_analyzer.go` — Git diff analysis
- `internal/quality/commit_scorer.go` — Commit message scoring
- `internal/db/quality.go` — Quality metrics storage/queries

## [0.2.0] — 2026-04-19

Major feature release: Context injection and enhanced tracking.

### Added
- **Task Run with Context** (`dandori task run KEY`):
  - Auto-fetch Jira issue (summary, description, AC)
  - Extract Confluence links from description
  - Fetch linked Confluence page content
  - Generate markdown context file for agent
  - Auto-sync results to Jira on completion
- **Enhanced Jira Completion Comment**:
  - Run statistics (agent, duration, cost, tokens, model)
  - Git HEAD before/after comparison
  - Files changed during the run
  - Acceptance Criteria extracted from task
  - Output location with report command
- **Cost Calculation** with model-specific pricing:
  - Sonnet 4.6: $3/$15 per 1M tokens (in/out)
  - Opus 4.5/4.6: $15/$75 per 1M tokens
  - Haiku 4.5: $0.80/$4 per 1M tokens
- Shell alias transparency via `dandori init`
- Watch daemon: `dandori watch [--once]`
- goreleaser for multi-platform releases
- GitHub Actions release workflow

### Fixed
- Tailer race condition: use channel for synchronization
- Context injection: prepend to user's -p prompt
- Claude session directory detection

### Tests
- 66 E2E tests across 15 groups (A-O)
- Real Jira + Confluence + Claude Code integration

## [0.1.0] — 2026-04-18 (Unreleased preparation)

### Added
- Shell alias transparency
- Watch daemon for orphan runs
- `internal/shellrc/` and `internal/watcher/` packages
- User guide documentation

## [0.1.0] — 2026-04-18

Initial release — all 8 implementation phases complete.

### Added
- **Phase 01** — Foundation: Go module, Cobra CLI, SQLite, config, hash chain
- **Phase 02** — 3-layer agent wrapper (fork/exec, tailer, semantic events), cost calculation
- **Phase 03** — Jira integration: client, poller, transitions, comments
- **Phase 04** — Confluence integration: client, storage↔markdown converter, reader/writer
- **Phase 05** — Monitoring server: PostgreSQL, REST API, SSE, dashboard
- **Phase 06** — Agent assignment: 4-component scorer, engine, REST API
- **Phase 07** — Analytics: 8 query types, CSV/JSON export, CLI commands
- **Phase 08** — E2E flow: Docker Compose, mock APIs, integration tests

### Commands
- `init`, `version`, `status`
- `run`, `event`, `sync`
- `task {start,done,info}`, `jira-sync`
- `conf-write`
- `analytics {runs,agents,cost,sprint}`
- `dashboard`
- `assign {suggest,set,list}`
