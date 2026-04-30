package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/phuc-nt/dandori-cli/internal/serverdb"
)

type IngestRequest struct {
	WorkstationID string        `json:"workstation_id"`
	Runs          []IngestRun   `json:"runs"`
	Events        []IngestEvent `json:"events"`
}

type IngestRun struct {
	ID               string   `json:"id"`
	JiraIssueKey     *string  `json:"jira_issue_key"`
	JiraSprintID     *string  `json:"jira_sprint_id"`
	AgentName        string   `json:"agent_name"`
	AgentType        string   `json:"agent_type"`
	User             string   `json:"user"`
	CWD              *string  `json:"cwd"`
	GitRemote        *string  `json:"git_remote"`
	GitHeadBefore    *string  `json:"git_head_before"`
	GitHeadAfter     *string  `json:"git_head_after"`
	Command          *string  `json:"command"`
	StartedAt        string   `json:"started_at"`
	EndedAt          *string  `json:"ended_at"`
	DurationSec      *float64 `json:"duration_sec"`
	ExitCode         *int     `json:"exit_code"`
	Status           string   `json:"status"`
	SessionID        *string  `json:"session_id"`
	InputTokens      int      `json:"input_tokens"`
	OutputTokens     int      `json:"output_tokens"`
	CacheReadTokens  int      `json:"cache_read_tokens"`
	CacheWriteTokens int      `json:"cache_write_tokens"`
	Model            *string  `json:"model"`
	CostUSD          float64  `json:"cost_usd"`
}

type IngestEvent struct {
	RunID     string `json:"run_id"`
	Layer     int    `json:"layer"`
	EventType string `json:"event_type"`
	Data      any    `json:"data"`
	Timestamp string `json:"ts"`
}

type IngestResponse struct {
	Accepted int `json:"accepted"`
	Errors   int `json:"errors"`
}

func (s *Server) handleIngestEvents(w http.ResponseWriter, r *http.Request) {
	var req IngestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	accepted := 0
	errors := 0

	for _, run := range req.Runs {
		startedAt, _ := time.Parse(time.RFC3339, run.StartedAt)
		var endedAt *time.Time
		if run.EndedAt != nil {
			t, _ := time.Parse(time.RFC3339, *run.EndedAt)
			endedAt = &t
		}

		dbRun := &serverdb.Run{
			ID:               run.ID,
			WorkstationID:    &req.WorkstationID,
			JiraIssueKey:     run.JiraIssueKey,
			JiraSprintID:     run.JiraSprintID,
			AgentName:        run.AgentName,
			AgentType:        run.AgentType,
			UserName:         run.User,
			CWD:              run.CWD,
			GitRemote:        run.GitRemote,
			GitHeadBefore:    run.GitHeadBefore,
			GitHeadAfter:     run.GitHeadAfter,
			Command:          run.Command,
			StartedAt:        startedAt,
			EndedAt:          endedAt,
			DurationSec:      run.DurationSec,
			ExitCode:         run.ExitCode,
			Status:           run.Status,
			SessionID:        run.SessionID,
			InputTokens:      run.InputTokens,
			OutputTokens:     run.OutputTokens,
			CacheReadTokens:  run.CacheReadTokens,
			CacheWriteTokens: run.CacheWriteTokens,
			Model:            run.Model,
			CostUSD:          run.CostUSD,
		}

		if err := s.db.UpsertRun(ctx, dbRun); err != nil {
			errors++
		} else {
			accepted++
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(IngestResponse{
		Accepted: accepted,
		Errors:   errors,
	})
}
