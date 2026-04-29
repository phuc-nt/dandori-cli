package attribution

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/phuc-nt/dandori-cli/internal/db"
)

// MessageCounts mirrors wrapper.MessageCounts to avoid an import cycle.
// Only the fields we aggregate into task_attribution are kept.
type MessageCounts struct {
	HumanTotal    int
	AgentTotal    int
	Interventions int
	Approvals     int
}

// sessionRecord is one runs row loaded for aggregation. Fields use
// nullable/empty-safe types because git_head_* may be null for sessions that
// died before recording.
type sessionRecord struct {
	RunID            string
	GitHeadBefore    string
	GitHeadAfter     string
	InputTokens      int
	OutputTokens     int
	CostUSD          float64
	SessionEndReason string
	HumanMessages    int
	Interventions    int
	Approvals        int
	JiraDoneAt       string
}

// aggregated is the per-task summary built from sessionRecords. Held in
// memory until upsertAttribution writes it to task_attribution.
type aggregated struct {
	SessionCount      int
	TotalAgentTokens  int
	TotalAgentCost    float64
	TotalIterations   int
	TotalHumanMsg     int
	TotalIntervention int
	TotalApproval     int
	Outcomes          map[string]int
	JiraDoneAt        string
}

// ComputeAndPersist loads all done/error runs for jiraKey, computes
// retention against finalHead, aggregates session stats, and writes the
// task_attribution row. No-op when there are no recorded sessions — keeps
// the table sparse for human-only tasks.
func ComputeAndPersist(d *db.LocalDB, jiraKey, repoPath, finalHead string) error {
	sessions, err := loadSessions(d, jiraKey)
	if err != nil {
		return fmt.Errorf("load sessions: %w", err)
	}
	if len(sessions) == 0 {
		return nil
	}

	diffs := make([]SessionDiff, 0, len(sessions))
	for _, s := range sessions {
		if s.GitHeadBefore != "" && s.GitHeadAfter != "" {
			diffs = append(diffs, SessionDiff{HeadBefore: s.GitHeadBefore, HeadAfter: s.GitHeadAfter})
		}
	}
	ret, err := ComputeRetention(repoPath, diffs, finalHead)
	if err != nil {
		return fmt.Errorf("compute retention: %w", err)
	}

	agg := aggregateSessionStats(d, sessions)
	return upsertAttribution(d, jiraKey, ret, agg, finalHead)
}

// loadSessions fetches every done/error run for a Jira key. Returns empty
// slice (not error) when the key has no runs — caller treats that as "skip".
func loadSessions(d *db.LocalDB, jiraKey string) ([]sessionRecord, error) {
	rows, err := d.Query(`SELECT id,
		COALESCE(git_head_before, ''), COALESCE(git_head_after, ''),
		COALESCE(input_tokens, 0), COALESCE(output_tokens, 0), COALESCE(cost_usd, 0),
		COALESCE(session_end_reason, ''),
		COALESCE(human_message_count, 0),
		COALESCE(human_intervention_count, 0),
		COALESCE(human_approval_count, 0),
		COALESCE(ended_at, started_at)
		FROM runs
		WHERE jira_issue_key = ? AND status IN ('done', 'error')
		ORDER BY started_at`, jiraKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []sessionRecord
	for rows.Next() {
		var s sessionRecord
		if err := rows.Scan(&s.RunID, &s.GitHeadBefore, &s.GitHeadAfter,
			&s.InputTokens, &s.OutputTokens, &s.CostUSD,
			&s.SessionEndReason, &s.HumanMessages,
			&s.Interventions, &s.Approvals, &s.JiraDoneAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// aggregateSessionStats sums per-session counters and counts iterations from
// task.iteration.start events. Falls back to a single iteration per session
// when the events table has nothing — older runs predating Layer-3 tracking.
func aggregateSessionStats(d *db.LocalDB, sessions []sessionRecord) aggregated {
	agg := aggregated{
		SessionCount: len(sessions),
		Outcomes:     map[string]int{},
	}
	runIDs := make([]string, 0, len(sessions))
	for _, s := range sessions {
		agg.TotalAgentTokens += s.InputTokens + s.OutputTokens
		agg.TotalAgentCost += s.CostUSD
		agg.TotalHumanMsg += s.HumanMessages
		agg.TotalIntervention += s.Interventions
		agg.TotalApproval += s.Approvals
		if s.SessionEndReason != "" {
			agg.Outcomes[s.SessionEndReason]++
		}
		if s.JiraDoneAt != "" && s.JiraDoneAt > agg.JiraDoneAt {
			agg.JiraDoneAt = s.JiraDoneAt
		}
		runIDs = append(runIDs, s.RunID)
	}
	agg.TotalIterations = countIterationStarts(d, runIDs)
	return agg
}

// countIterationStarts counts task.iteration.start events across the given
// run IDs. Errors are logged-and-ignored: attribution must not fail because
// of an event-table read.
func countIterationStarts(d *db.LocalDB, runIDs []string) int {
	if len(runIDs) == 0 {
		return 0
	}
	// Build IN (?, ?, ...) parameter list.
	placeholders := ""
	args := make([]any, 0, len(runIDs))
	for i, id := range runIDs {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args = append(args, id)
	}
	q := `SELECT COUNT(*) FROM events
		WHERE event_type = 'task.iteration.start' AND run_id IN (` + placeholders + `)`
	var n int
	if err := d.QueryRow(q, args...).Scan(&n); err != nil && err != sql.ErrNoRows {
		return 0
	}
	return n
}

// upsertAttribution writes the aggregated row using INSERT ... ON CONFLICT
// so re-running is idempotent.
func upsertAttribution(d *db.LocalDB, jiraKey string, ret RetentionResult, agg aggregated, finalHead string) error {
	rate := 0.0
	if denom := agg.TotalIntervention + agg.TotalApproval; denom > 0 {
		rate = float64(agg.TotalIntervention) / float64(denom)
	}
	outcomesJSON, err := json.Marshal(agg.Outcomes)
	if err != nil {
		return fmt.Errorf("marshal outcomes: %w", err)
	}
	doneAt := agg.JiraDoneAt
	if doneAt == "" {
		// Should not happen — runs always have started_at — but guard anyway.
		doneAt = "1970-01-01T00:00:00Z"
	}
	_, err = d.Exec(`INSERT INTO task_attribution (
		jira_issue_key, session_count, total_lines_final,
		lines_attributed_agent, lines_attributed_human,
		total_agent_tokens, total_agent_cost_usd,
		total_iterations, total_human_messages,
		total_intervention_count, intervention_rate,
		session_outcomes, git_head_at_jira_done, jira_done_at,
		computed_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))
	ON CONFLICT(jira_issue_key) DO UPDATE SET
		session_count = excluded.session_count,
		total_lines_final = excluded.total_lines_final,
		lines_attributed_agent = excluded.lines_attributed_agent,
		lines_attributed_human = excluded.lines_attributed_human,
		total_agent_tokens = excluded.total_agent_tokens,
		total_agent_cost_usd = excluded.total_agent_cost_usd,
		total_iterations = excluded.total_iterations,
		total_human_messages = excluded.total_human_messages,
		total_intervention_count = excluded.total_intervention_count,
		intervention_rate = excluded.intervention_rate,
		session_outcomes = excluded.session_outcomes,
		git_head_at_jira_done = excluded.git_head_at_jira_done,
		jira_done_at = excluded.jira_done_at,
		computed_at = datetime('now')`,
		jiraKey, agg.SessionCount, ret.TotalLinesFinal,
		ret.LinesAttributedAgent, ret.LinesAttributedHuman,
		agg.TotalAgentTokens, agg.TotalAgentCost,
		agg.TotalIterations, agg.TotalHumanMsg,
		agg.TotalIntervention, rate,
		string(outcomesJSON), finalHead, doneAt,
	)
	return err
}
