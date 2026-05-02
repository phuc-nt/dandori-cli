//go:build server

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/phuc-nt/dandori-cli/internal/analytics"
)

func (s *Server) registerAnalyticsRoutes() {
	s.router.Get("/api/analytics/agents", s.handleAnalyticsAgents)
	s.router.Get("/api/analytics/agents/compare", s.handleAnalyticsAgentsCompare)
	s.router.Get("/api/analytics/task-types", s.handleAnalyticsTaskTypes)
	s.router.Get("/api/analytics/cost", s.handleAnalyticsCost)
	s.router.Get("/api/analytics/cost/trend", s.handleAnalyticsCostTrend)
	s.router.Get("/api/analytics/sprints/{id}", s.handleAnalyticsSprint)
	s.router.Get("/api/analytics/tasks/{key}/cost", s.handleAnalyticsTaskCost)
	s.router.Get("/api/analytics/export", s.handleAnalyticsExport)
}

func (s *Server) handleAnalyticsAgents(w http.ResponseWriter, r *http.Request) {
	f := parseFilters(r)
	groupBy := r.URL.Query().Get("group_by")

	q := analytics.NewQuerier(s.db.Pool())

	var data any
	var err error
	if groupBy == "day" {
		data, err = q.AgentStatsDaily(r.Context(), f)
	} else {
		data, err = q.AgentStats(r.Context(), f, groupBy)
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	respondJSON(w, map[string]any{"data": data})
}

func (s *Server) handleAnalyticsAgentsCompare(w http.ResponseWriter, r *http.Request) {
	agents := parseAgentList(r)
	if len(agents) == 0 {
		http.Error(w, "agents parameter required", http.StatusBadRequest)
		return
	}

	f := parseFilters(r)
	q := analytics.NewQuerier(s.db.Pool())

	data, err := q.AgentCompare(r.Context(), agents, f)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	respondJSON(w, map[string]any{"agents": data})
}

func (s *Server) handleAnalyticsTaskTypes(w http.ResponseWriter, r *http.Request) {
	f := parseFilters(r)
	q := analytics.NewQuerier(s.db.Pool())

	data, err := q.TaskTypeStats(r.Context(), f)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	respondJSON(w, map[string]any{"data": data})
}

func (s *Server) handleAnalyticsCost(w http.ResponseWriter, r *http.Request) {
	f := parseFilters(r)
	groupBy := parseGroupBy(r)
	q := analytics.NewQuerier(s.db.Pool())

	data, err := q.CostBreakdown(r.Context(), f, groupBy)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	respondJSON(w, map[string]any{"data": data})
}

func (s *Server) handleAnalyticsCostTrend(w http.ResponseWriter, r *http.Request) {
	period, depth := parsePeriodAndDepth(r)
	q := analytics.NewQuerier(s.db.Pool())

	data, err := q.CostTrend(r.Context(), period, depth)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	respondJSON(w, map[string]any{"data": data})
}

func (s *Server) handleAnalyticsSprint(w http.ResponseWriter, r *http.Request) {
	sprintID := chi.URLParam(r, "id")
	q := analytics.NewQuerier(s.db.Pool())

	data, err := q.SprintSummary(r.Context(), sprintID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	respondJSON(w, data)
}

func (s *Server) handleAnalyticsTaskCost(w http.ResponseWriter, r *http.Request) {
	issueKey := chi.URLParam(r, "key")
	q := analytics.NewQuerier(s.db.Pool())

	data, err := q.TaskCostBreakdown(r.Context(), issueKey)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	respondJSON(w, data)
}

func (s *Server) handleAnalyticsExport(w http.ResponseWriter, r *http.Request) {
	format := parseExportFormat(r)
	queryType := r.URL.Query().Get("query")
	f := parseFilters(r)
	q := analytics.NewQuerier(s.db.Pool())

	var data any
	var err error

	switch queryType {
	case "agents":
		data, err = q.AgentStats(r.Context(), f, "")
	case "task-types":
		data, err = q.TaskTypeStats(r.Context(), f)
	case "cost":
		groupBy := parseGroupBy(r)
		data, err = q.CostBreakdown(r.Context(), f, groupBy)
	default:
		data, err = q.AgentStats(r.Context(), f, "")
		queryType = "agents"
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", contentTypeForFormat(format))

	if format == "csv" {
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", csvFilename(queryType)))
		analytics.ExportCSV(data, w)
	} else {
		analytics.ExportJSON(data, w)
	}
}

func parseFilters(r *http.Request) analytics.Filters {
	return analytics.Filters{
		Agent:     r.URL.Query().Get("agent"),
		Team:      r.URL.Query().Get("team"),
		Project:   r.URL.Query().Get("project"),
		SprintID:  r.URL.Query().Get("sprint"),
		IssueType: r.URL.Query().Get("issue_type"),
		From:      analytics.ParseTimeFilter(r.URL.Query().Get("from")),
		To:        analytics.ParseTimeFilter(r.URL.Query().Get("to")),
	}
}

func parseGroupBy(r *http.Request) string {
	groupBy := r.URL.Query().Get("group_by")
	switch groupBy {
	case "agent", "sprint", "task", "day", "week", "month":
		return groupBy
	default:
		return "agent"
	}
}

func parseExportFormat(r *http.Request) string {
	format := r.URL.Query().Get("format")
	if format == "csv" {
		return "csv"
	}
	return "json"
}

func parseAgentList(r *http.Request) []string {
	agents := r.URL.Query().Get("agents")
	if agents == "" {
		return nil
	}
	return strings.Split(agents, ",")
}

func parsePeriodAndDepth(r *http.Request) (string, int) {
	period := r.URL.Query().Get("period")
	switch period {
	case "day", "week", "month":
		// valid
	default:
		period = "week"
	}

	depth := 8
	if d := r.URL.Query().Get("depth"); d != "" {
		if parsed, err := strconv.Atoi(d); err == nil && parsed > 0 {
			depth = parsed
		}
	}

	return period, depth
}

func contentTypeForFormat(format string) string {
	if format == "csv" {
		return "text/csv"
	}
	return "application/json"
}

func csvFilename(queryType string) string {
	return fmt.Sprintf("%s-%s.csv", queryType, time.Now().Format("20060102-150405"))
}

func respondJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}
