package wrapper

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestExpectedClaudeProjectDir_SymlinkResolves is a regression test for the
// macOS /tmp → /private/tmp symlink issue. It verifies that
// expectedClaudeProjectDir resolves symlinks before encoding the path, so a
// wrapper started from a symlinked directory produces the same project-dir
// name as one started from the resolved real path.
//
// Without filepath.EvalSymlinks in expectedClaudeProjectDir, running from
// /tmp/foo would look for ~/.claude/projects/-tmp-foo while Claude Code writes
// sessions to ~/.claude/projects/-private-tmp-foo — causing cost_usd=0 for
// every run on macOS.
func TestExpectedClaudeProjectDir_SymlinkResolves(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks on Windows require elevated privileges; skipping")
	}

	// Create a real directory in a tmp location.
	realDir := t.TempDir()

	// Create a symlink pointing to the real dir.
	linkDir := realDir + "-symlink-link"
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Skipf("cannot create symlink (may need elevated privileges on this platform): %v", err)
	}
	defer os.Remove(linkDir)

	// Both paths must resolve to the same project dir.
	fromReal := expectedClaudeProjectDir(realDir)
	fromLink := expectedClaudeProjectDir(linkDir)

	if fromReal == "" {
		t.Fatal("expectedClaudeProjectDir returned empty for real path")
	}
	if fromLink == "" {
		t.Fatal("expectedClaudeProjectDir returned empty for symlink path")
	}
	if fromReal != fromLink {
		t.Errorf("symlink and real path produce different project dirs:\n  real  = %s\n  link  = %s\n  want equal",
			fromReal, fromLink)
	}
}

// TestExpectedClaudeProjectDir_SymlinkChain verifies multi-hop symlink chains
// (e.g. /tmp → /private/tmp on macOS where /tmp itself is a symlink).
func TestExpectedClaudeProjectDir_SymlinkChain(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks on Windows require elevated privileges; skipping")
	}

	// Create the real leaf directory.
	realDir := t.TempDir()

	// link1 → realDir
	link1 := realDir + "-chain-link1"
	if err := os.Symlink(realDir, link1); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	defer os.Remove(link1)

	// link2 → link1 (two-hop chain)
	link2 := realDir + "-chain-link2"
	if err := os.Symlink(link1, link2); err != nil {
		t.Skipf("cannot create second symlink in chain: %v", err)
	}
	defer os.Remove(link2)

	fromReal := expectedClaudeProjectDir(realDir)
	fromLink2 := expectedClaudeProjectDir(link2)

	if fromReal != fromLink2 {
		t.Errorf("two-hop symlink chain not fully resolved:\n  real  = %s\n  link2 = %s",
			fromReal, fromLink2)
	}
}

// TestSnapshotSessionDir_SymlinkDirFound verifies that SnapshotSessionDir
// called with a symlinked cwd correctly sets Dir to the resolved project dir,
// so the tailer can find real session files placed there by Claude Code.
func TestSnapshotSessionDir_SymlinkDirFound(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks on Windows require elevated privileges; skipping")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	// Create a temporary "real" working directory.
	realDir := t.TempDir()

	// Determine what project dir Claude would create for realDir.
	realResolved, _ := filepath.EvalSymlinks(realDir)
	encodedName := projectDirReplacer.Replace(realResolved)
	claudeProjectDir := filepath.Join(home, ".claude", "projects", encodedName)

	// Pre-create the project dir to simulate Claude having already run there.
	if err := os.MkdirAll(claudeProjectDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer os.RemoveAll(claudeProjectDir)

	// Create a symlink that points to the real working dir.
	linkDir := realDir + "-snap-link"
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	defer os.Remove(linkDir)

	// SnapshotSessionDir from the SYMLINK path must resolve to the same
	// claude project dir that was created for the REAL path.
	snap := SnapshotSessionDir(linkDir)
	if snap == nil {
		t.Fatal("snapshot nil")
	}
	if snap.Dir != claudeProjectDir {
		t.Errorf("snapshot.Dir = %q, want %q", snap.Dir, claudeProjectDir)
	}
}
