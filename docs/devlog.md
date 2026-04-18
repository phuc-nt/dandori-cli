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

---

## 2026-04-18 | Edge Case Testing (ck-scenario)

**Done:**
- Dùng `/ck:scenario` để phân tích 12 dimensions, skip 6 không relevant
- 64 edge case scenarios across 6 dimensions (Input Extremes, Timing, State Transitions, Error Cascades, Data Integrity, Integration)
- Edge test files cho config, db, wrapper, jira, server, sync

**Stats:** 128 tests pass, ~5000 LOC

**Report:** `plans/reports/scenario-260418-1500-edge-cases.md`

**Key edge cases covered:**
- Empty/malformed config, unicode paths
- DB corruption, concurrent writes, schema idempotent
- Wrapper context cancel, quick exit, empty command
- Jira rate limit 429, timeout, auth 401, 404
- Server SSE concurrent clients, buffer overflow
- Sync server timeout/500/unreachable, partial success

**CK Skills Used:**
- `/ck:scenario` — generate 64 edge case scenarios
- Scenario-driven test coverage improvement

---

## 2026-04-18 | Phase 07 Analytics (TDD)

**Done:**
- `internal/analytics/` package: types, queries, export (CSV/JSON)
- Query functions: AgentStats, AgentCompare, TaskTypeStats, CostBreakdown, CostTrend, SprintSummary, TaskCostBreakdown
- Server routes: `/api/analytics/*` (8 endpoints)
- CLI command: `dandori analytics cost|agents|sprint`
- Export: CSV + JSON download via `/api/analytics/export`

**Stats:** 151 tests pass

**TDD Flow:**
1. Write tests first (queries_test.go, export_test.go, routes_analytics_test.go)
2. Implement to make tests pass
3. Integrate with server + CLI

**API Endpoints:**
```
GET /api/analytics/agents
GET /api/analytics/agents/compare?agents=alpha,beta
GET /api/analytics/task-types
GET /api/analytics/cost?group_by=agent|sprint|task|day
GET /api/analytics/cost/trend?period=week&depth=8
GET /api/analytics/sprints/:id
GET /api/analytics/tasks/:key/cost
GET /api/analytics/export?query=agents&format=csv
```

**CLI:**
```
dandori analytics cost --group-by agent --sprint 42
dandori analytics agents --compare alpha,beta
dandori analytics sprint 42
```

---

## 2026-04-18 | Phase 04 Confluence Integration (TDD)

**Done:**
- `internal/confluence/` package: client, models, converter, reader, writer
- Storage Format ↔ Markdown converter (headings, lists, tables, code blocks, links, bold/italic)
- Page reader with local cache + TTL
- Report writer with XHTML template
- CLI command: `dandori conf-write --run ID | --task KEY`

**Stats:** 151 tests pass (35 confluence tests)

**TDD Flow:**
1. Write model tests (models.go)
2. Write converter tests (StorageToMarkdown, MarkdownToStorage)
3. Write client tests with httptest mocks
4. Write reader/writer tests with mock client
5. Write CLI command tests

**Components:**
- `client.go` — HTTP client (GET/POST/PUT, Basic Auth, retry)
- `models.go` — Page, PageBody, PageVersion, RunReport
- `converter.go` — Storage ↔ Markdown (regex-based)
- `reader.go` — FetchAndCache, cache TTL, ContextAssembler
- `writer.go` — CreateReport, RenderReportTemplate

**CLI:**
```
dandori conf-write --run abc123      # write report for run
dandori conf-write --task PROJ-123   # write report for latest run on task
dandori conf-write --run abc123 --dry-run  # preview without posting
```

**Config additions:**
```yaml
confluence:
  base_url: "https://example.atlassian.net/wiki"
  space_key: "CLITEST"
  reports_parent_page_id: "164207"
  auto_post: true
  cache_ttl_min: 60
  cloud: true
```

---

## 2026-04-18 | Phase 03+04 Integration Tests

**Done:**
- Fixed Jira time parsing (`JiraTime` custom type for multiple formats)
- Fixed Jira search API (migrated v2 → v3 `/rest/api/3/search/jql`)
- Fixed Jira polymorphic fields (`description` as ADF, `StoryPoints` as array)
- Fixed Confluence `Space.ID` type mismatch (`FlexID` for string/number)
- Integration tests with real Atlassian instance (CLITEST project)

**Jira Integration Tests (6/6 pass):**
- GetIssue: CLITEST-1 → "Add /hello endpoint"
- GetBoards: Board 3 found
- GetActiveSprint: Sprint 4 active
- GetSprintIssues: 4 issues (CLITEST-1 to CLITEST-4)
- SearchIssues: JQL query works
- AddComment: Comment posted successfully

**Confluence Integration Tests (4/4 pass):**
- SearchPages: 5+ pages in CLITEST space
- CreateAndGetPage: Page created and retrieved
- CreateReport: Agent run report page created
- ReaderCache: Page fetched and cached to markdown

**Fixes Applied:**
```go
// Jira time formats (multiple variants)
type JiraTime struct { time.Time }
formats := []string{
    "2006-01-02T15:04:05.000-0700",
    "2006-01-02T15:04:05.000Z",
    time.RFC3339,
}

// Confluence FlexID (handles string or number)
type FlexID string
func (f *FlexID) UnmarshalJSON(b []byte) error {...}
```

**Stats:** 190 tests pass across 14 packages

---

## 2026-04-18 | Phase 06 Agent Assignment

**Done:**
- Scorer: 4-component scoring algorithm (capability 40%, issue type 30%, history 20%, load balance 10%)
- Engine: rank agents, select best, generate human-readable explanation
- Server DB: agent_configs + assignments tables
- Server routes: `/api/agents`, `/api/assignments`
- CLI: `dandori assign suggest|set|list`

**Stats:** 204 tests pass across 15 packages

**Scoring Algorithm:**
```
Score(agent, task) = Σ (weight × match)
- Capability overlap (40%): intersect(agent.caps, task.labels ∪ components)
- Issue type preference (30%): exact match = 1.0, neutral = 0.5, mismatch = 0.0
- Historical success (20%): past success rate on same issue type
- Load balance (10%): 1.0 - (active_runs / max_concurrent)
```

**CLI Commands:**
```
dandori assign suggest PROJ-123       # Get agent suggestions with scores
dandori assign set PROJ-123 alpha     # Manually assign agent + post Jira comment
dandori assign list                   # List pending assignments from server
```

**API Endpoints:**
```
GET  /api/agents                      # List registered agents
GET  /api/agents/{name}               # Get agent with active run count
POST /api/agents                      # Register/update agent config
GET  /api/assignments                 # List assignments (filter by status/agent)
GET  /api/assignments/{id}            # Get assignment detail
POST /api/assignments/{id}/confirm    # Confirm assignment
```

**Poller Integration:**
- Auto-suggest on new task detected
- Post suggestion comment to Jira
- Reminder comment after 2h timeout (configurable)
- Track pending suggestions until confirmed

**History Provider:**
- Query past success rate from runs table
- Cache with configurable TTL (default 1h)
- Thread-safe with RWMutex

**Stats:** 208 tests pass across 15 packages

**Phase 06 Complete**

---

## 2026-04-18 | Phase 08 E2E Integration Tests

**Done:**
- Full E2E test suite with real Atlassian (CLITEST project)
- Test: Sprint fetch → Agent suggest → Jira comment → Run simulate → Confluence report
- Test: Confluence context assembly from multiple pages
- Test: Jira poller detecting tasks and posting suggestions

**E2E Results (real Atlassian):**
```
=== TestE2E_FullSprintCycle (3.15s) ===
  - Sprint: 4 issues detected
  - Scoring: CLITEST-1 (Story) → beta, CLITEST-2 (Bug) → alpha
  - Jira: Suggestion + completion comments posted
  - Confluence: Report page created with files, decisions, git diff

=== TestE2E_ConfluenceContextFetch (1.17s) ===
  - 8 pages found in CLITEST space
  - Context assembled: 832 bytes from 2 pages

=== TestE2E_JiraPollerFlow (3.89s) ===
  - 4 tasks detected
  - 4 suggestion comments auto-posted
```

**Commands Verified:**
```
dandori assign suggest CLITEST-1   ✅
dandori assign set CLITEST-1 alpha ✅ (comment posted)
```

**Stats:** 208 unit tests + 3 E2E tests = 211 total

**Phase 08 Complete**

---

## 2026-04-18 | Comprehensive Integration Test Coverage

**Done:**
- Expanded Jira integration tests from 6 to 22 test functions
- Expanded Confluence integration tests from 4 to 18 test functions
- Fixed test uniqueness issues (RunID with nanoseconds for unique page titles)

**Jira Integration Tests (22 tests):**
- GetIssue, GetIssueNotFound, GetIssueFields
- GetBoards, GetBoardsInvalidProject
- GetActiveSprint, GetActiveSprintInvalidBoard
- GetSprintIssues, GetSprintIssuesTypes
- SearchIssues, SearchIssuesByType, SearchIssuesByStatus, SearchIssuesInvalidJQL
- AddComment, AddCommentWithMarkdown, AddCommentInvalidIssue
- GetRemoteLinks, GetTransitions, AddLabel
- PollerSinglePoll, PollerNoNewTasks
- ExtractConfluenceLinks

**Confluence Integration Tests (18 tests):**
- SearchPages, SearchPagesWithTitle, SearchPagesInvalidSpace
- CreateAndGetPage, CreatePageWithParent, CreatePageWithRichContent
- GetPageNotFound, UpdatePage
- CreateReport, CreateReportWithDecisions, CreateReportWithLargeDiff
- ReaderCache, ReaderCacheMultiplePages, ReaderCacheInvalidation
- ContextAssembler, ContextAssemblerWithErrors
- StorageToMarkdownRealPage, RoundTripConversion

**E2E Tests (3 tests):**
- FullSprintCycle: Sprint → Assign → Comment → Run → Report → Verify
- ConfluenceContextFetch: Search → Cache → Assemble
- JiraPollerFlow: Detect → Suggest → Post

**Stats:** 265 unit tests + 22 Jira + 18 Confluence + 3 E2E = 308 total tests

**Run Commands:**
```bash
go test ./... -count=1                                    # unit tests
go test ./internal/jira/... -tags=integration -v          # Jira integration
go test ./internal/confluence/... -tags=integration -v    # Confluence integration
go test ./internal/integration/... -tags=e2e -v           # E2E tests
```
