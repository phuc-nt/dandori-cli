package metric

import (
	"fmt"
	"log/slog"
	"sort"
)

type MTTRResult struct {
	Window           MetricWindow
	SamplesUsed      int // resolved incidents with valid duration
	OngoingIncidents int // unresolved at query time
	BadDataSkipped   int // resolutiondate < created (data integrity issue)
	P50Seconds       float64
	P75Seconds       float64
	P90Seconds       float64
	InsufficientData bool
}

// ComputeTimeToRestore computes resolution-time percentiles for incidents
// CREATED in the window. Ongoing incidents are counted but excluded from
// percentiles (no resolution time yet); rows with resolved < created are
// dropped + warn-logged (Jira data integrity issue).
//
// Reuses the percentile helper from deployment.go (sorted, NIST linear
// interpolation).
func ComputeTimeToRestore(src jiraIncidentSource, q IncidentQuery) (MTTRResult, error) {
	if !q.Window.End.After(q.Window.Start) {
		return MTTRResult{}, fmt.Errorf("invalid window")
	}
	if len(q.Match.IssueTypes) == 0 && len(q.Match.Labels) == 0 {
		return MTTRResult{}, fmt.Errorf("incident match config is empty: set issue_types or labels")
	}

	incidents, err := fetchIncidents(src, q)
	if err != nil {
		return MTTRResult{}, fmt.Errorf("fetch incidents: %w", err)
	}

	res := MTTRResult{Window: q.Window}
	durations := make([]float64, 0, len(incidents))
	for _, inc := range incidents {
		if inc.ResolvedAt == nil {
			res.OngoingIncidents++
			continue
		}
		if inc.ResolvedAt.Before(inc.CreatedAt) {
			slog.Warn("mttr: skipping incident with resolved < created",
				"key", inc.Key, "created", inc.CreatedAt, "resolved", *inc.ResolvedAt)
			res.BadDataSkipped++
			continue
		}
		durations = append(durations, inc.ResolvedAt.Sub(inc.CreatedAt).Seconds())
	}

	res.SamplesUsed = len(durations)
	if len(durations) == 0 {
		res.InsufficientData = true
		return res, nil
	}
	sort.Float64s(durations)
	res.P50Seconds = percentile(durations, 50)
	res.P75Seconds = percentile(durations, 75)
	res.P90Seconds = percentile(durations, 90)
	return res, nil
}
