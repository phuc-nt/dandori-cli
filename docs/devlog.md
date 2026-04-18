# dandori-cli Devlog

## 2026-04-18 | Phase 01-03 Foundation

**Done:**
- Phase 01: Go module, Cobra CLI, SQLite, config, models, hash chain
- Phase 02: 3-layer wrapper (fork/exec, tailer, skill events), cost calc
- Phase 03: Jira client, poller, comments, transitions

**Stats:** 3153 LOC, 30 Go files, 88 tests

**Coverage:**
| Package | % |
|---------|---|
| util | 100 |
| model | 100 |
| event | 82 |
| db | 68 |
| config | 49 |
| jira | 41 |
| cmd | 32 |
| wrapper | 29 |

**CK Skills Used:**
- Không dùng skill catalog trong session này
- Implement trực tiếp theo plan có sẵn (`plans/260418-1301-dandori-cli/`)
- TDD approach: viết tests sau code, sau đó bổ sung tests để tăng coverage

**Commands Working:**
```
dandori init      # setup ~/.dandori/
dandori run       # wrap agent execution
dandori event     # Layer 3 events
dandori status    # view runs
dandori sync      # stub for Phase 05
dandori version
```

**Next:** Phase 04 (Confluence), Phase 06 (Assignment), Phase 07 (Analytics)

---

## 2026-04-18 | Phase 05 Monitoring Server

**Done:**
- Server entrypoint với Chi router
- PostgreSQL schema + connection pool
- Event ingest API (`POST /api/events`)
- Fleet live SSE endpoint
- Runs REST API
- CLI sync command với uploader

**Binaries:**
- `bin/dandori` — CLI
- `bin/dandori-server` — monitoring server

**Stats:** ~4500 LOC, 95+ tests

**Env vars for server:**
```
DANDORI_DB_HOST, DANDORI_DB_NAME, DANDORI_DB_USER, DANDORI_DB_PASSWORD
DANDORI_LISTEN (default :8080)
```
