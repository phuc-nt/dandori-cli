// Package watcher polls Claude session logs and records orphan runs —
// runs made directly (e.g. `claude` without the dandori wrapper).
package watcher

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/phuc-nt/dandori-cli/internal/db"
	"github.com/phuc-nt/dandori-cli/internal/util"
	"github.com/phuc-nt/dandori-cli/internal/watchctl"
	"github.com/phuc-nt/dandori-cli/internal/wrapper"
)

// Config configures a Watcher.
type Config struct {
	DB                 *db.LocalDB
	ClaudeProjectsRoot string        // ~/.claude/projects by default
	Interval           time.Duration // poll cadence; ignored in PollOnce
}

// Watcher polls Claude session logs and inserts orphan run rows for sessions
// that were not already tracked by the `dandori run` wrapper.
type Watcher struct {
	db                 *db.LocalDB
	claudeProjectsRoot string
	interval           time.Duration
}

// New constructs a Watcher. If Interval is zero it defaults to 60s.
func New(cfg Config) *Watcher {
	interval := cfg.Interval
	if interval <= 0 {
		interval = 60 * time.Second
	}
	return &Watcher{
		db:                 cfg.DB,
		claudeProjectsRoot: cfg.ClaudeProjectsRoot,
		interval:           interval,
	}
}

// DiscoverProjects returns absolute paths of subdirectories under root.
func DiscoverProjects(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, filepath.Join(root, e.Name()))
		}
	}
	return dirs, nil
}

// DiscoverSessions returns absolute paths of *.jsonl files in a project dir.
func DiscoverSessions(projectDir string) ([]string, error) {
	entries, err := os.ReadDir(projectDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var files []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		files = append(files, filepath.Join(projectDir, e.Name()))
	}
	return files, nil
}

// Run polls forever at w.interval, returning when ctx is cancelled.
func (w *Watcher) Run(ctx context.Context) error {
	// Initial poll, then tick
	if err := w.PollOnce(); err != nil {
		slog.Warn("watcher poll error", "error", err)
	}

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := w.PollOnce(); err != nil {
				slog.Warn("watcher poll error", "error", err)
			}
		}
	}
}

// PollOnce scans all projects under ClaudeProjectsRoot once.
// On completion it writes a timestamp to ~/.dandori/.watch-stamp (best-effort).
func (w *Watcher) PollOnce() error {
	projects, err := DiscoverProjects(w.claudeProjectsRoot)
	if err != nil {
		return fmt.Errorf("discover projects: %w", err)
	}

	for _, p := range projects {
		sessions, err := DiscoverSessions(p)
		if err != nil {
			slog.Warn("discover sessions", "dir", p, "error", err)
			continue
		}
		for _, s := range sessions {
			if err := w.handleSession(p, s); err != nil {
				slog.Warn("handle session", "file", s, "error", err)
			}
		}
	}

	// Best-effort stamp — lets `dandori watch status` report last poll time.
	watchctl.WriteStampFile()
	return nil
}

// handleSession decides whether to insert an orphan run for a given session file.
// If a run row already exists with this session_id (from wrapper or prior poll),
// it skips insertion.
func (w *Watcher) handleSession(projectDir, sessionFile string) error {
	sessionID := strings.TrimSuffix(filepath.Base(sessionFile), ".jsonl")

	// Skip if already tracked
	var exists int
	err := w.db.QueryRow("SELECT COUNT(*) FROM runs WHERE session_id = ?", sessionID).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check existing: %w", err)
	}
	if exists > 0 {
		return nil
	}

	// Parse tokens from session
	usage := parseSessionTokens(sessionFile)
	cost := wrapper.ComputeCost(usage)

	// Recover cwd from project dir name (Claude uses path-with-dashes)
	cwd := reconstructCWDFromProjectName(filepath.Base(projectDir))

	fi, err := os.Stat(sessionFile)
	if err != nil {
		return err
	}

	runID := util.GenerateRunID()
	currentUser := os.Getenv("USER")
	if currentUser == "" {
		currentUser = "unknown"
	}
	hostname, _ := os.Hostname()
	workstationID := "ws-" + hostname

	_, err = w.db.Exec(`
		INSERT INTO runs (
			id, agent_name, agent_type, user, workstation_id, cwd,
			started_at, ended_at, status, session_id,
			input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
			model, cost_usd, exit_code, duration_sec
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		runID, "orphan", "claude", currentUser, workstationID, cwd,
		fi.ModTime().Format(time.RFC3339), fi.ModTime().Format(time.RFC3339),
		"done", sessionID,
		usage.Input, usage.Output, usage.CacheRead, usage.CacheWrite,
		usage.Model, cost, 0, 0,
	)
	if err != nil {
		return fmt.Errorf("insert orphan run: %w", err)
	}

	slog.Info("watcher captured orphan run",
		"session", sessionID, "tokens", usage.Input+usage.Output, "cost", cost)
	return nil
}

// parseSessionTokens streams through the JSONL file and aggregates usage
// across all assistant messages.
func parseSessionTokens(path string) wrapper.TokenUsage {
	total := wrapper.TokenUsage{}
	f, err := os.Open(path)
	if err != nil {
		return total
	}
	defer f.Close()

	type msg struct {
		Type    string `json:"type"`
		Message struct {
			Model string `json:"model"`
			Usage struct {
				InputTokens              int `json:"input_tokens"`
				OutputTokens             int `json:"output_tokens"`
				CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
				CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			} `json:"usage"`
		} `json:"message"`
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 4*1024*1024) // allow large lines
	for scanner.Scan() {
		var m msg
		if err := json.Unmarshal(scanner.Bytes(), &m); err != nil {
			continue
		}
		if m.Type != "assistant" {
			continue
		}
		total.Input += m.Message.Usage.InputTokens
		total.Output += m.Message.Usage.OutputTokens
		total.CacheWrite += m.Message.Usage.CacheCreationInputTokens
		total.CacheRead += m.Message.Usage.CacheReadInputTokens
		if m.Message.Model != "" {
			total.Model = m.Message.Model
		}
	}
	return total
}

// reconstructCWDFromProjectName turns "-Users-phucnt-project" back into "/Users/phucnt/project".
// Imperfect because it cannot distinguish real dashes from path separators, but
// gives a useful hint in the DB.
func reconstructCWDFromProjectName(name string) string {
	if strings.HasPrefix(name, "-") {
		return strings.ReplaceAll(name, "-", "/")
	}
	return name
}
