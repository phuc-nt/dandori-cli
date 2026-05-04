# v0.10.5 Release Readiness Review

**Range:** v0.9.1..HEAD (8 commits, 71 files, +12.6k LOC)
**Date:** 2026-05-05
**Reviewer:** code-reviewer (Staff Eng pass)
**Pre-flight:** `go test ./...` green, `go vet ./...` clean, `gofmt -w` applied to 6 files, `-tags=server` compiles.

---

## Bugs (severity-ranked)

| # | Sev  | File:line                                    | Bug                                                                                                                                                                                                                                                                                              |
|---|------|----------------------------------------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| 1 | HIGH | `cmd/dashboard.go:166`                       | `addr := fmt.Sprintf(":%d", dashboardPort)` binds **all interfaces**, not loopback. Anyone on the LAN can hit `/api/audit-log`, `/api/events`, `/api/cost/*`, etc. and read engineer names, Jira keys, costs, agent run details. No auth on any endpoint. Fix: `127.0.0.1:%d` or add `--bind` flag defaulting to localhost. |
| 2 | MED  | `internal/db/audit_queries.go:244`           | `VerifyAuditChainWithAnchors` treats every `row.Scan` error as "row missing → tamper". A driver error / locked DB / disk error becomes a false-positive "anchor mismatch". Erodes trust in the verifier. Fix: distinguish `errors.Is(err, sql.ErrNoRows)` (set Valid=false) vs other errors (return real error).               |
| 3 | MED  | `internal/db/audit_queries.go:272`           | `AppendAuditEntry`: SELECT-then-INSERT without a transaction. Concurrent callers read same `prev_hash` → two rows with identical prev_hash → chain self-check breaks. Today only seed.go calls this (single-thread, safe). Doc says "useful for tests + admin actions" but doesn't warn callers. Fix: wrap in `BEGIN IMMEDIATE` tx or document concurrency contract. |
| 4 | MED  | `internal/jira/task_done_hook.go:54`         | `inserted++` counter increments per traversal, not per actual DB insert. `InsertBuglink` uses `INSERT OR IGNORE` and returns nil whether or not the row was added, so `RecordOnTaskDone`'s docstring claim ("repeat invocations for the same bug return 0") is **not** what the code does. Fix: read `RowsAffected` on `InsertBuglink` and propagate. |
| 5 | LOW  | `internal/db/audit_queries.go:184`           | `strings.EqualFold` on a sha256 hex hash — works, but EqualFold tolerates case differences which the chain shouldn't. If a future writer emits uppercase hex, both bytewise-distinct and bytewise-equal cases yield "valid". Cosmetic — still correct since `computeAuditHash` produces lowercase, but `==` would be stricter.            |
| 6 | LOW  | `cmd/audit.go:158`                           | `runAuditAnchor`: if Confluence upsert succeeded but `InsertAuditAnchor` row was IGNOREd (same `last_audit_id`), the local row's `confluence_page_id`/`version`/`status='anchored'` is never recorded. Currently guarded by `last.LastAuditID == tipID` early-out at L122, so unreachable today. Defensive: use `INSERT … ON CONFLICT(last_audit_id) DO UPDATE` to upgrade local-only → anchored. |
| 7 | LOW  | `internal/confluence/audit_anchor.go:172`    | `fmt.Sscanf(cells[1], "%d", &id)` — silent failure produces id=0, which is then a valid-looking row on round-trip. Hand-edited "garbage" pages get garbage rows preserved instead of stripped. Mitigated by header re-render but worth a `_, err := fmt.Sscanf` and skip-on-error.       |
| 8 | LOW  | `cmd/web/dashboard/widgets/qa.js:23,87`      | `fetch('/api/...').json()` with no try/catch and no `res.ok` check. A 500 response still gets `.json()`'d (which throws on non-JSON HTML error pages), bubbling an unhandled rejection that breaks the View. All 6 QA widgets, all 2 audit widgets share the pattern. Fix: wrap in `try/catch`, render an error state instead of crashing the View. |
| 9 | LOW  | `cmd/web/dashboard/widgets/audit.js:30,52`   | Event/audit table: `td>${e.id}</td>` and `td>${e.layer}</td>` are NOT escaped (relying on type=number). If backend ever returns a string in those slots (defensive), XSS slipthrough. Tighten: escape unconditionally or `Number(e.id)`.                                                          |
| 10| LOW  | `internal/server/po_endpoints.go:201`        | `daysRemaining` floor calc: `int(eom.Sub(now).Hours()/24) + 1`. On the last day of the month near 23:59, `eom.Sub(now).Hours()` is < 1 → `int(...)=0` → `+1` = 1, OK. On day 1, ~30 days → 30+1=31. Off-by-one across DST/leap edges since UTC is hardcoded. Acceptable for projection. |
| 11| LOW  | `internal/demo/seed.go:352`                  | `_ = pIdx` is a redundant ignore inside a `for pIdx, p := range projects` block where `pIdx` is referenced 4 lines above. Dead line — remove.                                                                                                                                                  |

---

## Sec / Compliance Notes (not blockers, but important)

- **No CSRF protection** on `POST /api/audit-log/verify` — read-only endpoint, low impact, but an attacker with LAN access (Bug 1) could DoS by spamming verify with `full=true` (100k row scan).
- **Anchor model only protects rows ≤ `last_audit_id`** at the time the anchor was taken. Rows appended after the most recent anchor are unpinned. Doc this in the audit-anchor handbook so users understand cadence matters (weekly anchor → 7-day tamper window for newest rows).
- **Confluence upsert is best-effort under network errors.** If `UpdatePage` fails after `SearchPages` succeeded, no local anchor is recorded — caller must re-run. Acceptable since `auditCmd` returns the error, but consider retry logic for flaky Confluence.

---

## SQL Injection / Data Leakage Audit

All endpoint params (po, qa, audit, eng, admin) use **parameterized queries** with `?` placeholders. No string concatenation into SQL. Filter values flow through `db.POFilter` struct → query-layer functions that bind params. ✅ Clean.

Project isolation: there is **no user concept** — all data is shared in one local SQLite. Acceptable for v0.10.5 (single-user CLI tool); document explicitly that this is not multi-tenant.

---

## Migration v10→v11

- Forward-only, `IF NOT EXISTS` on table + indexes — idempotent. ✅
- `UNIQUE(last_audit_id)` — correct constraint to enforce one anchor per tip.
- `INSERT OR REPLACE INTO schema_version (version) VALUES (11)` — assumes single-row schema_version table; consistent with prior migrations.
- Indexes on `anchored_at` and `last_audit_id` — both used in queries (`LatestAuditAnchor` ORDER BY id DESC, anchor lookups by audit id). ✅

---

## Demo Seed Cross-Project

- Idempotent via `seedTagCross` marker. ✅
- Path-isolated to `~/.dandori/demo.db` via `demoDBPath()`. Real DB is safe **unless** user sets `DANDORI_DB=...` env to a real DB path and runs `dandori demo --reset` — that wipes whatever the env points to. Document this footgun or add a confirmation prompt when `DANDORI_DB` is set.
- `ResetDB` uses `DELETE FROM` (not `TRUNCATE`/`DROP`), so AUTOINCREMENT counters drift but data is correctly cleared. Fine.

---

## Persona View Modules (XSS / Error Handling)

- All user-facing strings are routed through `escapeHtml()` from `widgets/shared.js`. ✅
- Numeric fields (`e.id`, `e.layer`, `r.id`) are interpolated unescaped — relying on backend typing. Defensive escape would close a tiny gap (Bug 9).
- No fetch error handling anywhere in the new widgets (Bug 8) — a single 500 silently breaks the entire View. Suggest a shared `safeFetch` helper before next milestone.

---

## Buglinks Hook (regression-proxy → buglinks transition)

- `RecordOnTaskDone` correctly gated by `isBugType` (case-insensitive substring "bug"). ✅
- Two paths: Jira link traversal + description tag scan. Both call `InsertBuglink` with `INSERT OR IGNORE` — idempotent at the DB layer.
- `inserted` counter is wrong (Bug 4). Caller likely logs/displays this; misleading metric.
- `lowerASCIIHook`/`containsHook` reinvent `strings.ToLower`/`strings.Contains` to "keep import footprint stable" — micro-optimization; `strings` is already imported elsewhere in the package. YAGNI.

---

## Trivial Fixes (inline)

- `internal/demo/seed.go:352` — drop `_ = pIdx`.
- `internal/jira/task_done_hook.go:84-109` — replace `lowerASCIIHook`/`containsHook` with `strings.Contains(strings.ToLower(t), "bug")` to match repo convention.
- `internal/db/audit_queries.go:184` — `r.CurrHash == expected` instead of `EqualFold(TrimSpace(...), expected)`; we own both sides.

---

## Verdict

**GO with caveats.**

The audit chain + anchor witness design is sound, the migration is clean, all SQL is parameterized, and 25 packages of tests pass. The Phase 05 dashboard-v2 + audit anchor work is feature-complete and demoable.

Two caveats before tagging v0.10.5:

1. **Bug 1 (dashboard binds to 0.0.0.0)** — must either flip the default to `127.0.0.1` or document that operators on shared networks should firewall the dashboard port. This is the only finding with realistic blast radius.
2. **Bug 2 (anchor verifier conflates DB errors with tamper)** — fix is 3 lines; ship in the same patch since it directly affects trust in the headline feature.

If both fixes land before tag, full GO. Otherwise GO-with-known-issues and file follow-ups for v0.10.6.

Bugs 3–11 can ride to v0.10.6.

---

## Unresolved Questions

- Is the dashboard intended to be reachable from other hosts on the LAN (e.g., team lead views the engineer's dashboard)? If yes, Bug 1 needs auth, not just loopback binding.
- What's the planned cadence for `dandori audit anchor`? If it's not on a timer/cron, document the manual cadence and the resulting tamper-detection window.
- Is `AppendAuditEntry` going to be called from the agent runner / Jira poller in v0.11? If so, fix Bug 3 (transactional append) before that call site lands.
