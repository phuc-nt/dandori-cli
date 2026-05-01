# 2026-05-01 — G9 dashboard redesign GA

## Summary

Cut over the G9 3-level analytics surface from `--experimental` to GA. The
single dashboard mux now serves the redesigned HTML unconditionally and
registers the G9 endpoints + landing detection. Legacy panels (Overview,
Agents, Cost charts, Recent Runs, Quality KPI) untouched.

Plan: `plans/260430-2039-g9-dashboard-redesign/phase-04-polish-ga.md`.

## What shipped (P4 a → c)

- **P4a — drilldowns:** `/api/g9/run/{id}/expand` (iterations + intent events)
  and `/api/g9/engineer/{name}` (50 runs + 4-bucket weekly retention sparkline).
  Frontend wires inline run-row expansion and engineer-name click.
- **P4b — polish:** project-hero sparklines (cost, tasks, $/task) reuse the
  cost-by-day series; mobile CSS at @375px (header wrap, table x-scroll, single
  column hero).
- **P4c — GA cutover:** dropped `dashboardExperimental` flag, removed
  `newExperimentalDashboardMux`, deleted legacy `dashboardHTML` const,
  renamed `dashboardHTMLv2` → `dashboardHTML`, folded G9 routes + landing
  into `newDashboardMux`. Pre-cut tag: `v0.6.x-pre-g9-ga`.

## Live test (GA gate)

Dogfood DB seeded at `/tmp/dandori-p3.db`: 36 runs across alice + bob, 3
projects (CLITEST, OTHER, DEMO), 5 layer-4 intent events, 3 metric_snapshots
(org + CLITEST + DEMO), 3 task.iteration.start events on CLITEST-2.

Per-level matrix (Playwright + curl, viewport 1280×900 unless noted):

| Cell | Org | Project (CLITEST) | Engineer (alice) |
|---|---|---|---|
| Hero tiles render with real numbers | ✅ | ✅ | ✅ runs table populated |
| Hero sparklines render | n/a (no hero sparklines for org) | ✅ 3 canvases 178×32 | ✅ retention canvas 918×80 |
| Period selector re-renders panels | ✅ | ✅ | ✅ |
| Compare toggle shows deltas | ✅ | ✅ | ✅ |
| Filter pills add/remove rescopes | ✅ | ✅ | ✅ |
| Insight cards render | ✅ wow-spike + retention-decay | ✅ | tolerated empty |
| Intent feed shows ≤20, click expands | ✅ 5 events, alice/bob | ⚠ hidden (see gap) | ✅ alice-only |
| DORA scorecard (org+project) / hidden (engineer) | ✅ Elite/High/High/High | ⚠ shows org snapshot (see gap) | ✅ hidden |
| Run-row click expands | ✅ | ✅ | ✅ |
| Numbers cross-check `dandori analytics` | ✅ overview $68.80 / 36 runs | ✅ | ✅ |

CWD test:
- ✅ `cd /tmp/CLITEST-fake && dandori dashboard` (origin = `…/CLITEST.git`)
  → `/api/g9/landing` returns `{role:"project", id:"CLITEST"}`
- ✅ `cd /tmp && dandori dashboard` → `{role:"org", id:""}`

Mobile: 375×812 → `scrollWidth==clientWidth==375`, no horizontal overflow.
Bookmark restore: `?role=org&period=28d&compare=true` → compare checkbox
checked, period selector value `28d`.

## Known gaps (deferred, not blocking GA)

1. **Project view has no scoped intent feed.** `g9-section` is hidden when
   `role=project` ("project has own panels"), but no project intent panel
   was added in P4a/P4b. Deferred — add `proj-intent-feed` with `?project=`
   filter in next iteration.
2. **`/api/g9/dora` ignores `?role=project&id=`.** Always returns the latest
   snapshot with empty team filter, so the project DORA scorecard shows org
   numbers. Need to thread role/id through `LatestSnapshot(team, format)`
   lookup. Trivial follow-up.

Both gaps are pre-existing in the experimental path — GA cutover didn't
introduce them, but they're now visible to all users so worth tracking.

## Tests

`go test ./...` clean. Notable updates:
- `cmd/dashboard_g9_test.go` — replaced `TestExperimentalFlagOff_KeepsLegacyMux`
  + `TestExperimentalFlagOn_RegistersG9Routes` with single
  `TestG9RoutesAlwaysRegistered`.
- `cmd/dashboard_p2_html_test.go` — renamed all `dashboardHTMLv2` references
  to `dashboardHTML`; replaced experimental-marker check with
  `TestDashboardHTML_ContainsG9Markers` (G9- + G9 Analytics badge).

## Commit chain

- P4a: drilldown handlers + frontend wiring + 6 RED→GREEN tests
- P4b: hero sparklines + mobile CSS @375
- P4c: GA cutover (this commit)
