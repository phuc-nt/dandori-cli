package shellrc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectShell(t *testing.T) {
	tests := []struct {
		shellEnv string
		want     string
	}{
		{"/bin/zsh", "zsh"},
		{"/usr/bin/bash", "bash"},
		{"/opt/homebrew/bin/zsh", "zsh"},
		{"/bin/fish", ""},
		{"", ""},
	}

	for _, tt := range tests {
		got := DetectShell(tt.shellEnv)
		if got != tt.want {
			t.Errorf("DetectShell(%q) = %q, want %q", tt.shellEnv, got, tt.want)
		}
	}
}

func TestRCFileName(t *testing.T) {
	tests := []struct {
		shell    string
		wantFile string
	}{
		{"zsh", ".zshrc"},
		{"bash", ".bashrc"},
		{"fish", ""},
	}

	for _, tt := range tests {
		got := RCFileName(tt.shell)
		if got != tt.wantFile {
			t.Errorf("RCFileName(%q) = %q, want %q", tt.shell, got, tt.wantFile)
		}
	}
}

func TestHasAliasBlock(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("no block", func(t *testing.T) {
		rcFile := filepath.Join(tmpDir, "no_block.zshrc")
		_ = os.WriteFile(rcFile, []byte("# plain rc\nexport PATH=/bin\n"), 0644)
		if HasAliasBlock(rcFile) {
			t.Error("expected false for file without alias block")
		}
	})

	t.Run("has block", func(t *testing.T) {
		rcFile := filepath.Join(tmpDir, "with_block.zshrc")
		content := "# original\n" + StartMarker + "\nalias claude='dandori run -- claude'\n" + EndMarker + "\n"
		_ = os.WriteFile(rcFile, []byte(content), 0644)
		if !HasAliasBlock(rcFile) {
			t.Error("expected true for file with alias block")
		}
	})

	t.Run("missing file", func(t *testing.T) {
		if HasAliasBlock(filepath.Join(tmpDir, "nonexistent.zshrc")) {
			t.Error("expected false for missing file")
		}
	})
}

// writeBlockFixture writes a rc file that contains the managed alias block
// (as left by v0.8 InstallAliases), plus surrounding user content.
func writeBlockFixture(t *testing.T, rcFile string) {
	t.Helper()
	content := "# original\nexport PATH=/bin\n\n" +
		StartMarker + "\nalias claude='dandori run -- claude'\nalias codex='dandori run -- codex'\n" + EndMarker + "\n"
	if err := os.WriteFile(rcFile, []byte(content), 0644); err != nil {
		t.Fatalf("writeBlockFixture: %v", err)
	}
}

func TestUninstallAliases_RemovesBlock(t *testing.T) {
	tmpDir := t.TempDir()
	rcFile := filepath.Join(tmpDir, ".zshrc")
	writeBlockFixture(t, rcFile)

	if err := UninstallAliases(rcFile); err != nil {
		t.Fatalf("UninstallAliases: %v", err)
	}

	content, _ := os.ReadFile(rcFile)
	s := string(content)
	if strings.Contains(s, StartMarker) {
		t.Error("start marker not removed")
	}
	if strings.Contains(s, EndMarker) {
		t.Error("end marker not removed")
	}
	if !strings.Contains(s, "# original") {
		t.Error("original content was destroyed")
	}
}

func TestUninstallAliases_Idempotent(t *testing.T) {
	tmpDir := t.TempDir()
	rcFile := filepath.Join(tmpDir, ".zshrc")
	writeBlockFixture(t, rcFile)

	// First uninstall
	if err := UninstallAliases(rcFile); err != nil {
		t.Fatalf("first uninstall: %v", err)
	}
	// Second uninstall should not error
	if err := UninstallAliases(rcFile); err != nil {
		t.Fatalf("second uninstall: %v", err)
	}
}

func TestUninstallAliases_MissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	rcFile := filepath.Join(tmpDir, "nonexistent.zshrc")
	// Must not error when file doesn't exist.
	if err := UninstallAliases(rcFile); err != nil {
		t.Fatalf("UninstallAliases on missing file: %v", err)
	}
}

func TestUninstallAliases_HandEditedBlock(t *testing.T) {
	// Simulates a user who hand-edited content OUTSIDE the markers.
	// The block itself should be removed; surrounding content preserved.
	tmpDir := t.TempDir()
	rcFile := filepath.Join(tmpDir, ".zshrc")
	content := "export A=1\n\n" +
		StartMarker + "\nalias claude='dandori run -- claude'\n" + EndMarker +
		"\nexport B=2\n"
	_ = os.WriteFile(rcFile, []byte(content), 0644)

	if err := UninstallAliases(rcFile); err != nil {
		t.Fatalf("UninstallAliases: %v", err)
	}

	result, _ := os.ReadFile(rcFile)
	s := string(result)
	if strings.Contains(s, StartMarker) {
		t.Error("marker not removed")
	}
	if !strings.Contains(s, "export A=1") {
		t.Error("content before block destroyed")
	}
	if !strings.Contains(s, "export B=2") {
		t.Error("content after block destroyed")
	}
}
