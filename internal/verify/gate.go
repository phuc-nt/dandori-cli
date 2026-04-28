package verify

import (
	"strings"
)

// GateConfig controls which checks run during PreSync.
type GateConfig struct {
	SemanticCheck bool   // Q3: path-match spec vs diff
	QualityGate   bool   // Q4: lint/test on changed files (skipped for doc-only)
	SkipLabel     string // Q2: Jira label that bypasses all gates (e.g. "skip-verify")
}

// DefaultGateConfig returns the recommended Bug #3 defaults.
func DefaultGateConfig() GateConfig {
	return GateConfig{
		SemanticCheck: true,
		QualityGate:   true,
		SkipLabel:     "skip-verify",
	}
}

// QualitySignal is the minimum surface PreSync needs from the quality
// package. Concrete callers pass a closure that wraps quality.Collector. We
// keep it as an interface so verify/ doesn't depend on quality/ directly,
// avoiding an import cycle with task_run.go.
type QualitySignal interface {
	// CountFailures returns (lintErrors, testFails). Both >0 → block.
	CountFailures() (lintErrors, testFails int)
}

// PreSyncInput collects everything PreSync needs.
type PreSyncInput struct {
	TaskDescription string
	JiraLabels      []string
	ChangedFiles    []string
	WorkspaceDir    string
	// Quality, if non-nil, is consulted only when QualityGate is enabled
	// AND the diff is not doc-only.
	Quality QualitySignal
}

// PreSyncResult is the combined gate verdict.
//
// Severity is "warn" (per Q1 decision) — caller should:
//   - if Pass: transition Jira to Done
//   - if !Pass: post Jira comment with Reason, leave ticket In Progress.
type PreSyncResult struct {
	Pass         bool
	Skipped      bool   // bypassed via SkipLabel
	Reason       string // human-readable summary for Jira comment
	Semantic     Result // empty when SemanticCheck disabled
	LintErrors   int
	TestFailures int
}

// PreSync runs all enabled gates and returns one combined verdict.
func PreSync(cfg GateConfig, in PreSyncInput) PreSyncResult {
	if cfg.SkipLabel != "" && hasLabel(in.JiraLabels, cfg.SkipLabel) {
		return PreSyncResult{
			Pass:    true,
			Skipped: true,
			Reason:  "gate skipped via '" + cfg.SkipLabel + "' label",
		}
	}

	var reasons []string
	pass := true
	out := PreSyncResult{}

	if cfg.SemanticCheck {
		sem := CheckResult(in.TaskDescription, in.ChangedFiles, in.WorkspaceDir)
		out.Semantic = sem
		switch {
		case sem.Inconclusive:
			// Q5: flag for review — don't block, but surface it.
			reasons = append(reasons, "semantic check inconclusive: "+sem.Reason)
			pass = false
		case !sem.Pass:
			reasons = append(reasons, "semantic check failed: "+sem.Reason)
			if len(sem.Missing) > 0 {
				reasons = append(reasons, "  spec referenced but diff did not touch: "+strings.Join(sem.Missing, ", "))
			}
			pass = false
		}
	}

	if cfg.QualityGate && in.Quality != nil && !IsDocOnly(in.ChangedFiles) {
		lintErrs, testFails := in.Quality.CountFailures()
		out.LintErrors = lintErrs
		out.TestFailures = testFails
		if lintErrs > 0 {
			reasons = append(reasons, "lint errors detected")
			pass = false
		}
		if testFails > 0 {
			reasons = append(reasons, "test failures detected")
			pass = false
		}
	}

	out.Pass = pass
	if pass {
		out.Reason = "all gates passed"
	} else {
		out.Reason = strings.Join(reasons, "; ")
	}
	return out
}

func hasLabel(labels []string, want string) bool {
	for _, l := range labels {
		if strings.EqualFold(l, want) {
			return true
		}
	}
	return false
}
