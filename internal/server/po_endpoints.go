// Package server — po_endpoints.go: PO View backend endpoints (Phase 02).
//
// Mounts 7 endpoints powering the PO/PDM persona dashboard:
//
//	GET /api/sprints                — list distinct sprints from runs.
//	GET /api/sprints/burndown?id=X  — daily progress within a sprint.
//	GET /api/cost/department        — daily cost grouped by department.
//	GET /api/cost/projection        — linear regression of last 14d cost.
//	GET /api/attribution/timeline   — weekly authorship/retention/intervention.
//	GET /api/tasks/lifecycle?key=X  — all runs of a given PBI in order.
//	GET /api/runs/lead-time         — duration-bucket distribution.
//
// All endpoints accept the common PO filter:
//
//	?from=YYYY-MM-DD&to=YYYY-MM-DD
//	?sprint=<id>  ?dept=<name>  ?project=<KEY>  ?engineer=<name>
//
// Cost projection uses simple least-squares linear regression on the last 14
// daily totals; the response includes a confidence interval (±1σ) and a
// "data_sufficient" flag set false when fewer than 7 non-zero days exist.
package server

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/phuc-nt/dandori-cli/internal/db"
)

// RegisterPORoutes mounts the 7 PO endpoints on mux.
func RegisterPORoutes(mux *http.ServeMux, store *db.LocalDB) {
	mux.HandleFunc("/api/sprints", handleSprintsList(store))
	mux.HandleFunc("/api/sprints/burndown", handleSprintBurndown(store))
	mux.HandleFunc("/api/sprints/runs", handleSprintRuns(store))
	mux.HandleFunc("/api/cost/department", handleCostByDepartment(store))
	mux.HandleFunc("/api/cost/projection", handleCostProjection(store))
	mux.HandleFunc("/api/attribution/timeline", handleAttributionTimeline(store))
	mux.HandleFunc("/api/tasks/lifecycle", handleTaskLifecycle(store))
	mux.HandleFunc("/api/runs/lead-time", handleLeadTime(store))
}

// parsePOFilter reads the common PO filter params from the request.
// Empty params produce a zero-value POFilter (no restriction).
// Returns HTTP 400 on invalid date strings.
func parsePOFilter(r *http.Request) (db.POFilter, error) {
	q := r.URL.Query()
	f := db.POFilter{
		Sprint:   q.Get("sprint"),
		Dept:     q.Get("dept"),
		Project:  q.Get("project"),
		Engineer: q.Get("engineer"),
	}
	if s := q.Get("from"); s != "" {
		t, err := time.ParseInLocation("2006-01-02", s, time.UTC)
		if err != nil {
			return f, fmt.Errorf("invalid from date %q", s)
		}
		f.From = t
	}
	if s := q.Get("to"); s != "" {
		t, err := time.ParseInLocation("2006-01-02", s, time.UTC)
		if err != nil {
			return f, fmt.Errorf("invalid to date %q", s)
		}
		f.To = t
	}
	return f, nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	http.Error(w, fmt.Sprintf(`{"error":%q}`, msg), code)
}

func handleSprintsList(store *db.LocalDB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sprints, err := store.ListSprints()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "sprints query failed")
			return
		}
		if sprints == nil {
			sprints = []db.SprintInfo{}
		}
		writeJSON(w, sprints)
	}
}

func handleSprintBurndown(store *db.LocalDB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		if id == "" {
			writeError(w, http.StatusBadRequest, "id query param required")
			return
		}
		days, err := store.SprintBurndown(id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "burndown query failed")
			return
		}
		if days == nil {
			days = []db.SprintBurndownDay{}
		}
		writeJSON(w, days)
	}
}

func handleSprintRuns(store *db.LocalDB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		if id == "" {
			writeError(w, http.StatusBadRequest, "id query param required")
			return
		}
		runs, err := store.SprintRuns(id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "sprint runs query failed")
			return
		}
		if runs == nil {
			runs = []db.SprintRunRow{}
		}
		writeJSON(w, runs)
	}
}

func handleCostByDepartment(store *db.LocalDB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f, err := parsePOFilter(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		rows, err := store.CostByDepartment(f)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "cost-by-department query failed")
			return
		}
		if rows == nil {
			rows = []db.CostByDeptDay{}
		}
		writeJSON(w, rows)
	}
}

// CostProjection is the response payload for /api/cost/projection.
type CostProjection struct {
	History        []db.DailyCost `json:"history"`         // last 14d daily totals (oldest → newest)
	Slope          float64        `json:"slope"`           // $/day
	Intercept      float64        `json:"intercept"`       // $ on day 0 (oldest)
	StdDev         float64        `json:"std_dev"`         // residual σ
	ProjectedEOM   float64        `json:"projected_eom"`   // projected total spend through end of current calendar month
	DaysToEOM      int            `json:"days_to_eom"`     // days remaining in month (incl. today)
	Spent          float64        `json:"spent"`           // sum of history (last 14d actual)
	DataSufficient bool           `json:"data_sufficient"` // true if ≥7 non-zero history days
	ConfidenceLow  float64        `json:"confidence_low"`  // EOM projection - 1σ * sqrt(daysToEOM)
	ConfidenceHigh float64        `json:"confidence_high"` // EOM projection + 1σ * sqrt(daysToEOM)
	DisclaimerNote string         `json:"disclaimer_note"`
}

func handleCostProjection(store *db.LocalDB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hist, err := store.DailyCostSeries(14)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "projection query failed")
			return
		}

		nonZero := 0
		var spent float64
		for _, d := range hist {
			spent += d.Cost
			if d.Cost > 0 {
				nonZero++
			}
		}

		resp := CostProjection{
			History:        hist,
			Spent:          spent,
			DataSufficient: nonZero >= 7,
			DisclaimerNote: "Linear regression over last 14 days of daily cost; ±1σ band.",
		}

		if resp.DataSufficient {
			slope, intercept, sigma := linearRegression(hist)
			resp.Slope = slope
			resp.Intercept = intercept
			resp.StdDev = sigma

			now := time.Now().UTC()
			// Use clean midnight boundaries so DST-shifted hours don't bias the count.
			// tomorrow midnight — today midnight gives an exact integer day count.
			todayMidnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
			eomMidnight := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, time.UTC)
			daysRemaining := int(eomMidnight.Sub(todayMidnight).Hours() / 24)
			if daysRemaining < 0 {
				daysRemaining = 0
			}
			resp.DaysToEOM = daysRemaining

			// Project month-to-date actual + linear extrapolation for remaining days.
			// Day index for tomorrow relative to oldest history day:
			tomorrowIdx := float64(len(hist)) // last history day was idx len-1, so day after = len
			var projectedRemaining float64
			for i := 0; i < daysRemaining; i++ {
				projectedRemaining += slope*(tomorrowIdx+float64(i)) + intercept
			}
			// Month-to-date actual: sum of history days that fall within current month.
			monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
			var mtd float64
			for _, d := range hist {
				t, _ := time.Parse("2006-01-02", d.Day)
				if !t.Before(monthStart) {
					mtd += d.Cost
				}
			}
			if projectedRemaining < 0 {
				projectedRemaining = 0
			}
			resp.ProjectedEOM = mtd + projectedRemaining
			band := sigma * math.Sqrt(float64(daysRemaining))
			resp.ConfidenceLow = resp.ProjectedEOM - band
			resp.ConfidenceHigh = resp.ProjectedEOM + band
			if resp.ConfidenceLow < 0 {
				resp.ConfidenceLow = 0
			}
			if resp.ConfidenceHigh < resp.ConfidenceLow {
				resp.ConfidenceHigh = resp.ConfidenceLow
			}
		}

		writeJSON(w, resp)
	}
}

// linearRegression fits y = slope*x + intercept against the cost series
// (x = day index 0..n-1) and returns the residual standard deviation.
func linearRegression(hist []db.DailyCost) (slope, intercept, sigma float64) {
	n := float64(len(hist))
	if n < 2 {
		return 0, 0, 0
	}
	var sumX, sumY, sumXY, sumXX float64
	for i, d := range hist {
		x := float64(i)
		y := d.Cost
		sumX += x
		sumY += y
		sumXY += x * y
		sumXX += x * x
	}
	denom := n*sumXX - sumX*sumX
	if denom == 0 {
		return 0, sumY / n, 0
	}
	slope = (n*sumXY - sumX*sumY) / denom
	intercept = (sumY - slope*sumX) / n

	// Residual σ.
	var ssr float64
	for i, d := range hist {
		yhat := slope*float64(i) + intercept
		diff := d.Cost - yhat
		ssr += diff * diff
	}
	sigma = math.Sqrt(ssr / n)
	return
}

func handleAttributionTimeline(store *db.LocalDB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f, err := parsePOFilter(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		points, err := store.AttributionTimeline(f)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "attribution query failed")
			return
		}
		if points == nil {
			points = []db.AttributionTimelinePoint{}
		}
		writeJSON(w, points)
	}
}

func handleTaskLifecycle(store *db.LocalDB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Query().Get("key")
		if key == "" {
			writeError(w, http.StatusBadRequest, "key query param required")
			return
		}
		runs, err := store.TaskLifecycle(key)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "lifecycle query failed")
			return
		}
		if runs == nil {
			runs = []db.LifecycleRun{}
		}
		writeJSON(w, runs)
	}
}

func handleLeadTime(store *db.LocalDB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f, err := parsePOFilter(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		buckets, err := store.LeadTimeDistribution(f)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "lead-time query failed")
			return
		}
		writeJSON(w, buckets)
	}
}
