// Package intent parses Claude session JSONL transcripts at run-end and
// extracts intent signals: the first user message, the final assistant summary,
// and agent reasoning blocks (thinking parts + narrative text before tool use).
//
// All extraction is fail-soft: errors are logged and zero values returned so
// that the wrapper's run-completion path is never interrupted.
package intent

import (
	"log/slog"
	"os"
)

const (
	maxIntentBytes     = 2 * 1024 // 2 KB cap for first_user_msg and summary
	maxReasoningBytes  = 1 * 1024 // 1 KB cap per reasoning block
	maxReasoningBlocks = 10       // max reasoning blocks stored per run
)

// ReasoningBlock is one captured reasoning signal from the session.
type ReasoningBlock struct {
	// Source is "thinking" for <thinking> parts or "narrative" for assistant
	// text that accompanies tool_use in the same message.
	Source string `json:"source"`
	Text   string `json:"text"`
}

// Result holds everything the extractor found in the JSONL transcript.
// All fields may be empty if the corresponding signal was absent.
type Result struct {
	FirstUserMsg string           `json:"first_user_msg"`
	Summary      string           `json:"summary"`
	Reasoning    []ReasoningBlock `json:"reasoning"`
	// Decisions holds heuristic decision-point detections (Phase 2).
	// Capped at maxDecisionsPerRun. May be empty when no patterns match.
	Decisions []Decision `json:"decisions,omitempty"`
	// SpecLinks holds the spec-linkage snapshot (Phase 3): Jira back-pointer
	// and Confluence URLs found in the first user message and cwd spec files.
	SpecLinks SpecLinks `json:"spec_links"`
}

// Extract parses the JSONL file at path for run runID and returns intent
// signals. If DANDORI_INTENT_DISABLED is set to a non-empty value, Extract
// returns an empty Result immediately.
//
// cwd is the working directory at run time (used for spec-file scanning in
// Phase 3). jiraKey is the Jira issue key already stored on the run; it is
// passed through verbatim into SpecLinks as a back-pointer.
//
// Extract never returns a non-nil error that would fail the run; callers
// should log any returned error at Warn level and continue.
func Extract(path, runID, cwd, jiraKey string) (Result, error) {
	if os.Getenv("DANDORI_INTENT_DISABLED") != "" {
		slog.Debug("intent extraction disabled via env", "run_id", runID)
		return Result{}, nil
	}

	var res Result
	var lastAssistantSummary string
	var reasoningBlocks []ReasoningBlock

	err := Walk(path, func(line parsedLine) {
		switch line.Type {
		case "user":
			if res.FirstUserMsg == "" {
				msg := firstTextFromUser(line.Parts)
				if msg != "" {
					res.FirstUserMsg = truncate(redactSecrets(msg), maxIntentBytes)
				}
			}
		case "assistant":
			// Collect reasoning signals from this message.
			if len(reasoningBlocks) < maxReasoningBlocks {
				reasoningBlocks = appendReasoningBlocks(reasoningBlocks, line.Parts)
			}
			// Track the last assistant summary candidate.
			if s := lastTextSummary(line.Parts); s != "" {
				lastAssistantSummary = s
			}
		}
	})

	if err != nil {
		return Result{}, err
	}

	if lastAssistantSummary != "" {
		res.Summary = truncate(redactSecrets(lastAssistantSummary), maxIntentBytes)
	}

	res.Reasoning = reasoningBlocks

	// Phase 2: run decision-point heuristics over the collected reasoning blocks.
	// ExtractDecisions is fail-soft by design (pure computation, no I/O).
	res.Decisions = ExtractDecisions(reasoningBlocks)

	// Phase 3: snapshot spec linkage (Jira back-pointer + Confluence URLs).
	// ExtractSpecLinks is fail-soft: unreadable cwd files are silently skipped.
	res.SpecLinks = ExtractSpecLinks(res.FirstUserMsg, cwd, jiraKey)

	slog.Debug("intent extracted",
		"run_id", runID,
		"has_first_msg", res.FirstUserMsg != "",
		"has_summary", res.Summary != "",
		"reasoning_blocks", len(res.Reasoning),
		"decisions", len(res.Decisions),
		"confluence_urls", len(res.SpecLinks.ConfluenceURLs),
	)

	return res, nil
}

// firstTextFromUser returns the first text part from a user message that is
// NOT a tool_result (i.e. the human's actual typed text).
func firstTextFromUser(parts []contentPart) string {
	for _, p := range parts {
		if p.Type == "text" && p.Text != "" {
			return p.Text
		}
	}
	return ""
}

// lastTextSummary returns the terminal text content part of an assistant
// message when the *last* part is "text" — meaning the agent is wrapping up
// rather than invoking a tool. Returns "" if the terminal part is not text.
func lastTextSummary(parts []contentPart) string {
	if len(parts) == 0 {
		return ""
	}
	last := parts[len(parts)-1]
	if last.Type == "text" && last.Text != "" {
		return last.Text
	}
	return ""
}

// appendReasoningBlocks collects reasoning signals from one assistant message:
//  1. Any "thinking" content part (extended CoT block).
//  2. Any "text" part that accompanies tool_use in the same message (narrative
//     before action — the agent is explaining what it will do next).
//
// Each block is independently capped and redacted. The global cap
// (maxReasoningBlocks) is enforced by the caller.
func appendReasoningBlocks(blocks []ReasoningBlock, parts []contentPart) []ReasoningBlock {
	hasToolUse := false
	for _, p := range parts {
		if p.Type == "tool_use" {
			hasToolUse = true
			break
		}
	}

	for _, p := range parts {
		if len(blocks) >= maxReasoningBlocks {
			break
		}
		switch p.Type {
		case "thinking":
			body := p.Thinking
			if body == "" {
				body = p.Text // some models put thinking body in "text"
			}
			if body != "" {
				blocks = append(blocks, ReasoningBlock{
					Source: "thinking",
					Text:   truncate(redactSecrets(body), maxReasoningBytes),
				})
			}
		case "text":
			if hasToolUse && p.Text != "" {
				blocks = append(blocks, ReasoningBlock{
					Source: "narrative",
					Text:   truncate(redactSecrets(p.Text), maxReasoningBytes),
				})
			}
		}
	}
	return blocks
}
