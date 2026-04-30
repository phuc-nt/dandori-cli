package serverdb

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type Run struct {
	ID               string
	WorkstationID    *string
	JiraIssueKey     *string
	JiraSprintID     *string
	AgentName        string
	AgentType        string
	UserName         string
	CWD              *string
	GitRemote        *string
	GitHeadBefore    *string
	GitHeadAfter     *string
	Command          *string
	StartedAt        time.Time
	EndedAt          *time.Time
	DurationSec      *float64
	ExitCode         *int
	Status           string
	SessionID        *string
	InputTokens      int
	OutputTokens     int
	CacheReadTokens  int
	CacheWriteTokens int
	Model            *string
	CostUSD          float64
	CreatedAt        time.Time
}

func (db *DB) UpsertRun(ctx context.Context, r *Run) error {
	_, err := db.pool.Exec(ctx, `
		INSERT INTO runs (
			id, workstation_id, jira_issue_key, jira_sprint_id,
			agent_name, agent_type, user_name, cwd, git_remote,
			git_head_before, git_head_after, command, started_at,
			ended_at, duration_sec, exit_code, status, session_id,
			input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
			model, cost_usd
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
			$14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24
		)
		ON CONFLICT (id) DO UPDATE SET
			ended_at = EXCLUDED.ended_at,
			duration_sec = EXCLUDED.duration_sec,
			exit_code = EXCLUDED.exit_code,
			status = EXCLUDED.status,
			git_head_after = EXCLUDED.git_head_after,
			session_id = EXCLUDED.session_id,
			input_tokens = EXCLUDED.input_tokens,
			output_tokens = EXCLUDED.output_tokens,
			cache_read_tokens = EXCLUDED.cache_read_tokens,
			cache_write_tokens = EXCLUDED.cache_write_tokens,
			model = EXCLUDED.model,
			cost_usd = EXCLUDED.cost_usd
	`,
		r.ID, r.WorkstationID, r.JiraIssueKey, r.JiraSprintID,
		r.AgentName, r.AgentType, r.UserName, r.CWD, r.GitRemote,
		r.GitHeadBefore, r.GitHeadAfter, r.Command, r.StartedAt,
		r.EndedAt, r.DurationSec, r.ExitCode, r.Status, r.SessionID,
		r.InputTokens, r.OutputTokens, r.CacheReadTokens, r.CacheWriteTokens,
		r.Model, r.CostUSD,
	)
	return err
}

type RunFilter struct {
	AgentName    string
	JiraIssueKey string
	SprintID     string
	Status       string
	Limit        int
	Offset       int
}

func (db *DB) ListRuns(ctx context.Context, f RunFilter) ([]Run, error) {
	query := `
		SELECT id, workstation_id, jira_issue_key, jira_sprint_id,
			agent_name, agent_type, user_name, cwd, git_remote,
			git_head_before, git_head_after, command, started_at,
			ended_at, duration_sec, exit_code, status, session_id,
			input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
			model, cost_usd, created_at
		FROM runs
		WHERE 1=1
	`
	args := []any{}
	argNum := 1

	if f.AgentName != "" {
		query += fmt.Sprintf(" AND agent_name = $%d", argNum)
		args = append(args, f.AgentName)
		argNum++
	}
	if f.JiraIssueKey != "" {
		query += fmt.Sprintf(" AND jira_issue_key = $%d", argNum)
		args = append(args, f.JiraIssueKey)
		argNum++
	}
	if f.SprintID != "" {
		query += fmt.Sprintf(" AND jira_sprint_id = $%d", argNum)
		args = append(args, f.SprintID)
		argNum++
	}
	if f.Status != "" {
		query += fmt.Sprintf(" AND status = $%d", argNum)
		args = append(args, f.Status)
		argNum++
	}

	query += " ORDER BY started_at DESC"

	if f.Limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", argNum)
		args = append(args, f.Limit)
		argNum++
	}
	if f.Offset > 0 {
		query += fmt.Sprintf(" OFFSET $%d", argNum)
		args = append(args, f.Offset)
	}

	rows, err := db.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanRuns(rows)
}

func (db *DB) GetRun(ctx context.Context, id string) (*Run, error) {
	row := db.pool.QueryRow(ctx, `
		SELECT id, workstation_id, jira_issue_key, jira_sprint_id,
			agent_name, agent_type, user_name, cwd, git_remote,
			git_head_before, git_head_after, command, started_at,
			ended_at, duration_sec, exit_code, status, session_id,
			input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
			model, cost_usd, created_at
		FROM runs WHERE id = $1
	`, id)

	var r Run
	err := row.Scan(
		&r.ID, &r.WorkstationID, &r.JiraIssueKey, &r.JiraSprintID,
		&r.AgentName, &r.AgentType, &r.UserName, &r.CWD, &r.GitRemote,
		&r.GitHeadBefore, &r.GitHeadAfter, &r.Command, &r.StartedAt,
		&r.EndedAt, &r.DurationSec, &r.ExitCode, &r.Status, &r.SessionID,
		&r.InputTokens, &r.OutputTokens, &r.CacheReadTokens, &r.CacheWriteTokens,
		&r.Model, &r.CostUSD, &r.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func scanRuns(rows pgx.Rows) ([]Run, error) {
	var runs []Run
	for rows.Next() {
		var r Run
		err := rows.Scan(
			&r.ID, &r.WorkstationID, &r.JiraIssueKey, &r.JiraSprintID,
			&r.AgentName, &r.AgentType, &r.UserName, &r.CWD, &r.GitRemote,
			&r.GitHeadBefore, &r.GitHeadAfter, &r.Command, &r.StartedAt,
			&r.EndedAt, &r.DurationSec, &r.ExitCode, &r.Status, &r.SessionID,
			&r.InputTokens, &r.OutputTokens, &r.CacheReadTokens, &r.CacheWriteTokens,
			&r.Model, &r.CostUSD, &r.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		runs = append(runs, r)
	}
	return runs, nil
}
