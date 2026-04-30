package quality

import (
	"context"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

// Config holds quality collection settings
type Config struct {
	Enabled     bool   `yaml:"enabled"`
	LintCommand string `yaml:"lint_command"`
	TestCommand string `yaml:"test_command"`
	Timeout     string `yaml:"timeout"`
}

// DefaultConfig returns default quality config.
// Enabled defaults to false to avoid spawning `go test` from every `dandori run`,
// which leaks ~30 go-build* dirs in $TMPDIR per run when the test suite times out
// (SIGKILL prevents go's cleanup). Opt in via `dandori init` or config.
func DefaultConfig() Config {
	return Config{
		Enabled:     false,
		LintCommand: "golangci-lint run --json --out-format json 2>/dev/null || true",
		TestCommand: "go test -json -count=1 ./... 2>&1 || true",
		Timeout:     "30s",
	}
}

// Collector captures lint/test snapshots
type Collector struct {
	cfg     Config
	timeout time.Duration
}

// NewCollector creates a new quality collector
func NewCollector(cfg Config) *Collector {
	timeout, err := time.ParseDuration(cfg.Timeout)
	if err != nil {
		timeout = 30 * time.Second
	}

	return &Collector{
		cfg:     cfg,
		timeout: timeout,
	}
}

// Snapshot captures current lint/test state
func (c *Collector) Snapshot(cwd string) *Snapshot {
	snap := &Snapshot{
		CapturedAt: time.Now(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	// Capture lint state
	if c.cfg.LintCommand != "" {
		lintErrors, lintWarnings, err := c.runLint(ctx, cwd)
		if err != nil {
			slog.Debug("lint capture failed", "error", err)
			snap.Error = "lint: " + err.Error()
		} else {
			snap.LintErrors = lintErrors
			snap.LintWarnings = lintWarnings
		}
	}

	// Capture test state
	if c.cfg.TestCommand != "" {
		total, passed, failed, skipped, err := c.runTests(ctx, cwd)
		if err != nil {
			slog.Debug("test capture failed", "error", err)
			if snap.Error != "" {
				snap.Error += "; "
			}
			snap.Error += "test: " + err.Error()
		} else {
			snap.TestsTotal = total
			snap.TestsPassed = passed
			snap.TestsFailed = failed
			snap.TestsSkipped = skipped
		}
	}

	return snap
}

// runLint executes lint command and parses output
func (c *Collector) runLint(ctx context.Context, cwd string) (int, int, error) {
	cmd := spawnCollectorCmd(ctx, c.cfg.LintCommand, c.timeout)
	cmd.Dir = cwd

	output, err := cmd.Output()
	if err != nil {
		// golangci-lint exits non-zero when issues found, that's OK
		if exitErr, ok := err.(*exec.ExitError); ok {
			// Use stderr if stdout is empty
			if len(output) == 0 && len(exitErr.Stderr) > 0 {
				output = exitErr.Stderr
			}
		}
	}

	return ParseGolangciLint(output)
}

// runTests executes test command and parses output
func (c *Collector) runTests(ctx context.Context, cwd string) (int, int, int, int, error) {
	cmd := spawnCollectorCmd(ctx, c.cfg.TestCommand, c.timeout)
	cmd.Dir = cwd
	// Prevent recursive quality collection: when dandori's own test suite runs
	// under a quality snapshot, TestCollector_Snapshot_RealProject must not
	// re-invoke a full snapshot (which would spawn another "go test ./..." and
	// create an unbounded recursive process tree). The test skips itself when
	// DANDORI_QUALITY_RUNNING is set.
	cmd.Env = append(cmd.Environ(), "DANDORI_QUALITY_RUNNING=1")

	output, err := cmd.Output()
	if err != nil {
		// go test exits non-zero when tests fail, that's OK
		if exitErr, ok := err.(*exec.ExitError); ok {
			if len(output) == 0 && len(exitErr.Stderr) > 0 {
				output = exitErr.Stderr
			}
		}
	}

	// Try JSON parsing first
	total, passed, failed, skipped, err := ParseGoTestJSON(output)
	if err != nil || total == 0 {
		// Fallback to summary parsing
		total, passed, failed, skipped = ParseTestSummary(string(output))
	}

	return total, passed, failed, skipped, nil
}

// SnapshotLintOnly captures only lint state (faster)
func (c *Collector) SnapshotLintOnly(cwd string) *Snapshot {
	snap := &Snapshot{
		CapturedAt: time.Now(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	if c.cfg.LintCommand != "" {
		lintErrors, lintWarnings, err := c.runLint(ctx, cwd)
		if err != nil {
			snap.Error = "lint: " + err.Error()
		} else {
			snap.LintErrors = lintErrors
			snap.LintWarnings = lintWarnings
		}
	}

	return snap
}

// FormatDelta returns human-readable delta string
func FormatDelta(before, after int, lowerIsBetter bool) string {
	delta := after - before
	if delta == 0 {
		return "→ (no change)"
	}

	sign := "+"
	improved := delta < 0
	if !lowerIsBetter {
		improved = delta > 0
	}

	if delta < 0 {
		sign = ""
	}

	status := ""
	if improved {
		status = " ✓"
	} else {
		status = " ✗"
	}

	return strings.TrimSpace(sign + string(rune('0'+abs(delta))) + status)
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
