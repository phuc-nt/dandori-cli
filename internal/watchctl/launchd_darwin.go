//go:build darwin

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
	launchdLabel   = "com.dandori.watch"
	launchdPlistFn = "com.dandori.watch.plist"
)

// plistTemplate is the launchd plist template. {binPath}, {home} are replaced at render time.
const plistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>com.dandori.watch</string>
	<key>ProgramArguments</key>
	<array>
		<string>{binPath}</string>
		<string>watch</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<key>StandardOutPath</key>
	<string>{home}/.dandori/logs/watch.out.log</string>
	<key>StandardErrorPath</key>
	<string>{home}/.dandori/logs/watch.err.log</string>
</dict>
</plist>
`

// Manager implements the watchctl daemon manager for macOS launchd.
type Manager struct{}

// New returns a Manager for the current platform.
func New() *Manager {
	return &Manager{}
}

// Path returns the path to the launchd plist file.
func (m *Manager) Path() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	return filepath.Join(home, "Library", "LaunchAgents", launchdPlistFn), nil
}

// RenderPlist renders the plist content with the given binary path and home directory.
// Exported for testing.
func RenderPlist(binPath, home string) string {
	s := plistTemplate
	s = strings.ReplaceAll(s, "{binPath}", binPath)
	s = strings.ReplaceAll(s, "{home}", home)
	return s
}

// Enable writes the plist file and loads it via launchctl. Idempotent.
func (m *Manager) Enable() error {
	plistPath, err := m.Path()
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

	// Ensure log directory exists.
	logDir := filepath.Join(home, ".dandori", "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}

	// Check for legacy daemon that would duplicate work.
	if err := checkLegacyDaemon(); err != nil {
		return err
	}

	// Check if already enabled.
	if _, err := os.Stat(plistPath); err == nil {
		loaded, _, _ := m.isRunning()
		if loaded {
			return ErrAlreadyEnabled
		}
	}

	// Write plist.
	content := RenderPlist(binPath, home)
	if err := os.MkdirAll(filepath.Dir(plistPath), 0755); err != nil {
		return fmt.Errorf("create LaunchAgents dir: %w", err)
	}
	if err := os.WriteFile(plistPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("write plist: %w", err)
	}

	// Load via launchctl. Try modern bootstrap first, fall back to legacy load.
	if err := launchctlLoad(plistPath); err != nil {
		// Remove plist on failure so state is clean.
		_ = os.Remove(plistPath)
		return fmt.Errorf("launchctl load: %w", err)
	}

	return nil
}

// Disable unloads and removes the plist. Idempotent — silent success if not installed.
func (m *Manager) Disable() error {
	plistPath, err := m.Path()
	if err != nil {
		return err
	}

	// If plist doesn't exist, nothing to do.
	if _, err := os.Stat(plistPath); os.IsNotExist(err) {
		return nil
	}

	// Unload (ignore error — may already be stopped).
	_ = launchctlUnload(plistPath)

	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove plist: %w", err)
	}

	return nil
}

// Status reports whether the daemon is loaded and running, plus when the stamp file was written.
func (m *Manager) Status() (loaded bool, running bool, since time.Time, err error) {
	plistPath, err := m.Path()
	if err != nil {
		return false, false, time.Time{}, err
	}

	if _, statErr := os.Stat(plistPath); os.IsNotExist(statErr) {
		return false, false, time.Time{}, nil
	}
	loaded = true

	running, since, err = m.isRunning()
	return loaded, running, since, err
}

// isRunning checks launchctl list for the dandori label and reads the stamp file.
func (m *Manager) isRunning() (bool, time.Time, error) {
	out, err := runWithTimeout("launchctl", "list", launchdLabel)
	running := err == nil && !strings.Contains(string(out), "Could not find service")

	since := readStampFile()
	return running, since, nil
}

// launchctlLoad attempts to load a plist using launchctl.
// Tries `bootstrap gui/<uid>` first (macOS 10.10+), falls back to `load -w`.
func launchctlLoad(plistPath string) error {
	uid := fmt.Sprintf("%d", os.Getuid())
	out, err := runWithTimeout("launchctl", "bootstrap", "gui/"+uid, plistPath)
	if err == nil {
		return nil
	}
	// Fall back to legacy load.
	out2, err2 := runWithTimeout("launchctl", "load", "-w", plistPath)
	if err2 != nil {
		return fmt.Errorf("bootstrap: %s; load: %s: %w", strings.TrimSpace(string(out)), strings.TrimSpace(string(out2)), err2)
	}
	return nil
}

func launchctlUnload(plistPath string) error {
	uid := fmt.Sprintf("%d", os.Getuid())
	if _, err := runWithTimeout("launchctl", "bootout", "gui/"+uid, plistPath); err == nil {
		return nil
	}
	_, err := runWithTimeout("launchctl", "unload", "-w", plistPath)
	return err
}

const legacyLabel = "com.phuc.dandori-watch"

// checkLegacyDaemon checks whether the legacy v0.8.x daemon label is still registered.
// If found, it returns an error with a warning instructing the user to remove it first.
func checkLegacyDaemon() error {
	out, err := runWithTimeout("launchctl", "list")
	if err != nil {
		// If launchctl list itself fails/times out, surface the error.
		return fmt.Errorf("launchctl list: %w", err)
	}
	if strings.Contains(string(out), legacyLabel) {
		fmt.Fprintf(os.Stderr, "⚠ Found legacy daemon '%s'. Run 'launchctl remove %s' to clean up, then re-run 'dandori watch enable'.\n", legacyLabel, legacyLabel)
		return fmt.Errorf("legacy daemon '%s' still registered; remove it before enabling", legacyLabel)
	}
	return nil
}
