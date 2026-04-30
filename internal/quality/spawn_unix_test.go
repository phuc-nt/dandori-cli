//go:build !windows

package quality

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestSpawnCollectorCmd_SIGTERM_AllowsCleanup verifies the cancellation path
// sends SIGTERM (not SIGKILL) so child processes can run their own cleanup.
//
// The shell script writes "started", traps TERM to write "cleaned" + exit, then
// sleeps. We cancel the context — if the trap fires, the marker file gets the
// "cleaned" line. With the old SIGKILL behaviour, the trap would never run.
func TestSpawnCollectorCmd_SIGTERM_AllowsCleanup(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "marker")

	script := `
		trap 'echo cleaned >> "` + marker + `"; exit 0' TERM
		echo started >> "` + marker + `"
		sleep 30 &
		wait $!
	`

	ctx, cancel := context.WithCancel(context.Background())
	cmd := spawnCollectorCmd(ctx, script, 5*time.Second)

	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}

	// Wait for the trap to be installed (script wrote "started").
	deadline := time.Now().Add(2 * time.Second)
	for {
		if data, _ := os.ReadFile(marker); len(data) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("script never wrote 'started' — trap may not be installed yet")
		}
		time.Sleep(20 * time.Millisecond)
	}

	cancel()
	_ = cmd.Wait() // exit status is irrelevant; we care about side effects

	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	if !contains(string(data), "cleaned") {
		t.Errorf("SIGTERM trap did not run; marker=%q\nThis means we regressed to SIGKILL.", string(data))
	}
}

// TestSpawnCollectorCmd_WaitDelay_IncludesGrace ensures callers don't have to
// pad their timeout by gracePeriod — the spawn helper does it for them.
func TestSpawnCollectorCmd_WaitDelay_IncludesGrace(t *testing.T) {
	cmd := spawnCollectorCmd(context.Background(), "true", 30*time.Second)
	want := 30*time.Second + gracePeriod
	if cmd.WaitDelay != want {
		t.Errorf("WaitDelay = %v, want %v (caller timeout + gracePeriod)", cmd.WaitDelay, want)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
