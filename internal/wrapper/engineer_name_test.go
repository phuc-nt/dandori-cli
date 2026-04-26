package wrapper

import (
	"database/sql"
	"testing"
	"time"

	"github.com/phuc-nt/dandori-cli/internal/model"
)

// TestInsertRunEngineerName verifies that insertRun persists engineer_name
// when the model.Run carries one (wrapper-owned run with known assignee).
func TestInsertRunEngineerName(t *testing.T) {
	localDB := setupTestDB(t)
	defer localDB.Close()

	run := &model.Run{
		ID:            "testrun0001aabb",
		AgentName:     "claude",
		AgentType:     "claude_code",
		User:          "phucnt",
		WorkstationID: "ws-test",
		CWD:           sql.NullString{String: "/tmp", Valid: true},
		Command:       sql.NullString{String: "claude --print ack", Valid: true},
		StartedAt:     time.Now(),
		Status:        model.StatusRunning,
		EngineerName:  "Phuc Nguyen",
	}

	if err := insertRun(localDB, run); err != nil {
		t.Fatalf("insertRun: %v", err)
	}

	var got string
	err := localDB.QueryRow(`SELECT COALESCE(engineer_name,'') FROM runs WHERE id = ?`, run.ID).Scan(&got)
	if err != nil {
		t.Fatalf("query engineer_name: %v", err)
	}
	if got != "Phuc Nguyen" {
		t.Errorf("engineer_name = %q, want %q", got, "Phuc Nguyen")
	}
}

// TestInsertRunDoesNotClobberEngineerName verifies that when a run row is
// pre-created with a non-NULL engineer_name (set by cmd/task_run.go from
// the Jira assignee) and then the wrapper calls insertRun with an empty
// engineer_name, the pre-created value is preserved (COALESCE semantics).
func TestInsertRunDoesNotClobberEngineerName(t *testing.T) {
	localDB := setupTestDB(t)
	defer localDB.Close()

	runID := "testrun0002ccdd"

	// Simulate pre-create by cmd/task_run.go: insert a pending row with engineer_name set.
	_, err := localDB.Exec(`
		INSERT INTO runs (id, jira_issue_key, agent_type, user, workstation_id, cwd, started_at, status, engineer_name)
		VALUES (?, 'CLITEST-99', 'claude_code', 'phucnt', 'ws-test', '/tmp', ?, 'pending', 'Phuc Nguyen')
	`, runID, time.Now().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("pre-create run: %v", err)
	}

	// Wrapper fires insertRun with empty EngineerName (no Jira context in wrapper path).
	run := &model.Run{
		ID:            runID,
		AgentName:     "claude",
		AgentType:     "claude_code",
		User:          "phucnt",
		WorkstationID: "ws-test",
		CWD:           sql.NullString{String: "/tmp", Valid: true},
		Command:       sql.NullString{String: "claude --print ack", Valid: true},
		StartedAt:     time.Now(),
		Status:        model.StatusRunning,
		EngineerName:  "", // wrapper doesn't know the assignee
	}

	if err := insertRun(localDB, run); err != nil {
		t.Fatalf("insertRun (wrapper): %v", err)
	}

	var got string
	err = localDB.QueryRow(`SELECT COALESCE(engineer_name,'') FROM runs WHERE id = ?`, runID).Scan(&got)
	if err != nil {
		t.Fatalf("query engineer_name: %v", err)
	}
	if got != "Phuc Nguyen" {
		t.Errorf("engineer_name = %q after wrapper upsert, want pre-created value %q", got, "Phuc Nguyen")
	}
}
