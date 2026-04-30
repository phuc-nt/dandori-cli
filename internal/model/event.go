package model

import "time"

type EventLayer int

const (
	LayerWrapper  EventLayer = 1
	LayerTailer   EventLayer = 2
	LayerSkill    EventLayer = 3
	LayerSemantic EventLayer = 4
)

type Event struct {
	ID        int64
	RunID     string
	Layer     EventLayer
	EventType string
	Data      string
	Timestamp time.Time
	Synced    bool
}

type WrapperEvent struct {
	Type      string `json:"type"`
	RunID     string `json:"run_id"`
	Command   string `json:"command,omitempty"`
	ExitCode  int    `json:"exit_code,omitempty"`
	StartedAt string `json:"started_at,omitempty"`
	EndedAt   string `json:"ended_at,omitempty"`
}

type TailerEvent struct {
	Type         string  `json:"type"`
	RunID        string  `json:"run_id"`
	SessionID    string  `json:"session_id,omitempty"`
	InputTokens  int     `json:"input_tokens,omitempty"`
	OutputTokens int     `json:"output_tokens,omitempty"`
	CacheRead    int     `json:"cache_read,omitempty"`
	CacheWrite   int     `json:"cache_write,omitempty"`
	Model        string  `json:"model,omitempty"`
	CostUSD      float64 `json:"cost_usd,omitempty"`
}

type SkillEvent struct {
	Type    string `json:"type"`
	RunID   string `json:"run_id"`
	Action  string `json:"action,omitempty"`
	Details any    `json:"details,omitempty"`
}
