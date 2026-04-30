package metric

import (
	"encoding/json"
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/phuc-nt/dandori-cli/internal/db"
)

func openMetricAttributionDB(t *testing.T) *db.LocalDB {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return d
}

// seedAttribution inserts one task_attribution row with the fields the
// aggregator actually reads. total_lines_final is derived from
// linesAgent + linesHuman so retention math is internally consistent.
func seedAttribution(t *testing.T, d *db.LocalDB, key string, sessions, linesAgent, linesHuman, iterations int, intRate, costUSD float64, doneAt time.Time) {
	t.Helper()
	_, err := d.Exec(`INSERT INTO task_attribution (
		jira_issue_key, session_count, total_lines_final,
		lines_attributed_agent, lines_attributed_human,
		total_agent_tokens, total_agent_cost_usd,
		total_iterations, total_human_messages,
		total_intervention_count, intervention_rate,
		session_outcomes, git_head_at_jira_done, jira_done_at, computed_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
		key, sessions, linesAgent+linesHuman,
		linesAgent, linesHuman,
		1000, costUSD,
		iterations, 5,
		2, intRate,
		`{"agent_finished":1}`, "deadbeef", doneAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("seed attribution: %v", err)
	}
}

func attributionWindow() MetricWindow {
	end := time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC)
	return MetricWindow{Start: end.AddDate(0, 0, -28), End: end}
}

// TestAggregateAttribution_HappyPath: 3 in-window tasks + 1 outside. Verifies
// totals, autonomy rate (intervention_rate < 0.2), retention p50.
func TestAggregateAttribution_HappyPath(t *testing.T) {
	d := openMetricAttributionDB(t)
	w := attributionWindow()
	mid := w.Start.Add(7 * 24 * time.Hour)
	outside := w.End.Add(2 * 24 * time.Hour)

	// retentions: 80/100=0.8, 50/100=0.5, 100/100=1.0 → sorted: 0.5, 0.8, 1.0
	seedAttribution(t, d, "T-1", 1, 80, 20, 1, 0.10, 0.5, mid)
	seedAttribution(t, d, "T-2", 2, 50, 50, 3, 0.40, 1.0, mid)
	seedAttribution(t, d, "T-3", 1, 100, 0, 1, 0.00, 0.3, mid)
	seedAttribution(t, d, "T-OUT", 1, 1, 0, 1, 0, 0.01, outside)

	res, err := AggregateAttribution(d, w)
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	if res.TasksTotal != 3 {
		t.Errorf("TasksTotal = %d, want 3", res.TasksTotal)
	}
	if res.TasksWithSession != 3 {
		t.Errorf("TasksWithSession = %d, want 3", res.TasksWithSession)
	}
	// 2 of 3 have intervention_rate < 0.2 (T-1 and T-3).
	if math.Abs(res.AgentAutonomyRate-2.0/3.0) > 0.01 {
		t.Errorf("AgentAutonomyRate = %f, want ~0.667", res.AgentAutonomyRate)
	}
	// Median retention of {0.5, 0.8, 1.0} = 0.8.
	if math.Abs(res.RetentionP50-0.8) > 0.01 {
		t.Errorf("RetentionP50 = %f, want 0.8", res.RetentionP50)
	}
	if res.InsufficientData {
		t.Errorf("InsufficientData = true, want false")
	}
}

// TestAggregateAttribution_InsufficientData: empty table → flag set.
func TestAggregateAttribution_InsufficientData(t *testing.T) {
	d := openMetricAttributionDB(t)
	res, err := AggregateAttribution(d, attributionWindow())
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	if !res.InsufficientData {
		t.Errorf("InsufficientData = false, want true")
	}
	if res.TasksTotal != 0 {
		t.Errorf("TasksTotal = %d, want 0", res.TasksTotal)
	}
}

// TestAggregateAttribution_AllZeroSignal_InsufficientData: rows exist but
// every one has zero tracked lines AND zero classified messages (the real
// pre-G7 dogfood pattern). Aggregator must flag insufficient rather than
// emit "0% retention, 0% autonomy" as if those were measurements.
func TestAggregateAttribution_AllZeroSignal_InsufficientData(t *testing.T) {
	d := openMetricAttributionDB(t)
	w := attributionWindow()
	mid := w.Start.Add(7 * 24 * time.Hour)
	// Insert rows with linesAgent=0, linesHuman=0, total_human_messages=0.
	// Direct INSERT to bypass seedAttribution's hardcoded humanMessages=5.
	for _, key := range []string{"Z-1", "Z-2", "Z-3"} {
		if _, err := d.Exec(`INSERT INTO task_attribution (
			jira_issue_key, session_count, total_lines_final,
			lines_attributed_agent, lines_attributed_human,
			total_agent_tokens, total_agent_cost_usd,
			total_iterations, total_human_messages,
			total_intervention_count, intervention_rate,
			session_outcomes, git_head_at_jira_done, jira_done_at, computed_at
		) VALUES (?, 1, 0, 0, 0, 1000, 0.5, 0, 0, 0, 0, '{}', 'deadbeef', ?, datetime('now'))`,
			key, mid.UTC().Format(time.RFC3339)); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	res, err := AggregateAttribution(d, w)
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	if !res.InsufficientData {
		t.Errorf("InsufficientData = false, want true (all-zero-signal rows)")
	}
	if res.AgentAutonomyRate != 0 {
		t.Errorf("AgentAutonomyRate = %f, want 0 (no classification signal)", res.AgentAutonomyRate)
	}
}

// TestFormatFaros_NoFlag_NoAttributionBlock: backward compatibility — without
// the flag, the faros payload must not contain task_attribution.
func TestFormatFaros_NoFlag_NoAttributionBlock(t *testing.T) {
	rep := minimalReport()
	body, err := FormatReport(rep, FormatFaros)
	if err != nil {
		t.Fatalf("format: %v", err)
	}
	if strings.Contains(string(body), "task_attribution") {
		t.Errorf("faros without flag should not contain task_attribution")
	}
}

// TestFormatFaros_WithAttribution: flag on + data → block present with
// expected fields.
func TestFormatFaros_WithAttribution(t *testing.T) {
	rep := minimalReport()
	rep.Config.IncludeAttribution = true
	rep.Attribution = &AttributionResult{
		TasksTotal:        10,
		TasksWithSession:  8,
		AgentAutonomyRate: 0.75,
		RetentionP50:      0.82,
		Window:            rep.Config.Window,
	}
	body, err := FormatReport(rep, FormatFaros)
	if err != nil {
		t.Fatalf("format: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	attr, ok := got["task_attribution"].(map[string]any)
	if !ok {
		t.Fatalf("task_attribution missing or wrong type: %v", got["task_attribution"])
	}
	if v, _ := attr["tasks_with_session"].(float64); v != 8 {
		t.Errorf("tasks_with_session = %v, want 8", attr["tasks_with_session"])
	}
}

// TestFormatFaros_AttributionInsufficient: flag on but data sparse → null
// block and "task_attribution" listed in insufficient_data.
func TestFormatFaros_AttributionInsufficient(t *testing.T) {
	rep := minimalReport()
	rep.Config.IncludeAttribution = true
	rep.Attribution = &AttributionResult{InsufficientData: true, Window: rep.Config.Window}
	body, err := FormatReport(rep, FormatFaros)
	if err != nil {
		t.Fatalf("format: %v", err)
	}
	if !strings.Contains(string(body), `"task_attribution": null`) {
		t.Errorf("expected null task_attribution; got: %s", string(body))
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	dq := got["data_quality"].(map[string]any)
	insuff, _ := dq["insufficient_data"].([]any)
	found := false
	for _, v := range insuff {
		if s, _ := v.(string); s == "task_attribution" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("data_quality.insufficient_data missing 'task_attribution': %v", insuff)
	}
}

// (minimalReport defined in export_test.go is reused here.)
