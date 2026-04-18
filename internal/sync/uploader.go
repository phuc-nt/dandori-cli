package sync

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/phuc-nt/dandori-cli/internal/db"
)

type Uploader struct {
	serverURL     string
	apiKey        string
	workstationID string
	httpClient    *http.Client
}

type UploadRequest struct {
	WorkstationID string      `json:"workstation_id"`
	Runs          []RunData   `json:"runs"`
	Events        []EventData `json:"events"`
}

type RunData struct {
	ID               string   `json:"id"`
	JiraIssueKey     *string  `json:"jira_issue_key,omitempty"`
	JiraSprintID     *string  `json:"jira_sprint_id,omitempty"`
	AgentName        string   `json:"agent_name"`
	AgentType        string   `json:"agent_type"`
	User             string   `json:"user"`
	CWD              *string  `json:"cwd,omitempty"`
	GitRemote        *string  `json:"git_remote,omitempty"`
	GitHeadBefore    *string  `json:"git_head_before,omitempty"`
	GitHeadAfter     *string  `json:"git_head_after,omitempty"`
	Command          *string  `json:"command,omitempty"`
	StartedAt        string   `json:"started_at"`
	EndedAt          *string  `json:"ended_at,omitempty"`
	DurationSec      *float64 `json:"duration_sec,omitempty"`
	ExitCode         *int     `json:"exit_code,omitempty"`
	Status           string   `json:"status"`
	SessionID        *string  `json:"session_id,omitempty"`
	InputTokens      int      `json:"input_tokens"`
	OutputTokens     int      `json:"output_tokens"`
	CacheReadTokens  int      `json:"cache_read_tokens"`
	CacheWriteTokens int      `json:"cache_write_tokens"`
	Model            *string  `json:"model,omitempty"`
	CostUSD          float64  `json:"cost_usd"`
}

type EventData struct {
	RunID     string `json:"run_id"`
	Layer     int    `json:"layer"`
	EventType string `json:"event_type"`
	Data      string `json:"data"`
	Timestamp string `json:"ts"`
}

type UploadResponse struct {
	Accepted int `json:"accepted"`
	Errors   int `json:"errors"`
}

func NewUploader(serverURL, apiKey, workstationID string) *Uploader {
	return &Uploader{
		serverURL:     serverURL,
		apiKey:        apiKey,
		workstationID: workstationID,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (u *Uploader) Sync(localDB *db.LocalDB, batchSize int) (*UploadResponse, error) {
	runs, err := u.getUnsyncedRuns(localDB, batchSize)
	if err != nil {
		return nil, fmt.Errorf("get unsynced runs: %w", err)
	}

	events, err := u.getUnsyncedEvents(localDB, batchSize)
	if err != nil {
		return nil, fmt.Errorf("get unsynced events: %w", err)
	}

	if len(runs) == 0 && len(events) == 0 {
		return &UploadResponse{}, nil
	}

	req := UploadRequest{
		WorkstationID: u.workstationID,
		Runs:          runs,
		Events:        events,
	}

	resp, err := u.upload(req)
	if err != nil {
		return nil, err
	}

	if resp.Accepted > 0 {
		if err := u.markRunsSynced(localDB, runs); err != nil {
			return resp, fmt.Errorf("mark runs synced: %w", err)
		}
		if err := u.markEventsSynced(localDB, events); err != nil {
			return resp, fmt.Errorf("mark events synced: %w", err)
		}
	}

	return resp, nil
}

func (u *Uploader) upload(req UploadRequest) (*UploadResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequest(http.MethodPost, u.serverURL+"/api/events", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if u.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+u.apiKey)
	}

	httpResp, err := u.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(httpResp.Body)
		return nil, fmt.Errorf("server error %d: %s", httpResp.StatusCode, string(respBody))
	}

	var resp UploadResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &resp, nil
}

func (u *Uploader) getUnsyncedRuns(localDB *db.LocalDB, limit int) ([]RunData, error) {
	rows, err := localDB.Query(`
		SELECT id, jira_issue_key, jira_sprint_id, agent_name, agent_type,
			user, cwd, git_remote, git_head_before, git_head_after,
			command, started_at, ended_at, duration_sec, exit_code,
			status, session_id, input_tokens, output_tokens,
			cache_read_tokens, cache_write_tokens, model, cost_usd
		FROM runs WHERE synced = 0 LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []RunData
	for rows.Next() {
		var r RunData
		var jiraKey, sprintID, cwd, gitRemote, gitBefore, gitAfter interface{}
		var command, endedAt, sessionID, model interface{}
		var durationSec, exitCode interface{}

		err := rows.Scan(
			&r.ID, &jiraKey, &sprintID, &r.AgentName, &r.AgentType,
			&r.User, &cwd, &gitRemote, &gitBefore, &gitAfter,
			&command, &r.StartedAt, &endedAt, &durationSec, &exitCode,
			&r.Status, &sessionID, &r.InputTokens, &r.OutputTokens,
			&r.CacheReadTokens, &r.CacheWriteTokens, &model, &r.CostUSD,
		)
		if err != nil {
			continue
		}

		if s, ok := jiraKey.(string); ok && s != "" {
			r.JiraIssueKey = &s
		}
		if s, ok := sprintID.(string); ok && s != "" {
			r.JiraSprintID = &s
		}
		if s, ok := cwd.(string); ok && s != "" {
			r.CWD = &s
		}
		if s, ok := gitRemote.(string); ok && s != "" {
			r.GitRemote = &s
		}
		if s, ok := gitBefore.(string); ok && s != "" {
			r.GitHeadBefore = &s
		}
		if s, ok := gitAfter.(string); ok && s != "" {
			r.GitHeadAfter = &s
		}
		if s, ok := command.(string); ok && s != "" {
			r.Command = &s
		}
		if s, ok := endedAt.(string); ok && s != "" {
			r.EndedAt = &s
		}
		if s, ok := sessionID.(string); ok && s != "" {
			r.SessionID = &s
		}
		if s, ok := model.(string); ok && s != "" {
			r.Model = &s
		}
		if f, ok := durationSec.(float64); ok {
			r.DurationSec = &f
		}
		if i, ok := exitCode.(int64); ok {
			ii := int(i)
			r.ExitCode = &ii
		}

		runs = append(runs, r)
	}

	return runs, nil
}

func (u *Uploader) getUnsyncedEvents(localDB *db.LocalDB, limit int) ([]EventData, error) {
	rows, err := localDB.Query(`
		SELECT run_id, layer, event_type, data, ts
		FROM events WHERE synced = 0 LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []EventData
	for rows.Next() {
		var e EventData
		if err := rows.Scan(&e.RunID, &e.Layer, &e.EventType, &e.Data, &e.Timestamp); err != nil {
			continue
		}
		events = append(events, e)
	}

	return events, nil
}

func (u *Uploader) markRunsSynced(localDB *db.LocalDB, runs []RunData) error {
	for _, r := range runs {
		if _, err := localDB.Exec(`UPDATE runs SET synced = 1 WHERE id = ?`, r.ID); err != nil {
			return err
		}
	}
	return nil
}

func (u *Uploader) markEventsSynced(localDB *db.LocalDB, events []EventData) error {
	for _, e := range events {
		if _, err := localDB.Exec(`UPDATE events SET synced = 1 WHERE run_id = ? AND event_type = ?`, e.RunID, e.EventType); err != nil {
			return err
		}
	}
	return nil
}
