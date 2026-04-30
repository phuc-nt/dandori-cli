package model

import (
	"database/sql"
	"time"
)

type RunStatus string

const (
	StatusRunning   RunStatus = "running"
	StatusDone      RunStatus = "done"
	StatusError     RunStatus = "error"
	StatusCancelled RunStatus = "cancelled"
)

type Run struct {
	ID               string
	JiraIssueKey     sql.NullString
	JiraSprintID     sql.NullString
	AgentName        string
	AgentType        string
	User             string
	WorkstationID    string
	CWD              sql.NullString
	GitRemote        sql.NullString
	GitHeadBefore    sql.NullString
	GitHeadAfter     sql.NullString
	Command          sql.NullString
	StartedAt        time.Time
	EndedAt          sql.NullTime
	DurationSec      sql.NullFloat64
	ExitCode         sql.NullInt32
	Status           RunStatus
	SessionID        sql.NullString
	InputTokens      int
	OutputTokens     int
	CacheReadTokens  int
	CacheWriteTokens int
	Model            sql.NullString
	CostUSD          float64
	EngineerName     string // Jira issue assignee DisplayName; empty if unassigned
	Synced           bool
	CreatedAt        time.Time
}

func (r *Run) IsComplete() bool {
	return r.Status == StatusDone || r.Status == StatusError || r.Status == StatusCancelled
}

func (r *Run) TotalTokens() int {
	return r.InputTokens + r.OutputTokens
}
