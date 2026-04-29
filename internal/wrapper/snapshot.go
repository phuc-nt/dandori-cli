package wrapper

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

type SessionSnapshot struct {
	Files map[string]time.Time
	Dir   string
}

// projectDirReplacer is the cwd→Claude-project-dir-name encoder. Mirrors
// Claude Code CLI's own rule (verified empirically 2026-04-29): every `/`,
// `_`, and `.` becomes `-`.
var projectDirReplacer = strings.NewReplacer("/", "-", "_", "-", ".", "-")

func SnapshotSessionDir(cwd string) *SessionSnapshot {
	claudeDir := expectedClaudeProjectDir(cwd)
	if claudeDir == "" {
		return &SessionSnapshot{Files: make(map[string]time.Time)}
	}

	snapshot := &SessionSnapshot{
		Files: make(map[string]time.Time),
		Dir:   claudeDir,
	}

	entries, err := os.ReadDir(claudeDir)
	if err != nil {
		// Dir doesn't exist yet — Claude will create it on first run.
		// Keep Dir set so the tailer can poll for it to appear.
		return snapshot
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		snapshot.Files[entry.Name()] = info.ModTime()
	}

	return snapshot
}

func DetectSessionID(cwd string, before *SessionSnapshot) string {
	if before == nil || before.Dir == "" {
		return ""
	}

	entries, err := os.ReadDir(before.Dir)
	if err != nil {
		return ""
	}

	var newestFile string
	var newestTime time.Time

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		prevTime, existed := before.Files[entry.Name()]
		if !existed || info.ModTime().After(prevTime) {
			if info.ModTime().After(newestTime) {
				newestTime = info.ModTime()
				newestFile = entry.Name()
			}
		}
	}

	if newestFile == "" {
		return ""
	}

	return strings.TrimSuffix(newestFile, ".jsonl")
}

// getClaudeProjectDir returns the project dir only if it already exists.
// Retained for callers that want existence-checked behaviour.
func getClaudeProjectDir(cwd string) string {
	projectDir := expectedClaudeProjectDir(cwd)
	if projectDir == "" {
		return ""
	}
	if _, err := os.Stat(projectDir); os.IsNotExist(err) {
		return ""
	}
	return projectDir
}

// expectedClaudeProjectDir computes the ~/.claude/projects/<encoded-cwd> path
// without requiring it to exist. Claude creates the dir lazily on first run,
// so the snapshot needs the expected path so the tailer can poll for it.
func expectedClaudeProjectDir(cwd string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	realCwd, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		realCwd = cwd
	}

	// Claude Code encodes the cwd into ~/.claude/projects/<name> by replacing
	// `/`, `_`, and `.` all with `-`. Earlier we only replaced `/` — when the
	// user's cwd contained `_` or `.` (e.g. `_dandori-cli`, `foo.bar`), the
	// wrapper polled a path that never existed, the tailer got nothing, and
	// the run was persisted with cost_usd=0. Mirror Claude's rule exactly.
	dirName := projectDirReplacer.Replace(realCwd)
	return filepath.Join(home, ".claude", "projects", dirName)
}

func GetSessionLogPath(cwd string, before *SessionSnapshot) string {
	sessionID := DetectSessionID(cwd, before)
	if sessionID == "" || before == nil || before.Dir == "" {
		return ""
	}
	return filepath.Join(before.Dir, sessionID+".jsonl")
}
