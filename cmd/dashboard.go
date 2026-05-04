package cmd

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/phuc-nt/dandori-cli/internal/db"
	"github.com/phuc-nt/dandori-cli/internal/server"
	"github.com/spf13/cobra"
)

//go:embed all:web/dashboard
var dashboardFS embed.FS

var dashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Open analytics dashboard in browser",
	Long:  "Start a local web server and open the analytics dashboard.",
	RunE:  runDashboard,
}

var (
	dashboardPort int
	dashboardBind string
)

func init() {
	rootCmd.AddCommand(dashboardCmd)
	dashboardCmd.Flags().IntVarP(&dashboardPort, "port", "p", 8088, "Port to serve dashboard")
	dashboardCmd.Flags().StringVar(&dashboardBind, "bind", "127.0.0.1", "Bind address (default: loopback only). Use 0.0.0.0 to expose on LAN — only do this if you trust the network.")
}

// extractProjectKey extracts the Jira project key prefix from an issue key.
// "CLITEST-99" → "CLITEST", "FOO-BAR-1" → "FOO", "noprefix" → "".
func extractProjectKey(issueKey string) string {
	idx := strings.Index(issueKey, "-")
	if idx <= 0 {
		return ""
	}
	return issueKey[:idx]
}

// detectDashboardLanding infers the default landing view from the current
// working directory. Best-effort — returns {Role:"org"} on any failure so
// the dashboard always starts up cleanly.
func detectDashboardLanding(store *db.LocalDB) server.Landing {
	cwd, err := os.Getwd()
	if err != nil {
		return server.Landing{Role: "org"}
	}
	keys, err := store.GetDistinctProjectKeys()
	if err != nil || len(keys) == 0 {
		return server.Landing{Role: "org"}
	}
	landing, _ := server.DetectLanding(cwd, keys)
	return landing
}

// newDashboardMux builds and returns the HTTP mux for the dashboard.
// Extracted so tests can call it directly without starting a server.
func newDashboardMux(store *db.LocalDB, jiraBaseURL string) *http.ServeMux {
	mux := http.NewServeMux()

	// Serve dashboard HTML with Jira URL injected
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		raw, err := dashboardFS.ReadFile("web/dashboard/index.html")
		if err != nil {
			http.Error(w, "dashboard unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		html := strings.ReplaceAll(string(raw), "{{JIRA_BASE_URL}}", jiraBaseURL)
		w.Write([]byte(html)) //nolint:errcheck
	})

	// Serve static assets (style.css, app.js) from web/dashboard/ sub-FS
	staticFS, err := fs.Sub(dashboardFS, "web/dashboard")
	if err != nil {
		panic(fmt.Sprintf("dashboard embed sub-FS: %v", err))
	}
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// API endpoints
	mux.HandleFunc("/api/overview", func(w http.ResponseWriter, r *http.Request) {
		runs, cost, tokens, _ := store.GetTotalStats()
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"runs": runs, "cost": cost, "tokens": tokens,
		})
	})

	mux.HandleFunc("/api/agents", func(w http.ResponseWriter, r *http.Request) {
		stats, _ := store.GetAgentStats()
		json.NewEncoder(w).Encode(stats) //nolint:errcheck
	})

	mux.HandleFunc("/api/cost/agent", func(w http.ResponseWriter, r *http.Request) {
		groups, _ := store.GetCostByAgent()
		json.NewEncoder(w).Encode(groups) //nolint:errcheck
	})

	server.RegisterCostRoutes(mux, store)

	mux.HandleFunc("/api/runs", func(w http.ResponseWriter, r *http.Request) {
		runs, _ := store.GetRecentRuns(50)
		json.NewEncoder(w).Encode(runs) //nolint:errcheck
	})

	// Quality KPI endpoints
	mux.HandleFunc("/api/quality/regression", qualityHandler(store, "regression"))
	mux.HandleFunc("/api/quality/bugs", qualityHandler(store, "bugs"))
	mux.HandleFunc("/api/quality/cost", qualityHandler(store, "cost"))

	// G9 routes (DORA scorecard, attribution, intent feed, drilldowns).
	server.RegisterG9Routes(mux, store)
	server.RegisterG9LandingRoute(mux, detectDashboardLanding(store))

	// Dashboard v2 unified Alert Center (replaces split g9/alerts + stale banners).
	server.RegisterAlertRoutes(mux, store)

	// Dashboard v2 KPI strip (14-day sparkline + WoW totals).
	server.RegisterKPIRoutes(mux, store)

	// Phase 02 PO View endpoints (sprints, burndown, cost-by-dept, projection,
	// attribution timeline, task lifecycle, lead-time distribution).
	server.RegisterPORoutes(mux, store)

	// Phase 03 Engineering View endpoints (agent compare, autonomy, funnel,
	// cache eff, cost per task, model mix, session end, duration histogram).
	server.RegisterEngRoutes(mux, store)

	// Phase 03 Admin View endpoints (workstation matrix, repo leaderboard).
	server.RegisterAdminRoutes(mux, store)

	// Phase 04 QA View endpoints (quality timeline, cost-quality scatter,
	// commit-msg distribution, bug hotspots, rework causes, intervention heatmap).
	server.RegisterQARoutes(mux, store)

	// Phase 04 Audit View endpoints (event stream, audit log, hash-chain verify).
	server.RegisterAuditRoutes(mux, store)

	return mux
}

func runDashboard(cmd *cobra.Command, args []string) error {
	store, err := db.Open("")
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	if err := store.Migrate(); err != nil {
		return fmt.Errorf("migrate db: %w", err)
	}

	// Get Jira URL from config
	jiraBaseURL := "https://jira.example.com"
	if cfg := Config(); cfg != nil && cfg.Jira.BaseURL != "" {
		jiraBaseURL = strings.TrimSuffix(cfg.Jira.BaseURL, "/")
	}

	mux := newDashboardMux(store, jiraBaseURL)

	addr := fmt.Sprintf("%s:%d", dashboardBind, dashboardPort)
	url := fmt.Sprintf("http://localhost:%d", dashboardPort)
	if dashboardBind != "127.0.0.1" && dashboardBind != "localhost" {
		fmt.Printf("⚠ Dashboard bound to %s — exposed beyond loopback. Audit log + cost data has no auth.\n", addr)
	}

	fmt.Printf("Starting dashboard at %s\n", url)
	fmt.Println("Press Ctrl+C to stop")

	// Open browser after short delay
	go func() {
		time.Sleep(500 * time.Millisecond)
		openBrowser(url)
	}()

	return http.ListenAndServe(addr, mux)
}

// qualityHandler returns an HTTP handler for the given quality KPI endpoint.
// kpi must be one of "regression", "bugs", "cost".
func qualityHandler(store *db.LocalDB, kpi string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		by := r.URL.Query().Get("by")
		switch by {
		case "", "agent", "engineer", "sprint":
			// valid
		default:
			w.Header().Set("Content-Type", "application/json")
			http.Error(w, `{"error":"invalid by: must be agent, engineer, or sprint"}`, http.StatusBadRequest)
			return
		}
		since := atoiOr(r.URL.Query().Get("since"), 0)
		w.Header().Set("Content-Type", "application/json")

		var data any
		var qerr error
		switch kpi {
		case "regression":
			data, qerr = store.RegressionRate(by, since)
		case "bugs":
			data, qerr = store.BugRate(by, since)
		case "cost":
			top := atoiOr(r.URL.Query().Get("top"), 50)
			data, qerr = store.QualityAdjustedCost(by, since, top)
		}
		if qerr != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, qerr.Error()), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(data) //nolint:errcheck
	}
}

// atoiOr parses s as an integer; returns def on empty string or parse error.
func atoiOr(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	}
	if cmd != nil {
		cmd.Start()
	}
}
