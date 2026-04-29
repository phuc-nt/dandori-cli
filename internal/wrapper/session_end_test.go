package wrapper

import "testing"

// TestSessionEndReason locks the exitCode → reason mapping that the wrapper
// persists into runs.session_end_reason. SIGINT (130) is the user pressing
// Ctrl+C mid-session; everything else non-zero is a generic error.
func TestSessionEndReason(t *testing.T) {
	cases := []struct {
		name     string
		exitCode int
		want     string
	}{
		{"clean exit", 0, "agent_finished"},
		{"sigint ctrl-c", 130, "user_interrupted"},
		{"generic error", 1, "error"},
		{"misuse error", 2, "error"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := sessionEndReason(c.exitCode)
			if got != c.want {
				t.Errorf("sessionEndReason(%d)=%q want %q", c.exitCode, got, c.want)
			}
		})
	}
}
