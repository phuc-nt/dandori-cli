//go:build darwin

package watchctl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestCheckLegacyDaemon_NoLegacy verifies that checkLegacyDaemon returns nil when
// launchctl list output does not contain the legacy label.
func TestCheckLegacyDaemon_NoLegacy(t *testing.T) {
	// Override runWithTimeout via the package-level variable for this test.
	orig := runWithTimeoutFn
	t.Cleanup(func() { runWithTimeoutFn = orig })

	runWithTimeoutFn = func(name string, args ...string) ([]byte, error) {
		// Simulate launchctl list output without the legacy label.
		return []byte("com.apple.something\ncom.dandori.watch\n"), nil
	}

	if err := checkLegacyDaemon(); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

// TestCheckLegacyDaemon_LegacyPresent verifies that checkLegacyDaemon returns an error
// and writes a warning when the legacy label is found in launchctl list output.
func TestCheckLegacyDaemon_LegacyPresent(t *testing.T) {
	orig := runWithTimeoutFn
	t.Cleanup(func() { runWithTimeoutFn = orig })

	runWithTimeoutFn = func(name string, args ...string) ([]byte, error) {
		// Simulate launchctl list output containing the legacy label.
		return []byte("com.phuc.dandori-watch\ncom.dandori.watch\n"), nil
	}

	// Redirect stderr to capture the warning.
	origStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	t.Cleanup(func() {
		os.Stderr = origStderr
	})

	err := checkLegacyDaemon()
	_ = w.Close()
	var buf [512]byte
	n, _ := r.Read(buf[:])
	stderrOut := string(buf[:n])

	if err == nil {
		t.Fatal("expected error when legacy label present, got nil")
	}
	if !strings.Contains(err.Error(), legacyLabel) {
		t.Errorf("error should mention legacy label, got: %v", err)
	}
	if !strings.Contains(stderrOut, legacyLabel) {
		t.Errorf("stderr warning should mention legacy label, got: %q", stderrOut)
	}
	if !strings.Contains(stderrOut, "launchctl remove") {
		t.Errorf("stderr warning should mention launchctl remove, got: %q", stderrOut)
	}
}

func TestRenderPlist_ContainsFields(t *testing.T) {
	binPath := "/usr/local/bin/dandori"
	home := "/Users/testuser"

	plist := RenderPlist(binPath, home)

	checks := []struct {
		name string
		want string
	}{
		{"label", "com.dandori.watch"},
		{"binPath", binPath},
		{"watch arg", "<string>watch</string>"},
		{"RunAtLoad", "<key>RunAtLoad</key>"},
		{"KeepAlive", "<key>KeepAlive</key>"},
		{"stdout log", home + "/.dandori/logs/watch.out.log"},
		{"stderr log", home + "/.dandori/logs/watch.err.log"},
	}

	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			if !strings.Contains(plist, c.want) {
				t.Errorf("plist missing %q\ngot:\n%s", c.want, plist)
			}
		})
	}
}

func TestRenderPlist_NoBracketPlaceholders(t *testing.T) {
	plist := RenderPlist("/bin/dandori", "/home/user")
	if strings.Contains(plist, "{") || strings.Contains(plist, "}") {
		t.Errorf("plist still has unreplaced placeholders:\n%s", plist)
	}
}

func TestManagerPath_ContainsLabel(t *testing.T) {
	m := New()
	path, err := m.Path()
	if err != nil {
		t.Fatalf("Path() error: %v", err)
	}
	if !strings.Contains(path, "com.dandori.watch.plist") {
		t.Errorf("expected plist filename in path, got %q", path)
	}
	if !strings.Contains(path, "LaunchAgents") {
		t.Errorf("expected LaunchAgents in path, got %q", path)
	}
}

func TestReadStampFile_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	// Override home for stamp file by writing directly to the expected location.
	home, _ := os.UserHomeDir()
	stampDir := filepath.Join(home, ".dandori")
	if err := os.MkdirAll(stampDir, 0755); err != nil {
		t.Skip("cannot create stamp dir:", err)
	}

	stampPath := filepath.Join(stampDir, ".watch-stamp")
	now := time.Now().UTC().Truncate(time.Second)
	_ = dir // suppress unused

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
	// Ensure stamp file does not exist.
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
	plistPath, err := m.Path()
	if err != nil {
		t.Fatalf("Path() error: %v", err)
	}

	// Make sure plist doesn't exist for this test.
	if _, err := os.Stat(plistPath); err == nil {
		t.Skip("plist already exists on this machine — skipping not-installed test")
	}

	loaded, running, since, err := m.Status()
	if err != nil {
		t.Fatalf("Status() error: %v", err)
	}
	if loaded {
		t.Error("expected loaded=false when plist absent")
	}
	if running {
		t.Error("expected running=false when plist absent")
	}
	if !since.IsZero() {
		t.Errorf("expected zero since when no stamp, got %v", since)
	}
}
