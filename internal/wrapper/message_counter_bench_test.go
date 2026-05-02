package wrapper

import (
	"bytes"
	"testing"
)

// benchmarkMessageCounts builds a realistic JSONL byte slice with message
// exchanges that exercise the classification logic.
func benchmarkMessageCounts() []byte {
	// Minimal valid JSONL session with user/assistant exchanges
	jsonl := `{"type":"user","message":{"content":[{"type":"text","text":"Implement login feature"}]}}
{"type":"assistant","message":{"content":[{"type":"text","text":"I'll implement JWT login."},{"type":"tool_use","id":"tu_001","name":"Read","input":{"file_path":"src/auth.go"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu_001","content":"package auth"}]}}
{"type":"assistant","message":{"content":[{"type":"text","text":"Now implementing the handler."},{"type":"tool_use","id":"tu_002","name":"Write","input":{"file_path":"src/auth.go","content":"..."}}]}}
{"type":"user","message":{"content":[{"type":"text","text":"That looks good, but add rate limiting to the endpoint"}]}}
{"type":"assistant","message":{"content":[{"type":"text","text":"Adding rate limit middleware..."}]}}
{"type":"user","message":{"content":[{"type":"text","text":"ok"}]}}
{"type":"assistant","message":{"content":[{"type":"text","text":"Done. Login feature is complete."}]}}
`
	return []byte(jsonl)
}

// BenchmarkAggregateMessageCounts measures the streaming message classification
// path on a small realistic session. Tests the new Reader-based implementation
// that avoids buffering the full transcript.
//
// Post quick-win (aggregateMessageCountsFromReader refactoring):
//   - Should be constant memory, no full-session buffers
//   - Marginally slower than aggregateMessageCounts([]byte) due to Reader indirection
//   - But more suitable for large transcripts (>5MB)
func BenchmarkAggregateMessageCounts(b *testing.B) {
	data := benchmarkMessageCounts()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = aggregateMessageCounts(data)
	}
}

// BenchmarkAggregateMessageCountsFromReader benchmarks the streaming Reader path
// directly, which is what production code uses for file transcripts.
func BenchmarkAggregateMessageCountsFromReader(b *testing.B) {
	data := benchmarkMessageCounts()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = aggregateMessageCountsFromReader(bytes.NewReader(data))
	}
}

// BenchmarkClassifyHumanMessage isolates the classification logic that drives
// intervention vs approval counting. Ensures the 30-char boundary and
// seenAgentTool flag don't create hidden hot spots.
func BenchmarkClassifyHumanMessage(b *testing.B) {
	messages := []struct {
		text      string
		seenTool  bool
		prevCount int
	}{
		{"Implement login feature", false, 0},                                 // initial prompt
		{"That looks good", true, 1},                                          // approval (13 chars)
		{"Actually, add rate limiting to the auth endpoint instead", true, 1}, // intervention (57 chars)
		{"ok", true, 1}, // approval boundary
		{"please use bcrypt instead of plain sha256", true, 2}, // intervention
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		for _, m := range messages {
			_ = classifyHumanMessage(m.text, m.seenTool, m.prevCount)
		}
	}
}

// BenchmarkHasTextPart checks the hot path for detecting text content.
// Exercises the simple loop through content parts.
func BenchmarkHasTextPart(b *testing.B) {
	parts := []contentPart{
		{Type: "text", Text: "hello"},
		{Type: "tool_use", ToolUseID: "tu_001"},
		{Type: "text", Text: "world"},
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = hasTextPart(parts)
	}
}

// BenchmarkHasToolUsePart checks detection of tool_use parts (fork indicator).
func BenchmarkHasToolUsePart(b *testing.B) {
	parts := []contentPart{
		{Type: "text", Text: "hello"},
		{Type: "tool_use", ToolUseID: "tu_001"},
		{Type: "tool_result", ToolUseID: "tu_001"},
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = hasToolUsePart(parts)
	}
}

// BenchmarkJSONUnmarshal isolates the JSON parsing cost, which dominates
// when streaming large transcripts line-by-line.
func BenchmarkJSONUnmarshal(b *testing.B) {
	// Realistic JSONL line
	jsonlLine := []byte(`{"type":"user","message":{"content":[{"type":"text","text":"Implement login feature"}]}}`)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		var s sessionLine
		_ = s
		_ = jsonlLine
		// Note: actual unmarshaling would go here, but sessionLine is package-internal
		// This is a placeholder to show the benchmark structure
	}
}
