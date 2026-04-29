package wrapper

import (
	"strings"
	"testing"
)

// TestClassifyHumanMessage locks the rules that turn raw human text into one
// of four buckets driving G7 attribution metrics. Boundaries matter: the
// 30-character cutoff between approval and intervention is the heuristic
// proxy for "did the user nudge vs redirect", and the seenAgentTool flag
// distinguishes initial framing from mid-session feedback.
func TestClassifyHumanMessage(t *testing.T) {
	cases := []struct {
		name           string
		text           string
		seenAgentTool  bool
		prevHumanCount int
		want           messageClass
	}{
		{"initial prompt before any tool", "Implement feature X", false, 0, classInitialPrompt},
		{"long msg before any tool still initial", "Long detailed initial framing with lots of context", false, 0, classInitialPrompt},
		{"second human msg after tool, long is intervention", "no, that's wrong, use the cached value instead", true, 1, classIntervention},
		{"approval short after tool", "ok continue", true, 1, classApproval},
		{"approval one word", "yes", true, 1, classApproval},
		{"approval at boundary 29 chars", strings.Repeat("a", 29), true, 1, classApproval},
		{"intervention at boundary 30 chars", strings.Repeat("a", 30), true, 1, classIntervention},
		{"empty text uncounted", "", true, 1, classUncounted},
		{"whitespace only uncounted", "   \n  ", true, 1, classUncounted},
		{"trim then measure", "  ok  ", true, 1, classApproval},
		{"second human before any tool is uncounted", "side comment", false, 1, classUncounted},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := classifyHumanMessage(c.text, c.seenAgentTool, c.prevHumanCount)
			if got != c.want {
				t.Errorf("classifyHumanMessage(%q, seenTool=%v, prev=%d) = %v, want %v",
					c.text, c.seenAgentTool, c.prevHumanCount, got, c.want)
			}
		})
	}
}
