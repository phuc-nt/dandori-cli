//go:build windows

package quality

import (
	"context"
	"os/exec"
	"time"
)

const gracePeriod = 2 * time.Second

// spawnCollectorCmd creates an exec.Cmd for a shell command. On Windows we
// rely on context cancellation + WaitDelay; process-group semantics differ
// from Unix and we don't currently target Windows for the verify-gate flow.
func spawnCollectorCmd(ctx context.Context, shellCmd string, waitDelay time.Duration) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "sh", "-c", shellCmd)
	cmd.WaitDelay = waitDelay + gracePeriod
	return cmd
}
