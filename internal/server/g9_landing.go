// Package server — g9_landing.go: CWD-aware landing detector for G9 dashboard.
// DetectLanding shell-outs to `git remote get-url origin` with a 500ms timeout
// and matches the repo basename against a list of known project keys.
// All errors fall back gracefully to {Role:"org"} — never propagate.
package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Landing describes the inferred default view for the current working directory.
type Landing struct {
	Role string `json:"role"` // "project" | "org"
	ID   string `json:"id"`   // project key when Role="project", empty otherwise
}

// DetectLanding infers the dashboard landing context from the git remote URL
// of the given directory, matched case-insensitively against knownProjects.
//
// Algorithm:
//  1. Run `git -C <cwd> remote get-url origin` with a 500ms timeout.
//  2. Extract the URL's last path segment and strip the ".git" suffix.
//  3. Compare (case-insensitive) against each entry in knownProjects.
//  4. First match → {Role:"project", ID:<matched key>}.
//  5. No match, no git repo, command error → {Role:"org"}, nil error.
//
// Errors are NEVER returned; the function always falls back to org landing.
func DetectLanding(cwd string, knownProjects []string) (Landing, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "-C", cwd, "remote", "get-url", "origin")
	out, err := cmd.Output()
	if err != nil {
		// Not a git repo, no origin, or timeout — fall back silently.
		return Landing{Role: "org"}, nil
	}

	remoteURL := strings.TrimSpace(string(out))
	// Extract basename of path component, stripping ".git".
	base := filepath.Base(remoteURL)
	base = strings.TrimSuffix(base, ".git")
	token := strings.ToUpper(base)

	for _, proj := range knownProjects {
		if strings.ToUpper(proj) == token {
			return Landing{Role: "project", ID: strings.ToUpper(proj)}, nil
		}
	}

	return Landing{Role: "org"}, nil
}

// handleG9Landing returns an HTTP handler that serves the cached Landing struct
// as JSON. The landing value is captured at server startup via DetectLanding.
func handleG9Landing(landing Landing) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(landing) //nolint:errcheck
	}
}

// RegisterG9LandingRoute mounts /api/g9/landing on mux, serving the pre-computed
// landing struct. Call at server startup after DetectLanding has run.
func RegisterG9LandingRoute(mux *http.ServeMux, landing Landing) {
	mux.HandleFunc("/api/g9/landing", handleG9Landing(landing))
}
