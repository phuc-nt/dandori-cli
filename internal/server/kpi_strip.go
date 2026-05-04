// Package server — kpi_strip.go: GET /api/kpi/strip handler.
//
// Returns 14-day daily series + computed totals for the current week
// and prior week so the frontend can render the KPI strip (4 tiles +
// 14d sparkline + WoW delta) without doing date math in JS.
package server

import (
	"encoding/json"
	"net/http"

	"github.com/phuc-nt/dandori-cli/internal/db"
)

// KPIStripResponse is the payload returned by /api/kpi/strip.
type KPIStripResponse struct {
	Days    []db.KPIDay `json:"days"`    // length = 14, oldest → newest
	Current KPITotals   `json:"current"` // last 7 days (days[7..13])
	Prior   KPITotals   `json:"prior"`   // 7 days before that (days[0..6])
}

type KPITotals struct {
	Runs   int     `json:"runs"`
	Cost   float64 `json:"cost"`
	Tokens int     `json:"tokens"`
}

func handleKPIStrip(store *db.LocalDB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		days, err := store.GetKPIDailyStats(14)
		if err != nil {
			http.Error(w, `{"error":"kpi query failed"}`, http.StatusInternalServerError)
			return
		}

		var resp KPIStripResponse
		resp.Days = days
		// Split: prior = first 7, current = last 7. If we get fewer rows than
		// expected (shouldn't happen given padding) we just return what we have.
		split := len(days) - 7
		if split < 0 {
			split = 0
		}
		for _, d := range days[:split] {
			resp.Prior.Runs += d.Runs
			resp.Prior.Cost += d.Cost
			resp.Prior.Tokens += d.Tokens
		}
		for _, d := range days[split:] {
			resp.Current.Runs += d.Runs
			resp.Current.Cost += d.Cost
			resp.Current.Tokens += d.Tokens
		}

		_ = json.NewEncoder(w).Encode(resp)
	}
}

// RegisterKPIRoutes mounts /api/kpi/strip on mux.
func RegisterKPIRoutes(mux *http.ServeMux, store *db.LocalDB) {
	mux.HandleFunc("/api/kpi/strip", handleKPIStrip(store))
}
