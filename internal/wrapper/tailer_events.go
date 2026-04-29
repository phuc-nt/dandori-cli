package wrapper

import (
	"encoding/json"
	"sort"
)

// SessionEvent is a Layer-3 semantic event extracted from a Claude session
// JSONL line. Pure data — Recorder writes them to the events table.
type SessionEvent struct {
	Type    string         // "tool.use" | "tool.result" | "skill.invoke"
	Payload map[string]any // serialised as the events.data JSON column
}

// sessionLine is the discriminator for top-level JSONL records. We only care
// about message-bearing types (assistant/user); other types (file-history-snapshot,
// queue-operation, last-prompt, attachment) are ignored.
type sessionLine struct {
	Type    string `json:"type"`
	Message struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// contentPart matches both assistant content (tool_use, text, thinking) and
// user content (tool_result, text). Only the fields needed for events are
// pulled — input/content payloads are inspected only for keys/sizes, never
// re-emitted, to avoid leaking secrets.
type contentPart struct {
	Type       string          `json:"type"`
	ID         string          `json:"id,omitempty"`          // tool_use id
	Name       string          `json:"name,omitempty"`        // tool name
	Input      json.RawMessage `json:"input,omitempty"`       // tool_use input (keys only)
	ToolUseID  string          `json:"tool_use_id,omitempty"` // tool_result link
	IsError    *bool           `json:"is_error,omitempty"`    // tool_result error flag
	RawContent json.RawMessage `json:"content,omitempty"`     // tool_result content (size only)
	Text       string          `json:"text,omitempty"`        // text part body (G7 message classifier)
}

// parseLineForEvents extracts zero-or-more SessionEvents from a single JSONL
// line. Pure: no DB writes, no I/O. Unknown types and malformed lines yield
// nil so the tailer can keep streaming without surfacing errors.
func parseLineForEvents(line []byte) []SessionEvent {
	var s sessionLine
	if err := json.Unmarshal(line, &s); err != nil {
		return nil
	}
	if s.Type != "assistant" && s.Type != "user" {
		return nil
	}
	if len(s.Message.Content) == 0 {
		return nil
	}

	var parts []contentPart
	if err := json.Unmarshal(s.Message.Content, &parts); err != nil {
		return nil
	}

	var events []SessionEvent
	for _, p := range parts {
		switch p.Type {
		case "tool_use":
			events = append(events, sessionEventFromToolUse(p)...)
		case "tool_result":
			events = append(events, sessionEventFromToolResult(p))
		}
	}
	return events
}

func sessionEventFromToolUse(p contentPart) []SessionEvent {
	payload := map[string]any{
		"tool":        p.Name,
		"tool_use_id": p.ID,
		"input_keys":  inputKeys(p.Input),
	}
	out := []SessionEvent{{Type: "tool.use", Payload: payload}}

	if p.Name == "Skill" {
		skillName := skillNameFromInput(p.Input)
		out = append(out, SessionEvent{
			Type: "skill.invoke",
			Payload: map[string]any{
				"skill_name":  skillName,
				"tool_use_id": p.ID,
			},
		})
	}
	return out
}

func sessionEventFromToolResult(p contentPart) SessionEvent {
	success := true
	if p.IsError != nil {
		success = !*p.IsError
	}
	return SessionEvent{
		Type: "tool.result",
		Payload: map[string]any{
			"tool_use_id": p.ToolUseID,
			"success":     success,
			"output_size": len(p.RawContent),
		},
	}
}

// inputKeys returns the top-level keys of a tool_use input map, sorted for
// determinism. Returns []string{} (not nil) so JSON marshals as [] not null.
func inputKeys(input json.RawMessage) []string {
	keys := []string{}
	if len(input) == 0 {
		return keys
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(input, &m); err != nil {
		return keys
	}
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// skillNameFromInput pulls the "skill" key out of a Skill tool_use input.
// Returns "" if absent or unparseable — caller still emits the event so that
// invocation count stays accurate even when the name is missing.
func skillNameFromInput(input json.RawMessage) string {
	if len(input) == 0 {
		return ""
	}
	var m struct {
		Skill string `json:"skill"`
	}
	if err := json.Unmarshal(input, &m); err != nil {
		return ""
	}
	return m.Skill
}
