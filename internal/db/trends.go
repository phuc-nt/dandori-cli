// Package db — trends.go: week-over-week trend analytics (v0.11 Phase 03).
//
// Answers "Am I getting better?" with 3 metrics bucketed by ISO week:
//   - success-rate : % of runs with exit_code=0
//   - cost         : avg cost_usd per run
//   - rework-rate  : % of runs whose task had any rework (non-success session_outcomes)
//
// Gap weeks (no runs) are returned with HasData=false so the dashboard can
// render null gaps in Chart.js instead of misleading zeros.
//
// Slope is computed via simple least-squares over HasData==true points only.
// "flat" threshold: |slope| < 0.5 pp/week.
//
// No schema changes. Uses existing idx_runs_started_at index.
package db

import (
	"fmt"
	"math"
	"time"
)

// TrendPoint is one ISO-week bucket in a trend series.
type TrendPoint struct {
	WeekStart string  `json:"week_start"` // YYYY-MM-DD (Monday, UTC)
	Value     float64 `json:"value"`      // metric value for this week
	RunCount  int     `json:"run_count"`  // number of runs contributing
	HasData   bool    `json:"has_data"`   // false = no runs this week (gap)
}

// SlopeLabel returns the human-readable direction label for a slope value.
// Flat threshold is ±0.5 pp/week (matches phase spec).
func SlopeLabel(slope float64, points int) string {
	if points < 3 {
		return "insufficient data"
	}
	const flatThreshold = 0.5
	if math.Abs(slope) < flatThreshold {
		return "flat"
	}
	if slope > 0 {
		return fmt.Sprintf("improving %.1f pp/week", slope)
	}
	return fmt.Sprintf("declining %.1f pp/week", -slope)
}

// Slope computes the least-squares linear regression slope (value per week-index)
// over the HasData==true points in the series. Returns 0.0 if fewer than 2
// data points exist (can't fit a line).
func Slope(points []TrendPoint) float64 {
	// Collect (x=weekIndex, y=value) for weeks that have data.
	type xy struct{ x, y float64 }
	var pts []xy
	for i, p := range points {
		if p.HasData {
			pts = append(pts, xy{float64(i), p.Value})
		}
	}
	n := float64(len(pts))
	if n < 2 {
		return 0
	}
	// Least-squares: slope = (n·Σxy - Σx·Σy) / (n·Σx² - (Σx)²)
	var sumX, sumY, sumXY, sumX2 float64
	for _, p := range pts {
		sumX += p.x
		sumY += p.y
		sumXY += p.x * p.y
		sumX2 += p.x * p.x
	}
	denom := n*sumX2 - sumX*sumX
	if math.Abs(denom) < 1e-9 {
		return 0
	}
	return (n*sumXY - sumX*sumY) / denom
}

// GetTrend returns weekly trend data for the given metric over the period
// [since, now). windowDays specifies the bucket width (7 for weekly).
//
// Valid metric values: "success-rate", "cost", "rework-rate".
// Returns an error for unknown metrics. Returns [] (not nil) when since is in
// the future or no runs exist.
func (l *LocalDB) GetTrend(metric string, since time.Time, windowDays int) ([]TrendPoint, error) {
	if windowDays <= 0 {
		windowDays = 7
	}
	switch metric {
	case "success-rate":
		return l.trendSuccessRate(since, windowDays)
	case "cost":
		return l.trendCost(since, windowDays)
	case "rework-rate":
		return l.trendReworkRate(since, windowDays)
	default:
		return nil, fmt.Errorf("unknown metric %q: valid values are success-rate, cost, rework-rate", metric)
	}
}

// trendSuccessRate returns weekly success-rate (% exit_code=0 runs) buckets.
func (l *LocalDB) trendSuccessRate(since time.Time, windowDays int) ([]TrendPoint, error) {
	q := `
		SELECT
			strftime('%Y-%W', started_at) AS bucket,
			date(started_at, 'weekday 1', '-6 days') AS week_start,
			CAST(SUM(CASE WHEN exit_code = 0 THEN 1.0 ELSE 0 END) / COUNT(*) * 100 AS REAL) AS value,
			COUNT(*) AS run_count
		FROM runs
		WHERE started_at >= ?
		GROUP BY bucket
		ORDER BY bucket ASC
	`
	rows, err := l.Query(q, since.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("trend success-rate query: %w", err)
	}
	defer rows.Close()
	return l.gapFillTrend(rows, since, windowDays)
}

// trendCost returns weekly avg-cost-per-run buckets.
func (l *LocalDB) trendCost(since time.Time, windowDays int) ([]TrendPoint, error) {
	q := `
		SELECT
			strftime('%Y-%W', started_at) AS bucket,
			date(started_at, 'weekday 1', '-6 days') AS week_start,
			COALESCE(AVG(cost_usd), 0) AS value,
			COUNT(*) AS run_count
		FROM runs
		WHERE started_at >= ?
		GROUP BY bucket
		ORDER BY bucket ASC
	`
	rows, err := l.Query(q, since.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("trend cost query: %w", err)
	}
	defer rows.Close()
	return l.gapFillTrend(rows, since, windowDays)
}

// trendReworkRate returns weekly rework-rate buckets.
// Rework rate = (runs whose jira_issue_key has any non-success session outcome) / total runs.
// We join task_attribution on jira_issue_key and check session_outcomes != '{}' and != ”.
func (l *LocalDB) trendReworkRate(since time.Time, windowDays int) ([]TrendPoint, error) {
	q := `
		SELECT
			strftime('%Y-%W', r.started_at) AS bucket,
			date(r.started_at, 'weekday 1', '-6 days') AS week_start,
			CAST(
				SUM(CASE WHEN ta.session_outcomes IS NOT NULL AND ta.session_outcomes != '' AND ta.session_outcomes != '{}' THEN 1.0 ELSE 0 END)
				/ COUNT(*) * 100
			AS REAL) AS value,
			COUNT(*) AS run_count
		FROM runs r
		LEFT JOIN task_attribution ta ON ta.jira_issue_key = r.jira_issue_key
		WHERE r.started_at >= ?
		GROUP BY bucket
		ORDER BY bucket ASC
	`
	rows, err := l.Query(q, since.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("trend rework-rate query: %w", err)
	}
	defer rows.Close()
	return l.gapFillTrend(rows, since, windowDays)
}

// dbTrendRow is a scanned row from the trend queries above.
type dbTrendRow struct {
	bucket    string
	weekStart string
	value     float64
	runCount  int
}

// gapFillTrend reads all rows from the cursor, builds the expected week list
// from [since, now) in windowDays steps, and fills gaps with HasData=false.
func (l *LocalDB) gapFillTrend(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
	Close() error
}, since time.Time, windowDays int) ([]TrendPoint, error) {
	defer rows.Close()

	byBucket := map[string]dbTrendRow{}
	for rows.Next() {
		var r dbTrendRow
		if err := rows.Scan(&r.bucket, &r.weekStart, &r.value, &r.runCount); err != nil {
			return nil, fmt.Errorf("trend scan: %w", err)
		}
		byBucket[r.bucket] = r
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("trend rows: %w", err)
	}

	// Build expected ISO-week keys from since → now.
	now := time.Now().UTC()
	// Normalise since to Monday of its week.
	weekStart := isoWeekMonday(since)
	var out []TrendPoint
	for !weekStart.After(now) {
		bucket := weekStart.Format("2006-01") // placeholder — match via weekStart date
		// SQLite strftime('%Y-%W', ...) is what we actually use — replicate the key.
		y, w := weekStart.ISOWeek()
		bucket = fmt.Sprintf("%04d-%02d", y, w)

		if r, ok := byBucket[bucket]; ok {
			out = append(out, TrendPoint{
				WeekStart: r.weekStart,
				Value:     roundTo1(r.value),
				RunCount:  r.runCount,
				HasData:   true,
			})
		} else {
			out = append(out, TrendPoint{
				WeekStart: weekStart.Format("2006-01-02"),
				Value:     0,
				RunCount:  0,
				HasData:   false,
			})
		}
		weekStart = weekStart.AddDate(0, 0, windowDays)
	}
	return out, nil
}

// isoWeekMonday returns the Monday of the ISO week containing t.
func isoWeekMonday(t time.Time) time.Time {
	t = t.UTC()
	weekday := int(t.Weekday())
	if weekday == 0 {
		weekday = 7 // Sunday → 7
	}
	return time.Date(t.Year(), t.Month(), t.Day()-weekday+1, 0, 0, 0, 0, time.UTC)
}
