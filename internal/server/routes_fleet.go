package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type FleetStats struct {
	ActiveRuns   int       `json:"active_runs"`
	TodayRuns    int       `json:"today_runs"`
	TodayCostUSD float64   `json:"today_cost_usd"`
	ActiveAgents []string  `json:"active_agents"`
	LastUpdate   time.Time `json:"last_update"`
}

func (s *Server) handleFleetLive(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	client := s.sse.AddClient()
	defer s.sse.RemoveClient(client)

	ctx := r.Context()

	stats, _ := s.getFleetStats(ctx)
	data, _ := json.Marshal(stats)
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()

	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-client:
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		}
	}
}

func (s *Server) getFleetStats(ctx context.Context) (*FleetStats, error) {
	stats := &FleetStats{
		LastUpdate: time.Now(),
	}

	row := s.db.Pool().QueryRow(ctx, `
		SELECT COUNT(*) FROM runs WHERE status = 'running'
	`)
	row.Scan(&stats.ActiveRuns)

	row = s.db.Pool().QueryRow(ctx, `
		SELECT COUNT(*), COALESCE(SUM(cost_usd), 0)
		FROM runs WHERE started_at >= CURRENT_DATE
	`)
	row.Scan(&stats.TodayRuns, &stats.TodayCostUSD)

	rows, _ := s.db.Pool().Query(ctx, `
		SELECT DISTINCT agent_name FROM runs WHERE status = 'running'
	`)
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var name string
			rows.Scan(&name)
			stats.ActiveAgents = append(stats.ActiveAgents, name)
		}
	}

	return stats, nil
}
