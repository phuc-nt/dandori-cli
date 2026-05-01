// Package server — g9_insights.go: /api/g9/insights handler.
// Runs all 5 insight heuristics via the insights package and returns a JSON
// array of cards. Scopes to a project when ?role=project&id=KEY is provided.
package server

import (
	"encoding/json"
	"net/http"

	"github.com/phuc-nt/dandori-cli/internal/db"
	"github.com/phuc-nt/dandori-cli/internal/insights"
)

// handleG9Insights runs the heuristic engine and returns the card array.
// Query params:
//   - ?role=project&id=CLITEST  — scope to one project key
//   - (no params)               — org-wide
func handleG9Insights(store *db.LocalDB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		role := r.URL.Query().Get("role")
		id := r.URL.Query().Get("id")

		projectKey := ""
		if role == "project" {
			projectKey = id
		}

		cards, err := insights.Compute(store, projectKey)
		if err != nil {
			http.Error(w, `{"error":"insights query failed"}`, http.StatusInternalServerError)
			return
		}

		// Guarantee [] not null in JSON output.
		if cards == nil {
			cards = []insights.Card{}
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(cards); err != nil {
			// Header already written; nothing useful we can do.
			_ = err
		}
	}
}
