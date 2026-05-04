// Package server — qa_endpoints.go: QA View backend endpoints (Phase 04).
//
// 6 read endpoints powering the QA persona dashboard:
//
//	GET /api/quality/timeline?project=X&weeks=N
//	GET /api/quality/scatter?limit=N
//	GET /api/quality/commit-msg
//	GET /api/bug-hotspots?weeks=N
//	GET /api/rework/causes
//	GET /api/intervention/heatmap?days=N
//
// Each handler returns [] (never null) so the frontend can rely on Array methods.
package server

import (
	"net/http"
	"strconv"

	"github.com/phuc-nt/dandori-cli/internal/db"
)

// RegisterQARoutes mounts the 6 QA endpoints on mux.
func RegisterQARoutes(mux *http.ServeMux, store *db.LocalDB) {
	mux.HandleFunc("/api/quality/timeline", handleQualityTimeline(store))
	mux.HandleFunc("/api/quality/scatter", handleQualityScatter(store))
	mux.HandleFunc("/api/quality/commit-msg", handleCommitMsgDist(store))
	mux.HandleFunc("/api/bug-hotspots", handleBugHotspots(store))
	mux.HandleFunc("/api/rework/causes", handleReworkCauses(store))
	mux.HandleFunc("/api/intervention/heatmap", handleInterventionHeatmap(store))
}

func handleQualityTimeline(store *db.LocalDB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		project := r.URL.Query().Get("project")
		weeks := 12
		if v := r.URL.Query().Get("weeks"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 52 {
				weeks = n
			}
		}
		points, err := store.QualityTimeline(project, weeks)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "quality timeline query failed")
			return
		}
		if points == nil {
			points = []db.QualityTimelinePoint{}
		}
		writeJSON(w, points)
	}
}

func handleQualityScatter(store *db.LocalDB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := 2000
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 10000 {
				limit = n
			}
		}
		pts, err := store.CostQualityScatter(limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "scatter query failed")
			return
		}
		if pts == nil {
			pts = []db.CostQualityPoint{}
		}
		writeJSON(w, pts)
	}
}

func handleCommitMsgDist(store *db.LocalDB) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		buckets, err := store.CommitMsgDistribution()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "commit msg query failed")
			return
		}
		if buckets == nil {
			buckets = []db.CommitMsgBucket{}
		}
		writeJSON(w, buckets)
	}
}

func handleBugHotspots(store *db.LocalDB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		weeks := 8
		if v := r.URL.Query().Get("weeks"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 52 {
				weeks = n
			}
		}
		cells, err := store.BugHotspots(weeks)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "bug hotspots query failed")
			return
		}
		if cells == nil {
			cells = []db.BugHotspotCell{}
		}
		writeJSON(w, cells)
	}
}

func handleReworkCauses(store *db.LocalDB) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		causes, err := store.ReworkCauses()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "rework causes query failed")
			return
		}
		if causes == nil {
			causes = []db.ReworkCause{}
		}
		writeJSON(w, causes)
	}
}

func handleInterventionHeatmap(store *db.LocalDB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		days := parseDays(r, 28)
		cells, err := store.InterventionHeatmap(days)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "intervention heatmap query failed")
			return
		}
		if cells == nil {
			cells = []db.InterventionCell{}
		}
		writeJSON(w, cells)
	}
}
