// Package server — g9_window.go: period-window parser for G9 dashboard handlers.
// ParsePeriodWindow is the single entry point; all G9 handlers call it to
// derive the (current, prior) time windows from query params.
package server

import (
	"fmt"
	"net/http"
	"time"
)

// Window is a closed-open time interval [Start, End) used by G9 handlers.
type Window struct {
	Start time.Time
	End   time.Time
}

// defaultDaysForRole returns the default window duration in days for a role.
// engineer→7, project→28, org→90. Unknown roles fall back to 28.
func defaultDaysForRole(role string) int {
	switch role {
	case "engineer":
		return 7
	case "project":
		return 28
	case "org":
		return 90
	default:
		return 28
	}
}

// ParsePeriodWindow derives the current window (and optionally the prior window)
// from the request's query parameters and the caller-supplied role string.
//
// Query parameters:
//   - ?period=7d|28d|90d   — named rolling window ending now
//   - ?period=custom&from=YYYY-MM-DD&to=YYYY-MM-DD — custom UTC-midnight range
//   - ?compare=true        — also return the prior window (same duration, immediately preceding)
//
// Defaults (when ?period= is absent): engineer=7d, project=28d, org=90d.
//
// Returns (current, nil, nil) when compare is absent/false.
// Returns (current, prior, nil) when compare=true.
// Returns (nil, nil, err) on invalid custom dates; callers map this to HTTP 400.
func ParsePeriodWindow(r *http.Request, role string) (current, prior *Window, err error) {
	q := r.URL.Query()
	period := q.Get("period")
	compare := q.Get("compare") == "true"

	now := time.Now().UTC()
	var cur Window

	switch period {
	case "custom":
		fromStr := q.Get("from")
		toStr := q.Get("to")
		if fromStr == "" {
			return nil, nil, fmt.Errorf("?period=custom requires &from=YYYY-MM-DD")
		}
		if toStr == "" {
			return nil, nil, fmt.Errorf("?period=custom requires &to=YYYY-MM-DD")
		}
		fromT, ferr := time.ParseInLocation("2006-01-02", fromStr, time.UTC)
		if ferr != nil {
			return nil, nil, fmt.Errorf("invalid from date %q: %w", fromStr, ferr)
		}
		toT, terr := time.ParseInLocation("2006-01-02", toStr, time.UTC)
		if terr != nil {
			return nil, nil, fmt.Errorf("invalid to date %q: %w", toStr, terr)
		}
		cur = Window{Start: fromT, End: toT}

	case "7d":
		cur = Window{Start: now.AddDate(0, 0, -7), End: now}
	case "28d":
		cur = Window{Start: now.AddDate(0, 0, -28), End: now}
	case "90d":
		cur = Window{Start: now.AddDate(0, 0, -90), End: now}

	default:
		// No period param — use role default.
		days := defaultDaysForRole(role)
		cur = Window{Start: now.AddDate(0, 0, -days), End: now}
	}

	current = &cur
	if !compare {
		return current, nil, nil
	}

	// Prior window: same duration, immediately preceding current.
	dur := cur.End.Sub(cur.Start)
	p := Window{
		Start: cur.Start.Add(-dur),
		End:   cur.Start,
	}
	prior = &p
	return current, prior, nil
}
