// Package server — trust_endpoints.go: GET /api/metrics/trust-index handler (v0.12).
//
//	GET /api/metrics/trust-index?days=28
//
// `days` — lookback window (default 28, max 365). Returns the composite Trust
// Index with its 3 components and band classification. When the window has
// no data (no task_attribution rows or no runs), HasData=false and band=no-data
// so the consumer renders a neutral state instead of a misleading 0.
package server

import (
	"net/http"

	"github.com/phuc-nt/dandori-cli/internal/db"
)

// RegisterTrustRoutes mounts /api/metrics/trust-index on mux.
func RegisterTrustRoutes(mux *http.ServeMux, store *db.LocalDB) {
	mux.HandleFunc("/api/metrics/trust-index", handleTrustIndex(store))
}

func handleTrustIndex(store *db.LocalDB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		days := parseDaysParam(r.URL.Query().Get("days"), 28, 365)
		res, err := store.GetTrustIndex(days)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "trust-index query failed: "+err.Error())
			return
		}
		writeJSON(w, res)
	}
}
