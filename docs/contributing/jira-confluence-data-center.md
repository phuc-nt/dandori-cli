# Contribution: Jira/Confluence Data Center support

> Branch: `feat/jira-confluence-datacenter`
> Mục tiêu: chạy được full luồng dandori-cli với Jira + Confluence **Data Center** của tổ chức (không phải Atlassian Cloud).

## Đọc trước (theo thứ tự)

1. [`docs/README.md`](../README.md) — handover 15 phút (vision + current state + 3 file phải đọc)
2. [`../../dandori-pitch/outer-harness.md`](../../../dandori-pitch/outer-harness.md) — outer harness là gì, tại sao tồn tại
3. [`../CLAUDE.md`](../../CLAUDE.md) — design principles + dev workflow của repo này
4. [`docs/status-assessment.md`](../status-assessment.md) — v0.4.0 đang ở đâu, gap nào còn lại
5. [`docs/setup-guide.md`](../setup-guide.md) — config hiện tại (Cloud-centric)
6. [`CHANGELOG.md`](../../CHANGELOG.md) — feature timeline, đặc biệt v0.2.0 (task run + context) và v0.4.0 (verify gate)

## Hiện trạng tích hợp Atlassian

Code đã có **flag `cloud: true/false`** trong config — phần khung sẵn sàng, nhưng chưa ai test thật trên Data Center. Cụ thể:

- **Auth đã tách**: Cloud dùng Basic (email + API token), DC dùng Bearer PAT — xem [`internal/jira/client.go`](../../internal/jira/client.go) và [`internal/confluence/client.go`](../../internal/confluence/client.go).
- **Endpoint pha trộn**: phần lớn Jira call dùng `/rest/api/2/...` (DC-compatible), nhưng search dùng `/rest/api/3/search/jql` (Cloud-only). Confluence dùng `/rest/api/content/...` (DC OK, Cloud cũng accept).
- **Comment format**: `AddComment` post body string plain — DC ăn được; trên Cloud thực tế đôi chỗ cần ADF (Atlassian Document Format). Hiện chưa có ADF builder.
- **Plan setup gốc** chỉ viết cho Cloud: [`plans/260418-1301-dandori-cli/setup-jira-confluence-cloud.md`](../../../plans/260418-1301-dandori-cli/setup-jira-confluence-cloud.md).
- **Phase docs** mô tả intent ban đầu của Jira/Confluence integration:
  - [`phase-03-jira-integration.md`](../../../plans/260418-1301-dandori-cli/phase-03-jira-integration.md)
  - [`phase-04-confluence-integration.md`](../../../plans/260418-1301-dandori-cli/phase-04-confluence-integration.md)

Toàn bộ E2E test, dogfooding (CLITEST2-*), verify gate live test — đều chạy trên Cloud. DC chưa bao giờ chạy thật.

## Mục tiêu của contribute này

**Chạy được full luồng `dandori task run KEY` end-to-end trên Jira/Confluence Data Center của tổ chức, với Claude Code thật.**

Full luồng nghĩa là (theo CHANGELOG v0.2.0):
1. Fetch Jira issue (summary, description, AC) qua DC API
2. Extract + fetch Confluence page links từ description (DC API)
3. Generate context file → wrap `claude` → execute
4. Sync kết quả về Jira: comment hoàn thành + transition status
5. (Optional nếu kịp) verify gate hoạt động trước khi transition Done — xem CHANGELOG v0.4.0

### Scope: cắt vừa đủ để chứng minh

**Không cần full feature parity với Cloud.** Chọn subset đủ để demo 1 chu trình hoàn chỉnh:

- ✅ Bắt buộc: `dandori task run KEY` chạy thông, Jira ticket được transition + comment hiển thị đúng, Confluence page đọc được.
- ⚠️ Nên có: `dandori task info KEY`, `dandori jira-sync` poll task từ board.
- ❌ Có thể bỏ qua trong scope này: `conf-write` post report ngược lên Confluence, analytics dashboard, agent assignment scoring, multi-board polling, ADF rich formatting.

### Định nghĩa "xong"

1. Có config mẫu `setup-jira-confluence-data-center.md` (đặt cạnh file Cloud trong `plans/260418-1301-dandori-cli/`).
2. Một ticket thật trên DC của tổ chức được dandori-cli xử lý end-to-end, screenshot/log đính kèm PR.
3. Devlog entry trong [`docs/devlog/`](../devlog/) ghi lại những gì DC khác Cloud (endpoint nào fail, payload nào phải sửa, auth quirk nào).
4. Test mới (unit hoặc integration) cover chỗ rẽ nhánh DC mà bạn thêm — không cần đủ coverage, đủ để CI bảo vệ regression.

Các điểm khác có thể discover trong quá trình làm — không cần liệt kê hết trước.

## Quy ước làm việc

- Branch: làm trên `feat/jira-confluence-datacenter` (đã tạo sẵn từ `main` tại v0.4.0).
- Commit convention + dev workflow: theo [`../CLAUDE.md`](../../CLAUDE.md) → "Coding Standards" + "When Done".
- Trước khi PR: `make lint` + `make test` clean. Nếu thêm integration test cần DC thật, đặt sau build tag để CI Cloud không chạy.
- PR description: link tới ticket DC đã test thật, paste log `dandori task run` + screenshot Jira sau khi sync.

## Liên hệ

Hỏi Phuc (tech lead) cho:
- Quyết định scope khi gặp Cloud-only behavior không trivial để map sang DC.
- Quyền tạo project test trên DC tổ chức nếu chưa có.
- Review PR.
