//go:build linux

// Package watchctl manages the dandori watch daemon lifecycle via OS-specific
// service managers (launchd on macOS, systemd on Linux).
package watchctl

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	systemdUnitName = "dandori-watch.service"
)

// unitTemplate is the systemd user unit template. {binPath}, {home} are replaced at render time.
const unitTemplate = `[Unit]
Description=dandori watch daemon

[Service]
ExecStart={binPath} watch
Restart=always
StandardOutput=append:{home}/.dandori/logs/watch.out.log
StandardError=append:{home}/.dandori/logs/watch.err.log

[Install]
WantedBy=default.target
`

// Manager implements the watchctl daemon manager for Linux systemd.
type Manager struct{}

// New returns a Manager for the current platform.
func New() *Manager {
	return &Manager{}
}

// Path returns the path to the systemd user unit file.
func (m *Manager) Path() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	return filepath.Join(home, ".config", "systemd", "user", systemdUnitName), nil
}

// RenderUnit renders the systemd unit content with the given binary path and home directory.
// Exported for testing.
func RenderUnit(binPath, home string) string {
	s := unitTemplate
	s = strings.ReplaceAll(s, "{binPath}", binPath)
	s = strings.ReplaceAll(s, "{home}", home)
	return s
}

// Enable writes the unit file and enables + starts the service. Idempotent.
func (m *Manager) Enable() error {
	unitPath, err := m.Path()
	if err != nil {
		return err
	}

	binPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home dir: %w", err)
	}

	// Check if XDG_RUNTIME_DIR is available (user systemd session running).
	if os.Getenv("XDG_RUNTIME_DIR") == "" {
		return fmt.Errorf("user systemd session unavailable (XDG_RUNTIME_DIR not set); run `dandori watch` manually")
	}

	// Check if already enabled.
	if _, err := os.Stat(unitPath); err == nil {
		if active, _ := isActive(); active {
			return ErrAlreadyEnabled
		}
	}

	// Ensure log directory exists.
	logDir := filepath.Join(home, ".dandori", "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}

	// Write unit file.
	content := RenderUnit(binPath, home)
	if err := os.MkdirAll(filepath.Dir(unitPath), 0755); err != nil {
		return fmt.Errorf("create systemd user dir: %w", err)
	}
	if err := os.WriteFile(unitPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("write unit file: %w", err)
	}

	// Reload daemon, then enable and start.
	if out, err := runWithTimeout("systemctl", "--user", "daemon-reload"); err != nil {
		return fmt.Errorf("daemon-reload: %s: %w", strings.TrimSpace(string(out)), err)
	}
	if out, err := runWithTimeout("systemctl", "--user", "enable", "--now", systemdUnitName); err != nil {
		// Clean up unit file on failure.
		_ = os.Remove(unitPath)
		return fmt.Errorf("enable --now: %s: %w", strings.TrimSpace(string(out)), err)
	}

	return nil
}

// Disable stops and disables the service, then removes the unit file. Idempotent.
func (m *Manager) Disable() error {
	unitPath, err := m.Path()
	if err != nil {
		return err
	}

	// If unit file doesn't exist, nothing to do.
	if _, err := os.Stat(unitPath); os.IsNotExist(err) {
		return nil
	}

	// Stop and disable (ignore errors — may already be inactive).
	_, _ = runWithTimeout("systemctl", "--user", "disable", "--now", systemdUnitName)
	_, _ = runWithTimeout("systemctl", "--user", "daemon-reload")

	if err := os.Remove(unitPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove unit file: %w", err)
	}

	return nil
}

// Status reports whether the daemon is installed and active, plus last poll time.
func (m *Manager) Status() (loaded bool, running bool, since time.Time, err error) {
	unitPath, err := m.Path()
	if err != nil {
		return false, false, time.Time{}, err
	}

	if _, statErr := os.Stat(unitPath); os.IsNotExist(statErr) {
		return false, false, time.Time{}, nil
	}
	loaded = true

	running, err = isActive()
	since = readStampFile()
	return loaded, running, since, err
}

// isActive calls systemctl --user is-active and returns true when output is "active".
func isActive() (bool, error) {
	out, err := runWithTimeout("systemctl", "--user", "is-active", systemdUnitName)
	return strings.TrimSpace(string(out)) == "active", err
}
