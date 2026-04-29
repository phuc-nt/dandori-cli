package wrapper

import (
	"bytes"
	"encoding/json"

	"github.com/phuc-nt/dandori-cli/internal/db"
)

// MessageCounts is the per-run summary of human/agent text exchanges that
// drives G7's intervention_rate metric. Persisted into the four runs columns
// added by migration v5; aggregated upward into task_attribution by Phase 03.
type MessageCounts struct {
	HumanTotal    int // every classified human-text message (initial + intervention + approval)
	AgentTotal    int // every assistant line carrying at least one text part
	Interventions int // human texts ≥30 chars after at least one agent tool_use
	Approvals     int // human texts <30 chars after at least one agent tool_use
}

// aggregateMessageCounts walks a JSONL transcript byte slice and returns the
// counts. Stateful: tracks whether we've seen an agent tool_use yet so the
// classifier can distinguish initial framing from mid-session feedback.
// Malformed or unrecognised lines are skipped silently — the tailer reads
// while Claude is still writing, so half-flushed records must not poison
// the count.
func aggregateMessageCounts(jsonl []byte) MessageCounts {
	var counts MessageCounts
	seenAgentTool := false
	humanSeen := 0

	for _, line := range bytes.Split(jsonl, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}

		var s sessionLine
		if err := json.Unmarshal(line, &s); err != nil {
			continue
		}
		if s.Type != "assistant" && s.Type != "user" {
			continue
		}
		if len(s.Message.Content) == 0 {
			continue
		}

		var parts []contentPart
		if err := json.Unmarshal(s.Message.Content, &parts); err != nil {
			continue
		}

		switch s.Type {
		case "assistant":
			if hasTextPart(parts) {
				counts.AgentTotal++
			}
			if hasToolUsePart(parts) {
				seenAgentTool = true
			}
		case "user":
			for _, p := range parts {
				if p.Type != "text" {
					continue
				}
				class := classifyHumanMessage(p.Text, seenAgentTool, humanSeen)
				if class == classUncounted {
					continue
				}
				counts.HumanTotal++
				humanSeen++
				switch class {
				case classIntervention:
					counts.Interventions++
				case classApproval:
					counts.Approvals++
				}
				// Only the first text part of a user line counts as a
				// "message" — the second is content authoring noise.
				break
			}
		}
	}

	return counts
}

func hasTextPart(parts []contentPart) bool {
	for _, p := range parts {
		if p.Type == "text" {
			return true
		}
	}
	return false
}

func hasToolUsePart(parts []contentPart) bool {
	for _, p := range parts {
		if p.Type == "tool_use" {
			return true
		}
	}
	return false
}

// persistMessageCounts writes the four counters into the runs row for the
// completed session. Called after the wrapper aggregates from the transcript;
// idempotent — re-running with the same counts is a no-op UPDATE.
func persistMessageCounts(d *db.LocalDB, runID string, c MessageCounts) error {
	_, err := d.Exec(`UPDATE runs SET
		human_message_count = ?,
		agent_message_count = ?,
		human_intervention_count = ?,
		human_approval_count = ?
		WHERE id = ?`,
		c.HumanTotal, c.AgentTotal, c.Interventions, c.Approvals, runID)
	return err
}
