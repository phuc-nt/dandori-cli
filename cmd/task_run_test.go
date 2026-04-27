package cmd

import (
	"reflect"
	"strings"
	"testing"
)

const ctxFile = "/var/folders/tmp/dandori-context-FOO-1.md"

func TestInjectClaudeContext_NoContextFile(t *testing.T) {
	in := []string{"claude", "--permission-mode", "acceptEdits"}
	got := injectClaudeContext(in, "")
	if !reflect.DeepEqual(got, in) {
		t.Errorf("expected unchanged, got %v", got)
	}
}

func TestInjectClaudeContext_NonClaudeCommand(t *testing.T) {
	in := []string{"codex", "run"}
	got := injectClaudeContext(in, ctxFile)
	if !reflect.DeepEqual(got, in) {
		t.Errorf("expected unchanged for non-claude, got %v", got)
	}
}

func TestInjectClaudeContext_AddsAddDirAndPrompt(t *testing.T) {
	in := []string{"claude", "--permission-mode", "acceptEdits"}
	got := injectClaudeContext(in, ctxFile)

	// -p must be appended with the context instruction
	if !containsArg(got, "-p") {
		t.Errorf("missing -p flag: %v", got)
	}
	// --add-dir must be auto-injected pointing at the context file's dir
	addDirVal, ok := argValue(got, "--add-dir")
	if !ok {
		t.Fatalf("missing --add-dir: %v", got)
	}
	if addDirVal != "/var/folders/tmp" {
		t.Errorf("--add-dir = %q, want %q", addDirVal, "/var/folders/tmp")
	}
}

func TestInjectClaudeContext_PreservesUserAddDir(t *testing.T) {
	in := []string{"claude", "--add-dir", "/some/path"}
	got := injectClaudeContext(in, ctxFile)

	count := 0
	for _, a := range got {
		if a == "--add-dir" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly one --add-dir (user-provided), got %d in %v", count, got)
	}
}

func TestInjectClaudeContext_SkipsAddDirWhenSkipPerms(t *testing.T) {
	// When --dangerously-skip-permissions is set, no allowlist applies, so
	// auto-injecting --add-dir is wasted noise.
	in := []string{"claude", "--dangerously-skip-permissions"}
	got := injectClaudeContext(in, ctxFile)

	if containsArg(got, "--add-dir") {
		t.Errorf("expected NO --add-dir with skip-permissions, got %v", got)
	}
	if !containsArg(got, "-p") {
		t.Errorf("missing -p: %v", got)
	}
}

func TestInjectClaudeContext_PrependsToExistingPrompt(t *testing.T) {
	in := []string{"claude", "-p", "user prompt here"}
	got := injectClaudeContext(in, ctxFile)

	v, ok := argValue(got, "-p")
	if !ok {
		t.Fatalf("missing -p: %v", got)
	}
	if !strings.Contains(v, "user prompt here") {
		t.Errorf("user prompt lost: %q", v)
	}
	if !strings.Contains(v, "context file at "+ctxFile) {
		t.Errorf("context instruction missing: %q", v)
	}
}

func containsArg(args []string, target string) bool {
	for _, a := range args {
		if a == target {
			return true
		}
	}
	return false
}

func argValue(args []string, flag string) (string, bool) {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1], true
		}
	}
	return "", false
}
