// Package server — admin_endpoints.go: Admin View backend endpoints (Phase 03).
//
// Mounts 2 endpoints powering the Admin persona dashboard:
//
//	GET /api/workstations?days=N — workstation × engineer matrix with anomaly flag.
//	GET /api/repos?days=N        — repo cost leaderboard with 14-day sparkline.
package server

import (
	"net/http"

	"github.com/phuc-nt/dandori-cli/internal/db"
)

// RegisterAdminRoutes mounts the 2 Admin endpoints on mux.
func RegisterAdminRoutes(mux *http.ServeMux, store *db.LocalDB) {
	mux.HandleFunc("/api/workstations", handleWorkstationMatrix(store))
	mux.HandleFunc("/api/repos", handleRepoLeaderboard(store))
}

func handleWorkstationMatrix(store *db.LocalDB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		days := parseDays(r, 28)
		rows, err := store.WorkstationMatrix(days)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "workstation matrix query failed")
			return
		}
		if rows == nil {
			rows = []db.WorkstationRow{}
		}
		writeJSON(w, rows)
	}
}

func handleRepoLeaderboard(store *db.LocalDB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		days := parseDays(r, 28)
		rows, err := store.RepoLeaderboard(days)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "repo leaderboard query failed")
			return
		}
		if rows == nil {
			rows = []db.RepoLeaderboardRow{}
		}
		writeJSON(w, rows)
	}
}
