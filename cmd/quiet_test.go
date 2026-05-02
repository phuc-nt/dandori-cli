package cmd

import (
	"strings"
	"testing"
)

func TestQuietFlagInHelp(t *testing.T) {
	output, err := executeCommand(rootCmd, "--help")
	if err != nil {
		t.Fatalf("help command failed: %v", err)
	}
	if !strings.Contains(output, "--quiet") {
		t.Error("--help should list --quiet flag")
	}
	if !strings.Contains(output, "-q") {
		t.Error("--help should list -q shorthand")
	}
}

func TestQuietAccessorDefault(t *testing.T) {
	// Reset to default state
	quiet = false
	if Quiet() {
		t.Error("Quiet() should return false by default")
	}
}

func TestQuietAccessorWhenSet(t *testing.T) {
	quiet = true
	defer func() { quiet = false }()
	if !Quiet() {
		t.Error("Quiet() should return true when quiet=true")
	}
}

func TestQuietMutualExclusionWithVerbose(t *testing.T) {
	// Reset flags after test
	origQuiet := quiet
	origVerbose := verbose
	defer func() {
		quiet = origQuiet
		verbose = origVerbose
	}()

	// executeCommand runs PersistentPreRunE via a real subcommand.
	// Use "version" as the lightest subcommand that triggers PersistentPreRunE.
	_, err := executeCommand(rootCmd, "-q", "-v", "version")
	if err == nil {
		t.Fatal("expected error when both -q and -v are set, got nil")
	}
	if !strings.Contains(err.Error(), "cannot use --quiet and --verbose together") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestQuietFlagParsed(t *testing.T) {
	origQuiet := quiet
	defer func() { quiet = origQuiet }()

	// Check that help output contains the quiet flag — flag is wired correctly.
	output, err := executeCommand(rootCmd, "run", "--help")
	if err != nil {
		t.Fatalf("run --help failed: %v", err)
	}
	// -q/--quiet is a persistent flag on root, should appear in any subcommand help
	if !strings.Contains(output, "quiet") {
		t.Error("run --help should inherit --quiet from root persistent flags")
	}
}

func TestGlobalFlagsIncludeQuiet(t *testing.T) {
	output, err := executeCommand(rootCmd, "--help")
	if err != nil {
		t.Fatalf("help failed: %v", err)
	}
	for _, flag := range []string{"--config", "--verbose", "--quiet"} {
		if !strings.Contains(output, flag) {
			t.Errorf("global help should contain flag: %s", flag)
		}
	}
}
