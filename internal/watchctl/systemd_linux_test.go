//go:build linux

package watchctl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRenderUnit_ContainsFields(t *testing.T) {
	binPath := "/usr/local/bin/dandori"
	home := "/home/testuser"

	unit := RenderUnit(binPath, home)

	checks := []struct {
		name string
		want string
	}{
		{"description", "dandori watch daemon"},
		{"ExecStart", "ExecStart=" + binPath + " watch"},
		{"Restart", "Restart=always"},
		{"stdout log", "append:" + home + "/.dandori/logs/watch.out.log"},
		{"stderr log", "append:" + home + "/.dandori/logs/watch.err.log"},
		{"WantedBy", "WantedBy=default.target"},
	}

	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			if !strings.Contains(unit, c.want) {
				t.Errorf("unit file missing %q\ngot:\n%s", c.want, unit)
			}
		})
	}
}

func TestRenderUnit_NoBracketPlaceholders(t *testing.T) {
	unit := RenderUnit("/bin/dandori", "/home/user")
	if strings.Contains(unit, "{") || strings.Contains(unit, "}") {
		t.Errorf("unit file still has unreplaced placeholders:\n%s", unit)
	}
}

func TestManagerPath_ContainsUnitName(t *testing.T) {
	m := New()
	path, err := m.Path()
	if err != nil {
		t.Fatalf("Path() error: %v", err)
	}
	if !strings.Contains(path, "dandori-watch.service") {
		t.Errorf("expected unit filename in path, got %q", path)
	}
	if !strings.Contains(path, ".config/systemd/user") {
		t.Errorf("expected systemd user dir in path, got %q", path)
	}
}

func TestReadStampFile_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	home, _ := os.UserHomeDir()
	stampDir := filepath.Join(home, ".dandori")
	if err := os.MkdirAll(stampDir, 0755); err != nil {
		t.Skip("cannot create stamp dir:", err)
	}

	stampPath := filepath.Join(stampDir, ".watch-stamp")
	now := time.Now().UTC().Truncate(time.Second)
	_ = dir

	if err := os.WriteFile(stampPath, []byte(now.Format(time.RFC3339)), 0644); err != nil {
		t.Fatalf("write stamp: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(stampPath) })

	got := readStampFile()
	if got.IsZero() {
		t.Fatal("readStampFile returned zero time")
	}
	if !got.Equal(now) {
		t.Errorf("want %v got %v", now, got)
	}
}

func TestReadStampFile_MissingFile(t *testing.T) {
	home, _ := os.UserHomeDir()
	stampPath := filepath.Join(home, ".dandori", ".watch-stamp")
	_ = os.Remove(stampPath)

	got := readStampFile()
	if !got.IsZero() {
		t.Errorf("expected zero time for missing stamp, got %v", got)
	}
}

func TestStatus_NotInstalled(t *testing.T) {
	m := New()
	unitPath, err := m.Path()
	if err != nil {
		t.Fatalf("Path() error: %v", err)
	}

	if _, err := os.Stat(unitPath); err == nil {
		t.Skip("unit file already exists on this machine — skipping not-installed test")
	}

	loaded, running, since, err := m.Status()
	if err != nil {
		t.Fatalf("Status() error: %v", err)
	}
	if loaded {
		t.Error("expected loaded=false when unit absent")
	}
	if running {
		t.Error("expected running=false when unit absent")
	}
	if !since.IsZero() {
		t.Errorf("expected zero since when no stamp, got %v", since)
	}
}
