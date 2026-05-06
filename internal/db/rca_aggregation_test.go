package db

import (
	"testing"
	"time"
)

// insertAttribution inserts a task_attribution row with a JSON session_outcomes map.
func insertAttribution(t *testing.T, d *LocalDB, jiraKey string, outcomes map[string]int, doneAt time.Time) {
	t.Helper()
	raw := `{}`
	if len(outcomes) > 0 {
		b, err := jsonMarshal(outcomes)
		if err != nil {
			t.Fatalf("marshal outcomes: %v", err)
		}
		raw = string(b)
	}
	_, err := d.Exec(`
		INSERT INTO task_attribution
			(jira_issue_key, session_count, total_lines_final,
			 lines_attributed_agent, lines_attributed_human,
			 session_outcomes, jira_done_at, computed_at)
		VALUES (?, 1, 0, 0, 0, ?, ?, datetime('now'))
	`, jiraKey, raw, doneAt.UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("insertAttribution %s: %v", jiraKey, err)
	}
}

// jsonMarshal is a tiny helper so the test file doesn't need to import encoding/json directly.
func jsonMarshal(m map[string]int) ([]byte, error) {
	// Build manually to avoid circular import concerns (already imported via db package).
	import_json_via_reflection := func() ([]byte, error) {
		s := "{"
		first := true
		for k, v := range m {
			if !first {
				s += ","
			}
			s += `"` + k + `":` + itoa(v)
			first = false
		}
		s += "}"
		return []byte(s), nil
	}
	return import_json_via_reflection()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	buf := make([]byte, 20)
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

func TestGetRcaBreakdown_EmptyDB(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	rows, err := d.GetRcaBreakdown(time.Now().AddDate(0, 0, -28))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("expected empty slice on empty DB, got %d rows", len(rows))
	}
}

func TestGetRcaBreakdown_SingleCause(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	now := time.Now().UTC()
	// One attribution with 3 lint_fail and 1 test_fail inside current window.
	insertAttribution(t, d, "FEAT-1", map[string]int{
		"lint_fail": 3,
		"test_fail": 1,
	}, now.AddDate(0, 0, -5))

	rows, err := d.GetRcaBreakdown(now.AddDate(0, 0, -28))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	idx := rcaIndex(rows)

	if r, ok := idx["lint_fail"]; !ok {
		t.Error("missing lint_fail row")
	} else {
		if r.Count != 3 {
			t.Errorf("lint_fail count = %d, want 3", r.Count)
		}
		if r.Pct <= 0 {
			t.Errorf("lint_fail pct = %.1f, want > 0", r.Pct)
		}
	}

	if r, ok := idx["test_fail"]; !ok {
		t.Error("missing test_fail row")
	} else if r.Count != 1 {
		t.Errorf("test_fail count = %d, want 1", r.Count)
	}
}

func TestGetRcaBreakdown_WoWDelta(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	now := time.Now().UTC()
	since := now.AddDate(0, 0, -28)

	// Prior window (28-56 days ago): 4 lint_fail total → 100%.
	insertAttribution(t, d, "OLD-1", map[string]int{"lint_fail": 4}, now.AddDate(0, 0, -40))

	// Current window (last 28 days): 2 lint_fail + 2 test_fail → 50% each.
	insertAttribution(t, d, "NEW-1", map[string]int{"lint_fail": 2, "test_fail": 2}, now.AddDate(0, 0, -5))

	rows, err := d.GetRcaBreakdown(since)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	idx := rcaIndex(rows)

	// lint_fail: current = 50%, prior = 100% → delta = -50pp
	if r, ok := idx["lint_fail"]; !ok {
		t.Error("missing lint_fail")
	} else {
		if r.Pct != 50.0 {
			t.Errorf("lint_fail pct = %.1f, want 50", r.Pct)
		}
		if r.WoWDelta >= 0 {
			t.Errorf("lint_fail WoWDelta = %.1f, want < 0 (was 100%% before)", r.WoWDelta)
		}
	}
}

func TestGetRcaBreakdown_WoWDeltaZeroOnFirstWindow(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	now := time.Now().UTC()
	// Only current window data, no prior.
	insertAttribution(t, d, "NEW-1", map[string]int{"lint_fail": 2}, now.AddDate(0, 0, -5))

	rows, err := d.GetRcaBreakdown(now.AddDate(0, 0, -28))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	idx := rcaIndex(rows)
	if r, ok := idx["lint_fail"]; ok {
		// Prior window empty → WoWDelta should be 0 (not Inf/NaN).
		if r.WoWDelta != 0 {
			t.Errorf("WoWDelta with no prior data = %.2f, want 0", r.WoWDelta)
		}
	}
}

func TestGetRcaBreakdown_TopAgentAttribution(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	now := time.Now().UTC()
	// Run by "alpha" on FEAT-1.
	_, err := d.Exec(`
		INSERT INTO runs (id, agent_name, jira_issue_key, exit_code, user, workstation_id, started_at, status)
		VALUES ('r1', 'alpha', 'FEAT-1', 1, 'tester', 'ws1', ?, 'failed')
	`, now.AddDate(0, 0, -5).Format(time.RFC3339))
	if err != nil {
		t.Fatalf("insert run: %v", err)
	}

	insertAttribution(t, d, "FEAT-1", map[string]int{"test_fail": 3}, now.AddDate(0, 0, -3))

	rows, err := d.GetRcaBreakdown(now.AddDate(0, 0, -28))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	idx := rcaIndex(rows)
	if r, ok := idx["test_fail"]; !ok {
		t.Error("missing test_fail")
	} else if r.TopAgent != "alpha" {
		t.Errorf("TopAgent = %q, want alpha", r.TopAgent)
	}
}

func TestGetRcaBreakdown_UnknownJsonKeyBucketsToOther(t *testing.T) {
	d := newEmptyLocalDB(t)
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	now := time.Now().UTC()
	// Raw JSON with an unknown key — should fold into "other".
	_, err := d.Exec(`
		INSERT INTO task_attribution
			(jira_issue_key, session_count, total_lines_final,
			 lines_attributed_agent, lines_attributed_human,
			 session_outcomes, jira_done_at, computed_at)
		VALUES ('X-1', 1, 0, 0, 0, '{"unknown_reason_xyz":5}', ?, datetime('now'))
	`, now.AddDate(0, 0, -2).UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	rows, err := d.GetRcaBreakdown(now.AddDate(0, 0, -28))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	idx := rcaIndex(rows)
	if r, ok := idx["other"]; !ok {
		t.Error("expected 'other' bucket for unknown JSON key")
	} else if r.Count != 5 {
		t.Errorf("other count = %d, want 5", r.Count)
	}
}

// rcaIndex converts []RcaRow to map[cause]RcaRow for test lookups.
func rcaIndex(rows []RcaRow) map[string]RcaRow {
	m := make(map[string]RcaRow, len(rows))
	for _, r := range rows {
		m[r.Cause] = r
	}
	return m
}
