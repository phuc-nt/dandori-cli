package metric

import (
	"fmt"
	"time"
)

// reworkSource is the subset of LocalDB methods used here. Defined as an
// interface so tests can stub without spinning up SQLite.
type reworkSource interface {
	TotalRunIDs(since, until time.Time, team string) ([]string, error)
	ReworkRunIDs(since, until time.Time, team string) ([]string, error)
}

// ReworkQuery configures a single Rework Rate computation.
type ReworkQuery struct {
	Window MetricWindow
	Filter TeamFilter
}

// ReworkResult is the computed Rework Rate plus the raw counts and threshold
// flag callers need to render dashboards or fail OKR gates.
type ReworkResult struct {
	Rate             float64      `json:"rate"`
	ReworkCount      int          `json:"rework_count"`
	TotalCount       int          `json:"total_count"`
	Window           MetricWindow `json:"window"`
	Team             string       `json:"team,omitempty"`
	Threshold        float64      `json:"threshold"`
	ThresholdVersion string       `json:"threshold_version"`
	ExceedsThreshold bool         `json:"exceeds_threshold"`
	InsufficientData bool         `json:"insufficient_data,omitempty"`
}

// ComputeReworkRate aggregates Layer-3 task.iteration.start events into a
// single Rework Rate value over q.Window. Cancelled runs stay in the
// denominator on purpose — see types.go for rationale.
func ComputeReworkRate(src reworkSource, q ReworkQuery) (ReworkResult, error) {
	if q.Window.End.Before(q.Window.Start) || q.Window.End.Equal(q.Window.Start) {
		return ReworkResult{}, fmt.Errorf("invalid window: end %s must be after start %s",
			q.Window.End.Format(time.RFC3339), q.Window.Start.Format(time.RFC3339))
	}

	totalIDs, err := src.TotalRunIDs(q.Window.Start, q.Window.End, q.Filter.Team)
	if err != nil {
		return ReworkResult{}, fmt.Errorf("total runs: %w", err)
	}
	reworkIDs, err := src.ReworkRunIDs(q.Window.Start, q.Window.End, q.Filter.Team)
	if err != nil {
		return ReworkResult{}, fmt.Errorf("rework runs: %w", err)
	}

	res := ReworkResult{
		ReworkCount:      len(reworkIDs),
		TotalCount:       len(totalIDs),
		Window:           q.Window,
		Team:             q.Filter.Team,
		Threshold:        ReworkThresholdV1,
		ThresholdVersion: ReworkThresholdTag,
	}
	if res.TotalCount == 0 {
		res.InsufficientData = true
		return res, nil
	}
	res.Rate = float64(res.ReworkCount) / float64(res.TotalCount)
	res.ExceedsThreshold = res.Rate > ReworkThresholdV1
	return res, nil
}
