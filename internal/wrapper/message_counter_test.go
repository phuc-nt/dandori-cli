package wrapper

import (
	"os"
	"path/filepath"
	"testing"
)

// TestAggregateMessageCounts_FromFixture walks a hand-crafted JSONL transcript
// and asserts the four counters that drive G7 attribution. The fixture
// represents a realistic mixed session: initial human framing, agent text +
// tool_use chain, an approval ("looks good"), then two long human pivots
// (interventions). Tool_result-only user lines must not bump human_total.
func TestAggregateMessageCounts_FromFixture(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "transcript-intervention.jsonl"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	got := aggregateMessageCounts(data)

	// AgentTotal = assistant lines with ≥1 text part (msg_a1 "I'll start...",
	// msg_a4 "Switching to HMAC..."). Pure tool_use lines are actions, not
	// "messages" in attribution semantics — see plan.md definition.
	want := MessageCounts{
		HumanTotal:    4, // initial + approval + 2 interventions
		AgentTotal:    2, // msg_a1 + msg_a4 (text-bearing assistant lines)
		Interventions: 2,
		Approvals:     1,
	}
	if got != want {
		t.Errorf("aggregateMessageCounts mismatch\n  got  %+v\n  want %+v", got, want)
	}
}

// TestAggregateMessageCounts_NoHumanMessages confirms the auto-execution path
// (e.g. one-shot `claude -p "..."` with no follow-up): only the assistant
// lines are counted, intervention/approval stay zero.
func TestAggregateMessageCounts_NoHumanMessages(t *testing.T) {
	jsonl := []byte(`{"type":"assistant","message":{"id":"a1","model":"claude","content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":1,"output_tokens":1}}}
{"type":"assistant","message":{"id":"a2","model":"claude","content":[{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"/x"}}],"usage":{"input_tokens":1,"output_tokens":1}}}
`)
	got := aggregateMessageCounts(jsonl)
	// Only the text-bearing assistant line counts; the tool_use-only line is
	// an action.
	want := MessageCounts{HumanTotal: 0, AgentTotal: 1, Interventions: 0, Approvals: 0}
	if got != want {
		t.Errorf("got %+v want %+v", got, want)
	}
}

// TestAggregateMessageCounts_SkipsToolResultOnlyUserLines covers the trap
// where a `user`-typed JSONL line is purely a tool_result envelope (no text).
// Those are auto-generated; counting them as human messages would inflate
// intervention rates by ~5–10x.
func TestAggregateMessageCounts_SkipsToolResultOnlyUserLines(t *testing.T) {
	jsonl := []byte(`{"type":"user","message":{"content":[{"type":"text","text":"go"}]}}
{"type":"assistant","message":{"id":"a1","model":"c","content":[{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"/x"}}],"usage":{"input_tokens":1,"output_tokens":1}}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","is_error":false,"content":"ok"}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","is_error":false,"content":"ok"}]}}
`)
	got := aggregateMessageCounts(jsonl)
	if got.HumanTotal != 1 {
		t.Errorf("HumanTotal=%d want 1 (only the initial 'go'), got %+v", got.HumanTotal, got)
	}
}

// TestAggregateMessageCounts_MalformedLineSkipped guarantees that one bad line
// in the middle of a transcript doesn't break the whole count — a real-world
// concern because the tailer reads while Claude is still writing.
func TestAggregateMessageCounts_MalformedLineSkipped(t *testing.T) {
	jsonl := []byte(`{"type":"user","message":{"content":[{"type":"text","text":"start"}]}}
this-is-not-json
{"type":"assistant","message":{"id":"a1","model":"c","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}}
`)
	got := aggregateMessageCounts(jsonl)
	if got.HumanTotal != 1 || got.AgentTotal != 1 {
		t.Errorf("malformed line should be skipped, got %+v", got)
	}
}

// TestPersistMessageCounts writes the counts to runs columns and reads back —
// proves the four columns added in v5 round-trip.
func TestPersistMessageCounts(t *testing.T) {
	d := openDBForIterationEnd(t)
	runID := "run-msg-1"
	if _, err := d.Exec(`INSERT INTO runs
		(id, agent_type, user, workstation_id, started_at, status)
		VALUES (?, 'claude_code', 'tester', 'ws-1', '2026-04-29T12:00:00Z', 'done')`, runID); err != nil {
		t.Fatalf("seed run: %v", err)
	}

	counts := MessageCounts{HumanTotal: 4, AgentTotal: 5, Interventions: 2, Approvals: 1}
	if err := persistMessageCounts(d, runID, counts); err != nil {
		t.Fatalf("persist: %v", err)
	}

	var got MessageCounts
	row := d.QueryRow(`SELECT human_message_count, agent_message_count,
		human_intervention_count, human_approval_count
		FROM runs WHERE id=?`, runID)
	if err := row.Scan(&got.HumanTotal, &got.AgentTotal, &got.Interventions, &got.Approvals); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if got != counts {
		t.Errorf("round trip mismatch: got %+v want %+v", got, counts)
	}
}
