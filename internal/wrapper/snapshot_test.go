package wrapper

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGetClaudeProjectDir(t *testing.T) {
	home, _ := os.UserHomeDir()

	dir := getClaudeProjectDir("/nonexistent/path")
	if dir != "" {
		t.Error("should return empty for nonexistent path")
	}

	testDir := filepath.Join(home, ".claude", "projects", "-tmp-test-project")
	os.MkdirAll(testDir, 0755)
	defer os.RemoveAll(testDir)

	dir = getClaudeProjectDir("/tmp/test-project")
	if dir != testDir {
		t.Errorf("dir = %s, want %s", dir, testDir)
	}
}

func TestSnapshotSessionDir(t *testing.T) {
	tmpDir := t.TempDir()

	snapshot := SnapshotSessionDir(tmpDir)

	if snapshot == nil {
		t.Fatal("snapshot should not be nil")
	}

	if snapshot.Files == nil {
		t.Error("Files map should be initialized")
	}
}

func TestDetectSessionID(t *testing.T) {
	tmpDir := t.TempDir()
	sessionDir := filepath.Join(tmpDir, ".claude", "projects", "test-hash")
	os.MkdirAll(sessionDir, 0755)

	snapshot := &SessionSnapshot{
		Files: make(map[string]time.Time),
		Dir:   sessionDir,
	}

	sessionID := DetectSessionID(tmpDir, snapshot)
	if sessionID != "" {
		t.Error("should return empty when no new files")
	}

	sessionFile := filepath.Join(sessionDir, "session-abc123.jsonl")
	os.WriteFile(sessionFile, []byte(`{"test": true}`), 0644)

	sessionID = DetectSessionID(tmpDir, snapshot)
	if sessionID != "session-abc123" {
		t.Errorf("sessionID = %s, want session-abc123", sessionID)
	}
}

func TestDetectSessionIDWithModifiedFile(t *testing.T) {
	tmpDir := t.TempDir()
	sessionDir := filepath.Join(tmpDir, ".claude", "projects", "test-hash")
	os.MkdirAll(sessionDir, 0755)

	sessionFile := filepath.Join(sessionDir, "existing-session.jsonl")
	os.WriteFile(sessionFile, []byte(`{"line": 1}`), 0644)

	info, _ := os.Stat(sessionFile)
	snapshot := &SessionSnapshot{
		Files: map[string]time.Time{
			"existing-session.jsonl": info.ModTime(),
		},
		Dir: sessionDir,
	}

	sessionID := DetectSessionID(tmpDir, snapshot)
	if sessionID != "" {
		t.Error("should return empty when file not modified")
	}

	time.Sleep(10 * time.Millisecond)
	os.WriteFile(sessionFile, []byte(`{"line": 1}\n{"line": 2}`), 0644)

	sessionID = DetectSessionID(tmpDir, snapshot)
	if sessionID != "existing-session" {
		t.Errorf("sessionID = %s, want existing-session", sessionID)
	}
}

func TestGetSessionLogPath(t *testing.T) {
	tmpDir := t.TempDir()
	sessionDir := filepath.Join(tmpDir, ".claude", "projects", "test-hash")
	os.MkdirAll(sessionDir, 0755)

	snapshot := &SessionSnapshot{
		Files: make(map[string]time.Time),
		Dir:   sessionDir,
	}

	sessionFile := filepath.Join(sessionDir, "new-session.jsonl")
	os.WriteFile(sessionFile, []byte(`{}`), 0644)

	path := GetSessionLogPath(tmpDir, snapshot)
	if path != sessionFile {
		t.Errorf("path = %s, want %s", path, sessionFile)
	}
}

func TestSnapshotSessionDir_CreatesDirPathEvenIfMissing(t *testing.T) {
	// Regression: when Claude hasn't run in the cwd yet, the project dir
	// doesn't exist. Snapshot must still return the expected path so the
	// tailer can poll for it to appear.
	tmpDir := t.TempDir()
	freshCwd := filepath.Join(tmpDir, "brand-new-workspace-"+time.Now().Format("150405"))
	if err := os.MkdirAll(freshCwd, 0755); err != nil {
		t.Fatal(err)
	}

	snapshot := SnapshotSessionDir(freshCwd)

	if snapshot == nil {
		t.Fatal("snapshot nil")
	}
	if snapshot.Dir == "" {
		t.Error("Dir should be set to expected path even when claude project dir doesn't exist yet")
	}
	home, _ := os.UserHomeDir()
	realCwd, _ := filepath.EvalSymlinks(freshCwd)
	expectedDir := filepath.Join(home, ".claude", "projects",
		"-"+filepath.Base(filepath.Dir(realCwd))+"-"+filepath.Base(realCwd))
	// Just check the path is under ~/.claude/projects — exact encoding already tested elsewhere.
	if !strings.HasPrefix(snapshot.Dir, filepath.Join(home, ".claude", "projects")) {
		t.Errorf("Dir = %s, want under ~/.claude/projects (expected approx %s)", snapshot.Dir, expectedDir)
	}
}

func TestSnapshotNilHandling(t *testing.T) {
	sessionID := DetectSessionID("/nonexistent", nil)
	if sessionID != "" {
		t.Error("should handle nil snapshot")
	}

	path := GetSessionLogPath("/nonexistent", nil)
	if path != "" {
		t.Error("should handle nil snapshot")
	}
}

// TestExpectedClaudeProjectDir_EncodingMatchesClaude locks the cwd→project-dir
// encoding to what Claude Code itself produces. Claude replaces `/`, `_`, and
// `.` all with `-` when constructing ~/.claude/projects/<name>; if our wrapper
// does anything different, it will tail the wrong file (or no file at all)
// and report cost_usd=0 for the run. Verified empirically against Claude Code
// CLI (see plans/reports/ / discussion 2026-04-29).
func TestExpectedClaudeProjectDir_EncodingMatchesClaude(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("home: %v", err)
	}
	prefix := filepath.Join(home, ".claude", "projects") + string(filepath.Separator)

	cases := []struct {
		name string
		cwd  string
		// suffix is what we expect AFTER ~/.claude/projects/.
		suffix string
	}{
		{"plain slashes", "/tmp/foo/bar", "-tmp-foo-bar"},
		{"underscore in dir name", "/tmp/foo_bar", "-tmp-foo-bar"},
		{"underscore at segment start", "/tmp/_dandori-cli", "-tmp--dandori-cli"},
		{"dot in dir name", "/tmp/foo.bar", "-tmp-foo-bar"},
		{"dotfile segment", "/tmp/.foo", "-tmp--foo"},
		{"mixed underscores and dots", "/a_b/c.d/e", "-a-b-c-d-e"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := expectedClaudeProjectDir(c.cwd)
			want := prefix + c.suffix
			if got != want {
				t.Errorf("expectedClaudeProjectDir(%q)\n  got  %q\n  want %q", c.cwd, got, want)
			}
		})
	}
}
