package cmd

import (
	"strings"
	"testing"
)

// TestEventReasonValidation exercises the --reason / --type validation
// branches in runEvent BEFORE any DB open, so we don't need a live store.
//
// We poke the package-level flag variables directly because cobra's
// SetArgs path also opens the DB on success — and we only want to assert
// the early-return error paths.
func TestEventReasonValidation(t *testing.T) {
	saved := struct {
		runID, evType, evData, reason string
	}{eventRunID, eventType, eventData, eventReason}
	t.Cleanup(func() {
		eventRunID, eventType, eventData, eventReason = saved.runID, saved.evType, saved.evData, saved.reason
	})

	cases := []struct {
		name        string
		evType      string
		reason      string
		wantErrSub  string
		wantSuccess bool // would reach DB step (we stop pre-DB by leaving runID unset)
	}{
		{name: "both set", evType: "decision", reason: "wrong_approach", wantErrSub: "mutually exclusive"},
		{name: "neither set", evType: "", reason: "", wantErrSub: "either --type or --reason"},
		{name: "invalid reason", evType: "", reason: "garbage", wantErrSub: `invalid --reason "garbage"`},
		{name: "valid reason lists options on error", evType: "", reason: "garbage", wantErrSub: "wrong_approach"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			eventRunID = "" // skip DB-step (won't be reached for error cases)
			eventType = c.evType
			eventData = "{}"
			eventReason = c.reason

			err := runEvent(eventCmd, nil)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !strings.Contains(err.Error(), c.wantErrSub) {
				t.Errorf("error = %q, want substring %q", err.Error(), c.wantErrSub)
			}
		})
	}
}

func TestEventReasonValidation_AcceptsAllInterventionBuckets(t *testing.T) {
	saved := struct {
		runID, evType, evData, reason string
	}{eventRunID, eventType, eventData, eventReason}
	t.Cleanup(func() {
		eventRunID, eventType, eventData, eventReason = saved.runID, saved.evType, saved.evData, saved.reason
	})

	for _, r := range []string{"wrong_approach", "scope_misunderstanding", "missing_context"} {
		eventRunID = "" // forces DB-step error (run not found) AFTER validation passes
		eventType = ""
		eventData = "{}"
		eventReason = r

		err := runEvent(eventCmd, nil)
		// We expect an error from DB/run lookup, NOT a validation error.
		if err != nil && strings.Contains(err.Error(), "invalid --reason") {
			t.Errorf("reason %q rejected by validation: %v", r, err)
		}
	}
}
