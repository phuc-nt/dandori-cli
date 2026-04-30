//go:build !windows

package quality

import (
	"context"
	"os/exec"
	"syscall"
	"time"
)

// gracePeriod is how long we let the child tree drain after SIGTERM before
// Go's WaitDelay machinery escalates to SIGKILL. 2s is enough for `go test`
// to reach its own deferred cleanup (which removes the go-build* scratch dirs)
// without making timeout-bound callers wait noticeably longer.
const gracePeriod = 2 * time.Second

// spawnCollectorCmd creates an exec.Cmd for a shell command with process-group
// isolation so that context cancellation kills the entire child tree, not just
// the "sh" parent. Without Setpgid the grandchild (e.g. "go test") survives sh's
// death, keeps the stdout pipe write-end open, and cmd.Output() blocks forever.
//
// On cancel/timeout we send SIGTERM to the whole process group first so `go test`
// can run its own cleanup (otherwise it leaks ~30 go-build* dirs in $TMPDIR per
// run). Go's exec package automatically escalates to SIGKILL after WaitDelay if
// the process is still alive — so waitDelay must be long enough to cover both
// the user-supplied timeout buffer AND the SIGTERM grace period.
func spawnCollectorCmd(ctx context.Context, shellCmd string, waitDelay time.Duration) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "sh", "-c", shellCmd)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
	cmd.WaitDelay = waitDelay + gracePeriod
	return cmd
}
