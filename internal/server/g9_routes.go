// Package server/g9_routes.go — experimental G9 dashboard API handlers.
// Registered only when --experimental flag is passed to `dandori dashboard`.
// All handlers are read-only; no data is mutated by this file.
package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/phuc-nt/dandori-cli/internal/db"
)

// RegisterG9Routes mounts the four G9 API endpoints onto mux.
// It must be called after legacy routes are registered so legacy handlers
// take precedence on any shared paths (there are none today, but defensive).
func RegisterG9Routes(mux *http.ServeMux, store *db.LocalDB) {
	mux.HandleFunc("/api/g9/level", handleG9Level(store))
	mux.HandleFunc("/api/g9/dora", handleG9DORA(store))
	mux.HandleFunc("/api/g9/attribution", handleG9Attribution(store))
	mux.HandleFunc("/api/g9/intent", handleG9Intent(store))
	mux.HandleFunc("/api/g9/iterations", handleG9Iterations(store))
	mux.HandleFunc("/api/g9/insights", handleG9Insights(store))
}

// normalizeDoraPayload converts a raw metric export payload (which may be in
// faros, oobeya, or raw format) into the canonical 4-key shape expected by the
// dashboard JS: deploy_frequency, lead_time, change_failure_rate, mttr.
// Unknown formats are returned as-is so the JS can still attempt to render.
func normalizeDoraPayload(raw map[string]any) map[string]any {
	// If already in canonical format, pass through.
	if _, ok := raw["deploy_frequency"]; ok {
		return raw
	}

	// faros format: has "metrics" sub-key.
	metricsRaw, hasFaros := raw["metrics"]
	if hasFaros {
		if m, ok := metricsRaw.(map[string]any); ok {
			normalized := map[string]any{}

			// deployment_frequency → deploy_frequency
			if df, ok := m["deployment_frequency"].(map[string]any); ok {
				rating := doraRating("deploy_frequency", df["value"])
				normalized["deploy_frequency"] = map[string]any{
					"value":  df["value"],
					"unit":   df["unit"],
					"rating": rating,
				}
			}
			// lead_time_for_changes → lead_time
			if lt, ok := m["lead_time_for_changes"].(map[string]any); ok {
				p50secs, _ := lt["p50_seconds"].(float64)
				p50days := p50secs / 86400
				rating := doraRating("lead_time", p50days)
				normalized["lead_time"] = map[string]any{
					"value":  p50days,
					"unit":   "days",
					"rating": rating,
				}
			}
			// change_failure_rate (same key)
			if cfr, ok := m["change_failure_rate"].(map[string]any); ok {
				rating := doraRating("change_failure_rate", cfr["value"])
				normalized["change_failure_rate"] = map[string]any{
					"value":  cfr["value"],
					"unit":   "ratio",
					"rating": rating,
				}
			}
			// time_to_restore_service → mttr
			if mttr, ok := m["time_to_restore_service"].(map[string]any); ok {
				p50secs, _ := mttr["p50_seconds"].(float64)
				p50hours := p50secs / 3600
				rating := doraRating("mttr", p50hours)
				normalized["mttr"] = map[string]any{
					"value":  p50hours,
					"unit":   "hours",
					"rating": rating,
				}
			}
			return normalized
		}
	}

	// Fallback: return raw unchanged.
	return raw
}

// doraRating maps a DORA metric value to an Elite/High/Medium/Low label
// per DORA 2023 benchmark thresholds.
func doraRating(metric string, val any) string {
	v, ok := val.(float64)
	if !ok {
		return ""
	}
	switch metric {
	case "deploy_frequency": // per day
		if v >= 1 {
			return "elite"
		}
		if v >= 1.0/7 {
			return "high"
		}
		if v >= 1.0/30 {
			return "medium"
		}
		return "low"
	case "lead_time": // days
		if v < 1 {
			return "elite"
		}
		if v < 7 {
			return "high"
		}
		if v < 30 {
			return "medium"
		}
		return "low"
	case "change_failure_rate": // ratio
		if v <= 0.05 {
			return "elite"
		}
		if v <= 0.10 {
			return "high"
		}
		if v <= 0.15 {
			return "medium"
		}
		return "low"
	case "mttr": // hours
		if v < 1 {
			return "elite"
		}
		if v < 24 {
			return "high"
		}
		if v < 168 {
			return "medium"
		}
		return "low"
	}
	return ""
}

// handleG9Level returns role/level context with run_count for the resolved period
// window. Supports ?role=, ?id=, ?period=, ?from=, ?to=, ?compare= params.
// run_count reflects only the runs within the period window and optional project scope.
func handleG9Level(store *db.LocalDB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		role := q.Get("role")
		if role == "" {
			role = "org"
		}
		id := q.Get("id")

		w.Header().Set("Content-Type", "application/json")

		cur, _, err := ParsePeriodWindow(r, role)
		if err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
			return
		}

		// Count runs within window, scoped to project if role=project.
		runCount, err := countRunsInWindow(store, cur, role, id)
		if err != nil {
			http.Error(w, `{"error":"level query failed"}`, http.StatusInternalServerError)
			return
		}

		resp := map[string]any{
			"role":      role,
			"run_count": runCount,
		}
		if id != "" {
			resp["id"] = id
		}
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}
}

// doraStaleHours is the threshold after which a metric snapshot is considered
// stale and the UI shows a warning banner. Set to 24h per spec.
const doraStaleHours = 24

// handleG9DORA serves the DORA scorecard from the latest metric_snapshot.
// If no snapshot exists or the latest is older than doraStaleHours, returns
// stale:true with a hint message instructing how to refresh.
func handleG9DORA(store *db.LocalDB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		snap, err := store.LatestSnapshot("", "json")
		if err != nil {
			http.Error(w, `{"error":"db error"}`, http.StatusInternalServerError)
			return
		}

		if snap == nil {
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"stale":   true,
				"message": "No metric snapshot found. Run: dandori metric export --include-attribution",
			})
			return
		}

		ageHours := time.Since(snap.CreatedAt).Hours()
		stale := ageHours > doraStaleHours

		// Parse the snapshot payload. Normalize to canonical DORA keys that the
		// dashboard JS understands: deploy_frequency, lead_time, change_failure_rate, mttr.
		// The faros export format uses deployment_frequency, lead_time_for_changes, etc.
		var raw map[string]any
		if err := json.Unmarshal([]byte(snap.Payload), &raw); err != nil {
			// Unparseable payload — surface as stale so UI doesn't break.
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"stale":     true,
				"message":   "Snapshot payload could not be parsed. Run: dandori metric export",
				"age_hours": ageHours,
			})
			return
		}

		metrics := normalizeDoraPayload(raw)

		resp := map[string]any{
			"stale":        stale,
			"age_hours":    ageHours,
			"metrics":      metrics,
			"snapshot_id":  snap.ID,
			"window_start": snap.WindowStart.UTC().Format(time.RFC3339),
			"window_end":   snap.WindowEnd.UTC().Format(time.RFC3339),
		}
		if stale {
			resp["message"] = "Data is stale (>24h). Run: dandori metric export --include-attribution"
		}
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}
}

// handleG9Attribution computes the attribution composite tile.
// Supports ?engineer= for engineer scope, ?period= for window selection,
// and ?compare=true to return {current:{...}, prior:{...}} shape.
// Without ?compare=true, response is the flat attribution object (backward-compatible).
func handleG9Attribution(store *db.LocalDB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		engineer := r.URL.Query().Get("engineer")
		compare := r.URL.Query().Get("compare") == "true"

		role := "org"
		if engineer != "" {
			role = "engineer"
		}

		cur, prior, perr := ParsePeriodWindow(r, role)
		if perr != nil {
			http.Error(w, `{"error":"`+perr.Error()+`"}`, http.StatusBadRequest)
			return
		}

		curPayload, err := computeAttribution(store, engineer, cur)
		if err != nil {
			http.Error(w, `{"error":"attribution query failed"}`, http.StatusInternalServerError)
			return
		}

		if !compare {
			// Backward-compatible flat response.
			json.NewEncoder(w).Encode(curPayload) //nolint:errcheck
			return
		}

		// Compare shape: {current:{...}, prior:{...}}.
		priorPayload, err := computeAttribution(store, engineer, prior)
		if err != nil {
			// Non-fatal: return compare with empty prior.
			priorPayload = map[string]any{}
		}

		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"current": curPayload,
			"prior":   priorPayload,
		})
	}
}

// handleG9Intent serves the intent recent-decisions feed.
// Supports ?engineer= to scope to one engineer.
func handleG9Intent(store *db.LocalDB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		engineer := r.URL.Query().Get("engineer")

		events, err := store.GetRecentIntentEvents(20, engineer)
		if err != nil {
			http.Error(w, `{"error":"intent query failed"}`, http.StatusInternalServerError)
			return
		}

		// Nil-safe: return [] not null for empty result.
		if events == nil {
			events = []db.RecentIntentEvent{}
		}
		json.NewEncoder(w).Encode(events) //nolint:errcheck
	}
}
