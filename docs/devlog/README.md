# dandori-cli Devlog

Chỉ giữ devlog **per-release** từ v0.5.0 trở đi. Chi tiết phase-level pre-v0.5 đã được tổng hợp trong [release-summary](../04-release-summary-v0.5.0-to-v0.8.0.md) và [CHANGELOG](../../CHANGELOG.md).

## Release devlogs

| Date | Release | Topic |
|---|---|---|
| 2026-04-30 | [v0.5.0](2026-04-30-v0.5.0-release.md) | G6 DORA + Rework Rate exporter, G7 agent contribution attribution |
| 2026-04-30 | [v0.6.0](2026-04-30-v0.6.0-release.md) | G8 intent preservation — 3 event types + `incident-report` + Jira G8 sections |
| 2026-05-01 | [v0.7.0](2026-05-01-v0.7.0-release.md) | G9 dashboard GA — 3-level surface, DORA scorecard, attribution composite, mobile-responsive |
| 2026-05-01 | [v0.8.0](2026-05-01-v0.8.0-release.md) | G10 dashboard expansion — KPI strip, alerts banner, sparklines, leaderboard, rework tile |
| 2026-05-02 | [v0.9.0](2026-05-02-v0.9.0-release.md) | UX overhaul — full init wizard, explicit subcommands, verify gate opt-in, watch daemon |
| 2026-05-03 | [v0.9.1](../../CHANGELOG.md) | Polish — `dandori doctor` health check, doc sweep, goreleaser server-tag fix |
| 2026-05-05 | [v0.10.5](2026-05-05-v0.10.5-release.md) | Phase 05 close — Dashboard v2 (5 persona views) + audit chain external anchor + buglinks + cross-project demo seed + release-readiness fixes |
| 2026-05-05 | [v0.10.6](2026-05-05-v0.10.6-bug-debt.md) | Bug debt cleanup — 9 deferred items from v0.10.5 review (race fix, counter fix, widget hardening, XSS escape, DST edge) |
| 2026-05-06 | [v0.11.0](2026-05-06-v0.11.0-solo-engineer-gaps.md) | Solo-engineer self-measurement — agent×task affinity matrix + structured RCA breakdown + week-over-week trend analytics |

## Stats (current)

- **25 packages green** under `go test ./...`
- **Vision coverage**: ~98% of outer-harness pillars (Phase 05 closed)
- **Release cadence**: 8 releases in ~2.5 weeks (Apr 30 → May 6)

## Cách viết devlog mới

Khi release tag mới, tạo file `YYYY-MM-DD-vX.Y.Z-release.md` theo template của [v0.8.0](2026-05-01-v0.8.0-release.md):

1. **What shipped** — bullet feature + impact 1 dòng
2. **Phase ship order** — bảng commit hash + scope
3. **Test posture** — số tests, browser sweep, live-test cross-check
4. **Lessons** — chỉ ghi điều mới học được lần này
5. **Docs updated** + **Plan reference** + **Open items deferred**

Mục tiêu: future-you (hoặc maintainer mới) đọc 1 file là đủ ngữ cảnh release.
