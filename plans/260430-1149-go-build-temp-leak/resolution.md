# Resolution — Go-build Temp Directory Leak

**Status:** Resolved (PR #4 merged 2026-04-30, commit `7ca2025`)
**Severity:** High → fixed
**Disk reclaimed:** ~211 GB (manual one-shot cleanup by user before fix)

---

## What was wrong

`dandori run` with `quality.enabled: true` (the previous default) spawned `go test ./...` after every agent run. When that subprocess hit the 30s timeout, the wrapper sent **SIGKILL**, which prevented Go's toolchain from running its own `defer os.RemoveAll(...)` cleanup → ~30 `go-build*` scratch dirs leaked into `$TMPDIR` per timed-out run.

One user accumulated ~43k–66k dirs / ~199 GB.

## What was fixed (PR #4)

Three changes, ordered by impact:

1. **SIGTERM + 2s grace before SIGKILL** (`internal/quality/spawn_unix.go`) — the actual root-cause fix. `cmd.Cancel` now sends SIGTERM to the process group so `go test` reaches its cleanup; Go's `WaitDelay` escalates to SIGKILL only if still alive after `gracePeriod`. **Applies to all users, including those with `quality.enabled: true` already set.**

2. **Default `quality.enabled` flipped to `false`** (`internal/quality/collector.go`, `internal/config/config.go`). `dandori init` now prompts to opt in. Defense in depth + opt-in for a feature that runs `go test` on every wrapper invocation.

3. **`dandori clean` command** (`cmd/clean.go`) — sweeps `$TMPDIR` for `go-build*` dirs older than 60 minutes (in-flight protection), reports reclaimable size, deletes only with `--force`. `GOCACHE` intentionally untouched.

## Tests added

- `TestSpawnCollectorCmd_SIGTERM_AllowsCleanup` — verifies the trap actually fires (regression-proof against returning to SIGKILL)
- `TestSpawnCollectorCmd_WaitDelay_IncludesGrace`
- `TestClean_DryRun_ReportsOnlyEligible`
- `TestClean_Force_DeletesEligibleOnly`
- `TestClean_Empty_NoMatches`
- `TestHumanBytes`

## Decisions

- **No GOTMPDIR isolation (Fix 3 in original plan)** — root cause already fixed by SIGTERM grace; isolation was defense in depth, not needed.
- **No integration build tag for nested `go test` (Fix 4)** — same reason; nested test no longer leaks.
- **No parent-ctx wiring for snapshot (Fix 5)** — Ctrl+C still waits up to 30s for snapshot timeout, but no longer leaks. Acceptable.
- **No troubleshooting doc (Fix 7)** — CHANGELOG entry covers user-facing impact; if questions come up, add docs reactively.

## Out of scope (intentionally)

- Programmatic deletion of pre-existing leaked dirs (user must run `dandori clean --force`)
- Touching `~/Library/Caches/go-build` (`GOCACHE`) — long-lived cache, valuable
- Shell rc changes for global `GOTMPDIR`

## References

- Original investigation: `issue.md` (this folder)
- PR: https://github.com/phuc-nt/dandori-cli/pull/4
- Merge commit: `7ca2025`
