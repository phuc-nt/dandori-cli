package model

import "time"

type AuditAction string

const (
	AuditRunStarted    AuditAction = "run_started"
	AuditRunCompleted  AuditAction = "run_completed"
	AuditRunFailed     AuditAction = "run_failed"
	AuditTaskAssigned  AuditAction = "task_assigned"
	AuditConfigChanged AuditAction = "config_changed"
	AuditSyncCompleted AuditAction = "sync_completed"
)

type AuditEvent struct {
	ID         int64
	PrevHash   string
	CurrHash   string
	Actor      string
	Action     AuditAction
	EntityType string
	EntityID   string
	Details    string
	Timestamp  time.Time
}
