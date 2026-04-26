package wrapper

import (
	"encoding/json"
	"os"
	"testing"
)

func TestParseLineForEvents_AssistantWithToolUse(t *testing.T) {
	line := []byte(`{"type":"assistant","message":{"id":"m1","model":"claude-opus-4-7","content":[{"type":"text","text":"hi"},{"type":"tool_use","id":"toolu_X","name":"Read","input":{"file_path":"/tmp/a"}}]}}`)
	events := parseLineForEvents(line)
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].Type != "tool.use" {
		t.Fatalf("type=%q, want tool.use", events[0].Type)
	}
	if events[0].Payload["tool"] != "Read" {
		t.Errorf("tool=%v, want Read", events[0].Payload["tool"])
	}
	if events[0].Payload["tool_use_id"] != "toolu_X" {
		t.Errorf("tool_use_id=%v, want toolu_X", events[0].Payload["tool_use_id"])
	}
	keys, _ := events[0].Payload["input_keys"].([]string)
	if len(keys) != 1 || keys[0] != "file_path" {
		t.Errorf("input_keys=%v, want [file_path]", events[0].Payload["input_keys"])
	}
}

func TestParseLineForEvents_AssistantMultipleToolUses(t *testing.T) {
	line := []byte(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"a","name":"Read","input":{}},{"type":"tool_use","id":"b","name":"Bash","input":{"command":"ls"}}]}}`)
	events := parseLineForEvents(line)
	if len(events) != 2 {
		t.Fatalf("got %d, want 2", len(events))
	}
	tools := []string{events[0].Payload["tool"].(string), events[1].Payload["tool"].(string)}
	if tools[0] != "Read" || tools[1] != "Bash" {
		t.Errorf("tools=%v, want [Read Bash]", tools)
	}
}

func TestParseLineForEvents_UserWithToolResult(t *testing.T) {
	line := []byte(`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_R1","is_error":false,"content":"some output here"}]}}`)
	events := parseLineForEvents(line)
	if len(events) != 1 {
		t.Fatalf("got %d, want 1", len(events))
	}
	if events[0].Type != "tool.result" {
		t.Fatalf("type=%q, want tool.result", events[0].Type)
	}
	if events[0].Payload["tool_use_id"] != "toolu_R1" {
		t.Errorf("tool_use_id=%v", events[0].Payload["tool_use_id"])
	}
	if events[0].Payload["success"] != true {
		t.Errorf("success=%v, want true", events[0].Payload["success"])
	}
	if size, ok := events[0].Payload["output_size"].(int); !ok || size <= 0 {
		t.Errorf("output_size=%v", events[0].Payload["output_size"])
	}
}

func TestParseLineForEvents_UserWithToolResultError(t *testing.T) {
	line := []byte(`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_B1","is_error":true,"content":"command not found"}]}}`)
	events := parseLineForEvents(line)
	if len(events) != 1 {
		t.Fatalf("got %d, want 1", len(events))
	}
	if events[0].Payload["success"] != false {
		t.Errorf("success=%v, want false", events[0].Payload["success"])
	}
}

func TestParseLineForEvents_SkillToolUseEmitsBoth(t *testing.T) {
	// A "Skill" tool_use should emit both tool.use and skill.invoke so analytics
	// can query either dimension without double-counting tool work.
	line := []byte(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_S","name":"Skill","input":{"skill":"ck-plan","args":"x"}}]}}`)
	events := parseLineForEvents(line)
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2 (tool.use + skill.invoke)", len(events))
	}
	types := map[string]bool{events[0].Type: true, events[1].Type: true}
	if !types["tool.use"] || !types["skill.invoke"] {
		t.Fatalf("types=%v, want tool.use + skill.invoke", types)
	}
	for _, e := range events {
		if e.Type == "skill.invoke" && e.Payload["skill_name"] != "ck-plan" {
			t.Errorf("skill_name=%v, want ck-plan", e.Payload["skill_name"])
		}
	}
}

func TestParseLineForEvents_NonEventTypes(t *testing.T) {
	cases := [][]byte{
		[]byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"hi"}]}}`),
		[]byte(`{"type":"file-history-snapshot","data":"x"}`),
		[]byte(`{"type":"user","message":{"content":[{"type":"text","text":"plain"}]}}`),
		[]byte(`{"type":"queue-operation"}`),
	}
	for i, line := range cases {
		if got := parseLineForEvents(line); len(got) != 0 {
			t.Errorf("case %d: got %d events, want 0: %+v", i, len(got), got)
		}
	}
}

func TestParseLineForEvents_InvalidJSON(t *testing.T) {
	if got := parseLineForEvents([]byte("not json")); len(got) != 0 {
		t.Errorf("got %d events for invalid json, want 0", len(got))
	}
	if got := parseLineForEvents([]byte("")); len(got) != 0 {
		t.Errorf("got %d events for empty, want 0", len(got))
	}
}

// TestParseLineForEvents_NoLeakOfRawValues asserts that input values are NOT
// included in the payload — only their keys. (Privacy: tool inputs may carry
// secrets like .env paths or webfetch URLs.)
func TestParseLineForEvents_NoLeakOfRawValues(t *testing.T) {
	line := []byte(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"x","name":"Read","input":{"file_path":"/secrets/.env"}}]}}`)
	events := parseLineForEvents(line)
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	raw, _ := json.Marshal(events[0].Payload)
	if string(raw) == "" {
		t.Fatal("empty payload")
	}
	if containsValue(raw, "/secrets/.env") {
		t.Errorf("payload leaked input value: %s", raw)
	}
}

func containsValue(b []byte, needle string) bool {
	return string(b) != "" && bytesContains(b, []byte(needle))
}

func bytesContains(haystack, needle []byte) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		match := true
		for j := 0; j < len(needle); j++ {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// TestParseLogFromOffset_BackwardCompatTokens verifies the existing token
// extraction path still works after refactor.
func TestParseLogFromOffset_BackwardCompatTokens(t *testing.T) {
	usage, _ := parseLogFromOffset("testdata/session-with-tools.jsonl", 0)
	// Sum across the 4 assistant messages: 100+150+200+80=530 input,
	// 20+15+30+10=75 output. mergeUsage sums across calls.
	if usage.Input == 0 || usage.Output == 0 {
		t.Errorf("token regression: usage=%+v", usage)
	}
	if usage.Model != "claude-opus-4-7" {
		t.Errorf("model=%q, want claude-opus-4-7", usage.Model)
	}
}

// TestParseLogFromOffset_FixtureCounts verifies the fixture itself yields the
// expected number of events when scanned line by line.
func TestParseLogFromOffset_FixtureCounts(t *testing.T) {
	data, err := os.ReadFile("testdata/session-with-tools.jsonl")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var toolUse, toolResult, skillInvoke int
	for _, line := range splitLines(data) {
		for _, e := range parseLineForEvents(line) {
			switch e.Type {
			case "tool.use":
				toolUse++
			case "tool.result":
				toolResult++
			case "skill.invoke":
				skillInvoke++
			}
		}
	}
	if toolUse < 4 {
		t.Errorf("tool.use=%d, want >=4", toolUse)
	}
	if toolResult < 4 {
		t.Errorf("tool.result=%d, want >=4", toolResult)
	}
	if skillInvoke < 1 {
		t.Errorf("skill.invoke=%d, want >=1", skillInvoke)
	}
}

func splitLines(b []byte) [][]byte {
	var out [][]byte
	start := 0
	for i, c := range b {
		if c == '\n' {
			if i > start {
				out = append(out, b[start:i])
			}
			start = i + 1
		}
	}
	if start < len(b) {
		out = append(out, b[start:])
	}
	return out
}
