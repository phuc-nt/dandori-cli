// Package server — pr_cycle_endpoints.go: GET /api/metrics/pr-cycle-time
// handler (v0.13). Diagnostic metric: median + p75 first-approval latency
// over PRs merged in the rolling window. Empty state surfaces when no
// approving reviews exist (solo engineer / auto-merge teams).
//
//	GET /api/metrics/pr-cycle-time?days=28
package server

import (
	"net/http"

	"github.com/phuc-nt/dandori-cli/internal/db"
)

// RegisterPRCycleRoutes mounts /api/metrics/pr-cycle-time on mux.
func RegisterPRCycleRoutes(mux *http.ServeMux, store *db.LocalDB) {
	mux.HandleFunc("/api/metrics/pr-cycle-time", handlePRCycleTime(store))
}

func handlePRCycleTime(store *db.LocalDB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		days := parseDaysParam(r.URL.Query().Get("days"), 28, 365)
		repo, ok := validateRepoParam(r.URL.Query().Get("repo"))
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid repo: expected owner/name")
			return
		}
		res, err := store.GetPRReviewCycleTimeByRepo(days, repo)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "pr-cycle-time query failed: "+err.Error())
			return
		}
		writeJSON(w, res)
	}
}
