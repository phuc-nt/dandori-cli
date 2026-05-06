// Package server — trend_endpoints.go: week-over-week trend endpoints (v0.11 Phase 03).
//
// Three URL paths all delegate to one handler with a hardcoded metric param:
//
//	GET /api/trends/success-rate?days=90&window=7
//	GET /api/trends/cost?days=90&window=7
//	GET /api/trends/rework-rate?days=90&window=7
//
// `days`   — lookback period in days (default 90, max 365).
// `window` — bucket width in days   (default 7,  only 7 is meaningful currently).
//
// All three return the same JSON shape: []TrendPoint.
// Empty result (no runs in window) returns [] not null.
package server

import (
	"net/http"
	"strconv"
	"time"

	"github.com/phuc-nt/dandori-cli/internal/db"
)

// RegisterTrendRoutes mounts the 3 trend endpoints on mux.
func RegisterTrendRoutes(mux *http.ServeMux, store *db.LocalDB) {
	mux.HandleFunc("/api/trends/success-rate", handleTrend(store, "success-rate"))
	mux.HandleFunc("/api/trends/cost", handleTrend(store, "cost"))
	mux.HandleFunc("/api/trends/rework-rate", handleTrend(store, "rework-rate"))
}

func handleTrend(store *db.LocalDB, metric string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		days := parseDaysParam(r.URL.Query().Get("days"), 90, 365)
		window := parseDaysParam(r.URL.Query().Get("window"), 7, 90)
		since := time.Now().UTC().AddDate(0, 0, -days)

		pts, err := store.GetTrend(metric, since, window)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "trend query failed: "+err.Error())
			return
		}
		if pts == nil {
			pts = []db.TrendPoint{}
		}
		writeJSON(w, pts)
	}
}

// parseDaysParam parses an integer query param with a default and max clamp.
func parseDaysParam(s string, def, max int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}
