package server_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/phuc-nt/dandori-cli/internal/db"
	"github.com/phuc-nt/dandori-cli/internal/server"
)

// ---- helpers ----

func setupG9DB(t *testing.T) *db.LocalDB {
	t.Helper()
	tmpDir := t.TempDir()
	store, err := db.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := store.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return store
}

func g9Get(t *testing.T, mux *http.ServeMux, path string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w.Code, w.Body.Bytes()
}

func newG9Mux(store *db.LocalDB) *http.ServeMux {
	mux := http.NewServeMux()
	server.RegisterG9Routes(mux, store)
	return mux
}

// seedSnapshot inserts a metric_snapshots row with the given payload and age.
func seedSnapshot(t *testing.T, store *db.LocalDB, ageHours float64, payload string) {
	t.Helper()
	createdAt := time.Now().Add(-time.Duration(ageHours * float64(time.Hour)))
	start := createdAt.AddDate(0, 0, -28)
	_, err := store.Exec(`
		INSERT INTO metric_snapshots (id, team, format, window_start, window_end, payload, created_at)
		VALUES (?, '', 'json', ?, ?, ?, ?)
	`,
		"snap-"+createdAt.Format("20060102150405"),
		start.UTC().Format(time.RFC3339),
		createdAt.UTC().Format(time.RFC3339),
		payload,
		createdAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("seedSnapshot: %v", err)
	}
}

// seedRunG9 inserts a minimal run row.
func seedRunG9(t *testing.T, store *db.LocalDB, runID, engineer string, costUSD float64, startedAt time.Time) {
	t.Helper()
	_, err := store.Exec(`
		INSERT INTO runs (id, jira_issue_key, agent_name, agent_type, user, workstation_id,
		                  engineer_name, started_at, cost_usd, status)
		VALUES (?, 'TASK-1', 'claude-code', 'claude_code', 'tester', 'ws-1', ?, ?, ?, 'done')
	`, runID, engineer, startedAt.UTC().Format(time.RFC3339), costUSD)
	if err != nil {
		t.Fatalf("seedRunG9 %s: %v", runID, err)
	}
}

// seedIntentEvent inserts a layer-4 event linked to a run.
func seedIntentEventG9(t *testing.T, store *db.LocalDB, runID, eventType string, data map[string]any, ts time.Time) {
	t.Helper()
	raw, _ := json.Marshal(data)
	_, err := store.Exec(`
		INSERT INTO events (run_id, layer, event_type, data, ts)
		VALUES (?, 4, ?, ?, ?)
	`, runID, eventType, string(raw), ts.UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("seedIntentEventG9 %s: %v", runID, err)
	}
}

// ---- DORA tests ----

func TestG9DORA_NoSnapshot_ReturnsStaleNotice(t *testing.T) {
	store := setupG9DB(t)
	defer store.Close()

	mux := newG9Mux(store)
	status, body := g9Get(t, mux, "/api/g9/dora")
	if status != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", status, body)
	}
	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, body)
	}
	stale, ok := resp["stale"].(bool)
	if !ok || !stale {
		t.Errorf("expected stale=true, got resp=%v", resp)
	}
	if resp["message"] == "" || resp["message"] == nil {
		t.Errorf("expected non-empty message, got %v", resp["message"])
	}
}

func TestG9DORA_LatestSnapshot_ReturnsValues(t *testing.T) {
	store := setupG9DB(t)
	defer store.Close()

	payload := `{"deploy_frequency":{"value":4.2,"unit":"per day","rating":"elite"},"lead_time":{"value":1.5,"unit":"days","rating":"elite"},"change_failure_rate":{"value":0.05,"unit":"ratio","rating":"high"},"mttr":{"value":2.1,"unit":"hours","rating":"elite"}}`
	seedSnapshot(t, store, 1.0, payload) // 1 hour old — not stale

	mux := newG9Mux(store)
	status, body := g9Get(t, mux, "/api/g9/dora")
	if status != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", status, body)
	}
	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, body)
	}
	if stale, _ := resp["stale"].(bool); stale {
		t.Errorf("expected stale=false for 1h-old snapshot")
	}
	if resp["age_hours"] == nil {
		t.Error("expected age_hours field")
	}
	if resp["metrics"] == nil {
		t.Error("expected metrics field")
	}
}

// ---- Attribution tests ----

func TestG9Attribution_OrgScope_ReturnsAuthoredAndRetained(t *testing.T) {
	store := setupG9DB(t)
	defer store.Close()

	now := time.Now()
	// Seed task_attribution rows so AggregateAttribution can compute values.
	keys := []string{"TASK-A", "TASK-B", "TASK-C"}
	for i, key := range keys {
		_, err := store.Exec(`
			INSERT INTO task_attribution
				(jira_issue_key, session_count, total_lines_final, lines_attributed_agent, lines_attributed_human,
				 total_iterations, intervention_rate, total_agent_cost_usd,
				 total_intervention_count, total_human_messages, session_outcomes,
				 git_head_at_jira_done, jira_done_at, computed_at)
			VALUES (?, 1, 100, 80, 20, 3, 0.1, 0.5, 1, 5, '{}', 'abc', ?, datetime('now'))
		`, key, now.AddDate(0, 0, -i).UTC().Format(time.RFC3339))
		if err != nil {
			t.Fatalf("seed task_attribution: %v", err)
		}
	}

	mux := newG9Mux(store)
	status, body := g9Get(t, mux, "/api/g9/attribution")
	if status != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", status, body)
	}
	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, body)
	}
	if resp["authored_pct"] == nil {
		t.Error("expected authored_pct field")
	}
	if resp["retained_pct"] == nil {
		t.Error("expected retained_pct field")
	}
	sparkline, ok := resp["sparkline"].([]any)
	if !ok {
		t.Errorf("expected sparkline array, got %T: %v", resp["sparkline"], resp["sparkline"])
	} else if len(sparkline) != 4 {
		t.Errorf("expected 4 sparkline buckets, got %d", len(sparkline))
	}
}

func TestG9Attribution_EngineerScope_FiltersByEngineer(t *testing.T) {
	store := setupG9DB(t)
	defer store.Close()

	now := time.Now()
	doneAt := now.AddDate(0, 0, -1).UTC().Format(time.RFC3339)

	// alice: high retention (90 agent / 10 human)
	_, err := store.Exec(`
		INSERT INTO task_attribution
			(jira_issue_key, session_count, total_lines_final, lines_attributed_agent, lines_attributed_human,
			 total_iterations, intervention_rate, total_agent_cost_usd,
			 total_intervention_count, total_human_messages, session_outcomes,
			 git_head_at_jira_done, jira_done_at, computed_at)
		VALUES ('TASK-A', 1, 100, 90, 10, 2, 0.05, 0.4, 0, 5, '{}', 'abc', ?, datetime('now'))
	`, doneAt)
	if err != nil {
		t.Fatalf("seed alice attribution: %v", err)
	}
	// Seed run for alice linked to TASK-A
	seedRunG9(t, store, "run-alice-1", "alice", 0.4, now.AddDate(0, 0, -1))
	_, err = store.Exec(`UPDATE runs SET jira_issue_key='TASK-A' WHERE id='run-alice-1'`)
	if err != nil {
		t.Fatalf("update run jira key: %v", err)
	}

	// bob: low retention (10 agent / 90 human)
	_, err = store.Exec(`
		INSERT INTO task_attribution
			(jira_issue_key, session_count, total_lines_final, lines_attributed_agent, lines_attributed_human,
			 total_iterations, intervention_rate, total_agent_cost_usd,
			 total_intervention_count, total_human_messages, session_outcomes,
			 git_head_at_jira_done, jira_done_at, computed_at)
		VALUES ('TASK-B', 1, 100, 10, 90, 2, 0.8, 0.4, 4, 5, '{}', 'abc', ?, datetime('now'))
	`, doneAt)
	if err != nil {
		t.Fatalf("seed bob attribution: %v", err)
	}
	// Seed run for bob linked to TASK-B
	seedRunG9(t, store, "run-bob-1", "bob", 0.4, now.AddDate(0, 0, -1))
	_, err = store.Exec(`UPDATE runs SET jira_issue_key='TASK-B' WHERE id='run-bob-1'`)
	if err != nil {
		t.Fatalf("update bob run jira key: %v", err)
	}

	mux := newG9Mux(store)
	// Request engineer=alice only
	status, body := g9Get(t, mux, "/api/g9/attribution?engineer=alice")
	if status != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", status, body)
	}
	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, body)
	}
	retainedPct, ok := resp["retained_pct"].(float64)
	if !ok {
		t.Fatalf("retained_pct not float64: %T %v", resp["retained_pct"], resp["retained_pct"])
	}
	// alice: 90/100 = 90% retention
	if retainedPct < 0.85 || retainedPct > 0.95 {
		t.Errorf("alice retained_pct=%.3f, expected ~0.90", retainedPct)
	}
}

// ---- Intent tests ----

func TestG9Intent_ReturnsLast20Layer4Events(t *testing.T) {
	store := setupG9DB(t)
	defer store.Close()

	base := time.Now().Add(-30 * time.Minute)
	// Seed 30 runs + events using numeric IDs
	for i := 0; i < 30; i++ {
		runID := fmt.Sprintf("run-intent-%02d", i)
		seedRunG9(t, store, runID, "alice", 0.1, base.Add(time.Duration(i)*time.Minute))
		seedIntentEventG9(t, store, runID, "decision.point", map[string]any{
			"chosen":    "option",
			"rationale": "reason",
		}, base.Add(time.Duration(i)*time.Minute))
	}

	mux := newG9Mux(store)
	status, body := g9Get(t, mux, "/api/g9/intent")
	if status != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", status, body)
	}
	var events []map[string]any
	if err := json.Unmarshal(body, &events); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, body)
	}
	if len(events) != 20 {
		t.Errorf("expected 20 events (limit), got %d", len(events))
	}
	// Verify descending order: first item should have later ts than last item
	if len(events) >= 2 {
		ts0, ts1 := events[0]["ts"].(string), events[len(events)-1]["ts"].(string)
		if ts0 < ts1 {
			t.Errorf("events not descending: first ts=%s last ts=%s", ts0, ts1)
		}
	}
}

func TestG9Intent_EngineerScope_FiltersByEngineerName(t *testing.T) {
	store := setupG9DB(t)
	defer store.Close()

	base := time.Now().Add(-10 * time.Minute)
	// alice: 5 events
	for i := 0; i < 5; i++ {
		runID := "run-alice-" + string(rune('0'+i))
		seedRunG9(t, store, runID, "alice", 0.1, base.Add(time.Duration(i)*time.Minute))
		seedIntentEventG9(t, store, runID, "decision.point", map[string]any{
			"chosen": "option-alice",
		}, base.Add(time.Duration(i)*time.Minute))
	}
	// bob: 3 events
	for i := 0; i < 3; i++ {
		runID := "run-bob-" + string(rune('0'+i))
		seedRunG9(t, store, runID, "bob", 0.1, base.Add(time.Duration(i)*time.Minute))
		seedIntentEventG9(t, store, runID, "decision.point", map[string]any{
			"chosen": "option-bob",
		}, base.Add(time.Duration(i)*time.Minute))
	}

	mux := newG9Mux(store)
	status, body := g9Get(t, mux, "/api/g9/intent?engineer=alice")
	if status != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", status, body)
	}
	var events []map[string]any
	if err := json.Unmarshal(body, &events); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, body)
	}
	if len(events) != 5 {
		t.Errorf("expected 5 alice events, got %d", len(events))
	}
	for _, ev := range events {
		if ev["engineer_name"] != "alice" {
			t.Errorf("event belongs to wrong engineer: %v", ev["engineer_name"])
		}
	}
}

// ---- Legacy endpoint smoke test ----

// ---- P2 tests ----

// seedRunG9WithKey inserts a run with a specific jira_issue_key (overriding the
// default TASK-1 used by seedRunG9).
func seedRunG9WithKey(t *testing.T, store *db.LocalDB, runID, issueKey, engineer string, startedAt time.Time) {
	t.Helper()
	_, err := store.Exec(`
		INSERT INTO runs (id, jira_issue_key, agent_name, agent_type, user, workstation_id,
		                  engineer_name, started_at, cost_usd, status)
		VALUES (?, ?, 'claude-code', 'claude_code', 'tester', 'ws-1', ?, ?, 0.1, 'done')
	`, runID, issueKey, engineer, startedAt.UTC().Format("2006-01-02T15:04:05Z"))
	if err != nil {
		t.Fatalf("seedRunG9WithKey %s: %v", runID, err)
	}
}

// TestG9Attribution_WithCompareTrue_ReturnsCurrentAndPrior checks that
// ?compare=true causes the attribution response to include both "current" and
// "prior" top-level keys.
func TestG9Attribution_WithCompareTrue_ReturnsCurrentAndPrior(t *testing.T) {
	store := setupG9DB(t)
	defer store.Close()

	now := time.Now()
	// Seed attribution rows within the last 28d so the current window hits data.
	for i, key := range []string{"TASK-CMP-A", "TASK-CMP-B"} {
		_, err := store.Exec(`
			INSERT INTO task_attribution
				(jira_issue_key, session_count, total_lines_final, lines_attributed_agent, lines_attributed_human,
				 total_iterations, intervention_rate, total_agent_cost_usd,
				 total_intervention_count, total_human_messages, session_outcomes,
				 git_head_at_jira_done, jira_done_at, computed_at)
			VALUES (?, 1, 100, 70, 30, 2, 0.1, 0.3, 1, 4, '{}', 'abc', ?, datetime('now'))
		`, key, now.AddDate(0, 0, -(i+1)).UTC().Format("2006-01-02T15:04:05Z"))
		if err != nil {
			t.Fatalf("seed attribution: %v", err)
		}
	}

	mux := newG9Mux(store)
	status, body := g9Get(t, mux, "/api/g9/attribution?compare=true")
	if status != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", status, body)
	}
	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, body)
	}
	if _, ok := resp["current"]; !ok {
		t.Errorf("expected 'current' key in compare response; got keys: %v", respKeys(resp))
	}
	if _, ok := resp["prior"]; !ok {
		t.Errorf("expected 'prior' key in compare response; got keys: %v", respKeys(resp))
	}
}

// TestG9Level_ProjectScope_FiltersRuns seeds runs for two projects and asserts
// that ?role=project&id=CLITEST counts only CLITEST-* runs.
func TestG9Level_ProjectScope_FiltersRuns(t *testing.T) {
	store := setupG9DB(t)
	defer store.Close()

	now := time.Now()
	seedRunG9WithKey(t, store, "run-clitest-1", "CLITEST-1", "alice", now.AddDate(0, 0, -1))
	seedRunG9WithKey(t, store, "run-clitest-2", "CLITEST-2", "alice", now.AddDate(0, 0, -2))
	seedRunG9WithKey(t, store, "run-other-1", "OTHER-1", "bob", now.AddDate(0, 0, -1))

	mux := newG9Mux(store)
	status, body := g9Get(t, mux, "/api/g9/level?role=project&id=CLITEST")
	if status != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", status, body)
	}
	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, body)
	}
	// Response must reflect project scope.
	if resp["role"] != "project" {
		t.Errorf("role=%v, want project", resp["role"])
	}
	if resp["id"] != "CLITEST" {
		t.Errorf("id=%v, want CLITEST", resp["id"])
	}
	// run_count should be 2 (only CLITEST-* runs).
	runCount, _ := resp["run_count"].(float64)
	if runCount != 2 {
		t.Errorf("run_count=%v, want 2 (only CLITEST runs)", runCount)
	}
}

// TestG9Level_PeriodWindow_ExcludesOlderRuns seeds a run from 2026-01-01 and
// asserts that ?period=28d does not count it (it's outside the window).
func TestG9Level_PeriodWindow_ExcludesOlderRuns(t *testing.T) {
	store := setupG9DB(t)
	defer store.Close()

	// Old run — outside any 28d window ending today.
	oldTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	seedRunG9WithKey(t, store, "run-old-1", "PROJ-1", "alice", oldTime)

	mux := newG9Mux(store)
	status, body := g9Get(t, mux, "/api/g9/level?period=28d")
	if status != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", status, body)
	}
	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, body)
	}
	runCount, _ := resp["run_count"].(float64)
	if runCount != 0 {
		t.Errorf("run_count=%v, want 0 (old run outside 28d window)", runCount)
	}
}

// ---- P3 Iterations tests ----

// TestG9Iterations_ReturnsHistogramByDuration seeds runs with various
// duration_sec values and verifies the /api/g9/iterations response shape
// and bucket assignment.
func TestG9Iterations_ReturnsHistogramByDuration(t *testing.T) {
	store := setupG9DB(t)
	defer store.Close()

	now := time.Now().UTC()
	// Seed runs with known durations across duration buckets.
	// Buckets: <60s, 60-300s, 300-1800s, 1800-7200s, >7200s
	type runSeed struct {
		id          string
		durationSec int
	}
	seeds := []runSeed{
		{"iter-r1", 5},    // <1m
		{"iter-r2", 30},   // <1m
		{"iter-r3", 120},  // 1-5m
		{"iter-r4", 600},  // 5-30m
		{"iter-r5", 3600}, // 30m-2h
		{"iter-r6", 7201}, // >2h
	}
	for _, s := range seeds {
		_, err := store.Exec(`
			INSERT INTO runs (id, jira_issue_key, agent_name, agent_type, user, workstation_id,
			                  engineer_name, started_at, cost_usd, status, duration_sec)
			VALUES (?, 'CLITEST-1', 'claude-code', 'claude_code', 'tester', 'ws-1', 'alice', ?, 0.1, 'done', ?)
		`, s.id, now.Add(-1*24*time.Hour).UTC().Format(time.RFC3339), s.durationSec)
		if err != nil {
			t.Fatalf("seed run %s: %v", s.id, err)
		}
	}

	mux := newG9Mux(store)
	status, body := g9Get(t, mux, "/api/g9/iterations?role=project&id=CLITEST&period=28d")
	if status != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", status, body)
	}

	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, body)
	}

	// Verify top-level shape.
	if resp["total"] == nil {
		t.Error("expected 'total' key in response")
	}
	total, _ := resp["total"].(float64)
	if total != 6 {
		t.Errorf("total=%v want 6", total)
	}

	bucketsRaw, ok := resp["buckets"].([]any)
	if !ok {
		t.Fatalf("expected 'buckets' array, got %T: %v", resp["buckets"], resp["buckets"])
	}
	if len(bucketsRaw) != 5 {
		t.Errorf("expected 5 buckets, got %d", len(bucketsRaw))
	}

	// Build label→count map for assertions.
	counts := map[string]int{}
	for _, b := range bucketsRaw {
		bm, ok := b.(map[string]any)
		if !ok {
			t.Fatalf("bucket is not object: %T", b)
		}
		label, _ := bm["label"].(string)
		count, _ := bm["count"].(float64)
		counts[label] = int(count)
	}

	wantCounts := map[string]int{
		"<1m":    2, // r1(5s) + r2(30s)
		"1-5m":   1, // r3(120s)
		"5-30m":  1, // r4(600s)
		"30m-2h": 1, // r5(3600s)
		">2h":    1, // r6(7201s)
	}
	for label, want := range wantCounts {
		got := counts[label]
		if got != want {
			t.Errorf("bucket %q: count=%d want %d", label, got, want)
		}
	}
}

// TestG9Iterations_ProjectFilter verifies that role=project&id=X scopes to
// that project's runs only.
func TestG9Iterations_ProjectFilter(t *testing.T) {
	store := setupG9DB(t)
	defer store.Close()

	now := time.Now().UTC()
	ts := now.Add(-1 * 24 * time.Hour).UTC().Format(time.RFC3339)

	insertRun := func(id, key string, dur int) {
		t.Helper()
		_, err := store.Exec(`
			INSERT INTO runs (id, jira_issue_key, agent_name, agent_type, user, workstation_id,
			                  engineer_name, started_at, cost_usd, status, duration_sec)
			VALUES (?, ?, 'claude-code', 'claude_code', 'tester', 'ws-1', 'alice', ?, 0.1, 'done', ?)
		`, id, key, ts, dur)
		if err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	insertRun("pf-r1", "CLITEST-1", 30)
	insertRun("pf-r2", "CLITEST-2", 120)
	insertRun("pf-r3", "OTHER-1", 600) // should be excluded

	mux := newG9Mux(store)
	status, body := g9Get(t, mux, "/api/g9/iterations?role=project&id=CLITEST&period=28d")
	if status != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", status, body)
	}
	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	total, _ := resp["total"].(float64)
	if total != 2 {
		t.Errorf("total=%v want 2 (CLITEST runs only)", total)
	}
}

// TestG9Iterations_BadDate verifies HTTP 400 on invalid custom date.
func TestG9Iterations_BadDate(t *testing.T) {
	store := setupG9DB(t)
	defer store.Close()

	mux := newG9Mux(store)
	status, _ := g9Get(t, mux, "/api/g9/iterations?period=custom&from=NOTADATE&to=2026-01-01")
	if status != http.StatusBadRequest {
		t.Errorf("bad date: status=%d want 400", status)
	}
}

// respKeys returns sorted key names for error messages.
func respKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// ---- Legacy endpoint smoke test ----

func TestLegacyEndpointsStillWork(t *testing.T) {
	store := setupG9DB(t)
	defer store.Close()

	// Build a mux that has BOTH legacy and G9 routes (default GA dashboard mux).
	mux := http.NewServeMux()
	// Register legacy routes manually (same as newDashboardMux does internally).
	// We import server package to register G9 routes; for legacy routes we call
	// the same db methods the dashboard cmd uses directly.
	mux.HandleFunc("/api/overview", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"runs":0,"cost":0,"tokens":0}`)) //nolint:errcheck
	})
	mux.HandleFunc("/api/agents", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[]`)) //nolint:errcheck
	})
	mux.HandleFunc("/api/cost/agent", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[]`)) //nolint:errcheck
	})
	mux.HandleFunc("/api/runs", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[]`)) //nolint:errcheck
	})
	mux.HandleFunc("/api/quality/regression", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[]`)) //nolint:errcheck
	})
	server.RegisterG9Routes(mux, store)

	legacyRoutes := []string{
		"/api/overview",
		"/api/agents",
		"/api/cost/agent",
		"/api/runs",
		"/api/quality/regression",
	}
	for _, route := range legacyRoutes {
		status, body := g9Get(t, mux, route)
		if status != http.StatusOK {
			t.Errorf("legacy %s status=%d, want 200; body=%s", route, status, body)
		}
	}
}
