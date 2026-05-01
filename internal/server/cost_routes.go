// Package server — cost_routes.go: /api/cost/task and /api/cost/day handlers
// with optional ?project= and ?period= / ?from= / ?to= filter support.
package server

import (
	"encoding/json"
	"net/http"

	"github.com/phuc-nt/dandori-cli/internal/db"
)

// RegisterCostRoutes mounts the two cost API endpoints onto mux.
// Both endpoints support optional query parameters:
//   - ?project=KEY  — filter to runs whose jira_issue_key starts with KEY-
//   - ?period=7d|28d|90d|custom  — rolling or custom date window
//   - ?from=YYYY-MM-DD&to=YYYY-MM-DD  — explicit range (requires period=custom)
//
// Returns HTTP 400 on invalid date parameters.
func RegisterCostRoutes(mux *http.ServeMux, store *db.LocalDB) {
	mux.HandleFunc("/api/cost/task", costHandler(store, "task"))
	mux.HandleFunc("/api/cost/day", costHandler(store, "day"))
}

// costHandler returns an http.HandlerFunc that serves cost data filtered by
// optional project and period parameters. kind must be "task" or "day".
func costHandler(store *db.LocalDB, kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		filter, err := parseCostFilter(r)
		if err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
			return
		}

		var groups []db.LocalCostGroup
		switch kind {
		case "task":
			groups, err = store.GetCostByTaskFiltered(filter)
		default: // "day"
			groups, err = store.GetCostByDayFiltered(filter)
		}
		if err != nil {
			http.Error(w, `{"error":"query failed"}`, http.StatusInternalServerError)
			return
		}

		// Return [] rather than null for empty result.
		if groups == nil {
			groups = []db.LocalCostGroup{}
		}
		json.NewEncoder(w).Encode(groups) //nolint:errcheck
	}
}

// parseCostFilter reads ?project=, ?period=, ?from=, ?to= from the request.
// Period parsing is delegated to ParsePeriodWindow (role="org" so default = 90d,
// but an empty ?period= param means no time restriction — zero Window).
// Returns an error only when ?period=custom has an invalid date.
func parseCostFilter(r *http.Request) (db.CostFilter, error) {
	q := r.URL.Query()
	project := q.Get("project")

	// Only invoke period parsing when the caller explicitly asked for it.
	hasPeriod := q.Get("period") != "" || q.Get("from") != "" || q.Get("to") != ""
	var filter db.CostFilter
	filter.ProjectKey = project

	if hasPeriod {
		cur, _, err := ParsePeriodWindow(r, "org")
		if err != nil {
			return db.CostFilter{}, err
		}
		filter.From = cur.Start
		filter.To = cur.End
	}

	return filter, nil
}
