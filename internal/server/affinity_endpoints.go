// Package server — affinity_endpoints.go: agent × task-type affinity endpoint (v0.11 Phase 01).
//
// GET /api/analytics/agent-task-affinity?since=28d
//
// Returns JSON []AffinityCell: agent, task_type, runs, success_rate.
// `since` accepts integer-day values with optional "d" suffix (e.g. "28d" or "28").
// Default: 28 days. Max: 365 days (clamp silently).
// Empty result set returns [] not null.
package server

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/phuc-nt/dandori-cli/internal/db"
)

// RegisterAffinityRoutes mounts the affinity endpoint on mux.
func RegisterAffinityRoutes(mux *http.ServeMux, store *db.LocalDB) {
	mux.HandleFunc("/api/analytics/agent-task-affinity", handleAgentTaskAffinity(store))
}

func handleAgentTaskAffinity(store *db.LocalDB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		days := parseSinceDays(r.URL.Query().Get("since"), 28)
		since := time.Now().UTC().AddDate(0, 0, -days)

		cells, err := store.GetAgentTaskAffinity(since)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "agent task affinity query failed")
			return
		}
		if cells == nil {
			cells = []db.AffinityCell{}
		}
		writeJSON(w, cells)
	}
}

// parseSinceDays parses a "since" query parameter value like "28d", "90d", or "28"
// into an integer number of days. Returns def on empty or invalid input.
// Clamps to [1, 365].
func parseSinceDays(s string, def int) int {
	if s == "" {
		return def
	}
	s = strings.TrimSuffix(strings.TrimSpace(s), "d")
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return def
	}
	if n > 365 {
		return 365
	}
	return n
}
