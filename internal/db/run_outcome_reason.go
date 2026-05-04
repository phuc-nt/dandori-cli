package db

// RunOutcomeReason is the canonical enum for why a run/session ended.
//
// Values are the wire/storage strings (snake_case) — they appear as JSON keys
// in task_attribution.session_outcomes and as the literal value of
// runs.session_end_reason. Adding a new constant requires bumping
// SchemaVersion + writing a migration that maps any pre-existing free-text
// values to one of these.
type RunOutcomeReason string

const (
	// Wrapper-emitted (3 historical values from sessionEndReason).
	ReasonAgentFinished   RunOutcomeReason = "agent_finished"
	ReasonUserInterrupted RunOutcomeReason = "user_interrupted"
	ReasonError           RunOutcomeReason = "error"

	// Finer buckets derived from events table (Phase 04 dashboard wanted
	// these but the wrapper never wrote them — see ClassifyRunOutcome).
	ReasonTestFail        RunOutcomeReason = "test_fail"
	ReasonLintFail        RunOutcomeReason = "lint_fail"
	ReasonHumanReject     RunOutcomeReason = "human_reject"
	ReasonTimeout         RunOutcomeReason = "timeout"
	ReasonPolicyViolation RunOutcomeReason = "policy_violation"

	ReasonOther RunOutcomeReason = "other"
)

// ReasonOrder is the canonical display order for dashboard widgets.
// ReworkCauses() returns rows in this order, padded with zeros.
var ReasonOrder = []RunOutcomeReason{
	ReasonTestFail,
	ReasonLintFail,
	ReasonHumanReject,
	ReasonTimeout,
	ReasonPolicyViolation,
	ReasonError,
	ReasonUserInterrupted,
	ReasonAgentFinished,
	ReasonOther,
}

// ClassifyRunOutcome inspects a run + its events to pick the most specific
// applicable reason. Order of precedence (most specific first):
//
//  1. events table contains test.fail / lint.fail / policy.violation /
//     approval.denied → matching finer bucket.
//  2. runs.session_end_reason has a literal value → map to enum.
//  3. fallback → ReasonOther.
//
// Errors during event scan are logged-by-omission (returns wrapper value);
// classification must never block the caller.
func (l *LocalDB) ClassifyRunOutcome(runID string) RunOutcomeReason {
	if runID == "" {
		return ReasonOther
	}

	if r := classifyByEvents(l, runID); r != "" {
		return r
	}

	var sessionEnd string
	_ = l.QueryRow(`SELECT COALESCE(session_end_reason, '') FROM runs WHERE id = ?`, runID).Scan(&sessionEnd)
	return mapSessionEndReason(sessionEnd)
}

// classifyByEvents looks for the finer-grained signals. Returns "" when no
// event matches (caller falls through).
func classifyByEvents(l *LocalDB, runID string) RunOutcomeReason {
	checks := []struct {
		eventType string
		reason    RunOutcomeReason
	}{
		{"test.fail", ReasonTestFail},
		{"lint.fail", ReasonLintFail},
		{"policy.violation", ReasonPolicyViolation},
		{"approval.denied", ReasonHumanReject},
		{"run.timeout", ReasonTimeout},
	}
	for _, c := range checks {
		var n int
		err := l.QueryRow(`SELECT COUNT(*) FROM events WHERE run_id = ? AND event_type = ?`, runID, c.eventType).Scan(&n)
		if err == nil && n > 0 {
			return c.reason
		}
	}
	return ""
}

// mapSessionEndReason converts the wrapper's free-text session_end_reason
// into the typed enum. Unknown strings become ReasonOther so old data
// doesn't crash classification.
func mapSessionEndReason(raw string) RunOutcomeReason {
	switch RunOutcomeReason(raw) {
	case ReasonAgentFinished, ReasonUserInterrupted, ReasonError:
		return RunOutcomeReason(raw)
	case "":
		return ReasonOther
	default:
		return ReasonOther
	}
}

// LegacyBucketReworkReason maps a free-text reason string from pre-Phase-05
// task_attribution.session_outcomes JSON into the new enum. Used only by the
// v8→v9 migration; do not call from new code.
func LegacyBucketReworkReason(reason string) RunOutcomeReason {
	r := lowerASCII(reason)
	switch {
	case contains(r, "test"):
		return ReasonTestFail
	case contains(r, "lint"):
		return ReasonLintFail
	case contains(r, "reject"), contains(r, "human"):
		return ReasonHumanReject
	case contains(r, "timeout"), contains(r, "timed out"):
		return ReasonTimeout
	case contains(r, "policy"):
		return ReasonPolicyViolation
	case r == string(ReasonAgentFinished):
		return ReasonAgentFinished
	case r == string(ReasonUserInterrupted):
		return ReasonUserInterrupted
	case r == string(ReasonError):
		return ReasonError
	case r == "":
		return ReasonOther
	default:
		return ReasonOther
	}
}

// Tiny helpers — avoid pulling strings package into this file just for
// these two so it stays focused on the enum.
func lowerASCII(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

func contains(haystack, needle string) bool {
	if needle == "" {
		return true
	}
	if len(needle) > len(haystack) {
		return false
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
