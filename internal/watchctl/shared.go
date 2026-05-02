// Package watchctl manages the dandori watch daemon lifecycle via OS-specific
// service managers (launchd on macOS, systemd on Linux).
package watchctl

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const cmdTimeout = 10 * time.Second

// runWithTimeoutFn is the function used to execute external commands with a timeout.
// It can be replaced in tests to avoid real subprocess execution.
var runWithTimeoutFn = defaultRunWithTimeout

// runWithTimeout runs the named command via runWithTimeoutFn (injectable for tests).
func runWithTimeout(name string, args ...string) ([]byte, error) {
	return runWithTimeoutFn(name, args...)
}

// defaultRunWithTimeout is the real implementation: runs name with a 10-second context timeout.
func defaultRunWithTimeout(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return out, fmt.Errorf("%s timed out after 10s: %w", name, ctx.Err())
		}
		return out, err
	}
	return out, nil
}

// ErrAlreadyEnabled is returned by Enable when the daemon is already installed and running.
var ErrAlreadyEnabled = errors.New("daemon already enabled")

// stampFilePath returns the path to the watch poll stamp file (~/.dandori/.watch-stamp).
func stampFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".dandori", ".watch-stamp"), nil
}

// WriteStampFile writes the current UTC time in RFC3339 format to the stamp file.
// Best-effort: errors are logged but not returned.
func WriteStampFile() {
	path, err := stampFilePath()
	if err != nil {
		slog.Warn("watchctl: get stamp file path", "error", err)
		return
	}
	if err := os.WriteFile(path, []byte(time.Now().UTC().Format(time.RFC3339)), 0644); err != nil {
		slog.Warn("watchctl: write stamp file", "error", err)
	}
}

// readStampFile reads the stamp file and parses the timestamp.
// Returns zero time if the file is missing or unparseable.
func readStampFile() time.Time {
	path, err := stampFilePath()
	if err != nil {
		return time.Time{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(string(data)))
	if err != nil {
		return time.Time{}
	}
	return t
}
