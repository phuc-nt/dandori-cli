# Tóm tắt giá trị 4 bản release: v0.5.0 → v0.8.0

> Hành trình từ "đo được" → "nhớ được" → "thấy được" cho mô hình **PO/QA điều phối agent developers**.

## Story arc

```
v0.5.0  Đo lường         G6 cost telemetry + G7 quality alerts
v0.6.0  Bộ nhớ           G8 intent preservation (sprint/PBI/agent context)
v0.7.0  Trực quan hoá    G9 3-level dashboard GA (engineer · project · org)
v0.8.0  Mở rộng          G10 dashboard expansion — KPI strip, alerts banner, sparklines, leaderboard, rework
```

Mỗi bản release đóng một câu hỏi cốt lõi mà PO/PDM phải trả lời để vận hành đội agent:

| Release | Câu hỏi đóng được | Module chính |
|---|---|---|
| v0.5.0 | "Bao nhiêu? Có gì hỏng?" | G6 cost + G7 alerts |
| v0.6.0 | "Vì sao agent làm việc này?" | G8 intent context |
| v0.7.0 | "Ai làm gì, hiệu quả ra sao?" | G9 dashboard |
| v0.8.0 | "Có nguy cơ gì? Xu hướng đi đâu?" | G10 expansion |

---

## v0.5.0 — Lớp đo lường (Đo lường được, mới quản lý được)

### Người dùng nhận được gì

- **G6 — Cost telemetry**: mọi run của agent được ghi token in/out × giá model (Sonnet/Opus/Haiku) → cost USD chính xác đến cent
- **G7 — Quality alerts**: phát hiện cost-multiple bất thường (run đắt gấp N lần trung bình) và AC-dip (acceptance-criteria pass-rate giảm)
- **CLI**: `dandori analytics cost --by engineer|department`, `dandori analytics all` (4-block: cost + leaderboard + quality + alerts)

### Tác động

- PO lần đầu trả lời được "tháng này engineer X đốt bao nhiêu" mà không phải xuất Excel thủ công
- QA có alert chủ động khi chất lượng giảm thay vì chờ retro

---

## v0.6.0 — Lớp bộ nhớ (Agent không còn "quên" sprint)

### Người dùng nhận được gì

- **G8 — Intent preservation**: `dandori task run KEY` tự fetch Jira issue + Confluence linked docs → inject thành context cho agent
- **3-layer instrumentation**: fork/exec wrapper + session-log tailer + semantic events — bắt được run kể cả khi engineer bypass alias
- **Watch daemon**: `dandori watch` chạy nền, capture orphan runs

### Tác động

- Agent có toàn cảnh PBI (description, AC, design doc) thay vì chỉ một câu prompt rời rạc → giảm số vòng làm-lại
- PO không cần soạn lại context mỗi lần giao việc — link Jira → Confluence là đủ
- "Tracking-by-default": muốn không-track còn khó hơn track

---

## v0.7.0 — Lớp trực quan hoá (G9 Dashboard GA)

### Người dùng nhận được gì

- **3 góc nhìn**: engineer (cá nhân) / project (sprint) / org (toàn công ty), CWD-aware landing
- **DORA scorecard**: deployment frequency · lead time · change failure rate · MTTR
- **Attribution composite**: ai đóng góp gì cho project nào
- **Intent feed + Insight engine**: timeline có ngữ cảnh, tự rút insight (top engineer theo cost/run/quality)
- **Drilldowns + mobile-responsive**: click engineer/project/agent để khoan sâu, xem được trên điện thoại

### Tác động

- 274 → 835 unit tests (+561 tests cho dashboard + analytics layer) — phản ánh độ phủ test cho UI
- PO/PDM có một-trang-tổng-quan để standup thay vì mở 4 tab CLI
- QA có nơi xem alerts có ngữ cảnh (drilldown thẳng vào run gây cost-multiple)

---

## v0.8.0 — Mở rộng (G10) — Lấp 5 khoảng trống G9 GA + 1 fix mislabel

### Người dùng nhận được gì

| Module | Giá trị |
|---|---|
| **Engineer KPI strip** | 5 hero tile (cost/runs/intervention/autonomy/success) + WoW arrow trên cost — engineer tự thấy mình tuần này so với tuần trước |
| **Org alerts banner** | Banner cảnh báo cost-multiple + AC-dip ngay trên top, drilldown link sẵn |
| **DORA history sparklines** | 12 snapshot gần nhất, màu sắc tôn trọng hướng tốt-xấu của từng metric |
| **Engineer × Agent leaderboard** | Top-20 cặp engineer-agent, sortable, click engineer drill-in |
| **Rework rate tile** | `rework_runs / total_runs` 28d, ngưỡng 0.10 + WoW pp — phát hiện sớm "team đang đi vòng" |
| **Iteration Distribution rewire** | Sửa mislabel G9: trước bucket theo *duration*, giờ bucket theo *round count* — đúng tên gọi |

### Tác động

- 835 → 858 unit tests (+23) — không nổ test khi mở rộng đáng kể bề mặt UI
- 6 phase ship liên tục trong một buổi chiều + 1 polish commit (browser sweep bắt 3 lỗi mà unit test không thấy)
- **Bài học release-readiness**: TDD + curl live-test chưa đủ — phải có browser visual sweep ở 1440 desktop và 375 mobile để bắt scope-transition bug và CSS layout regression. Bổ sung vào release-readiness checklist từ v0.9.0.

---

## Bảng tăng trưởng

| Chỉ số | v0.5.0 | v0.6.0 | v0.7.0 | v0.8.0 |
|---|---|---|---|---|
| **Unit tests** | 274 | 274 | 835 | 858 |
| **Lớp giá trị mới** | Đo lường | Bộ nhớ | Trực quan hoá | Cảnh báo & xu hướng |
| **Câu hỏi PO trả lời được** | "Bao nhiêu? Có gì hỏng?" | "Vì sao?" | "Ai làm gì?" | "Nguy cơ ở đâu?" |
| **Surface UI mới** | CLI 4-block | — | Dashboard 3 scope | KPI strip + banner + sparkline + leaderboard + rework |
| **Vision coverage** | ~70% | ~80% | ~93% | ~97% |

---

## Tác động lên persona

### PO / PDM
- v0.5.0: thay được công việc "xuất report cost cuối tháng"
- v0.6.0: bỏ được công việc "soạn context khi giao task"
- v0.7.0: bỏ được công việc "mở 4 tab để standup"
- v0.8.0: chủ động thấy nguy cơ rework/AC-dip thay vì chờ retro

### QA
- v0.5.0: có cost-multiple + AC-dip alerts thay vì kiểm thủ công
- v0.7.0: drilldown từ alert vào run gốc (1 click thay vì 5 query)
- v0.8.0: rework rate tile = chỉ báo sớm "team đi vòng" trước khi velocity tụt

### Engineer (con người, làm việc cùng agent)
- v0.6.0: agent không còn hỏi-lại-context lặp đi lặp lại
- v0.7.0: thấy được cá nhân mình so với team (engineer scope)
- v0.8.0: thấy được "mình tuần này vs tuần trước" qua KPI strip + WoW

---

## Vision validation

4 release liên tiếp đóng đủ các pillar của outer-harness vision:

- ✅ **Tracking & Audit** (v0.5.0–v0.6.0)
- ✅ **Analytics multi-dimensional** (v0.5.0 CLI → v0.7.0 GUI)
- ✅ **Jira/Confluence integration** (v0.6.0 G8)
- ✅ **Quality gates** (v0.5.0 G7 → v0.8.0 rework rate)
- ✅ **Agent assignment** (đã có từ trước, ổn định qua các release)
- ✅ **Insight & alerting** (v0.7.0 insight engine → v0.8.0 alerts banner)

Mô hình **"PO/QA điều phối agent developers"** không còn là lý thuyết — `dandori-cli` đã có đủ cơ sở dữ liệu thực tế để PO ra quyết định mỗi sprint, QA chủ động phòng ngừa thay vì xử lý hậu kỳ, và engineer hợp tác hiệu quả với agent thay vì giám sát thủ công.

---

## Tài liệu liên quan

- [CHANGELOG](../CHANGELOG.md) — chi tiết per-release
- [Devlog v0.8.0](devlog/2026-05-01-v0.8.0-release.md) — bài học release gần nhất
- [User Guide](02-user-guide.md) — cách dùng các tính năng
- [Outer Harness vision](https://phuc-nt.github.io/dandori-pitch/outer-harness.html) — tầm nhìn gốc
