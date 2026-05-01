// Package server — g9_iterations.go: /api/g9/iterations handler.
// Returns a duration histogram bucketed into 5 ranges for runs matching
// the given role/id scope and period window.
package server

import (
	"encoding/json"
	"net/http"

	"github.com/phuc-nt/dandori-cli/internal/db"
)

// durationBucket maps a duration in seconds to a human-readable label.
// Thresholds: <60s, 60-300s, 300-1800s, 1800-7200s, >7200s.
func durationBucket(sec int) string {
	switch {
	case sec < 60:
		return "<1m"
	case sec < 300:
		return "1-5m"
	case sec < 1800:
		return "5-30m"
	case sec <= 7200:
		return "30m-2h"
	default:
		return ">2h"
	}
}

// bucketOrder defines the canonical display order for histogram buckets.
var bucketOrder = []string{"<1m", "1-5m", "5-30m", "30m-2h", ">2h"}

// iterationHistogram queries runs matching the given window and optional project
// scope (role="project", id=projectKey), then returns the duration histogram.
func iterationHistogram(store *db.LocalDB, role, id string, w *Window) (map[string]any, error) {
	startStr := w.Start.UTC().Format("2006-01-02T15:04:05Z")
	endStr := w.End.UTC().Format("2006-01-02T15:04:05Z")

	var query string
	var args []any

	if role == "project" && id != "" {
		query = `
			SELECT COALESCE(duration_sec, 0)
			FROM runs
			WHERE started_at >= ? AND started_at < ?
			  AND jira_issue_key LIKE ?
			  AND duration_sec IS NOT NULL
		`
		args = []any{startStr, endStr, id + "-%"}
	} else {
		query = `
			SELECT COALESCE(duration_sec, 0)
			FROM runs
			WHERE started_at >= ? AND started_at < ?
			  AND duration_sec IS NOT NULL
		`
		args = []any{startStr, endStr}
	}

	rows, err := store.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := map[string]int{
		"<1m": 0, "1-5m": 0, "5-30m": 0, "30m-2h": 0, ">2h": 0,
	}
	total := 0

	for rows.Next() {
		var sec int
		if err := rows.Scan(&sec); err != nil {
			return nil, err
		}
		counts[durationBucket(sec)]++
		total++
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	buckets := make([]map[string]any, len(bucketOrder))
	for i, label := range bucketOrder {
		buckets[i] = map[string]any{
			"label": label,
			"count": counts[label],
		}
	}

	return map[string]any{
		"buckets": buckets,
		"total":   total,
	}, nil
}

// handleG9Iterations handles GET /api/g9/iterations.
// Supports ?role=, ?id=, ?period=, ?from=, ?to= params.
func handleG9Iterations(store *db.LocalDB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		q := r.URL.Query()
		role := q.Get("role")
		if role == "" {
			role = "org"
		}
		id := q.Get("id")

		cur, _, err := ParsePeriodWindow(r, role)
		if err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
			return
		}

		result, err := iterationHistogram(store, role, id, cur)
		if err != nil {
			http.Error(w, `{"error":"iterations query failed"}`, http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(result) //nolint:errcheck
	}
}
