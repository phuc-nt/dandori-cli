package server

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/phuc-nt/dandori-cli/internal/serverdb"
)

type Server struct {
	db     *serverdb.DB
	router *chi.Mux
	sse    *SSEHub
}

type Config struct {
	Listen         string
	AdminKeys      []string
	SSEIntervalSec int
}

func New(db *serverdb.DB, cfg Config) *Server {
	s := &Server{
		db:     db,
		router: chi.NewRouter(),
		sse:    NewSSEHub(),
	}

	s.router.Use(middleware.Logger)
	s.router.Use(middleware.Recoverer)
	s.router.Use(middleware.Timeout(60 * time.Second))

	s.router.Get("/api/health", s.handleHealth)
	s.router.Post("/api/events", s.handleIngestEvents)
	s.router.Get("/api/fleet/live", s.handleFleetLive)
	s.router.Get("/api/runs", s.handleListRuns)
	s.router.Get("/api/runs/{id}", s.handleGetRun)

	s.router.Get("/", s.handleDashboard)

	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

func (s *Server) StartSSEBroadcast(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				stats, err := s.getFleetStats(ctx)
				if err != nil {
					slog.Error("fleet stats", "error", err)
					continue
				}
				s.sse.Broadcast(stats)
			}
		}
	}()
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(`<!DOCTYPE html>
<html>
<head><title>Dandori Dashboard</title></head>
<body>
<h1>Dandori Fleet Dashboard</h1>
<div id="fleet-status">Loading...</div>
<script>
const es = new EventSource('/api/fleet/live');
es.onmessage = (e) => {
  document.getElementById('fleet-status').innerHTML = '<pre>' + e.data + '</pre>';
};
</script>
</body>
</html>`))
}
