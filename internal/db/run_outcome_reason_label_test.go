package db

import "testing"

// TestReasonLabel_AllConstants pins the display labels for every reason
// listed in ReasonOrder. If someone adds a new constant without a label,
// this test fails on the `default` fallthrough.
func TestReasonLabel_AllConstants(t *testing.T) {
	want := map[RunOutcomeReason]string{
		ReasonTestFail:              "Test failure",
		ReasonLintFail:              "Lint failure",
		ReasonHumanReject:           "Human reject",
		ReasonWrongApproach:         "Wrong approach",
		ReasonScopeMisunderstanding: "Scope misunderstanding",
		ReasonMissingContext:        "Missing context",
		ReasonTimeout:               "Timeout",
		ReasonPolicyViolation:       "Policy violation",
		ReasonError:                 "Error",
		ReasonUserInterrupted:       "User interrupted",
		ReasonAgentFinished:         "Agent finished",
		ReasonOther:                 "Other",
	}
	for r, expected := range want {
		got := ReasonLabel(r)
		if got != expected {
			t.Errorf("ReasonLabel(%q) = %q, want %q", r, got, expected)
		}
	}

	// Sanity check: every reason in ReasonOrder has an explicit label
	// (i.e. doesn't fall through to the default branch).
	for _, r := range ReasonOrder {
		if _, ok := want[r]; !ok {
			t.Errorf("ReasonOrder includes %q but no label is pinned in this test — add it", r)
		}
	}
}

func TestKnownReasons_MatchesReasonOrder(t *testing.T) {
	got := KnownReasons()
	if len(got) != len(ReasonOrder) {
		t.Fatalf("len(KnownReasons)=%d, len(ReasonOrder)=%d", len(got), len(ReasonOrder))
	}
	for i, r := range ReasonOrder {
		if got[i] != r {
			t.Errorf("KnownReasons[%d] = %q, want %q", i, got[i], r)
		}
	}
}

func TestMapSessionEndReason_AcceptsInterventionBuckets(t *testing.T) {
	cases := []struct {
		in   string
		want RunOutcomeReason
	}{
		{"wrong_approach", ReasonWrongApproach},
		{"scope_misunderstanding", ReasonScopeMisunderstanding},
		{"missing_context", ReasonMissingContext},
		{"agent_finished", ReasonAgentFinished},
		{"", ReasonOther},
		{"bogus_string", ReasonOther},
	}
	for _, c := range cases {
		if got := mapSessionEndReason(c.in); got != c.want {
			t.Errorf("mapSessionEndReason(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
