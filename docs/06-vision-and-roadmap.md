# Vision & Roadmap — dandori-cli

> Tổng kết: **vấn đề user nào đã giải**, **giá trị ở scale công ty vài chục nghìn engineer**, và **các gap còn lại** cần lấp trước khi bán enterprise.

---

## 1. Vision (outer-harness)

> AI agent là **người thực thi giỏi**, nhưng tổ chức thiếu **lớp quản lý quanh agent**. Lớp đó = outer harness.

5 pillar mà mọi tổ chức phải có để vận hành agent an toàn:

| Pillar | Câu hỏi tổ chức phải trả lời | dandori-cli (v0.8.0) |
|---|---|---|
| 1. Cost Attribution | "$X/tháng tiền AI đi đâu, ai dùng?" | ✅ 100% |
| 2. Audit & Trust | "Agent commit code sai, ai chịu trách nhiệm?" | ✅ 100% |
| 3. Task Tracking | "Sprint này agent đang làm gì, đến đâu?" | ✅ 100% |
| 4. Quality Gates | "Code agent ra có đủ tốt để merge?" | ⚠️ ~75% |
| 5. Knowledge Flow | "Senior nghỉ — kinh nghiệm prompt/spec ở đâu?" | ⚠️ ~55% |

**Mục tiêu xa**: chứng minh dự án phần mềm có thể vận hành bởi **PO/PDM + QA + AI agent** (không cần human developer trong loop).

---

## 2. Roadmap đã đi (v0.1.0 → v0.8.0, ~2 tuần)

| Lớp | Release | Module | Vấn đề user nào được giải |
|---|---|---|---|
| **Foundation** | v0.1.0–0.4.0 | 8 phase + verify gate | Tracking-by-default + Jira/Confluence integration |
| **Đo lường** | v0.5.0 | G6 DORA + G7 attribution | "Tiền đi đâu? Code agent đóng góp bao nhiêu %?" |
| **Bộ nhớ** | v0.6.0 | G8 intent preservation + incident-report | "Vì sao agent quyết định thế? RCA mất bao lâu?" |
| **Trực quan hoá** | v0.7.0 | G9 dashboard 3-level GA | "Standup không cần mở 4 tab CLI" |
| **Cảnh báo & xu hướng** | v0.8.0 | G10 expansion | "Có nguy cơ gì sắp xảy ra? Tuần này tệ hơn tuần trước?" |

Chi tiết per-release: [04-release-summary-v0.5.0-to-v0.8.0.md](04-release-summary-v0.5.0-to-v0.8.0.md).

---

## 3. Vấn đề cụ thể đã giải cho từng persona

### Engineer (làm việc cùng agent)
- **Trước**: copy-paste Jira description vào Claude, agent quên ngữ cảnh → 3-4 vòng làm-lại
- **Giờ**: `dandori task run KEY` → agent có Jira issue + Confluence linked docs sẵn; KPI strip (G10) cho engineer thấy WoW của chính mình

### PO/PDM
- **Trước**: cuối tháng export Excel cộng tay cost; standup mở 4 tab CLI; "ai làm gì sprint này" hỏi miệng
- **Giờ**: `dandori dashboard` 1 trang — cost per engineer · leaderboard · alerts · DORA. `dandori analytics cost --by department` cắt theo bộ phận trong 1 lệnh

### QA
- **Trước**: phát hiện chất lượng giảm sau khi đã retro xong sprint
- **Giờ**: org alerts banner + rework rate tile = chỉ báo sớm. Drilldown 1 click vào run gây cost-multiple thay vì 5 query SQL

### Lead / Tech Director
- **Trước**: không có DORA cho team-có-AI; không biết autonomy rate
- **Giờ**: `dandori metric export` chuẩn DORA + Faros/Oobeya schema; agent_autonomy_rate, code-retention p50/p90, cost-per-retained-line

### Audit / Compliance
- **Trước**: agent commit code lỗi → đào git log thủ công
- **Giờ**: hash-chain audit + 3-layer instrumentation + `intent.extracted` (lý do agent chọn approach) + spec back-links

---

## 4. Giá trị ở scale công ty 30,000 engineer

Giả định 30% (9,000) đang dùng AI agent — tỷ lệ thực tế các bigtech 2025–26.

| Trục | Phân tích | Tiết kiệm ước tính/năm |
|---|---|---|
| **Cost optimization** | Hoá đơn ~$1.8M/tháng = $21.6M/năm. 15-30% spend là "không cần thiết". Cắt 20% với cost-attribution. | **$4.3M** |
| **Productivity (autonomy)** | 9,000 dev × 1h/ngày giám sát agent × $150/h × 240 ngày = $324M/năm. Tăng autonomy rate +10% (G7 attribution metric). | **$32M** |
| **Quality (rework)** | Rework rate >10% × vài trăm task/tuần × $500/task. Giảm 5pp với org alerts + rework tile (G10). | **$10–15M** |
| **Knowledge handover** | Senior xoay vai trò → junior đọc incident-report thay vì hỏi 1h/ngày. | **$5–10M** |
| **Compliance** | Audit-ready trail = blocker để IPO / merger / bán enterprise. | (option value, không quy đổi) |
| **Tổng** | | **~$50–60M/năm trên 9,000 active AI users** |

Tỷ lệ ROI vs license: kể cả pricing $50/dev/năm × 9,000 = $450k → **ROI 100x+**.

---

## 5. Các gap còn lại — giải thích chi tiết

dandori-cli v0.8.0 đã đóng 4/5 pillar. Còn các gap sau, mỗi cái có **vấn đề business cụ thể** ở scale lớn:

### Gap 1 — Multi-agent orchestration (G1) · P1 · ~16h

**Vấn đề user**: Công ty 30k engineer không lock vào 1 vendor AI. Một team thử Codex để review code, team khác dùng Claude cho refactor, team khác dùng Gemini cho data work. Khi PO hỏi "team nào dùng agent nào tốt hơn?", không có cơ sở so sánh.

**Cụ thể thiếu gì**:
- Pricing table chỉ có Claude Sonnet/Opus/Haiku — không có Codex / Copilot / Gemini
- Session parser chỉ đọc Claude Code JSONL format — Codex/Copilot có format khác
- Không có side-by-side compare: cùng 1 task, agent A vs agent B, ai retain code nhiều hơn, ai cost ít hơn, ai autonomy cao hơn

**Tại sao P1 ở scale lớn**: bigtech evaluate dandori sẽ hỏi "tôi đang dùng cả Copilot Workspace + Claude Code, tool này quản được cả hai không?" — câu trả lời "chỉ Claude" = mất deal.

**Hướng giải**:
1. Abstract pricing table: load từ config thay vì hardcode
2. Pluggable session parser: interface `SessionExtractor`, mỗi vendor 1 implementation
3. Bổ sung `dandori analytics compare --agents alpha,beta` với DORA + autonomy + retention side-by-side

---

### Gap 2 — Context inheritance (G2) · P2 · ~8h

**Vấn đề user**: Senior nghỉ, project pass cho junior. Senior có "DNA prompt" tích lũy 6 tháng — preferred coding style, các edge case đã test, lý do không chọn approach X. Junior bắt đầu lại từ zero, agent cũng "quên" mọi context project-level.

**Cụ thể thiếu gì**:
- `parent_run_id` không được track — không biết run hiện tại kế thừa từ run nào
- Không có per-ticket aggregation: tất cả run của 1 PBI không gom thành 1 view "đây là toàn bộ context PBI này có"
- Không có org-level / project-level prompt store — mỗi engineer cấu hình riêng

**Tại sao P2**: không phải blocker để bán, nhưng là **moat** dài hạn. Công ty nào dùng dandori 6 tháng sẽ tích lũy được tài sản context — switching cost cao.

**Hướng giải**:
1. Schema: thêm `runs.parent_run_id` + `runs.ticket_id` (đã có jira_key, cần aggregate API)
2. Endpoint `GET /api/ticket/:key/context` trả về union các intent + decision của mọi run trong PBI
3. CLI `dandori task context KEY` in ra context kế thừa khi engineer bắt đầu run mới

---

### Gap 3 — Skill library (G3) · P3 · ~24h

**Vấn đề user**: Senior viết được prompt verify-able cho task "migrate API v1 → v2" — đã chạy 30 lần, success rate 95%. Nhưng prompt đó nằm trong shell history của senior, không ai khác dùng được. 9,000 engineer trong tổ chức = 9,000 lần phát minh lại bánh xe.

**Cụ thể thiếu gì**:
- Layer-3 đã track tools/skills agent dùng (đã có), nhưng chưa có **registry** prompt đã verify
- Không có convention chia sẻ: làm sao một engineer publish "prompt template X" để engineer khác `dandori skill use X`
- Không có scoring: skill nào đã được dùng nhiều, success rate cao

**Tại sao P3**: lớn nhất về effort (24h) nhưng impact chậm — cần có critical mass user trước khi network effect đẻ ra giá trị. P1/P2 nên ship trước.

**Hướng giải**:
1. Schema: bảng `skills` (name, prompt_template, author, project, success_rate, use_count)
2. CLI: `dandori skill publish <name>` (push từ run thành công), `dandori skill use <name>` (pull về làm context)
3. Confluence integration: skill có thể link tới page mô tả + AC

---

### Gap 4 — Jira/Confluence Data Center (G5) · P2 · Tùng owns

**Vấn đề user**: Bigtech finance / health / gov thường KHÔNG cho dữ liệu source-of-truth ra Cloud. Họ chạy Atlassian Data Center on-prem. dandori-cli hiện tại chỉ test với Atlassian Cloud API.

**Cụ thể thiếu gì**:
- API endpoint khác (`/rest/api/2/` vs `/rest/api/latest/`)
- Auth khác (PAT vs API token)
- Một số field schema khác (custom field IDs, status workflow)

**Tại sao P2**: không phải tất cả công ty cần, nhưng những công ty CẦN thường là deal lớn nhất ($M+ contract). Branch `feat/jira-confluence-datacenter` đã tồn tại — Tùng owns.

**Hướng giải**: branch hiện tại — abstract Jira/Confluence client thành interface, 2 implementation Cloud + DC.

---

### Gap 5 — Spec ↔ Decision linkage (G8 v2)

**Vấn đề user**: G8 v1 đã capture được "agent chọn sliding window over fixed expiry" — heuristic regex từ thinking blocks. Nhưng chưa link được decision đó về **acceptance criterion nào nó address** trong Jira / **section nào trong Confluence design doc**.

**Cụ thể thiếu gì**:
- Decision hiện ghi `chosen` + `rejected` + `rationale` text — không có pointer về `AC#3 trong CLITEST-12` hay `Section 4.2 trong design doc`
- Audit hỏi "decision này tuân thủ requirement nào?" → không trace được tự động

**Tại sao P2**: compliance khắt khe (finance/health) cần. Engineering team thông thường có thể sống không có.

**Hướng giải**:
1. Agent cooperation: prompt agent emit `[DECISION: chose X over Y because Z, addresses AC#3]` markers — bỏ heuristic regex
2. Schema: `decision_point.spec_refs[]` chứa Jira AC index + Confluence section anchor
3. Incident-report bổ sung "Compliance Trace" section

---

### Gap 6 — Department breakdown axis (G10 stretch)

**Vấn đề user**: KPI strip + mix leaderboard hiện cắt theo engineer. PO của Org-level muốn thấy "Platform team vs Growth team — bên nào dùng AI hiệu quả hơn?".

**Cụ thể thiếu gì**:
- Trường `runs.department` chưa có schema/seed (deferred từ v0.8.0)
- Cost analytics đã có `--by department` (manual mapping qua config), nhưng dashboard widgets chưa wire

**Tại sao thấp ưu tiên**: dữ liệu workaround được qua config mapping engineer→department. Native field trong DB cần migration.

**Hướng giải**: schema migration v7 + seed department từ HR API hoặc config; dashboard widgets thêm `?group=department`.

---

## 6. Roadmap đề xuất sau v0.8.0

Theo thứ tự ưu tiên cho mục tiêu **bán enterprise scale 30k engineer**:

```
v0.9.0  G1 multi-agent          → unblock bigtech multi-vendor
v0.10.0 G2 context inheritance  → moat dài hạn
v0.11.0 G5 Jira/Confluence DC   → unlock finance/health/gov segment
v0.12.0 G3 skill library        → network-effect khi user base đủ lớn
v0.13.0 G8 v2 spec linkage      → compliance-grade audit
v1.0.0  Department axis + polish + stable API freeze
```

Lý do thứ tự:
- **G1 trước G3** vì G1 là blocker hard (mất deal); G3 là nice-to-have (network effect chậm)
- **G2 trước G5** vì G2 là single-codebase work; G5 là branch separate (Tùng owns)
- **v1.0.0 chốt** sau khi 5/5 pillar đạt 95%+ và có API stable cho tích hợp 3rd-party

---

## 7. Tóm lại

dandori-cli ở v0.8.0 đã **đóng 4/5 pillar outer-harness** cho mô hình PO/QA + agent. Ở scale công ty vài chục nghìn engineer:

- **Cost** đo + cắt được → tiết kiệm hàng triệu USD
- **Productivity** đo autonomy rate → tối ưu vòng giám sát con người
- **Quality** có chỉ báo sớm rework → tránh rò rỉ chi phí ẩn
- **Audit** sẵn trail → không phải refactor compliance phút chót
- **Knowledge** preserve intent → chống brain-drain khi senior xoay vai trò

Vấn đề **user thực sự được giải**: không còn "đẹp trên slide nhưng không quản được trong thực tế". Mọi run agent đều có cost · audit · intent · attribution trong DB local — không phải build BI từ đầu, không phải mock-up.

Còn 6 gap (G1 → G6 trên) trước khi tự tin bán enterprise. G1 là blocker cứng nhất; G2 + G5 là mở rộng segment; G3 là moat dài hạn.

---

## See Also

- [04-release-summary-v0.5.0-to-v0.8.0.md](04-release-summary-v0.5.0-to-v0.8.0.md) — chi tiết per-release
- [Outer Harness vision](https://phuc-nt.github.io/dandori-pitch/outer-harness.html) — tầm nhìn gốc
- [CHANGELOG](../CHANGELOG.md) — release notes per-version
