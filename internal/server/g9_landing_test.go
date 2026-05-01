package server_test

import (
	"os/exec"
	"testing"

	"github.com/phuc-nt/dandori-cli/internal/server"
)

// TestDetectLanding_GitRemoteMatchesProject creates a real git repo in a temp
// dir, sets origin to a URL ending in "clitest.git", and verifies the detector
// returns role=project, id=CLITEST.
func TestDetectLanding_GitRemoteMatchesProject(t *testing.T) {
	dir := t.TempDir()

	// Initialise git repo and add remote.
	if err := exec.Command("git", "-C", dir, "init").Run(); err != nil {
		t.Skipf("git not available: %v", err)
	}
	if err := exec.Command("git", "-C", dir, "remote", "add", "origin", "git@github.com:acme/clitest.git").Run(); err != nil {
		t.Fatalf("git remote add: %v", err)
	}

	landing, err := server.DetectLanding(dir, []string{"CLITEST"})
	if err != nil {
		t.Fatalf("DetectLanding returned error: %v", err)
	}
	if landing.Role != "project" {
		t.Errorf("Role=%q, want %q", landing.Role, "project")
	}
	if landing.ID != "CLITEST" {
		t.Errorf("ID=%q, want %q", landing.ID, "CLITEST")
	}
}

// TestDetectLanding_NoMatch_FallsBackToOrg verifies that when the git remote
// basename does not match any known project, the detector returns role=org.
func TestDetectLanding_NoMatch_FallsBackToOrg(t *testing.T) {
	dir := t.TempDir()

	if err := exec.Command("git", "-C", dir, "init").Run(); err != nil {
		t.Skipf("git not available: %v", err)
	}
	if err := exec.Command("git", "-C", dir, "remote", "add", "origin", "git@github.com:acme/somerepo.git").Run(); err != nil {
		t.Fatalf("git remote add: %v", err)
	}

	landing, err := server.DetectLanding(dir, []string{"CLITEST", "MYPROJ"})
	if err != nil {
		t.Fatalf("DetectLanding returned unexpected error: %v", err)
	}
	if landing.Role != "org" {
		t.Errorf("Role=%q, want %q", landing.Role, "org")
	}
}

// TestDetectLanding_NoGitRepo_FallsBackToOrg verifies that when the directory
// is not a git repo the detector returns role=org and NO error (graceful fallback).
func TestDetectLanding_NoGitRepo_FallsBackToOrg(t *testing.T) {
	dir := t.TempDir() // plain directory, no git init

	landing, err := server.DetectLanding(dir, []string{"CLITEST"})
	if err != nil {
		t.Errorf("expected no error for non-git dir, got: %v", err)
	}
	if landing.Role != "org" {
		t.Errorf("Role=%q, want %q", landing.Role, "org")
	}
}
