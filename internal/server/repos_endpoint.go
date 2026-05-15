// Package server — repos_endpoint.go: GET /api/metrics/repos handler
// (v0.14). Returns the list of repos with merged PRs in the rolling
// window, ordered by merged count desc. Powers the dashboard's per-repo
// dropdown — UI hides it when len < 2.
//
//	GET /api/metrics/repos?days=28
package server

import (
	"net/http"
	"regexp"

	"github.com/phuc-nt/dandori-cli/internal/db"
)

// repoNameRe validates the `owner/name` format used by GitHub. Refuses
// path traversal, wildcards, query strings, and other patterns that
// could escape the bound SQL parameter.
var repoNameRe = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)

// validateRepoParam returns (cleaned, ok). Empty input is OK (org-wide
// query); malformed input is not.
func validateRepoParam(raw string) (string, bool) {
	if raw == "" {
		return "", true
	}
	if !repoNameRe.MatchString(raw) {
		return "", false
	}
	return raw, true
}

// RegisterReposRoute mounts /api/metrics/repos on mux.
func RegisterReposRoute(mux *http.ServeMux, store *db.LocalDB) {
	mux.HandleFunc("/api/metrics/repos", handleRepos(store))
}

func handleRepos(store *db.LocalDB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		days := parseDaysParam(r.URL.Query().Get("days"), 28, 365)
		out, err := store.ListReposWithMergedPRs(days)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "repos query failed: "+err.Error())
			return
		}
		// Always return a JSON array, never `null`, so the dashboard
		// can do `data.length < 2` without a guard.
		if out == nil {
			out = []db.RepoSummary{}
		}
		writeJSON(w, out)
	}
}
