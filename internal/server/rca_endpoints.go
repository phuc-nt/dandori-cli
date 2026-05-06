// Package server — rca_endpoints.go: structured RCA breakdown endpoint (v0.11 Phase 02).
//
// GET /api/rca/breakdown?since=28d
//
// Returns JSON []RcaRow: cause, count, pct, top_agent, top_task_type, wow_delta.
// `since` accepts integer-day values with optional "d" suffix (e.g. "28d" or "28").
// Default: 28 days. Parsed by parseSinceDays() defined in affinity_endpoints.go.
// Empty result returns [] not null.
package server

import (
	"net/http"
	"time"

	"github.com/phuc-nt/dandori-cli/internal/db"
)

// RegisterRcaRoutes mounts the RCA endpoint on mux.
func RegisterRcaRoutes(mux *http.ServeMux, store *db.LocalDB) {
	mux.HandleFunc("/api/rca/breakdown", handleRcaBreakdown(store))
}

func handleRcaBreakdown(store *db.LocalDB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		days := parseSinceDays(r.URL.Query().Get("since"), 28)
		since := time.Now().UTC().AddDate(0, 0, -days)

		rows, err := store.GetRcaBreakdown(since)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "rca breakdown query failed")
			return
		}
		if rows == nil {
			rows = []db.RcaRow{}
		}
		writeJSON(w, rows)
	}
}
