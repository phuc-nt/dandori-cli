//go:build server

package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/phuc-nt/dandori-cli/internal/serverdb"
)

func (s *Server) handleListAssignments(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	status := r.URL.Query().Get("status")
	agentName := r.URL.Query().Get("agent")

	var assignments []serverdb.AssignmentRow
	var err error

	if agentName != "" {
		assignments, err = serverdb.ListAssignmentsForAgent(ctx, s.db.Pool(), agentName, status)
	} else if status == "pending" {
		assignments, err = serverdb.ListPendingAssignments(ctx, s.db.Pool())
	} else {
		// List all - simplified query
		assignments, err = serverdb.ListPendingAssignments(ctx, s.db.Pool())
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]any{
		"assignments": assignments,
		"count":       len(assignments),
	})
}

func (s *Server) handleGetAssignment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	idStr := chi.URLParam(r, "id")

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	var a serverdb.AssignmentRow
	err = s.db.Pool().QueryRow(ctx, `
		SELECT id, jira_issue_key, suggested_agent, suggested_score, suggestion_reason, confirmed_agent, status, suggested_at, confirmed_at, reminder_sent
		FROM assignments WHERE id = $1
	`, id).Scan(&a.ID, &a.JiraIssueKey, &a.SuggestedAgent, &a.SuggestedScore, &a.SuggestionReason, &a.ConfirmedAgent, &a.Status, &a.SuggestedAt, &a.ConfirmedAt, &a.ReminderSent)

	if err != nil {
		http.Error(w, "assignment not found", http.StatusNotFound)
		return
	}

	json.NewEncoder(w).Encode(a)
}

func (s *Server) handleConfirmAssignment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	idStr := chi.URLParam(r, "id")

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	var req struct {
		Agent string `json:"agent"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Agent == "" {
		http.Error(w, "agent is required", http.StatusBadRequest)
		return
	}

	if err := serverdb.ConfirmAssignment(ctx, s.db.Pool(), id, req.Agent); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "confirmed"})
}

func (s *Server) handleListAgentConfigs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	activeOnly := r.URL.Query().Get("active") == "true"

	configs, err := serverdb.ListAgentConfigs(ctx, s.db.Pool(), activeOnly)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]any{
		"agents": configs,
		"count":  len(configs),
	})
}

func (s *Server) handleGetAgentConfig(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	name := chi.URLParam(r, "name")

	cfg, err := serverdb.GetAgentConfig(ctx, s.db.Pool(), name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if cfg == nil {
		http.Error(w, "agent not found", http.StatusNotFound)
		return
	}

	// Include active run count
	activeRuns, _ := serverdb.CountActiveRuns(ctx, s.db.Pool(), name)

	json.NewEncoder(w).Encode(map[string]any{
		"agent":       cfg,
		"active_runs": activeRuns,
	})
}

func (s *Server) handleUpsertAgentConfig(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var cfg serverdb.AgentConfigRow
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if cfg.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	if cfg.AgentType == "" {
		cfg.AgentType = "claude_code"
	}
	if cfg.MaxConcurrent == 0 {
		cfg.MaxConcurrent = 3
	}

	if err := serverdb.UpsertAgentConfig(ctx, s.db.Pool(), cfg); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
