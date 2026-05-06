# dandori-cli — Documentation

> CLI outer harness cho mô hình **PO/PDM + QA + AI agent** vận hành dự án phần mềm. Wrap Claude Code, track mọi run, tích hợp Jira + Confluence, dashboard 3-level analytics.

**Current**: v0.11.0 · 25 packages green under `go test ./...` · vision coverage ~98% (Phase 05 closed) · solo-engineer self-measurement complete

---

## Cho người dùng (engineer · PO/PDM · QA)

| Nếu bạn cần... | Đọc |
|---|---|
| Cài đặt + cấu hình lần đầu | [01-setup-guide.md](01-setup-guide.md) |
| Sử dụng theo từng use case | [02-user-guide.md](02-user-guide.md) |
| Gặp lỗi / troubleshoot | [03-faq.md](03-faq.md) |
| Hiểu giá trị 4 release gần nhất (v0.5.0 → v0.8.0) | [04-release-summary-v0.5.0-to-v0.8.0.md](04-release-summary-v0.5.0-to-v0.8.0.md) |
| Vision · giá trị enterprise scale · gap còn lại | [06-vision-and-roadmap.md](06-vision-and-roadmap.md) |
| Kiến trúc hệ thống (overview) | [07-system-architecture.md](07-system-architecture.md) |
| Lịch sử thay đổi chi tiết | [../CHANGELOG.md](../CHANGELOG.md) |

## Cho stakeholder (lead · sponsor · audit)

- **Tầm nhìn**: [outer-harness](https://phuc-nt.github.io/dandori-pitch/outer-harness.html) — vì sao cần lớp quản lý quanh AI agent
- **Giá trị đã giao**: [04-release-summary-v0.5.0-to-v0.8.0.md](04-release-summary-v0.5.0-to-v0.8.0.md) — 4 release đóng câu hỏi gì cho PO/QA
- **Roadmap + giá trị enterprise**: [06-vision-and-roadmap.md](06-vision-and-roadmap.md) — ROI ở scale 9,000 active AI users + 6 gap chi tiết
- **Bằng chứng vận hành**: [../CHANGELOG.md](../CHANGELOG.md) + [devlog/](devlog/) — release cadence + bài học triển khai

## Cho maintainer (contribute · release · debug)

| Nếu bạn cần... | Đọc |
|---|---|
| Quy trình release (tag → goreleaser → Homebrew tap) | [05-release-setup.md](05-release-setup.md) |
| Devlog từng release (v0.5.0 → v0.8.0) | [devlog/](devlog/) |
| Architecture overview (CLI · 3-layer · data flow) | [07-system-architecture.md](07-system-architecture.md) |
| 8 phase implementation gốc | [../../plans/260418-1301-dandori-cli/plan.md](../../plans/260418-1301-dandori-cli/plan.md) |
| Convention code Go + thiết kế nguyên tắc | [../CLAUDE.md](../CLAUDE.md) |

## Reference (deep-dive theo feature)

| Feature | Module |
|---|---|
| **G6** — DORA + Rework Rate export | [reference/01-metric-export.md](reference/01-metric-export.md) |
| **G7** — Agent contribution attribution | [reference/02-agent-attribution.md](reference/02-agent-attribution.md) |
| **G8** — Intent preservation (RCA via incident-report) | [reference/03-intent-preservation.md](reference/03-intent-preservation.md) |

---

## Source code điểm vào

```
cmd/                  → CLI entry + subcommands (dashboard, task, analytics, ...)
internal/runner/      → 3-layer instrumentation, session tailer
internal/store/       → SQLite schema + queries
internal/jira/        → Jira client, poller
internal/confluence/  → Confluence read/write
internal/analytics/   → query types, alerts, KPI
internal/server/      → G9 dashboard handlers + g10 expansion
internal/intent/      → G8 extraction
internal/attribution/ → G7 line-blame + intervention classifier
internal/metric/      → G6 DORA exporter
```

## Smoke test

```bash
make build && make test
./bin/dandori version
./bin/dandori dashboard   # → http://localhost:8088
```
