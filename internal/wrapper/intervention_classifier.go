package wrapper

import "strings"

// messageClass labels what a human-text message means inside an agent session
// for G7 attribution. Only intervention/approval feed the intervention_rate
// metric — initialPrompt is the task framing (no agent context yet) and
// uncounted covers tool_result-only or noise.
type messageClass int

const (
	classUncounted messageClass = iota
	classInitialPrompt
	classIntervention
	classApproval
)

// interventionLengthThreshold separates "approval" (short ack like "ok",
// "yes", "looks good") from "intervention" (the user redirecting the agent).
// 30 characters is a heuristic proxy — see plan.md "Intervention classifier
// proxy v1" for the rationale and known limitations.
const interventionLengthThreshold = 30

// classifyHumanMessage decides which bucket a single user-text message falls
// into. seenAgentTool is the running flag "has any agent tool_use happened
// before this message?"; prevHumanCount is "how many human texts have we
// already classified in this session?". Order matters: anything before the
// first agent tool_use that is also the first human message is the initial
// prompt; anything else before tool_use is uncounted noise.
func classifyHumanMessage(text string, seenAgentTool bool, prevHumanCount int) messageClass {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return classUncounted
	}
	if !seenAgentTool {
		if prevHumanCount == 0 {
			return classInitialPrompt
		}
		return classUncounted
	}
	if len(trimmed) >= interventionLengthThreshold {
		return classIntervention
	}
	return classApproval
}
