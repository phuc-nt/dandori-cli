# Known Issue — Claude báo "Write permission are not being granted"

> Ghi nhận: 2026-04-29 · Reporter: Tùng (alpha-jira / DC test) · Severity: P2 (UX, có workaround)
> Liên quan: [`jira-confluence-data-center.md`](jira-confluence-data-center.md) (workflow `dandori task run KEY`)

## Triệu chứng

Tùng chạy `dandori task run KEY` trên môi trường DC. dandori fetch context OK (Jira issue, Confluence pages), generate context file OK, sau đó wrap Claude execute. Claude phản hồi:

> Write permission are not being granted in this session.

Rồi in ra một đống code change trên màn hình bảo user review thay vì tự `Write`/`Edit` file.

## Root cause — đã xác minh

**Bug ở dandori-cli, không phải Claude config phía user.**

[`cmd/task_run.go:585-628`](../../cmd/task_run.go#L585) — hàm `injectClaudeContext` auto-inject 2 thứ vào agent command khi user không pass agent flag thủ công:

1. `-p "<context instruction>"` — chuyển Claude vào **print mode** (non-interactive headless).
2. `--add-dir <tempDir>` — allow đọc context file.

Nhưng **KHÔNG auto-inject** `--permission-mode acceptEdits`.

Hệ quả: Claude vào print mode + permission mode = `default` (ask before tool use). Vì non-interactive nên không ask được → tool calls (Write/Edit) bị **deny silently** → Claude rơi về fallback "in code ra screen, bảo human review".

Tests trong [`cmd/task_run_test.go`](../../cmd/task_run_test.go) đều khởi đầu fixture bằng `{"claude", "--permission-mode", "acceptEdits"}` hoặc `--dangerously-skip-permissions` — **giả định ngầm là user pass permission flag**, nhưng default UX `dandori task run KEY` (không có `--`) thì không ai pass cả.

## Workaround (ngay lập tức, không cần fix code)

Pass agent flag thủ công khi gọi:

```bash
dandori task run KEY -- claude --permission-mode acceptEdits
```

hoặc:

```bash
dandori task run KEY -- claude --dangerously-skip-permissions
```

Cả hai cho phép Claude write/edit trực tiếp trong print mode.

## Fix đề xuất (1-line change)

Trong [`cmd/task_run.go:603-626`](../../cmd/task_run.go#L603), bổ sung detection + auto-inject `--permission-mode acceptEdits` khi user chưa pass:

```go
hasPermMode := false
for _, arg := range out {
    if arg == "--permission-mode" {
        hasPermMode = true
    }
}
// ... existing hasAddDir / hasSkipPerms detection ...

if !hasPermMode && !hasSkipPerms {
    out = append(out, "--permission-mode", "acceptEdits")
}
```

Effort: ~10 phút code + 2 unit test cases (with/without user-supplied flag) trong `task_run_test.go`.

## Phụ thuộc khác cần check song song

Cùng class với 4 bug Tùng đã report ở `fix/conf-write-bugs` — UX edge của `dandori task run` khi user không pass agent flag. Khi fix, gộp luôn:

1. **MCP Atlassian tự bật** (Tùng report cùng issue): nếu user đã `claude mcp add atlassian` từ trước, server vẫn active trong session dandori spawn → Claude có thể fetch Confluence qua MCP thay vì context file.
   - **Mitigation tạm:** thêm dòng vào context file: *"Không dùng MCP Atlassian trong session này. Dùng context fetched-from-DC ở dưới."*
   - **Mitigation lâu dài:** dandori spawn Claude với env var hoặc flag để disable MCP server (cần research Claude CLI flag — có thể `CLAUDE_DISABLE_MCP=1` hoặc `--mcp-config <empty>`).

## Branch / scope

Bug này **không phải scope của branch DC** — nó là CLI UX bug ảnh hưởng cả Cloud lẫn DC. Đề xuất:

- Tách branch `fix/claude-spawn-permission-mode` từ `main`.
- Ship hotfix trong v0.4.1 (cùng đợt với fix MCP-disable nếu khả thi).
- Branch DC không cần đụng — sau khi v0.4.1 ship, rebase DC branch lên main là đủ.

## Câu hỏi mở

- Default `acceptEdits` có quá permissive cho org enterprise? `plan` mode an toàn hơn nhưng thì không Write được. Có nên expose qua dandori config: `agent.claude.default_permission_mode`?
- Disable MCP servers khi spawn từ dandori — Claude CLI có flag chính thức nào không, hay phải dùng env var trick?
