package quality

import "time"

// Snapshot captures lint/test state at a point in time
type Snapshot struct {
	LintErrors   int
	LintWarnings int
	TestsTotal   int
	TestsPassed  int
	TestsFailed  int
	TestsSkipped int
	CapturedAt   time.Time
	Error        string // Non-empty if capture failed
}

// Metrics stores quality comparison between before/after snapshots
type Metrics struct {
	RunID string

	// Before (Phase 01)
	LintErrorsBefore   int
	LintWarningsBefore int
	TestsTotalBefore   int
	TestsPassedBefore  int
	TestsFailedBefore  int

	// After (Phase 01)
	LintErrorsAfter   int
	LintWarningsAfter int
	TestsTotalAfter   int
	TestsPassedAfter  int
	TestsFailedAfter  int

	// Computed deltas (Phase 01)
	LintDelta  int // Negative = improvement (fewer errors)
	TestsDelta int // Positive = improvement (more passing)

	// Git metrics (Phase 02)
	LinesAdded       int
	LinesRemoved     int
	FilesChanged     int
	CommitCount      int
	CommitMsgQuality float64 // 0-1 score

	CreatedAt time.Time
}

// ComputeMetrics calculates deltas between before and after snapshots
func ComputeMetrics(runID string, before, after *Snapshot) *Metrics {
	m := &Metrics{
		RunID:     runID,
		CreatedAt: time.Now(),
	}

	if before != nil {
		m.LintErrorsBefore = before.LintErrors
		m.LintWarningsBefore = before.LintWarnings
		m.TestsTotalBefore = before.TestsTotal
		m.TestsPassedBefore = before.TestsPassed
		m.TestsFailedBefore = before.TestsFailed
	}

	if after != nil {
		m.LintErrorsAfter = after.LintErrors
		m.LintWarningsAfter = after.LintWarnings
		m.TestsTotalAfter = after.TestsTotal
		m.TestsPassedAfter = after.TestsPassed
		m.TestsFailedAfter = after.TestsFailed
	}

	// Compute deltas
	m.LintDelta = m.LintErrorsAfter - m.LintErrorsBefore    // Negative = improvement
	m.TestsDelta = m.TestsPassedAfter - m.TestsPassedBefore // Positive = improvement

	return m
}

// IsImproved returns true if quality improved overall
func (m *Metrics) IsImproved() bool {
	// Fewer lint errors OR more passing tests
	return m.LintDelta < 0 || m.TestsDelta > 0
}

// CompositeScore calculates weighted quality score (0-100)
// Weights: lint 25%, tests 30%, git hygiene 15%, rework penalty 30%
func (m *Metrics) CompositeScore() float64 {
	var score float64

	// Lint improvement (25%): fewer errors = better
	if m.LintErrorsBefore > 0 {
		lintImprovement := float64(-m.LintDelta) / float64(m.LintErrorsBefore)
		if lintImprovement > 1 {
			lintImprovement = 1
		}
		if lintImprovement < -1 {
			lintImprovement = -1
		}
		score += (lintImprovement + 1) / 2 * 25 // Normalize to 0-25
	} else if m.LintErrorsAfter == 0 {
		score += 25 // No lint errors = perfect
	}

	// Test improvement (30%): more passing = better
	if m.TestsTotalAfter > 0 {
		passRate := float64(m.TestsPassedAfter) / float64(m.TestsTotalAfter) * 30
		score += passRate
	}

	// Commit quality (15%): conventional commits
	score += m.CommitMsgQuality * 15

	// Code churn penalty avoided (30%): reasonable changes
	// More than 500 lines changed starts reducing score
	if m.LinesAdded+m.LinesRemoved > 0 {
		churn := float64(m.LinesAdded + m.LinesRemoved)
		if churn <= 100 {
			score += 30 // Small focused changes = good
		} else if churn <= 500 {
			score += 30 * (1 - (churn-100)/400) // Linear decay
		}
		// Massive changes (>500 lines) get 0 for this component
	} else {
		score += 15 // No changes = neutral
	}

	return score
}

// Summary returns a human-readable summary
func (m *Metrics) Summary() string {
	if m.LintErrorsBefore == 0 && m.LintErrorsAfter == 0 &&
		m.TestsPassedBefore == 0 && m.TestsPassedAfter == 0 {
		return "No quality data"
	}

	return ""
}
