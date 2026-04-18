package wrapper

import (
	"os"
	"path/filepath"
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
