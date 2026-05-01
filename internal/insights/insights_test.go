package insights_test

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/phuc-nt/dandori-cli/internal/db"
	"github.com/phuc-nt/dandori-cli/internal/insights"
)

// setupInsightsDB creates a fresh SQLite test DB with schema applied.
func setupInsightsDB(t *testing.T) *db.LocalDB {
	t.Helper()
	tmpDir := t.TempDir()
	store, err := db.Open(filepath.Join(tmpDir, "insights-test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := store.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// insertRun inserts a run row with the given fields.
func insertRun(t *testing.T, store *db.LocalDB, id, agentName, engineerName, jiraKey string,
	costUSD float64, humanIntervention int, startedAt time.Time) {
	t.Helper()
	_, err := store.Exec(`
		INSERT INTO runs
			(id, agent_name, agent_type, user, workstation_id, engineer_name,
			 jira_issue_key, started_at, cost_usd, status, exit_code,
			 human_intervention_count)
		VALUES (?, ?, 'claude_code', 'tester', 'ws-1', ?, ?, ?, ?, 'done', 0, ?)
	`, id, agentName, engineerName, jiraKey,
		startedAt.UTC().Format(time.RFC3339), costUSD, humanIntervention)
	if err != nil {
		t.Fatalf("insertRun %s: %v", id, err)
	}
}

// ---- WoW cost spike ----

func TestWoWCostSpike_DetectsSpike(t *testing.T) {
	store := setupInsightsDB(t)
	now := time.Now().UTC()

	// Last week: $10 total across 10 runs
	for i := 0; i < 10; i++ {
		insertRun(t, store,
			fmt.Sprintf("lw-%d", i), "claude-code", "alice", "PROJ-1",
			1.0, 0,
			now.AddDate(0, 0, -10+i%3), // spreads over last week
		)
	}
	// This week: $15 total (50% spike, ratio=1.5)
	for i := 0; i < 5; i++ {
		insertRun(t, store,
			fmt.Sprintf("tw-%d", i), "claude-code", "alice", "PROJ-1",
			3.0, 0,
			now.AddDate(0, 0, -i%3), // within this week
		)
	}

	cards, err := insights.Compute(store, "")
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}

	var spikeCards []insights.Card
	for _, c := range cards {
		if c.ID == "wow-spike" {
			spikeCards = append(spikeCards, c)
		}
	}
	if len(spikeCards) != 1 {
		t.Errorf("expected 1 wow-spike card, got %d; all cards: %+v", len(spikeCards), cards)
	}
	if len(spikeCards) > 0 {
		c := spikeCards[0]
		if c.Severity != "high" && c.Severity != "medium" {
			t.Errorf("wow-spike severity=%q, want high or medium", c.Severity)
		}
		if c.Title == "" {
			t.Error("wow-spike card has empty title")
		}
		if c.Body == "" {
			t.Error("wow-spike card has empty body")
		}
	}
}

func TestWoWCostSpike_NoSpike(t *testing.T) {
	store := setupInsightsDB(t)
	now := time.Now().UTC()

	// Last week: $10
	for i := 0; i < 10; i++ {
		insertRun(t, store,
			fmt.Sprintf("lw-ns-%d", i), "claude-code", "alice", "PROJ-2",
			1.0, 0,
			now.AddDate(0, 0, -10+i%3),
		)
	}
	// This week: $10 (no spike)
	for i := 0; i < 10; i++ {
		insertRun(t, store,
			fmt.Sprintf("tw-ns-%d", i), "claude-code", "alice", "PROJ-2",
			1.0, 0,
			now.AddDate(0, 0, -i%3),
		)
	}

	cards, err := insights.Compute(store, "")
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}

	for _, c := range cards {
		if c.ID == "wow-spike" {
			t.Errorf("expected no wow-spike card, but got one: %+v", c)
		}
	}
}

// ---- Retention decay ----

func TestRetentionDecay_DetectsDelta(t *testing.T) {
	store := setupInsightsDB(t)
	now := time.Now().UTC()

	// alice: older runs (8–27 days ago) — 0% intervention rate over 20 runs.
	// These are in the 28d window but NOT in the 7d window.
	for i := 0; i < 20; i++ {
		insertRun(t, store,
			fmt.Sprintf("alice-old-clean-%d", i), "claude-code", "alice", "PROJ-1",
			0.5, 0, now.AddDate(0, 0, -27+i), // day -27 to -8
		)
	}

	// alice: last 7d — 40% intervention rate (5 runs, 2 interventions).
	for i := 0; i < 3; i++ {
		insertRun(t, store,
			fmt.Sprintf("alice-7d-clean-%d", i), "claude-code", "alice", "PROJ-1",
			0.5, 0, now.Add(-time.Duration(i+1)*24*time.Hour),
		)
	}
	for i := 0; i < 2; i++ {
		insertRun(t, store,
			fmt.Sprintf("alice-7d-inter-%d", i), "claude-code", "alice", "PROJ-1",
			0.5, 1, now.Add(-time.Duration(i+1)*24*time.Hour),
		)
	}
	// 7d rate = 2/5 = 0.40; 28d rate = 2/25 = 0.08; delta = 0.32 > 0.10 ✓

	cards, err := insights.Compute(store, "")
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}

	var found bool
	for _, c := range cards {
		if c.ID == "retention-decay-alice" {
			found = true
			if c.Body == "" {
				t.Error("retention-decay-alice body is empty")
			}
		}
	}
	if !found {
		t.Errorf("expected retention-decay-alice card; got cards: %+v", cards)
	}
}

func TestRetentionDecay_BelowMinSample(t *testing.T) {
	store := setupInsightsDB(t)
	now := time.Now().UTC()

	// Only 3 runs in last 7d — below min sample of 5 (all with interventions to
	// maximise delta, but should still be excluded due to sample size).
	for i := 0; i < 3; i++ {
		insertRun(t, store,
			fmt.Sprintf("alice-lowsample-7d-%d", i), "claude-code", "alice", "PROJ-3",
			0.5, 1, now.Add(-time.Duration(i+1)*24*time.Hour),
		)
	}
	// 20 runs strictly older than 7d (8–27 days ago) with 0 interventions.
	for i := 0; i < 20; i++ {
		insertRun(t, store,
			fmt.Sprintf("alice-lowsample-old-%d", i), "claude-code", "alice", "PROJ-3",
			0.5, 0, now.AddDate(0, 0, -27+i), // day -27 to -8, all outside 7d window
		)
	}
	// 7d: runs=3 — below min sample of 5 → no card expected.

	cards, err := insights.Compute(store, "")
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}

	for _, c := range cards {
		if c.ID == "retention-decay-alice" {
			t.Errorf("expected no retention-decay-alice card (below min sample), got: %+v", c)
		}
	}
}

// ---- Intervention cluster ----

func TestInterventionCluster_DetectsHighRate(t *testing.T) {
	store := setupInsightsDB(t)
	now := time.Now().UTC()

	// alice + claude-code: 6 runs, 4 interventions → rate=0.67 > 0.5
	for i := 0; i < 2; i++ {
		insertRun(t, store,
			fmt.Sprintf("cluster-clean-%d", i), "claude-code", "alice", "PROJ-1",
			0.5, 0, now.AddDate(0, 0, -20+i),
		)
	}
	for i := 0; i < 4; i++ {
		insertRun(t, store,
			fmt.Sprintf("cluster-inter-%d", i), "claude-code", "alice", "PROJ-1",
			0.5, 1, now.AddDate(0, 0, -20+i),
		)
	}

	cards, err := insights.Compute(store, "")
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}

	var found bool
	for _, c := range cards {
		if c.ID == "intervention-cluster-alice-claude-code" {
			found = true
			if c.Severity != "high" {
				t.Errorf("cluster card severity=%q, want high", c.Severity)
			}
		}
	}
	if !found {
		t.Errorf("expected intervention-cluster-alice-claude-code card; got: %+v", cards)
	}
}

// ---- Cost outlier task ----

func TestCostOutlierTask_DetectsOutlier(t *testing.T) {
	store := setupInsightsDB(t)
	now := time.Now().UTC()

	// 10 normal tasks at $1 each — gives project mean ~$2.73 with outlier included,
	// stddev ~$5.46, threshold ~$19.11. The outlier at $20 exceeds threshold.
	for i := 0; i < 10; i++ {
		insertRun(t, store,
			fmt.Sprintf("normal-task-%d", i), "claude-code", "alice",
			fmt.Sprintf("CLITEST-%d", 100+i),
			1.0, 0, now.AddDate(0, 0, -i-1),
		)
	}
	// 1 outlier task at $20 — clearly above mean+3σ and above 5*mean
	insertRun(t, store, "outlier-run", "claude-code", "alice", "CLITEST-999",
		20.0, 0, now.AddDate(0, 0, -1))

	cards, err := insights.Compute(store, "")
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}

	var found bool
	for _, c := range cards {
		if c.ID == "cost-outlier-CLITEST-999" {
			found = true
			if c.Body == "" {
				t.Error("cost-outlier body is empty")
			}
		}
	}
	if !found {
		t.Errorf("expected cost-outlier-CLITEST-999 card; got: %+v", cards)
	}
}

// ---- DORA traffic light ----

func TestDORATrafficLight_ReadsSnapshot(t *testing.T) {
	store := setupInsightsDB(t)
	now := time.Now().UTC()

	// Insert a metric_snapshot with faros format payload
	payload := map[string]any{
		"metrics": map[string]any{
			"deployment_frequency": map[string]any{
				"value": 4.2,
				"unit":  "per day",
			},
			"lead_time_for_changes": map[string]any{
				"p50_seconds": float64(3600), // 1h = elite
			},
			"change_failure_rate": map[string]any{
				"value": 0.03, // elite
			},
			"time_to_restore_service": map[string]any{
				"p50_seconds": float64(1800), // 0.5h = elite
			},
		},
	}
	payloadJSON, _ := json.Marshal(payload)
	start := now.AddDate(0, 0, -28)
	_, err := store.Exec(`
		INSERT INTO metric_snapshots (id, team, format, window_start, window_end, payload, created_at)
		VALUES ('snap-test', '', 'json', ?, ?, ?, ?)
	`, start.UTC().Format(time.RFC3339), now.UTC().Format(time.RFC3339),
		string(payloadJSON), now.UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("insert snapshot: %v", err)
	}

	cards, err := insights.Compute(store, "")
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}

	var found bool
	for _, c := range cards {
		if c.ID == "dora-traffic-light" {
			found = true
			if c.Body == "" {
				t.Error("dora-traffic-light body is empty")
			}
		}
	}
	if !found {
		t.Errorf("expected dora-traffic-light card; got: %+v", cards)
	}
}

func TestDORATrafficLight_NoSnapshot_NoCard(t *testing.T) {
	store := setupInsightsDB(t)

	cards, err := insights.Compute(store, "")
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}

	for _, c := range cards {
		if c.ID == "dora-traffic-light" {
			t.Errorf("expected no dora-traffic-light card when no snapshot; got: %+v", c)
		}
	}
}

// ---- Empty result ----

func TestCompute_EmptyDB_ReturnsEmptySlice(t *testing.T) {
	store := setupInsightsDB(t)

	cards, err := insights.Compute(store, "")
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if cards == nil {
		t.Error("expected empty slice, got nil")
	}
}
