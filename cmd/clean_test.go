package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// runCleanIn redirects $TMPDIR to tmp for the duration of the call so the
// command sees only the fixtures we plant. Returns the captured stdout.
func runCleanIn(t *testing.T, tmp string, force bool) string {
	t.Helper()
	t.Setenv("TMPDIR", tmp)

	prevForce := cleanForce
	cleanForce = force
	t.Cleanup(func() { cleanForce = prevForce })

	var buf bytes.Buffer
	cleanCmd.SetOut(&buf)
	cleanCmd.SetErr(&buf)
	t.Cleanup(func() {
		cleanCmd.SetOut(nil)
		cleanCmd.SetErr(nil)
	})

	if err := runClean(cleanCmd, nil); err != nil {
		t.Fatalf("runClean: %v", err)
	}
	return buf.String()
}

func mkGoBuildDir(t *testing.T, parent, name string, age time.Duration, payload int) string {
	t.Helper()
	full := filepath.Join(parent, name)
	if err := os.MkdirAll(full, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", full, err)
	}
	if payload > 0 {
		if err := os.WriteFile(filepath.Join(full, "obj"), bytes.Repeat([]byte("x"), payload), 0o644); err != nil {
			t.Fatalf("write payload: %v", err)
		}
	}
	mtime := time.Now().Add(-age)
	if err := os.Chtimes(full, mtime, mtime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	return full
}

// TestClean_DryRun_ReportsOnlyEligible plants a mix of stale, fresh, and
// unrelated dirs. Dry run must report eligible count + size, must NOT delete.
func TestClean_DryRun_ReportsOnlyEligible(t *testing.T) {
	tmp := t.TempDir()

	stale1 := mkGoBuildDir(t, tmp, "go-build123", 2*time.Hour, 1024)
	stale2 := mkGoBuildDir(t, tmp, "go-build456", 90*time.Minute, 2048)
	young := mkGoBuildDir(t, tmp, "go-build789", 5*time.Minute, 512)
	unrelated := mkGoBuildDir(t, tmp, "fastembed_cache", 2*time.Hour, 4096) // wrong prefix

	out := runCleanIn(t, tmp, false)

	if !strings.Contains(out, "go-build* dirs found:        3") {
		t.Errorf("expected matched=3 (only go-build prefix), got:\n%s", out)
	}
	if !strings.Contains(out, "eligible (older than 60m):   2") {
		t.Errorf("expected eligible=2, got:\n%s", out)
	}
	if !strings.Contains(out, "skipped (in-flight, <60m):   1") {
		t.Errorf("expected skipped=1, got:\n%s", out)
	}
	if !strings.Contains(out, "dry run") {
		t.Errorf("expected dry-run banner, got:\n%s", out)
	}

	for _, p := range []string{stale1, stale2, young, unrelated} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("dry run must not delete %s: %v", p, err)
		}
	}
}

// TestClean_Force_DeletesEligibleOnly verifies --force removes stale dirs but
// leaves young dirs (in-flight protection) and unrelated dirs alone.
func TestClean_Force_DeletesEligibleOnly(t *testing.T) {
	tmp := t.TempDir()

	stale := mkGoBuildDir(t, tmp, "go-build-old", 2*time.Hour, 256)
	young := mkGoBuildDir(t, tmp, "go-build-new", 1*time.Minute, 256)
	unrelated := mkGoBuildDir(t, tmp, "puppeteer_dev_chrome_profile-x", 5*time.Hour, 256)

	out := runCleanIn(t, tmp, true)

	if !strings.Contains(out, "Deleted 1 / 1 eligible dirs.") {
		t.Errorf("expected delete summary, got:\n%s", out)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale dir should be gone, stat err=%v", err)
	}
	if _, err := os.Stat(young); err != nil {
		t.Errorf("young dir must survive: %v", err)
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Errorf("unrelated dir must survive: %v", err)
	}
}

// TestClean_Empty_NoMatches makes sure an empty $TMPDIR doesn't error and
// still prints the summary lines (so users running for the first time see
// confirmation that the scan ran).
func TestClean_Empty_NoMatches(t *testing.T) {
	tmp := t.TempDir()
	out := runCleanIn(t, tmp, false)

	if !strings.Contains(out, "go-build* dirs found:        0") {
		t.Errorf("expected matched=0, got:\n%s", out)
	}
	if !strings.Contains(out, "total reclaimable size:") {
		t.Errorf("expected size line even when zero, got:\n%s", out)
	}
}

func TestHumanBytes(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{2048, "2.0 KiB"},
		{5 * 1024 * 1024, "5.0 MiB"},
		{3 * 1024 * 1024 * 1024, "3.0 GiB"},
	}
	for _, c := range cases {
		if got := humanBytes(c.in); got != c.want {
			t.Errorf("humanBytes(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}
