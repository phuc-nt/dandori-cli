package attribution

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// testRepo is a throwaway git repo built in t.TempDir(). Only used by
// retention tests — keeps them deterministic by controlling exactly which
// commits exist and which files they touch. NOT compiled into production
// builds (file lives next to *_test.go siblings via the _test naming below).

type testRepo struct {
	t    *testing.T
	path string
}

// newTestRepo initialises a fresh repo with author/email config and an empty
// initial commit so subsequent commits have a parent. The repo is rooted in
// a t.TempDir() that the test framework cleans up automatically.
func newTestRepo(t *testing.T) *testRepo {
	t.Helper()
	dir := t.TempDir()

	r := &testRepo{t: t, path: dir}
	r.run("git", "init", "-q", "-b", "main")
	r.run("git", "config", "user.email", "test@example.com")
	r.run("git", "config", "user.name", "Tester")
	r.run("git", "config", "commit.gpgsign", "false")
	// Empty seed commit so HEAD always points to something.
	r.run("git", "commit", "--allow-empty", "-q", "-m", "init")
	return r
}

// commit writes path with content (creating parent dirs) and commits. Returns
// the new HEAD sha. Each call is one commit.
func (r *testRepo) commit(relPath, content string) string {
	r.t.Helper()
	full := filepath.Join(r.path, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		r.t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		r.t.Fatalf("write %s: %v", relPath, err)
	}
	r.run("git", "add", relPath)
	r.run("git", "commit", "-q", "-m", "edit "+relPath)
	return r.head()
}

// remove deletes a tracked file and commits the deletion. Returns new HEAD.
func (r *testRepo) remove(relPath string) string {
	r.t.Helper()
	r.run("git", "rm", "-q", relPath)
	r.run("git", "commit", "-q", "-m", "remove "+relPath)
	return r.head()
}

// head returns the current HEAD sha (full).
func (r *testRepo) head() string {
	r.t.Helper()
	out := r.runOut("git", "rev-parse", "HEAD")
	return strings.TrimSpace(out)
}

func (r *testRepo) run(args ...string) {
	r.t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = r.path
	if out, err := cmd.CombinedOutput(); err != nil {
		r.t.Fatalf("%s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func (r *testRepo) runOut(args ...string) string {
	r.t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = r.path
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("%s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}
