package cmd

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/phuc-nt/dandori-cli/internal/wrapper"
)

// TestPrintRunSummary_Format verifies the summary line format on a buffer.
func TestPrintRunSummary_Format(t *testing.T) {
	result := &wrapper.Result{
		RunID:    "run-abc123",
		CostUSD:  0.05,
		Duration: 7 * time.Second,
	}

	var buf bytes.Buffer
	printRunSummary(&buf, result)
	out := buf.String()

	if !strings.Contains(out, "✓ Run tracked") {
		t.Errorf("missing checkmark prefix: %q", out)
	}
	if !strings.Contains(out, "id: run-abc123") {
		t.Errorf("missing run_id: %q", out)
	}
	if !strings.Contains(out, "$0.05") {
		t.Errorf("missing cost: %q", out)
	}
	if !strings.Contains(out, "7s") {
		t.Errorf("missing duration: %q", out)
	}
	if !strings.Contains(out, "localhost:8088") {
		t.Errorf("missing dashboard URL: %q", out)
	}
}

// TestPrintRunSummary_QuietSuppresses verifies the Quiet() accessor controls
// whether the summary is printed. The actual suppression happens in runRun's
// if !Quiet() guard — we verify the accessor here to keep test isolation.
func TestPrintRunSummary_QuietSuppresses(t *testing.T) {
	quiet = true
	defer func() { quiet = false }()

	if !Quiet() {
		t.Error("Quiet() should return true when quiet=true")
	}

	// When Quiet() is true, runRun skips both printRunSummary and printFirstRunTip.
	// Verify printRunSummary itself still works (caller is responsible for gate).
	result := &wrapper.Result{RunID: "run-q", CostUSD: 0.01, Duration: 3 * time.Second}
	var buf bytes.Buffer
	printRunSummary(&buf, result)
	if !strings.Contains(buf.String(), "run-q") {
		t.Errorf("printRunSummary wrote nothing, expected run ID in output")
	}
}

// TestFormatRunCost verifies the $X.XX formatting.
func TestFormatRunCost(t *testing.T) {
	tests := []struct {
		usd  float64
		want string
	}{
		{0, "$0.00"},
		{0.05, "$0.05"},
		{1.234, "$1.23"},
		{12.999, "$13.00"},
	}
	for _, tt := range tests {
		got := formatRunCost(tt.usd)
		if got != tt.want {
			t.Errorf("formatRunCost(%v) = %q, want %q", tt.usd, got, tt.want)
		}
	}
}

// TestFormatRunDuration verifies seconds and minutes formatting.
func TestFormatRunDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{5 * time.Second, "5s"},
		{59 * time.Second, "59s"},
		{60 * time.Second, "1m00s"},
		{125 * time.Second, "2m05s"},
		{3600 * time.Second, "60m00s"},
	}
	for _, tt := range tests {
		got := formatRunDuration(tt.d)
		if got != tt.want {
			t.Errorf("formatRunDuration(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

// TestPrintFirstRunTip_FirstRun verifies tip is shown on first run.
func TestPrintFirstRunTip_FirstRun(t *testing.T) {
	localDB := openTestDB(t)

	// Insert one run row so the table exists and has exactly the run we're simulating.
	_, err := localDB.Exec(`
		INSERT INTO runs (id, agent_name, agent_type, user, workstation_id, started_at, status)
		VALUES ('run-first', 'default', 'claude_code', 'testuser', 'ws-test', '2026-01-01T00:00:00Z', 'done')
	`)
	if err != nil {
		t.Fatalf("insert run: %v", err)
	}

	var buf bytes.Buffer
	printFirstRunTip(&buf, localDB, "run-first")
	out := buf.String()

	if !strings.Contains(out, "dandori analytics trend") {
		t.Errorf("expected tip line, got: %q", out)
	}
}

// TestPrintFirstRunTip_SubsequentRun verifies tip is NOT shown after first run.
func TestPrintFirstRunTip_SubsequentRun(t *testing.T) {
	localDB := openTestDB(t)

	// Insert two runs — one prior, one current.
	for _, id := range []string{"run-prior", "run-current"} {
		_, err := localDB.Exec(`
			INSERT INTO runs (id, agent_name, agent_type, user, workstation_id, started_at, status)
			VALUES (?, 'default', 'claude_code', 'testuser', 'ws-test', '2026-01-01T00:00:00Z', 'done')
		`, id)
		if err != nil {
			t.Fatalf("insert run %s: %v", id, err)
		}
	}

	var buf bytes.Buffer
	printFirstRunTip(&buf, localDB, "run-current")
	out := buf.String()

	if strings.Contains(out, "dandori analytics trend") {
		t.Errorf("tip should NOT appear for subsequent runs, got: %q", out)
	}
}
