package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

func ParseLogLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("invalid log level: %q", s)
	}
}

type Config struct {
	ServerURL  string           `yaml:"server_url"`
	APIKey     string           `yaml:"api_key"`
	LogLevel   string           `yaml:"log_level"`
	Jira       JiraConfig       `yaml:"jira"`
	Confluence ConfluenceConfig `yaml:"confluence"`
	Agent      AgentConfig      `yaml:"agent"`
	Project    ProjectConfig    `yaml:"project"`
	Sync       SyncConfig       `yaml:"sync"`
	Quality    QualityConfig    `yaml:"quality"`
	Wrapper    WrapperConfig    `yaml:"wrapper"`
	Verify     VerifyConfig     `yaml:"verify"`
	Metric     MetricConfig     `yaml:"metric"`
}

// MetricConfig drives `dandori metric export`. All fields optional; defaults
// from internal/metric.DefaultJiraStatusConfig are used when empty.
type MetricConfig struct {
	ReleaseStatusNames    []string `yaml:"release_status_names"`
	InProgressStatusNames []string `yaml:"in_progress_status_names"`
	IncidentIssueTypes    []string `yaml:"incident_issue_types"`
	IncidentLabels        []string `yaml:"incident_labels"`
	JQLExtra              string   `yaml:"jql_extra"` // applied to deploy + incident search, e.g. `AND project = PAY`
}

// VerifyConfig — Bug #3 pre-sync gate (semantic check + quality gate).
// See plans/260428-0812-bug-3-fake-completion-spike/plan.md.
type VerifyConfig struct {
	SemanticCheck bool   `yaml:"semantic_check"` // Q3: path-match spec vs diff
	QualityGate   bool   `yaml:"quality_gate"`   // Q4: lint/test on changed files
	SkipLabel     string `yaml:"skip_label"`     // Q2: Jira label to bypass gates
}

type WrapperConfig struct {
	PostExitTimeout string `yaml:"post_exit_timeout"`
}

type QualityConfig struct {
	Enabled     bool   `yaml:"enabled"`
	LintCommand string `yaml:"lint_command"`
	TestCommand string `yaml:"test_command"`
	Timeout     string `yaml:"timeout"`
}

type JiraConfig struct {
	BaseURL string `yaml:"base_url"`
	User    string `yaml:"user"`
	Token   string `yaml:"token"`
	// BoardID is the legacy single-board field kept for backward compat.
	// New configs should prefer board_ids (a list) so the poller can watch
	// multiple projects at once. ResolveBoardIDs merges both.
	BoardID         int               `yaml:"board_id"`
	BoardIDs        []int             `yaml:"board_ids"`
	PollIntervalSec int               `yaml:"poll_interval_sec"`
	AgentFieldID    string            `yaml:"agent_field_id"`
	StatusMapping   map[string]string `yaml:"status_mapping"`
	Cloud           bool              `yaml:"cloud"`
}

// ResolveBoardIDs returns the deduped union of BoardID + BoardIDs, preserving
// order with BoardID first when present. Empty result means no board is
// configured — callers must error out.
func (j JiraConfig) ResolveBoardIDs() []int {
	seen := make(map[int]bool)
	var out []int
	if j.BoardID > 0 {
		seen[j.BoardID] = true
		out = append(out, j.BoardID)
	}
	for _, id := range j.BoardIDs {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

type ConfluenceConfig struct {
	BaseURL             string `yaml:"base_url"`
	User                string `yaml:"user"`
	Token               string `yaml:"token"`
	SpaceKey            string `yaml:"space_key"`
	ReportsParentPageID string `yaml:"reports_parent_page_id"`
	AutoPost            bool   `yaml:"auto_post"`
	CacheTTLMin         int    `yaml:"cache_ttl_min"`
	Cloud               bool   `yaml:"cloud"`
}

type AgentConfig struct {
	Type         string   `yaml:"type"`
	Name         string   `yaml:"name"`
	Capabilities []string `yaml:"capabilities"`
}

type ProjectConfig struct {
	Key  string `yaml:"key"`
	Team string `yaml:"team"`
}

type SyncConfig struct {
	IntervalSec int `yaml:"interval_sec"`
	BatchSize   int `yaml:"batch_size"`
}

func DefaultConfig() *Config {
	return &Config{
		ServerURL: "http://localhost:8080",
		LogLevel:  "info",
		Agent: AgentConfig{
			Type: "claude_code",
			Name: "default",
		},
		Sync: SyncConfig{
			IntervalSec: 300,
			BatchSize:   100,
		},
		Quality: QualityConfig{
			Enabled:     true,
			LintCommand: "golangci-lint run --json --out-format json 2>/dev/null || true",
			TestCommand: "go test -json -count=1 ./... 2>&1 || true",
			Timeout:     "30s",
		},
		Wrapper: WrapperConfig{
			PostExitTimeout: "10s",
		},
		Verify: VerifyConfig{
			SemanticCheck: true,
			QualityGate:   true,
			SkipLabel:     "skip-verify",
		},
	}
}

func ConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	return filepath.Join(home, ".dandori"), nil
}

func ConfigPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yaml"), nil
}

func DBPath() (string, error) {
	if env := os.Getenv("DANDORI_DB"); env != "" {
		return env, nil
	}
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	// Honor `~/.dandori/active_db` pointer file if present — used by
	// `dandori demo --use` to swap in demo.db without editing config.
	pointer := filepath.Join(dir, "active_db")
	if data, err := os.ReadFile(pointer); err == nil {
		p := strings.TrimSpace(string(data))
		if p != "" {
			return p, nil
		}
	}
	return filepath.Join(dir, "local.db"), nil
}

func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	if path == "" {
		var err error
		path, err = ConfigPath()
		if err != nil {
			return nil, err
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			applyEnvOverrides(cfg)
			return cfg, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	applyEnvOverrides(cfg)
	return cfg, nil
}

func Save(cfg *Config, path string) error {
	if path == "" {
		var err error
		path, err = ConfigPath()
		if err != nil {
			return err
		}
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	return nil
}

func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("DANDORI_SERVER_URL"); v != "" {
		cfg.ServerURL = v
	}
	if v := os.Getenv("DANDORI_API_KEY"); v != "" {
		cfg.APIKey = v
	}
	if v := os.Getenv("DANDORI_JIRA_BASE_URL"); v != "" {
		cfg.Jira.BaseURL = v
	}
	if v := os.Getenv("DANDORI_JIRA_USER"); v != "" {
		cfg.Jira.User = v
	}
	if v := os.Getenv("DANDORI_JIRA_TOKEN"); v != "" {
		cfg.Jira.Token = v
	}
	if v := os.Getenv("DANDORI_CONFLUENCE_BASE_URL"); v != "" {
		cfg.Confluence.BaseURL = v
	}
	if v := os.Getenv("DANDORI_CONFLUENCE_SPACE_KEY"); v != "" {
		cfg.Confluence.SpaceKey = v
	}
	if v := os.Getenv("DANDORI_CONFLUENCE_REPORTS_PARENT_PAGE_ID"); v != "" {
		cfg.Confluence.ReportsParentPageID = v
	}
	if v := os.Getenv("DANDORI_AGENT_TYPE"); v != "" {
		cfg.Agent.Type = v
	}
	if v := os.Getenv("DANDORI_AGENT_NAME"); v != "" {
		cfg.Agent.Name = v
	}
	if v := os.Getenv("DANDORI_AGENT_CAPABILITIES"); v != "" {
		cfg.Agent.Capabilities = strings.Split(v, ",")
	}
	if v := os.Getenv("DANDORI_PROJECT_KEY"); v != "" {
		cfg.Project.Key = v
	}
	if v := os.Getenv("DANDORI_PROJECT_TEAM"); v != "" {
		cfg.Project.Team = v
	}
	if v := os.Getenv("DANDORI_LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}
}
