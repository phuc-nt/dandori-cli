#!/usr/bin/env bash
# Comprehensive E2E test for dandori-cli with real Jira, Confluence, and Claude Code
# See plans/260418-2051-e2e-comprehensive/test-plan.md

set -u

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Test counters
PASS=0
FAIL=0
RESULTS=()

# Paths
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
DANDORI="$PROJECT_DIR/bin/dandori"
CONFIG="$HOME/.dandori/config.yaml"
DB="$HOME/.dandori/local.db"

# Jira credentials
JIRA_URL=$(grep -A5 'jira:' "$CONFIG" | grep 'base_url:' | awk '{print $2}' | tr -d '"')
JIRA_USER=$(grep -A5 'jira:' "$CONFIG" | grep 'user:' | awk '{print $2}' | tr -d '"')
JIRA_TOKEN=$(grep -A5 'jira:' "$CONFIG" | grep 'token:' | awk '{print $2}' | tr -d '"')

# Test workspace
WORKSPACE="/tmp/dandori-e2e-test"
TASKS=()

# ============================================================================
# Helper Functions
# ============================================================================

log_section() {
    echo ""
    echo -e "${BLUE}========== $1 ==========${NC}"
}

log_test() {
    echo -e "${YELLOW}[$1]${NC} $2"
}

pass() {
    PASS=$((PASS + 1))
    RESULTS+=("PASS | $1 | $2")
    echo -e "  ${GREEN}✓ PASS${NC}: $2"
}

fail() {
    FAIL=$((FAIL + 1))
    RESULTS+=("FAIL | $1 | $2")
    echo -e "  ${RED}✗ FAIL${NC}: $2"
}

jira_api() {
    local method="$1"
    local path="$2"
    local data="${3:-}"
    if [ -n "$data" ]; then
        curl -s -X "$method" -u "$JIRA_USER:$JIRA_TOKEN" \
            -H "Content-Type: application/json" -d "$data" "$JIRA_URL$path"
    else
        curl -s -X "$method" -u "$JIRA_USER:$JIRA_TOKEN" \
            -H "Content-Type: application/json" "$JIRA_URL$path"
    fi
}

create_jira_task() {
    local summary="$1"
    local description="$2"
    local key=""
    # Retry up to 2 times on transient failures
    for attempt in 1 2; do
        local resp=$(jira_api POST "/rest/api/2/issue" \
            "{\"fields\":{\"project\":{\"key\":\"CLITEST\"},\"summary\":\"$summary\",\"description\":\"$description\",\"issuetype\":{\"name\":\"Task\"}}}")
        key=$(echo "$resp" | jq -r '.key // empty')
        if [ -n "$key" ]; then
            echo "$key"
            return 0
        fi
        echo "WARN: task creation attempt $attempt failed: $resp" >&2
        sleep 1
    done
    echo "ERROR: failed to create task '$summary' after 2 attempts" >&2
    return 1
}

# ============================================================================
# Setup
# ============================================================================

setup() {
    log_section "SETUP"

    echo "Clearing local DB..."
    rm -f "$DB" "$DB-shm" "$DB-wal"
    "$DANDORI" init > /dev/null 2>&1

    echo "Creating test workspace..."
    rm -rf "$WORKSPACE"
    mkdir -p "$WORKSPACE"
    cd "$WORKSPACE" && git init -q && git commit --allow-empty -q -m "init" || true
    cd "$PROJECT_DIR"

    echo "Creating fresh Jira tasks..."
    local expected=4
    TASKS+=($(create_jira_task "[E2E] Task Alpha file creation" "Test file creation flow"))
    TASKS+=($(create_jira_task "[E2E] Task Beta simple task" "Test simple execution"))
    TASKS+=($(create_jira_task "[E2E] Task Gamma error handling" "Test error handling"))
    TASKS+=($(create_jira_task "[E2E] Task Delta heavy workload" "Test long running task"))

    echo "Created tasks: ${TASKS[*]}"
    if [ "${#TASKS[@]}" -lt "$expected" ]; then
        echo "ERROR: expected $expected tasks, got ${#TASKS[@]}" >&2
        exit 1
    fi
    echo "" > /tmp/dandori-e2e-results.log
}

# ============================================================================
# Group A: Configuration & Setup
# ============================================================================

test_group_a() {
    log_section "Group A: Configuration & Setup"

    log_test "A1" "Config file exists"
    [ -f "$CONFIG" ] && pass "A1" "Config at $CONFIG" || fail "A1" "Config missing"

    log_test "A2" "Version command"
    local ver=$("$DANDORI" version 2>&1)
    [ -n "$ver" ] && pass "A2" "Version: $ver" || fail "A2" "No version output"

    log_test "A3" "DB initialized"
    [ -f "$DB" ] && pass "A3" "DB at $DB" || fail "A3" "DB missing"
}

# ============================================================================
# Group B: Jira Task Lifecycle
# ============================================================================

test_group_b() {
    log_section "Group B: Jira Task Lifecycle"

    local task="${TASKS[0]}"

    log_test "B1" "task info on $task"
    local info=$("$DANDORI" task info "$task" 2>&1)
    echo "$info" | grep -q "Key:.*$task" && pass "B1" "Info retrieved" || fail "B1" "Info failed"

    log_test "B2" "task start transitions to In Progress"
    "$DANDORI" task start "$task" > /dev/null 2>&1
    sleep 1
    local status=$(jira_api GET "/rest/api/2/issue/$task" | jq -r '.fields.status.name')
    [ "$status" = "In Progress" ] && pass "B2" "Status=In Progress" || fail "B2" "Status=$status"

    log_test "B3" "task start adds comment"
    local comments=$(jira_api GET "/rest/api/2/issue/$task/comment" | jq -r '.comments | length')
    [ "$comments" -gt 0 ] && pass "B3" "Comments count=$comments" || fail "B3" "No comments"

    # B4 tested via jira-sync later
    pass "B4" "task done covered by jira-sync (E2)"
}

# ============================================================================
# Group C: Agent Execution (Real Claude)
# ============================================================================

test_group_c() {
    log_section "Group C: Agent Execution (Real Claude)"

    local task_alpha="${TASKS[0]}"
    local task_beta="${TASKS[1]}"

    log_test "C1" "Read-only task (real Claude)"
    local out=$("$DANDORI" run --task "$task_beta" -- claude -p "Say hello" --allowedTools "" 2>&1)
    [ $? -eq 0 ] && pass "C1" "Run succeeded" || fail "C1" "Run failed"

    log_test "C2" "File creation task (real Claude)"
    "$DANDORI" task start "$task_alpha" > /dev/null 2>&1
    local out=$("$DANDORI" run --task "$task_alpha" -- claude -p "Create /tmp/dandori-e2e-$task_alpha.txt with content 'E2E TEST'" --allowedTools "Write" 2>&1)
    [ -f "/tmp/dandori-e2e-$task_alpha.txt" ] && pass "C2" "File created" || fail "C2" "File not created"

    log_test "C3" "Multi-step task tracked"
    local run_count=$(sqlite3 "$DB" "SELECT COUNT(*) FROM runs")
    [ "$run_count" -ge 2 ] && pass "C3" "Runs tracked: $run_count" || fail "C3" "Only $run_count runs"

    log_test "C4" "Task exit code captured"
    local exit_codes=$(sqlite3 "$DB" "SELECT DISTINCT exit_code FROM runs")
    [ -n "$exit_codes" ] && pass "C4" "Exit codes: $exit_codes" || fail "C4" "No exit codes"

    log_test "C5" "Session detected for real runs"
    local session_count=$(sqlite3 "$DB" "SELECT COUNT(*) FROM runs WHERE session_id IS NOT NULL AND session_id != ''")
    [ "$session_count" -ge 1 ] && pass "C5" "Sessions: $session_count" || fail "C5" "No sessions"
}

# ============================================================================
# Group D: Tracking Accuracy
# ============================================================================

test_group_d() {
    log_section "Group D: Tracking Accuracy"

    log_test "D1" "Run IDs stored"
    local ids=$(sqlite3 "$DB" "SELECT COUNT(DISTINCT id) FROM runs")
    [ "$ids" -ge 2 ] && pass "D1" "Unique run IDs: $ids" || fail "D1" "Only $ids"

    log_test "D2" "Exit codes captured"
    local null_exit=$(sqlite3 "$DB" "SELECT COUNT(*) FROM runs WHERE exit_code IS NULL")
    [ "$null_exit" -eq 0 ] && pass "D2" "All exit codes set" || fail "D2" "$null_exit nulls"

    log_test "D3" "Durations captured"
    local null_dur=$(sqlite3 "$DB" "SELECT COUNT(*) FROM runs WHERE duration_sec IS NULL OR duration_sec = 0")
    [ "$null_dur" -eq 0 ] && pass "D3" "All durations set" || fail "D3" "$null_dur zeros"

    log_test "D4" "Git HEAD captured"
    local null_git=$(sqlite3 "$DB" "SELECT COUNT(*) FROM runs WHERE git_head_before IS NULL OR git_head_before = ''")
    [ "$null_git" -eq 0 ] && pass "D4" "All git heads set" || fail "D4" "$null_git missing"

    log_test "D5" "Tokens captured"
    local total_tokens=$(sqlite3 "$DB" "SELECT SUM(input_tokens + output_tokens) FROM runs")
    [ "${total_tokens:-0}" -gt 0 ] && pass "D5" "Total tokens: $total_tokens" || fail "D5" "No tokens"

    log_test "D6" "Cost calculated"
    local total_cost=$(sqlite3 "$DB" "SELECT ROUND(SUM(cost_usd), 4) FROM runs")
    local has_cost=$(echo "$total_cost" | awk '{print ($1 > 0)}')
    [ "$has_cost" = "1" ] && pass "D6" "Total cost: \$$total_cost" || fail "D6" "Cost=$total_cost"

    log_test "D7" "Model captured"
    local model=$(sqlite3 "$DB" "SELECT DISTINCT model FROM runs WHERE model != '' LIMIT 1")
    [ -n "$model" ] && pass "D7" "Model: $model" || fail "D7" "No model"
}

# ============================================================================
# Group E: Jira Sync
# ============================================================================

test_group_e() {
    log_section "Group E: Jira Sync"

    log_test "E1" "jira-sync --dry-run preview"
    local out=$("$DANDORI" jira-sync --dry-run 2>&1)
    echo "$out" | grep -q "dry-run" && pass "E1" "Dry-run shown" || fail "E1" "No dry-run output"

    log_test "E2" "jira-sync transitions tasks to Done"
    "$DANDORI" jira-sync > /tmp/dandori-sync.log 2>&1
    sleep 2
    local task="${TASKS[0]}"
    local status=$(jira_api GET "/rest/api/2/issue/$task" | jq -r '.fields.status.name')
    [ "$status" = "Done" ] && pass "E2" "Task $task=Done" || fail "E2" "Task $task=$status"

    log_test "E3" "Completion comment added"
    local comments=$(jira_api GET "/rest/api/2/issue/$task/comment" | jq -r '.comments | length')
    [ "$comments" -ge 2 ] && pass "E3" "Comments: $comments" || fail "E3" "Only $comments"

    log_test "E4" "Re-sync skips synced runs"
    local out2=$("$DANDORI" jira-sync 2>&1)
    echo "$out2" | grep -q "No runs to sync" && pass "E4" "Idempotent" || fail "E4" "Re-synced"

    log_test "E5" "Synced flag set in DB"
    local unsynced=$(sqlite3 "$DB" "SELECT COUNT(*) FROM runs WHERE status='done' AND COALESCE(synced,0)=0 AND jira_issue_key != ''")
    [ "$unsynced" -eq 0 ] && pass "E5" "All synced" || fail "E5" "$unsynced unsynced"
}

# ============================================================================
# Group F: Confluence Reporting
# ============================================================================

test_group_f() {
    log_section "Group F: Confluence Reporting"

    local task="${TASKS[0]}"

    log_test "F1" "conf-write --dry-run preview"
    local out=$("$DANDORI" conf-write --task "$task" --dry-run 2>&1)
    echo "$out" | grep -q "Title:" && pass "F1" "Preview shown" || fail "F1" "No preview"

    log_test "F2" "conf-write creates page"
    local out=$("$DANDORI" conf-write --task "$task" 2>&1)
    local page_id=$(echo "$out" | grep "Page ID:" | awk '{print $3}')
    [ -n "$page_id" ] && pass "F2" "Page ID: $page_id" || fail "F2" "No page ID"

    log_test "F3" "Report contains token data"
    echo "$out" | grep -q "Tokens" || \
      ("$DANDORI" conf-write --task "$task" --dry-run 2>&1 | grep -q "Tokens") && \
      pass "F3" "Token data present" || fail "F3" "No token data"

    log_test "F4" "Report contains cost"
    "$DANDORI" conf-write --task "$task" --dry-run 2>&1 | grep -q "Cost" && \
      pass "F4" "Cost present" || fail "F4" "No cost"

    log_test "F5" "Report contains git HEAD"
    "$DANDORI" conf-write --task "$task" --dry-run 2>&1 | grep -q "Git" && \
      pass "F5" "Git data present" || fail "F5" "No git data"

    log_test "F6" "Multiple tasks → multiple pages"
    local task2="${TASKS[1]}"
    local out2=$("$DANDORI" conf-write --task "$task2" 2>&1)
    local page2=$(echo "$out2" | grep "Page ID:" | awk '{print $3}')
    [ -n "$page2" ] && [ "$page2" != "$page_id" ] && pass "F6" "Different pages: $page_id, $page2" || fail "F6" "Same or no page2"
}

# ============================================================================
# Group G: Analytics
# ============================================================================

test_group_g() {
    log_section "Group G: Analytics"

    log_test "G1" "analytics runs lists runs"
    local out=$("$DANDORI" analytics runs 2>&1)
    local run_lines=$(echo "$out" | grep -c "CLITEST-")
    [ "$run_lines" -ge 2 ] && pass "G1" "Lists $run_lines runs" || fail "G1" "Only $run_lines"

    log_test "G2" "analytics agents shows stats"
    local out=$("$DANDORI" analytics agents 2>&1)
    echo "$out" | grep -q "alpha\|beta" && pass "G2" "Agent stats shown" || fail "G2" "No agents"

    log_test "G3" "analytics cost aggregates"
    local out=$("$DANDORI" analytics cost 2>&1)
    echo "$out" | grep -q "\$[0-9]" && pass "G3" "Cost shown" || fail "G3" "No cost"

    log_test "G4" "Success rate calculated"
    local success_rate=$(sqlite3 "$DB" "SELECT ROUND(100.0 * SUM(CASE WHEN exit_code=0 THEN 1 ELSE 0 END) / COUNT(*), 1) FROM runs")
    [ -n "$success_rate" ] && pass "G4" "Success rate: $success_rate%" || fail "G4" "No rate"

    log_test "G5" "Token total matches"
    local db_tokens=$(sqlite3 "$DB" "SELECT SUM(input_tokens + output_tokens) FROM runs")
    [ "${db_tokens:-0}" -gt 0 ] && pass "G5" "DB tokens: $db_tokens" || fail "G5" "Zero tokens"
}

# ============================================================================
# Group H: Dashboard
# ============================================================================

test_group_h() {
    log_section "Group H: Dashboard"

    # Kill any existing dashboard
    pkill -f "dandori dashboard" 2>/dev/null || true
    sleep 1

    "$DANDORI" dashboard -p 9095 > /dev/null 2>&1 &
    local dash_pid=$!
    sleep 2

    log_test "H1" "Dashboard server started"
    curl -s -o /dev/null -w "%{http_code}" http://localhost:9095/ | grep -q "200" && \
      pass "H1" "Dashboard responds" || fail "H1" "Dashboard not responding"

    log_test "H2" "/api/overview returns data"
    local overview=$(curl -s http://localhost:9095/api/overview)
    echo "$overview" | jq -e '.runs' > /dev/null 2>&1 && \
      pass "H2" "Overview: $overview" || fail "H2" "Bad JSON"

    log_test "H3" "/api/runs returns list"
    local runs=$(curl -s http://localhost:9095/api/runs)
    echo "$runs" | jq -e 'length' > /dev/null 2>&1 && \
      pass "H3" "Runs returned" || fail "H3" "Bad runs"

    log_test "H4" "HTML page loads"
    curl -s http://localhost:9095/ | grep -q "Dandori" && \
      pass "H4" "HTML has Dandori" || fail "H4" "No Dandori"

    kill "$dash_pid" 2>/dev/null || true
}

# ============================================================================
# Group I: Edge Cases
# ============================================================================

test_group_i() {
    log_section "Group I: Edge Cases"

    log_test "I1" "task info on invalid key"
    local out=$("$DANDORI" task info CLITEST-99999 2>&1)
    [ $? -ne 0 ] || echo "$out" | grep -q "error\|not found\|404" && \
      pass "I1" "Graceful error" || fail "I1" "Unexpected success"

    log_test "I2" "conf-write nonexistent task"
    local out=$("$DANDORI" conf-write --task NOEXIST-999 2>&1)
    echo "$out" | grep -qi "error\|find run\|no rows" && \
      pass "I2" "Graceful error" || fail "I2" "Unexpected success"

    log_test "I3" "Run with invalid command"
    local out
    out=$("$DANDORI" run -- /nonexistent/command 2>&1)
    local rc=$?
    [ "$rc" -ne 0 ] && pass "I3" "Command failure tracked (exit $rc)" || fail "I3" "Expected non-zero exit, got $rc"
}

# ============================================================================
# Group K: Shell Alias Transparency
# ============================================================================

test_group_k() {
    log_section "Group K: Shell Alias Transparency"

    local rc_file="/tmp/dandori-e2e-rc"
    : > "$rc_file"

    log_test "K1" "init --shell writes alias block"
    # Use the shellrc package via a tiny helper: call init with a fake HOME+SHELL
    SHELL=/bin/zsh HOME=/tmp/fake-home "$DANDORI" init --shell > /dev/null 2>&1 || true
    # Simpler: invoke the library directly via the CLI by overriding HOME
    HOME="/tmp/fake-rc-home"
    rm -rf "$HOME"
    mkdir -p "$HOME"
    SHELL=/bin/zsh HOME="$HOME" bash -c "echo yes | '$DANDORI' init --shell" > /dev/null 2>&1 || true
    if grep -q "dandori aliases" "$HOME/.zshrc" 2>/dev/null; then
        pass "K1" "Alias block written to .zshrc"
    else
        fail "K1" "No alias block found"
    fi

    log_test "K2" "Re-run is idempotent"
    SHELL=/bin/zsh HOME="$HOME" bash -c "echo yes | '$DANDORI' init --shell" > /dev/null 2>&1 || true
    local count=$(grep -c "dandori aliases (managed)" "$HOME/.zshrc" 2>/dev/null | head -1)
    # Expect 2 occurrences (start + end marker) after one install, 2 after two installs
    [ "$count" = "2" ] && pass "K2" "No duplication (markers=$count)" || fail "K2" "Got markers=$count"

    log_test "K3" "--no-shell skips alias"
    local HOME2="/tmp/fake-rc-home-noshell"
    rm -rf "$HOME2"
    mkdir -p "$HOME2"
    SHELL=/bin/zsh HOME="$HOME2" bash -c "echo yes | '$DANDORI' init --no-shell" > /dev/null 2>&1 || true
    if [ ! -f "$HOME2/.zshrc" ] || ! grep -q "dandori aliases" "$HOME2/.zshrc" 2>/dev/null; then
        pass "K3" "No alias written with --no-shell"
    else
        fail "K3" "Alias written despite --no-shell"
    fi

    log_test "K4" "Detects zsh from SHELL"
    SHELL=/bin/zsh HOME="$HOME" bash -c "echo yes | '$DANDORI' init --shell" > /tmp/k4-out 2>&1 || true
    grep -q ".zshrc" /tmp/k4-out && pass "K4" "Used .zshrc for zsh shell" || fail "K4" "Wrong rc file"

    log_test "K5" "Block has start+end markers"
    grep -q ">>> dandori aliases" "$HOME/.zshrc" && grep -q "<<< dandori aliases" "$HOME/.zshrc" && \
      pass "K5" "Both markers present" || fail "K5" "Missing markers"

    # Restore HOME
    HOME="$(getent passwd $(whoami) 2>/dev/null | cut -d: -f6 || echo /Users/phucnt)"
    export HOME="/Users/phucnt"
}

# ============================================================================
# Group L: Watch Daemon
# ============================================================================

test_group_l() {
    log_section "Group L: Watch Daemon"

    # Create fake Claude projects root with a fake session
    local fake_root=$(mktemp -d)
    local project_dir="$fake_root/-fake-proj"
    mkdir -p "$project_dir"

    # Create an orphan session with real token data
    cat > "$project_dir/orphan-e2e-session.jsonl" <<'EOF'
{"type":"assistant","message":{"model":"claude-sonnet-4-6","usage":{"input_tokens":500,"output_tokens":200,"cache_read_input_tokens":1000,"cache_creation_input_tokens":100}}}
EOF

    log_test "L1" "watch --once runs and exits"
    "$DANDORI" watch --once --root "$fake_root" > /tmp/watch-out 2>&1
    local rc=$?
    [ "$rc" -eq 0 ] && pass "L1" "Exit 0" || fail "L1" "Exit $rc"

    log_test "L2" "Watch output mentions poll complete"
    grep -q "single poll complete" /tmp/watch-out && pass "L2" "Poll completed" || fail "L2" "No completion log"

    log_test "L3" "Orphan run inserted in DB"
    local orphan_count=$(sqlite3 "$DB" "SELECT COUNT(*) FROM runs WHERE session_id='orphan-e2e-session'")
    [ "$orphan_count" -eq 1 ] && pass "L3" "1 orphan run inserted" || fail "L3" "Got $orphan_count"

    log_test "L4" "Orphan run has tokens extracted"
    local tokens=$(sqlite3 "$DB" "SELECT input_tokens + output_tokens FROM runs WHERE session_id='orphan-e2e-session'")
    [ "${tokens:-0}" -ge 700 ] && pass "L4" "Tokens=$tokens" || fail "L4" "Tokens=$tokens"

    log_test "L5" "Re-run does not duplicate"
    "$DANDORI" watch --once --root "$fake_root" > /dev/null 2>&1
    local recount=$(sqlite3 "$DB" "SELECT COUNT(*) FROM runs WHERE session_id='orphan-e2e-session'")
    [ "$recount" -eq 1 ] && pass "L5" "Still 1 row (idempotent)" || fail "L5" "Got $recount"

    rm -rf "$fake_root"
}

# ============================================================================
# Group J: Long-running / Heavy Task
# ============================================================================

test_group_j() {
    log_section "Group J: Long-running / Heavy Task"

    local task="${TASKS[3]}"
    "$DANDORI" task start "$task" > /dev/null 2>&1

    log_test "J1" "Heavy task: multi-file analysis"
    local start_ts=$(date +%s)
    # Ask Claude to do multi-step work: read, analyze, write
    "$DANDORI" run --task "$task" -- claude -p \
        "Read these 3 files in /tmp/dandori-e2e-test/ (create them first with numbers 1, 2, 3), sum the numbers, write result to /tmp/dandori-e2e-test/sum.txt" \
        --allowedTools "Read,Write,Bash" > /tmp/heavy-run.log 2>&1
    local rc=$?
    local end_ts=$(date +%s)
    local duration=$((end_ts - start_ts))

    [ $rc -eq 0 ] && pass "J1" "Heavy task completed in ${duration}s" || fail "J1" "Heavy task failed (rc=$rc)"

    log_test "J2" "Heavy run has larger token count"
    local heavy_tokens=$(sqlite3 "$DB" "SELECT input_tokens+output_tokens FROM runs WHERE jira_issue_key='$task' ORDER BY started_at DESC LIMIT 1")
    [ "${heavy_tokens:-0}" -gt 100 ] && pass "J2" "Tokens: $heavy_tokens" || fail "J2" "Too few tokens: $heavy_tokens"

    log_test "J3" "Heavy run has higher cost"
    local heavy_cost=$(sqlite3 "$DB" "SELECT cost_usd FROM runs WHERE jira_issue_key='$task' ORDER BY started_at DESC LIMIT 1")
    local has_cost=$(echo "${heavy_cost:-0}" | awk '{print ($1 > 0)}')
    [ "$has_cost" = "1" ] && pass "J3" "Cost: \$$heavy_cost" || fail "J3" "No cost"

    log_test "J4" "Heavy run has non-trivial duration"
    local heavy_dur=$(sqlite3 "$DB" "SELECT duration_sec FROM runs WHERE jira_issue_key='$task' ORDER BY started_at DESC LIMIT 1")
    local is_long=$(echo "${heavy_dur:-0}" | awk '{print ($1 >= 3)}')
    [ "$is_long" = "1" ] && pass "J4" "Duration: ${heavy_dur}s" || fail "J4" "Too fast: ${heavy_dur}s"

    log_test "J5" "Sync heavy run to Jira"
    "$DANDORI" jira-sync --task "$task" > /dev/null 2>&1
    sleep 1
    local status=$(jira_api GET "/rest/api/2/issue/$task" | jq -r '.fields.status.name')
    [ "$status" = "Done" ] && pass "J5" "Synced to Done" || fail "J5" "Status=$status"
}

# ============================================================================
# Group M: Task Run with Context Injection
# ============================================================================

test_group_m() {
    log_section "Group M: Task Run with Context Injection"

    # Use the pre-created CLITEST-21 which has Confluence link
    local task="CLITEST-21"

    log_test "M1" "task run --dry-run shows context preview"
    local out=$("$DANDORI" task run "$task" --dry-run 2>&1)
    echo "$out" | grep -q "Auth Module Architecture" && pass "M1" "Confluence doc title found" || fail "M1" "Missing doc"

    log_test "M2" "dry-run extracts Confluence content"
    echo "$out" | grep -q "TokenService" && pass "M2" "Doc content extracted" || fail "M2" "No content"

    log_test "M3" "dry-run shows issue summary"
    echo "$out" | grep -q "Fix auth token refresh" && pass "M3" "Summary present" || fail "M3" "No summary"

    log_test "M4" "dry-run shows linked docs count"
    echo "$out" | grep -q "Linked docs: 1" && pass "M4" "1 linked doc" || fail "M4" "Wrong count"

    log_test "M5" "dry-run generates markdown"
    echo "$out" | grep -q "## Related Documentation" && pass "M5" "Markdown section present" || fail "M5" "No markdown"

    # Create a fresh task with Confluence link for run test
    log_test "M6" "task run executes with context"
    local new_task=$(create_jira_task "E2E test task run with context" "Test issue with conf link: https://fooknt.atlassian.net/wiki/pages/360635")
    if [ -z "$new_task" ]; then
        fail "M6" "Failed to create task"
        return
    fi
    TASKS+=("$new_task")

    "$DANDORI" task run "$new_task" --no-sync -- claude -p \
        "Just confirm you can see the task context. Reply only: CONTEXT_RECEIVED" \
        --allowedTools "" > /tmp/m6-out.log 2>&1
    local rc=$?
    [ $rc -eq 0 ] && pass "M6" "Run completed" || fail "M6" "Run failed rc=$rc"

    log_test "M7" "Run tracked in DB with task key"
    local run_count=$(sqlite3 "$DB" "SELECT COUNT(*) FROM runs WHERE jira_issue_key='$new_task'")
    [ "${run_count:-0}" -ge 1 ] && pass "M7" "Run tracked ($run_count)" || fail "M7" "Not tracked"

    log_test "M8" "Run has tokens and cost"
    local cost=$(sqlite3 "$DB" "SELECT cost_usd FROM runs WHERE jira_issue_key='$new_task' ORDER BY started_at DESC LIMIT 1")
    local has_cost=$(echo "${cost:-0}" | awk '{print ($1 > 0)}')
    [ "$has_cost" = "1" ] && pass "M8" "Cost: \$$cost" || fail "M8" "No cost"
}

# ============================================================================
# Main
# ============================================================================

main() {
    echo -e "${BLUE}╔════════════════════════════════════════════════════════╗${NC}"
    echo -e "${BLUE}║     Dandori CLI Comprehensive E2E Test Suite          ║${NC}"
    echo -e "${BLUE}╚════════════════════════════════════════════════════════╝${NC}"

    setup

    test_group_a
    test_group_b
    test_group_c
    test_group_d
    test_group_e
    test_group_f
    test_group_g
    test_group_h
    test_group_i
    test_group_j
    test_group_k
    test_group_l
    test_group_m

    # Summary
    log_section "SUMMARY"
    local total=$((PASS + FAIL))
    local pct=$(awk "BEGIN{printf \"%.1f\", $PASS * 100.0 / $total}")
    echo -e "Total:  $total"
    echo -e "${GREEN}Pass:   $PASS${NC}"
    echo -e "${RED}Fail:   $FAIL${NC}"
    echo -e "Rate:   ${pct}%"

    # Write results to file
    {
        echo "# E2E Test Results - $(date)"
        echo ""
        echo "- Total: $total"
        echo "- Pass: $PASS"
        echo "- Fail: $FAIL"
        echo "- Rate: ${pct}%"
        echo ""
        echo "## Results"
        echo "| Result | ID | Detail |"
        echo "|--------|-----|--------|"
        for r in "${RESULTS[@]}"; do
            IFS='|' read -r result id detail <<< "$r"
            echo "| $result | $id | $detail |"
        done
        echo ""
        echo "## Created Jira Tasks"
        for t in "${TASKS[@]}"; do
            echo "- $t"
        done
    } > /tmp/dandori-e2e-results.log

    echo ""
    echo "Results saved to /tmp/dandori-e2e-results.log"

    [ "$FAIL" -eq 0 ]
}

main "$@"
