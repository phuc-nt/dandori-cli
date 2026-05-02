package intent

import (
	"bufio"
	"encoding/json"
	"log/slog"
	"os"
)

// sessionLine is a minimal view of a Claude JSONL transcript line.
// Only the fields needed for intent extraction are decoded; heavy payloads
// (tool inputs, tool results) are left as raw JSON to avoid allocations.
type sessionLine struct {
	Type    string `json:"type"`
	Message struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// contentPart represents one element inside message.content.
type contentPart struct {
	Type      string `json:"type"`                  // text | thinking | tool_use | tool_result
	Text      string `json:"text,omitempty"`        // text / thinking body
	Thinking  string `json:"thinking,omitempty"`    // extended thinking (some models)
	ToolUseID string `json:"tool_use_id,omitempty"` // tool_result linkage
}

// parsedLine is the minimal decoded representation passed to the extractor.
type parsedLine struct {
	Type  string        // "user" | "assistant"
	Parts []contentPart // decoded content parts
}

// Walk opens path and streams it line-by-line, yielding parsedLine values via
// the callback. Malformed lines are logged at DEBUG and skipped — callers must
// never see a partial line. Walk returns only I/O errors (file not found, etc.);
// JSON parse errors on individual lines are not returned.
func Walk(path string, fn func(parsedLine)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// Raise buffer limit to 4 MB — some JSONL lines (tool results) can be large.
	scanner.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)

	for scanner.Scan() {
		raw := scanner.Bytes()
		if len(raw) == 0 {
			continue
		}

		var sl sessionLine
		if err := json.Unmarshal(raw, &sl); err != nil {
			slog.Debug("intent/walker: skip malformed line", "error", err)
			continue
		}
		if sl.Type != "user" && sl.Type != "assistant" {
			continue
		}
		if len(sl.Message.Content) == 0 {
			continue
		}

		var parts []contentPart
		if err := json.Unmarshal(sl.Message.Content, &parts); err != nil {
			slog.Debug("intent/walker: skip malformed content", "error", err)
			continue
		}

		fn(parsedLine{Type: sl.Type, Parts: parts})
	}

	return scanner.Err()
}
