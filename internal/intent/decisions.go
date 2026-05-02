package intent

import (
	"regexp"
	"strings"
)

// maxDecisionsPerRun caps how many decision.point events we emit per session.
// Most runs have 0–3 decisions; beyond 5 the heuristic signal-to-noise ratio
// degrades rapidly and false positives clutter Jira incident reports.
const maxDecisionsPerRun = 5

// maxDecisionFieldBytes caps each Decision text field (chosen, rejected items,
// rationale). Reasoning blocks are already capped at 1 KB in P1, so this is a
// second safety net keeping the overall event payload well under 1 KB.
const maxDecisionFieldBytes = 200

// Decision represents one detected decision moment inside a reasoning block.
// Stored verbatim as the `data` JSON for event_type="decision.point".
type Decision struct {
	// Chosen is the option the agent selected.
	Chosen string `json:"chosen"`
	// Rejected holds the alternatives that were not selected.
	Rejected []string `json:"rejected,omitempty"`
	// Rationale is the agent's stated reason (≤200 chars). May be empty.
	Rationale string `json:"rationale,omitempty"`
	// TsOffsetSec is seconds from the start of the first reasoning block
	// in this run. 0 when block timestamp is absent (common case).
	TsOffsetSec int `json:"ts_offset_sec"`
}

// decisionPatterns are the heuristic regex patterns for recognising decision
// moments in reasoning text. Compiled once at package init — safe for
// concurrent use. Order matters: more specific patterns appear first.
//
// Each entry carries capture semantics:
//   1. "I'll go with X because Y"  → chosen=X, rationale=Y
//   2. "using X over/instead of Y" → chosen=X, rejected=[Y]
//   3. "better to X than/rather than Y" → chosen=X, rejected=[Y]
//   4. "decided to X"              → chosen=X, no rejected
//   5. "could either X or Y"       → candidates only (need follow-up block)
var decisionPatterns = []*regexp.Regexp{
	// Pattern 1: "I'll go with X because Y"  (high confidence)
	regexp.MustCompile(`(?i)i'?ll go with (.+?) because (.+)`),
	// Pattern 2: "using X over/instead of Y"  (high confidence)
	regexp.MustCompile(`(?i)using (.+?) (?:over|instead of) (.+)`),
	// Pattern 3: "better to X than/rather than Y"  (high confidence)
	regexp.MustCompile(`(?i)better to (.+?) (?:than|rather than) (.+)`),
	// Pattern 4: "decided to X"  (medium confidence, no alternative captured)
	regexp.MustCompile(`(?i)decided to (.+?)\.`),
	// Pattern 5: "could [either] X or Y"  (low confidence — needs follow-up)
	regexp.MustCompile(`(?i)could (?:either )?(.+?) or (.+?)\.`),
}

// patternCount must equal len(decisionPatterns) — used for index-based dispatch.
const patternCount = 5

// followUpRe matches confirming phrases after a "could either…or…" candidate block.
// Promoted to package-level to avoid per-call recompilation inside findFollowUp.
var followUpRe = regexp.MustCompile(`(?i)(?:decided|chose|choosing|going with|i'?ll (?:go with|use)|will use)\s+(.+?)(?:\.|,|$)`)

// ExtractDecisions scans reasoning blocks for decision patterns and returns
// up to maxDecisionsPerRun Decision values. The cap is enforced early so that
// iteration stops once the limit is reached. Blocks with no matching pattern
// are skipped silently.
//
// The "could either … or …" pattern (Pattern 5) is tentative: after capturing
// both candidates, ExtractDecisions looks at the next 2 blocks for a
// "decided"/"chose"/"going with" follow-up. If one is found it promotes the
// detection to a real Decision; otherwise the tentative match is dropped.
//
// All field values are redacted (reusing P1 redact.go) and capped to
// maxDecisionFieldBytes before returning.
func ExtractDecisions(blocks []ReasoningBlock) []Decision {
	if len(blocks) == 0 {
		return nil
	}

	var decisions []Decision

	for i, block := range blocks {
		if len(decisions) >= maxDecisionsPerRun {
			break
		}

		text := block.Text

		// Try patterns in order; first match wins for this block.
		matched := false
		for pIdx, re := range decisionPatterns {
			sub := re.FindStringSubmatch(text)
			if sub == nil {
				continue
			}
			matched = true

			switch pIdx {
			case 0: // "I'll go with X because Y" → chosen, rationale
				d := Decision{
					Chosen:    capField(sub[1]),
					Rationale: capField(sub[2]),
				}
				decisions = append(decisions, d)

			case 1: // "using X over/instead of Y" → chosen, rejected[0]
				d := Decision{
					Chosen:   capField(sub[1]),
					Rejected: []string{capField(sub[2])},
				}
				decisions = append(decisions, d)

			case 2: // "better to X than/rather than Y" → chosen, rejected[0]
				d := Decision{
					Chosen:   capField(sub[1]),
					Rejected: []string{capField(sub[2])},
				}
				decisions = append(decisions, d)

			case 3: // "decided to X" → chosen only
				d := Decision{
					Chosen: capField(sub[1]),
				}
				decisions = append(decisions, d)

			case 4: // "could either X or Y" — tentative, needs follow-up
				candidateA := capField(sub[1])
				candidateB := capField(sub[2])

				// Look ahead at the next 2 blocks for a confirming phrase.
				chosen := findFollowUp(blocks, i+1, 2)
				if chosen == "" {
					// No follow-up found within 2 blocks — drop this tentative match.
					matched = false
					break
				}
				d := Decision{
					Chosen:   capField(chosen),
					Rejected: rejectCandidates(capField(chosen), candidateA, candidateB),
				}
				decisions = append(decisions, d)
			}

			break // one pattern per block
		}
		_ = matched // suppress unused warning
	}

	return decisions
}

// findFollowUp scans up to maxLook blocks starting at startIdx for a phrase
// that confirms a decision ("decided", "chose", "going with", "I'll go with",
// "use X"). Returns the captured noun phrase, or "" if not found.
func findFollowUp(blocks []ReasoningBlock, startIdx, maxLook int) string {
	for i := startIdx; i < len(blocks) && i < startIdx+maxLook; i++ {
		sub := followUpRe.FindStringSubmatch(blocks[i].Text)
		if sub != nil {
			return strings.TrimSpace(sub[1])
		}
	}
	return ""
}

// rejectCandidates builds the rejected list for the "could either…or…" case
// by dropping whichever candidate matches the chosen value (case-insensitive).
func rejectCandidates(chosen, a, b string) []string {
	lc := strings.ToLower(chosen)
	la := strings.ToLower(a)
	lb := strings.ToLower(b)

	if strings.Contains(la, lc) || strings.Contains(lc, la) {
		return []string{b}
	}
	if strings.Contains(lb, lc) || strings.Contains(lc, lb) {
		return []string{a}
	}
	// Can't determine — return both so no info is lost.
	return []string{a, b}
}

// capField redacts secrets then truncates to maxDecisionFieldBytes.
func capField(s string) string {
	s = strings.TrimSpace(s)
	return truncate(redactSecrets(s), maxDecisionFieldBytes)
}
