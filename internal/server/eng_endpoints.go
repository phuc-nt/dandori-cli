// Package server — eng_endpoints.go: Engineering View backend endpoints (Phase 03).
//
// Mounts 8 endpoints powering the Engineering persona dashboard:
//
//	GET /api/agents/compare?a=X&b=Y    — two-agent metric pack comparison.
//	GET /api/autonomy?engineer=X&days=N — daily autonomy ratio time series.
//	GET /api/approvals/funnel           — count by approval event type.
//	GET /api/cache-efficiency?days=N    — daily cache hit-rate series.
//	GET /api/cost-per-task?days=N       — daily cost / distinct PBI series.
//	GET /api/model-mix?days=N           — runs+cost grouped by model.
//	GET /api/session-end-reasons?days=N — daily (day × reason) series.
//	GET /api/duration-histogram?days=N  — IQR-binned duration histogram.
package server

import (
	"net/http"
	"strconv"

	"github.com/phuc-nt/dandori-cli/internal/db"
)

// RegisterEngRoutes mounts the 8 Engineering endpoints on mux.
func RegisterEngRoutes(mux *http.ServeMux, store *db.LocalDB) {
	mux.HandleFunc("/api/agents/compare", handleAgentsCompare(store))
	mux.HandleFunc("/api/autonomy", handleAutonomy(store))
	mux.HandleFunc("/api/approvals/funnel", handleApprovalFunnel(store))
	mux.HandleFunc("/api/cache-efficiency", handleCacheEfficiency(store))
	mux.HandleFunc("/api/cost-per-task", handleCostPerTask(store))
	mux.HandleFunc("/api/model-mix", handleModelMix(store))
	mux.HandleFunc("/api/session-end-reasons", handleSessionEndReasons(store))
	mux.HandleFunc("/api/duration-histogram", handleDurationHistogram(store))
}

// parseDays reads ?days=N (default fallback applied by query helper).
func parseDays(r *http.Request, def int) int {
	s := r.URL.Query().Get("days")
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

// AgentsCompareResponse is the payload for /api/agents/compare.
type AgentsCompareResponse struct {
	A db.AgentMetricPack `json:"a"`
	B db.AgentMetricPack `json:"b"`
}

func handleAgentsCompare(store *db.LocalDB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a := r.URL.Query().Get("a")
		b := r.URL.Query().Get("b")
		if a == "" || b == "" {
			writeError(w, http.StatusBadRequest, "a and b query params required")
			return
		}
		packA, err := store.AgentMetrics(a)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "agent A query failed")
			return
		}
		packB, err := store.AgentMetrics(b)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "agent B query failed")
			return
		}
		writeJSON(w, AgentsCompareResponse{A: packA, B: packB})
	}
}

func handleAutonomy(store *db.LocalDB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		eng := r.URL.Query().Get("engineer")
		days := parseDays(r, 14)
		series, err := store.AutonomyTimeline(eng, days)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "autonomy query failed")
			return
		}
		if series == nil {
			series = []db.AutonomyDay{}
		}
		writeJSON(w, series)
	}
}

func handleApprovalFunnel(store *db.LocalDB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		steps, err := store.ApprovalFunnel()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "funnel query failed")
			return
		}
		if steps == nil {
			steps = []db.FunnelStep{}
		}
		writeJSON(w, steps)
	}
}

func handleCacheEfficiency(store *db.LocalDB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		days := parseDays(r, 14)
		points, err := store.CacheEfficiencyTimeline(days)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "cache-efficiency query failed")
			return
		}
		if points == nil {
			points = []db.CacheEffPoint{}
		}
		writeJSON(w, points)
	}
}

func handleCostPerTask(store *db.LocalDB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		days := parseDays(r, 28)
		points, err := store.CostPerTaskTimeline(days)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "cost-per-task query failed")
			return
		}
		if points == nil {
			points = []db.CostPerTaskPoint{}
		}
		writeJSON(w, points)
	}
}

func handleModelMix(store *db.LocalDB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		days := parseDays(r, 28)
		mix, err := store.ModelMix(days)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "model-mix query failed")
			return
		}
		if mix == nil {
			mix = []db.ModelMixRow{}
		}
		writeJSON(w, mix)
	}
}

func handleSessionEndReasons(store *db.LocalDB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		days := parseDays(r, 28)
		points, err := store.SessionEndReasons(days)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "session-end query failed")
			return
		}
		if points == nil {
			points = []db.SessionEndPoint{}
		}
		writeJSON(w, points)
	}
}

func handleDurationHistogram(store *db.LocalDB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		days := parseDays(r, 28)
		buckets, err := store.DurationHistogram(days)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "duration-histogram query failed")
			return
		}
		if buckets == nil {
			buckets = []db.DurationBucket{}
		}
		writeJSON(w, buckets)
	}
}
