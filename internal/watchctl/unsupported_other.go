//go:build !darwin && !linux

// Package watchctl manages the dandori watch daemon lifecycle via OS-specific
// service managers (launchd on macOS, systemd on Linux).
package watchctl

import (
	"errors"
	"time"
)

var errUnsupported = errors.New(
	"auto-daemon not supported on this OS\n" +
		"Run `dandori watch` manually in a separate terminal,\n" +
		"or use Task Scheduler / your OS service manager to schedule it.",
)

// Manager is a no-op stub for unsupported platforms.
type Manager struct{}

// New returns a Manager for the current platform.
func New() *Manager {
	return &Manager{}
}

// Path always returns an error on unsupported platforms.
func (m *Manager) Path() (string, error) {
	return "", errUnsupported
}

// Enable always returns an error on unsupported platforms.
func (m *Manager) Enable() error {
	return errUnsupported
}

// Disable always returns an error on unsupported platforms.
func (m *Manager) Disable() error {
	return errUnsupported
}

// Status always returns an error on unsupported platforms.
func (m *Manager) Status() (loaded bool, running bool, since time.Time, err error) {
	return false, false, time.Time{}, errUnsupported
}
