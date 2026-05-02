package cmd

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phuc-nt/dandori-cli/internal/config"
)

// setupJiraServer returns an httptest server that responds to /rest/api/3/myself.
func setupJiraServer(t *testing.T, statusCode int, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/api/3/myself" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(statusCode)
			_, _ = w.Write([]byte(body))
			return
		}
		http.NotFound(w, r)
	}))
}

// setupConfluenceServer returns an httptest server that responds to space lookups.
func setupConfluenceServer(t *testing.T, spaceKey string, statusCode int, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cloudPath := "/wiki/rest/api/space/" + spaceKey
		dcPath := "/rest/api/space/" + spaceKey
		if r.URL.Path == cloudPath || r.URL.Path == dcPath {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(statusCode)
			_, _ = w.Write([]byte(body))
			return
		}
		http.NotFound(w, r)
	}))
}

func TestRunWizard_HappyPath(t *testing.T) {
	jiraSrv := setupJiraServer(t, 200, `{"accountId":"u1","displayName":"Test User"}`)
	defer jiraSrv.Close()

	confSrv := setupConfluenceServer(t, "ENG", 200, `{"key":"ENG","name":"Engineering"}`)
	defer confSrv.Close()

	// Compose stdin fixture — one answer per prompt in wizard order.
	lines := []string{
		"http://localhost:8080", // Server URL
		jiraSrv.URL,             // Jira base URL
		"user@example.com",      // Jira email
		"mytoken",               // Jira API token (plain in non-tty)
		"PROJ",                  // Jira project key
		// Jira connection test happens automatically
		"y",         // Enable Confluence?
		confSrv.URL, // Confluence base URL
		"ENG",       // Confluence space key
		// Confluence connection test happens automatically
		"myagent", // Agent name
		"n",       // Quality tracking
		"n",       // Watch daemon
	}
	stdinContent := strings.Join(lines, "\n") + "\n"

	// Write to a temp dir config path.
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Redirect stdin for the wizard.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteString(stdinContent); err != nil {
		t.Fatal(err)
	}
	w.Close()

	origStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = origStdin }()

	cfg := config.DefaultConfig()
	if err := runWizard(cfg); err != nil {
		t.Fatalf("runWizard returned error: %v", err)
	}

	// Validate cfg struct directly — do NOT reload via config.Load which would
	// apply env overrides (DANDORI_JIRA_TOKEN etc.) and mask test values.
	if cfg.Jira.User != "user@example.com" {
		t.Errorf("jira.user = %q, want user@example.com", cfg.Jira.User)
	}
	if cfg.Jira.Token != "mytoken" {
		t.Errorf("jira.token = %q, want mytoken", cfg.Jira.Token)
	}
	if cfg.Project.Key != "PROJ" {
		t.Errorf("project.key = %q, want PROJ", cfg.Project.Key)
	}
	if cfg.Confluence.SpaceKey != "ENG" {
		t.Errorf("confluence.space_key = %q, want ENG", cfg.Confluence.SpaceKey)
	}
	if cfg.Confluence.BaseURL != confSrv.URL {
		t.Errorf("confluence.base_url = %q, want %s", cfg.Confluence.BaseURL, confSrv.URL)
	}
	if cfg.Agent.Name != "myagent" {
		t.Errorf("agent.name = %q, want myagent", cfg.Agent.Name)
	}

	// Also verify round-trip save (that Save doesn't error).
	if err := config.Save(cfg, configPath); err != nil {
		t.Fatalf("save config: %v", err)
	}
}

func TestRunWizard_JiraConnFail_SaveAnyway(t *testing.T) {
	jiraSrv := setupJiraServer(t, 401, `{"message":"Unauthorized"}`)
	defer jiraSrv.Close()

	lines := []string{
		"",             // Server URL (keep default)
		jiraSrv.URL,    // Jira base URL
		"bad@test.com", // Jira email
		"badtoken",     // Jira API token
		"BAD",          // project key
		"y",            // save anyway after 401
		"n",            // disable Confluence
		"",             // agent name (keep default)
		"n",            // quality
		"n",            // watch daemon
	}
	stdinContent := strings.Join(lines, "\n") + "\n"

	r, w, _ := os.Pipe()
	_, _ = w.WriteString(stdinContent)
	w.Close()
	origStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = origStdin }()

	cfg := config.DefaultConfig()
	err := runWizard(cfg)
	// User chose save anyway — should succeed.
	if err != nil {
		t.Fatalf("runWizard returned error: %v", err)
	}
}

func TestRunWizard_JiraConnFail_Abort(t *testing.T) {
	jiraSrv := setupJiraServer(t, 401, `{"message":"Unauthorized"}`)
	defer jiraSrv.Close()

	lines := []string{
		"",
		jiraSrv.URL,
		"bad@test.com",
		"badtoken",
		"BAD",
		"n", // do NOT save anyway
	}
	stdinContent := strings.Join(lines, "\n") + "\n"

	r, w, _ := os.Pipe()
	_, _ = w.WriteString(stdinContent)
	w.Close()
	origStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = origStdin }()

	cfg := config.DefaultConfig()
	err := runWizard(cfg)
	if err == nil {
		t.Fatal("expected error when user aborts after Jira failure, got nil")
	}
	if !strings.Contains(err.Error(), "aborted") {
		t.Errorf("error %q does not mention 'aborted'", err.Error())
	}
}

func TestDeriveConfluenceURL(t *testing.T) {
	tests := []struct {
		jiraURL string
		want    string
	}{
		{"https://acme.atlassian.net", "https://acme.atlassian.net/wiki"},
		{"https://acme.atlassian.net/", "https://acme.atlassian.net/wiki"},
		{"https://acme.atlassian.net/wiki", "https://acme.atlassian.net/wiki"},
		{"https://jira.internal.corp", ""},
		{"http://localhost:8080", ""},
		{"", ""},
	}
	for _, tc := range tests {
		got := deriveConfluenceURL(tc.jiraURL)
		if got != tc.want {
			t.Errorf("deriveConfluenceURL(%q) = %q, want %q", tc.jiraURL, got, tc.want)
		}
	}
}
