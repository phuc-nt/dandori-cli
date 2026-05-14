package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		in      string
		want    slog.Level
		wantErr bool
	}{
		{"", slog.LevelInfo, false},
		{"info", slog.LevelInfo, false},
		{"INFO", slog.LevelInfo, false},
		{"debug", slog.LevelDebug, false},
		{" Debug ", slog.LevelDebug, false},
		{"warn", slog.LevelWarn, false},
		{"warning", slog.LevelWarn, false},
		{"error", slog.LevelError, false},
		{"trace", slog.LevelInfo, true},
		{"bogus", slog.LevelInfo, true},
	}
	for _, tt := range tests {
		got, err := ParseLogLevel(tt.in)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseLogLevel(%q) err = %v, wantErr %v", tt.in, err, tt.wantErr)
		}
		if got != tt.want {
			t.Errorf("ParseLogLevel(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestDefaultLogLevel(t *testing.T) {
	if got := DefaultConfig().LogLevel; got != "info" {
		t.Errorf("default log level = %q, want %q", got, "info")
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.ServerURL != "http://localhost:8080" {
		t.Errorf("expected default server URL, got %s", cfg.ServerURL)
	}
	if cfg.Agent.Type != "claude_code" {
		t.Errorf("expected agent type claude_code, got %s", cfg.Agent.Type)
	}
	if cfg.Sync.IntervalSec != 300 {
		t.Errorf("expected sync interval 300, got %d", cfg.Sync.IntervalSec)
	}
	if cfg.GitHub.Enabled {
		t.Error("expected GitHub disabled by default")
	}
	if cfg.GitHub.PollIntervalSec != 300 {
		t.Errorf("expected GitHub poll interval 300, got %d", cfg.GitHub.PollIntervalSec)
	}
}

func TestEnvOverrides_GitHub(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.yaml")

	cfg := DefaultConfig()
	if err := Save(cfg, path); err != nil {
		t.Fatalf("save config: %v", err)
	}

	os.Setenv("DANDORI_GITHUB_REPO", "owner/repo")
	os.Setenv("DANDORI_GITHUB_TOKEN", "ghp_xxx")
	os.Setenv("DANDORI_GITHUB_ENABLED", "true")
	defer func() {
		os.Unsetenv("DANDORI_GITHUB_REPO")
		os.Unsetenv("DANDORI_GITHUB_TOKEN")
		os.Unsetenv("DANDORI_GITHUB_ENABLED")
	}()

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if loaded.GitHub.Repo != "owner/repo" || loaded.GitHub.Token != "ghp_xxx" || !loaded.GitHub.Enabled {
		t.Errorf("github env overrides failed: %+v", loaded.GitHub)
	}
}

func TestLoadSave(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.yaml")

	cfg := DefaultConfig()
	cfg.ServerURL = "https://test.example.com"
	cfg.Jira.BaseURL = "https://jira.example.com"
	cfg.Agent.Name = "test-agent"
	cfg.Agent.Capabilities = []string{"backend", "testing"}

	if err := Save(cfg, path); err != nil {
		t.Fatalf("save config: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if loaded.ServerURL != cfg.ServerURL {
		t.Errorf("server URL mismatch: got %s, want %s", loaded.ServerURL, cfg.ServerURL)
	}
	if loaded.Jira.BaseURL != cfg.Jira.BaseURL {
		t.Errorf("jira base URL mismatch: got %s, want %s", loaded.Jira.BaseURL, cfg.Jira.BaseURL)
	}
	if loaded.Agent.Name != cfg.Agent.Name {
		t.Errorf("agent name mismatch: got %s, want %s", loaded.Agent.Name, cfg.Agent.Name)
	}
	if len(loaded.Agent.Capabilities) != 2 {
		t.Errorf("expected 2 capabilities, got %d", len(loaded.Agent.Capabilities))
	}
}

func TestEnvOverrides(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.yaml")

	cfg := DefaultConfig()
	if err := Save(cfg, path); err != nil {
		t.Fatalf("save config: %v", err)
	}

	os.Setenv("DANDORI_SERVER_URL", "https://env.example.com")
	os.Setenv("DANDORI_API_KEY", "test-key")
	os.Setenv("DANDORI_AGENT_CAPABILITIES", "go,rust")
	defer func() {
		os.Unsetenv("DANDORI_SERVER_URL")
		os.Unsetenv("DANDORI_API_KEY")
		os.Unsetenv("DANDORI_AGENT_CAPABILITIES")
	}()

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if loaded.ServerURL != "https://env.example.com" {
		t.Errorf("env override failed for server URL: got %s", loaded.ServerURL)
	}
	if loaded.APIKey != "test-key" {
		t.Errorf("env override failed for API key: got %s", loaded.APIKey)
	}
	if len(loaded.Agent.Capabilities) != 2 || loaded.Agent.Capabilities[0] != "go" {
		t.Errorf("env override failed for capabilities: got %v", loaded.Agent.Capabilities)
	}
}

func TestLoadNonExistent(t *testing.T) {
	cfg, err := Load("/nonexistent/path/config.yaml")
	if err != nil {
		t.Fatalf("expected no error for nonexistent file, got %v", err)
	}
	if cfg.ServerURL != "http://localhost:8080" {
		t.Errorf("expected default config, got %s", cfg.ServerURL)
	}
}

func TestJiraConfig_ResolveBoardIDs(t *testing.T) {
	tests := []struct {
		name string
		j    JiraConfig
		want []int
	}{
		{"empty", JiraConfig{}, nil},
		{"single legacy", JiraConfig{BoardID: 3}, []int{3}},
		{"list only", JiraConfig{BoardIDs: []int{4, 5}}, []int{4, 5}},
		{"merged dedupe", JiraConfig{BoardID: 3, BoardIDs: []int{4, 3, 5}}, []int{3, 4, 5}},
		{"ignores zero/negative", JiraConfig{BoardID: 0, BoardIDs: []int{4, -1, 0, 5}}, []int{4, 5}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.j.ResolveBoardIDs()
			if len(got) != len(tt.want) {
				t.Fatalf("len = %d, want %d (got %v want %v)", len(got), len(tt.want), got, tt.want)
			}
			for i, v := range tt.want {
				if got[i] != v {
					t.Errorf("[%d] = %d, want %d", i, got[i], v)
				}
			}
		})
	}
}
