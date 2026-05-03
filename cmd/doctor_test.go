package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phuc-nt/dandori-cli/internal/config"
)

// TestDoctorCmd_Registered verifies the 'doctor' subcommand is wired on rootCmd.
func TestDoctorCmd_Registered(t *testing.T) {
	for _, sub := range rootCmd.Commands() {
		if sub.Name() == "doctor" {
			return
		}
	}
	t.Fatal("'doctor' subcommand not registered on rootCmd")
}

// TestCheckConfig covers the 4 config states: nil, missing Jira, missing
// Confluence, and complete. The real Jira/Confluence probes are skipped
// because they hit network — covered by their own healthcheck_test files.
func TestCheckConfig(t *testing.T) {
	tests := []struct {
		name   string
		cfg    *config.Config
		wantOK bool
		want   string // substring match in detail
	}{
		{"nil config", nil, false, "not loaded"},
		{
			"missing Jira",
			&config.Config{Confluence: config.ConfluenceConfig{BaseURL: "x", SpaceKey: "Y"}},
			false,
			"Jira credentials missing",
		},
		{
			"missing Confluence",
			&config.Config{Jira: config.JiraConfig{BaseURL: "x", User: "u", Token: "t"}},
			false,
			"Confluence",
		},
		{
			"complete",
			&config.Config{
				Jira:       config.JiraConfig{BaseURL: "https://x.atlassian.net", User: "u", Token: "t"},
				Confluence: config.ConfluenceConfig{BaseURL: "https://x.atlassian.net/wiki", SpaceKey: "Y"},
			},
			true,
			"loaded with Jira",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := checkConfig(tt.cfg)
			if c.ok != tt.wantOK {
				t.Errorf("ok = %v, want %v (detail=%q)", c.ok, tt.wantOK, c.detail)
			}
			if !strings.Contains(c.detail, tt.want) {
				t.Errorf("detail = %q, want substring %q", c.detail, tt.want)
			}
		})
	}
}

// TestCheckClaudeBinary asserts behaviour by manipulating PATH. We can't
// guarantee 'claude' is installed on the test runner, so we test both states
// by first clearing PATH (forces not-found), then pointing PATH at a directory
// containing a fake 'claude' executable.
func TestCheckClaudeBinary(t *testing.T) {
	origPath := os.Getenv("PATH")
	t.Cleanup(func() { _ = os.Setenv("PATH", origPath) })

	t.Run("not found", func(t *testing.T) {
		_ = os.Setenv("PATH", "/nonexistent-doctor-test-path")
		c := checkClaudeBinary()
		if c.ok {
			t.Error("expected ok=false when PATH has no claude")
		}
		if !strings.Contains(c.detail, "not found") {
			t.Errorf("detail = %q, want 'not found'", c.detail)
		}
	})

	t.Run("found", func(t *testing.T) {
		dir := t.TempDir()
		fake := filepath.Join(dir, "claude")
		if err := os.WriteFile(fake, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		_ = os.Setenv("PATH", dir)
		c := checkClaudeBinary()
		if !c.ok {
			t.Errorf("expected ok=true, got detail=%q", c.detail)
		}
		if c.detail != fake {
			t.Errorf("detail = %q, want %q", c.detail, fake)
		}
	})
}

// TestCheckDB exercises the writability probe against a temp HOME so we don't
// touch the real ~/.dandori directory.
func TestCheckDB(t *testing.T) {
	origHome := os.Getenv("HOME")
	t.Cleanup(func() { _ = os.Setenv("HOME", origHome) })

	t.Run("missing file (creatable)", func(t *testing.T) {
		home := t.TempDir()
		_ = os.Setenv("HOME", home)
		// Pre-create ~/.dandori dir so DBPath() resolves cleanly
		if err := os.MkdirAll(filepath.Join(home, ".dandori"), 0o755); err != nil {
			t.Fatal(err)
		}
		c := checkDB()
		if !c.ok {
			t.Errorf("expected ok=true, got detail=%q", c.detail)
		}
	})

	t.Run("existing file (writable)", func(t *testing.T) {
		home := t.TempDir()
		_ = os.Setenv("HOME", home)
		dandoriDir := filepath.Join(home, ".dandori")
		if err := os.MkdirAll(dandoriDir, 0o755); err != nil {
			t.Fatal(err)
		}
		dbPath := filepath.Join(dandoriDir, "local.db")
		if err := os.WriteFile(dbPath, []byte("dummy"), 0o644); err != nil {
			t.Fatal(err)
		}
		c := checkDB()
		if !c.ok {
			t.Errorf("expected ok=true, got detail=%q", c.detail)
		}
		if c.detail != dbPath {
			t.Errorf("detail = %q, want %q", c.detail, dbPath)
		}
	})
}

// TestCheckJira_SkipsWhenIncomplete verifies the early-return when config is
// missing — we don't want doctor to make a network call against an empty URL.
func TestCheckJira_SkipsWhenIncomplete(t *testing.T) {
	c := checkJira(nil)
	if c.ok {
		t.Error("expected ok=false for nil config")
	}
	if !strings.Contains(c.detail, "skipped") {
		t.Errorf("detail = %q, want 'skipped'", c.detail)
	}
}

// TestCheckConfluence_SkipsWhenIncomplete mirrors the Jira test.
func TestCheckConfluence_SkipsWhenIncomplete(t *testing.T) {
	c := checkConfluence(&config.Config{})
	if c.ok {
		t.Error("expected ok=false for empty config")
	}
	if !strings.Contains(c.detail, "skipped") {
		t.Errorf("detail = %q, want 'skipped'", c.detail)
	}
}
