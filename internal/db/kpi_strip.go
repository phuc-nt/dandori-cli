// Package db — kpi_strip.go: 14-day daily aggregates for the dashboard
// KPI strip widget (Dashboard v2 Phase 01 Step 5).
//
// Returns one row per day for the last 14 calendar days (oldest → newest),
// padded with zeroes so the consumer can plot a fixed-width sparkline
// without gap-filling logic. Day key is UTC date.
package db

import "time"

type KPIDay struct {
	Day    string  `json:"day"` // YYYY-MM-DD (UTC)
	Runs   int     `json:"runs"`
	Cost   float64 `json:"cost"`
	Tokens int     `json:"tokens"`
}

// GetKPIDailyStats returns 14 KPIDay rows ending at today (UTC).
// Days with no runs return zero values rather than being omitted.
func (l *LocalDB) GetKPIDailyStats(days int) ([]KPIDay, error) {
	if days <= 0 {
		days = 14
	}

	now := time.Now().UTC()
	from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).
		AddDate(0, 0, -(days - 1))

	rows, err := l.Query(`
		SELECT
			date(started_at) AS day,
			COUNT(*)         AS runs,
			COALESCE(SUM(cost_usd), 0) AS cost,
			COALESCE(SUM(input_tokens + output_tokens), 0) AS tokens
		FROM runs
		WHERE started_at >= ?
		GROUP BY date(started_at)
	`, from.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byDay := map[string]KPIDay{}
	for rows.Next() {
		var d KPIDay
		if err := rows.Scan(&d.Day, &d.Runs, &d.Cost, &d.Tokens); err != nil {
			return nil, err
		}
		byDay[d.Day] = d
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]KPIDay, 0, days)
	for i := 0; i < days; i++ {
		key := from.AddDate(0, 0, i).Format("2006-01-02")
		if d, ok := byDay[key]; ok {
			out = append(out, d)
		} else {
			out = append(out, KPIDay{Day: key})
		}
	}
	return out, nil
}
