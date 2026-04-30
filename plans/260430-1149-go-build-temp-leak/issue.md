# Go-build Temp Directory Leak — Issue & Root Cause

**Reported:** 2026-04-30
**Severity:** High (199 GB disk consumed on user's Mac)
**Status:** Investigated — fix pending

---

## Symptom

User's `$TMPDIR` (`/private/var/folders/xb/zb6j3jjd03xb7b8q8wbpyb5c0000gn/T`) accumulated **~43,752 `go-build*` folders** consuming **~199 GB**.

Each folder ~3 MB. These are Go compiler scratch dirs that should auto-delete on `go` command exit. They survive only when the `go` process is killed before reaching its cleanup path (e.g., SIGKILL).

Other coincidental leftovers in $TMPDIR (out of dandori-cli scope): `fastembed_cache`, `mlx_whisper_*`, `puppeteer_dev_chrome_profile-*`, `node-compile-cache`, `jiti`. These come from other tools (Claude Code, Node toolchain) — not dandori-cli.

---

## Root Cause

Two confirmed leak sources, both in dandori-cli's quality collection feature.

### Primary leak — production (`dandori run` / `dandori task run`)

**File:** `internal/quality/collector.go:110` (`runTests`)
**Trigger:** Every `dandori run` invocation when `quality.enabled = true` (the default).

Flow:
```
dandori run -- claude "..."
  └─ wrapper.Run(ctx)
       ├─ [before] qualityCollector.SnapshotLintOnly(cwd)   ← lint only, no leak
       ├─ exec.CommandContext(ctx, "claude", ...)            ← agent runs
       └─ [after]  qualityCollector.Snapshot(cwd)            ← calls runTests
                     └─ spawnCollectorCmd("go test -json -count=1 ./...")
                          └─ go test creates ~30 go-build dirs in $TMPDIR
                               └─ if cwd is a Go repo with test suite > 30s
                                    → 30s timeout fires
                                    → spawn_unix.go SIGKILLs process group
                                    → go test never reaches cleanup
                                    → ~30 dirs leak per run
```

**SIGKILL site:** `internal/quality/spawn_unix.go:24`
```go
cmd.Cancel = func() error {
    return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
cmd.WaitDelay = waitDelay  // = 30s
```
Unconditional SIGKILL. No SIGTERM grace period. `go test` cannot cleanup.

**Default that makes this fire silently:**
- `internal/quality/collector.go:22` — `Enabled: true`
- `internal/config/config.go:147` — `Enabled: true`

### Secondary leak — development (`make test` / `go test ./...`)

**File:** `internal/quality/collector_test.go:97` (`TestCollector_Snapshot_RealProject`)

Test calls `collector.Snapshot(".")` which spawns a **nested** `go test ./...` from inside the already-running `go test ./...`. Adds ~30 extra dirs per `make test` invocation.

Guards exist but are insufficient:
- `testing.Short()` skip — but `make test` does not pass `-short`
- `DANDORI_QUALITY_RUNNING` env skip — only set on the subprocess env (line 117 of collector.go), not at the top-level `go test` invocation, so the parent test still runs

CI workflow `.github/workflows/ci.yml:36` runs `go test -race -count=1 ./...` (no `-short`) → same leak on CI runners (ephemeral, not user's machine — but wasteful).

### Math check

~60 dirs per full `make test` (30 real + 30 nested). User's biggest day (Apr 26) had 25,494 dirs ⇒ ~425 `make test` runs in one day. Consistent with intensive iterative development.

---

## Findings (investigation summary)

Full report: `../../plans/reports/debugger-260430-1151-go-build-temp-leak.md` (workspace-level reports dir).

| # | Area | Finding |
|---|------|---------|
| 1 | TempDir usage in Go code | All `os.MkdirTemp` / `t.TempDir()` sites have proper cleanup. Leak is NOT from explicit temp dir creation — it's from the Go toolchain's own `go-build*` dirs. |
| 2 | Subprocess spawning | Only `internal/quality/collector.go` spawns `go test`. SIGKILL is the kill mechanism. No other Go-toolchain subprocesses. |
| 3 | Signal handling | `cmd/run.go:86-91` registers SIGINT/SIGTERM and cancels parent ctx. But `wrapper.go:234` post-agent snapshot uses `context.Background()` with its own timeout — NOT wired to parent ctx. Ctrl+C during snapshot only fires after 30s timeout → SIGKILL → leak. |
| 4 | Makefile / CI | `make test` runs `go test -v ./...` without `-short`. CI same. No `GOTMPDIR` overrides. |
| 5 | Pre-commit hooks | None. |
| 6 | Environment | No `GOTMPDIR` / `GOCACHE` overrides in shell rc files or project `.envrc`. Standard `$TMPDIR` location. |
| 7 | External tools | dandori-cli does NOT call fastembed / mlx_whisper / puppeteer. Those leftovers are from other tools sharing $TMPDIR. |
| 8 | Smoking gun | `internal/quality/collector.go:110` (production) + `internal/quality/collector_test.go:97` (dev). High confidence. |

---

## Suspect Sites (ranked)

1. **`internal/quality/collector.go:110`** — `runTests` spawns `go test` every `dandori run`. Default-on. Top suspect.
2. **`internal/quality/collector_test.go:97`** — nested `go test` from within `go test ./...`. Fires every `make test`.
3. **`internal/quality/spawn_unix.go:24`** — unconditional SIGKILL. The kill mechanism that prevents cleanup.
4. **`internal/wrapper/wrapper.go:234`** — post-agent snapshot uses `context.Background()` instead of inheriting parent ctx; Ctrl+C cannot interrupt cleanly.
5. **`internal/config/config.go:147`** — `Quality.Enabled = true` default, makes the leak silent for all users.

---

## Proposed Fixes

To be implemented by follow-up agent. Order matters: fix 1 stops the bleed for users without code changes; later fixes harden the mechanism.

### Fix 1 — Default `quality.enabled: false` (stop the bleed)
- `internal/quality/collector.go:22` — `Enabled: true` → `Enabled: false`
- `internal/config/config.go:147` — same
- Update `~/.dandori/config.yaml` template / docs — opt-in
- Update any test that relies on the default

### Fix 2 — Replace SIGKILL with SIGTERM + grace period
- `internal/quality/spawn_unix.go:20-25` — send SIGTERM first, wait ~2s, then SIGKILL
- Lets `go test` reach its own deferred cleanup → no leak even on timeout
- Pattern:
  ```go
  cmd.Cancel = func() error {
      if cmd.Process == nil { return nil }
      _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
      return nil  // WaitDelay handles SIGKILL fallback
  }
  cmd.WaitDelay = 2 * time.Second  // grace, then SIGKILL
  ```
  (Verify Go's `WaitDelay` semantics — it does send SIGKILL after delay.)

### Fix 3 — Isolate Go scratch dirs to dandori-managed location
- In `runTests` (and `runLint` for symmetry), prepend `GOTMPDIR=$HOME/.dandori/tmp/go-build` to the shell command
- Create the dir if missing
- Even if a leak slips through, it's contained to a known location and easy to bulk-delete

### Fix 4 — Mark `TestCollector_Snapshot_RealProject` as integration-only
- Add `//go:build integration` build tag to either the test file or just that test (move to `collector_integration_test.go`)
- Update Makefile: keep `make test` lightweight, add `make test-integration` that opts in
- Stops the nested `go test` during default dev test runs

### Fix 5 — Wire post-agent snapshot to parent context
- `internal/wrapper/wrapper.go:234` — snapshot should derive its ctx from the parent so Ctrl+C cancels it immediately rather than waiting 30s
- Add a separate short timeout layered on the parent ctx (`context.WithTimeout(ctx, snapshotTimeout)`)

### Fix 6 — Add `dandori clean` command
- New file: `cmd/clean.go`
- Subactions:
  - `go clean -cache`
  - `go clean -testcache`
  - `find $TMPDIR -maxdepth 1 -name 'go-build*' -mmin +60 -exec rm -rf {} +` (use Go's `filepath.Walk` + `os.RemoveAll`, not shell)
  - `rm -rf ~/.dandori/tmp/go-build/*` (after Fix 3 lands)
- Default behavior: `--dry-run` reports sizes only
- Flag `--force` to actually delete
- Print summary: dirs found, total bytes, dirs deleted

### Fix 7 — User-facing docs
- New file: `docs/troubleshooting/disk-cleanup.md`
- Sections:
  - What happened (43k dirs / 199 GB symptom)
  - Why (SIGKILL on go test timeout)
  - What we changed (point to fixes 1-5)
  - Manual cleanup recipe (one-liner for `find ... -exec rm`)
  - How to use `dandori clean` going forward

### Fix 8 — Commit message
`fix: prevent temp directory leak in go-build cache`

Body should reference this issue file and the investigation report.

---

## Out of Scope (do NOT do)

- DO NOT delete the existing 43,752 `go-build*` folders programmatically. User must run `dandori clean` (after Fix 6) themselves.
- DO NOT change `GOCACHE` location (different from `GOTMPDIR` — `GOCACHE` is a long-lived cache that should stay where it is at `~/Library/Caches/go-build`).
- DO NOT modify the `claude` / `codex` shell aliases or wrapper invocation path.
- DO NOT add a launchd plist auto-cleaner without explicit user request.

---

## Acceptance Criteria

1. Fresh `dandori init` writes `quality.enabled: false` in template config.
2. With `quality.enabled: true` set explicitly, `dandori run` from a Go repo whose tests exceed 30s does NOT leak more than 1 `go-build` dir per run (SIGTERM grace lets cleanup fire).
3. `make test` from a clean checkout creates 0 nested `go-build` dirs (integration test excluded by default).
4. `dandori clean --dry-run` reports current $TMPDIR/go-build* count and total size correctly.
5. `dandori clean --force` removes only `go-build*` dirs older than 60 minutes (safety margin for currently-running compilations).
6. Documentation at `docs/troubleshooting/disk-cleanup.md` explains the issue and resolution in plain language.
7. All existing tests pass. New tests cover: SIGTERM-grace path in spawn_unix, `dandori clean` dry-run output parsing.
8. `go test ./...` and `golangci-lint run` clean.

---

## Files To Touch

Code:
- `internal/quality/collector.go` (default flip, optional GOTMPDIR injection)
- `internal/quality/spawn_unix.go` (SIGTERM+grace)
- `internal/quality/collector_test.go` (integration build tag)
- `internal/wrapper/wrapper.go` (parent ctx wiring at line 234)
- `internal/config/config.go` (default flip at line 147)
- `cmd/clean.go` (NEW — `dandori clean` command)
- `Makefile` (NEW `test-integration` target)

Docs:
- `docs/troubleshooting/disk-cleanup.md` (NEW)
- `docs/devlog.md` (append entry per repo convention)

Tests:
- `internal/quality/spawn_unix_test.go` (verify SIGTERM grace behavior)
- `cmd/clean_test.go` (NEW — dry-run reporting, age threshold)

---

## Manual Cleanup Already Performed by User (2026-04-30)

User ran a one-shot cleanup before the fix landed. Result: reclaimed **~211 GB**, freed disk from 98% → 50% full.

```bash
T=/private/var/folders/xb/zb6j3jjd03xb7b8q8wbpyb5c0000gn/T

# Pre-check: no live go processes (safe to delete)
ps aux | grep -E 'go build|go test|gopls' | grep -v grep   # → empty

# Count before
sudo find $T -maxdepth 1 -type d -name 'go-build*' -mmin +60 | wc -l
# → 66,497

# Delete go-build* older than 1h
sudo find $T -maxdepth 1 -type d -name 'go-build*' -mmin +60 -exec rm -rf {} +

# Delete empty BlobRegistryFiles-* (Safari/Webkit leftovers, unrelated)
sudo find $T -maxdepth 1 -type d -name 'BlobRegistryFiles-*' -empty -delete
```

**Disk delta:**
| | Used | Free | Capacity |
|---|---|---|---|
| Before | 429 Gi | 9.5 Gi | 98% |
| After  | 218 Gi | 220 Gi | 50% |

**$TMPDIR size now:** 2.2 GB (down from ~211 GB).

Notes for the implementing agent:
- The 66,497 count was higher than the original 43,752 measurement — likely additional `go-build` dirs accumulated between investigation and cleanup, OR the original count missed some entries. Real leak rate is at least as bad as estimated.
- This manual fix is **one-shot**. The leak mechanism is unchanged. Without the code fixes (Fix 1-5), `$TMPDIR` will refill at the same rate.
- The `dandori clean` command (Fix 6) should mirror this exact cleanup recipe: `find $TMPDIR -maxdepth 1 -name 'go-build*' -mmin +60 -exec rm -rf {} +`. The `-mmin +60` guard is correct — protects in-flight `go test` runs.
- The user used `sudo` because the find scan needs to read folders the user may not have list access to. Ideally `dandori clean` runs without sudo (it only needs to remove dirs the user owns); test on a fresh machine. If `Permission denied` on stat, ignore that entry rather than failing the whole sweep.

---

## References

- Investigation report (full): `/Users/phucnt/workspace/dandori-workspace/plans/reports/debugger-260430-1151-go-build-temp-leak.md`
- Related code: `internal/quality/`, `internal/wrapper/wrapper.go`, `cmd/run.go`
- Repo CLAUDE.md: `dandori-cli/CLAUDE.md` (for dev workflow conventions)

---

## Unresolved Questions

1. Does the user typically run `dandori run` from inside the dandori-cli source tree, or from a separate workspace? If always separate, production leak rate is much lower than feared.
2. Should `dandori clean` also clean `~/Library/Caches/go-build` (`GOCACHE`)? Current proposal says no (long-lived cache, valuable). Confirm.
3. Should we set `GOTMPDIR` globally for the user (via shell rc) or only per dandori subprocess? Per-subprocess is safer; global is more thorough but invasive.
4. Default `quality.enabled: false` is the safe call, but does it regress the analytics value prop the project markets? Consider a middle path: enable only when `cwd != git root of any active dandori-cli project` (avoids self-test loop) — but adds complexity.
