package cmd

import (
	"encoding/json"
	"fmt"
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

var dashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Open analytics dashboard in browser",
	Long:  "Start a local web server and open the analytics dashboard.",
	RunE:  runDashboard,
}

var dashboardPort int

func init() {
	rootCmd.AddCommand(dashboardCmd)
	dashboardCmd.Flags().IntVarP(&dashboardPort, "port", "p", 8088, "Port to serve dashboard")
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
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		html := strings.ReplaceAll(dashboardHTML, "{{JIRA_BASE_URL}}", jiraBaseURL)
		w.Write([]byte(html)) //nolint:errcheck
	})

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

	return mux
}

func runDashboard(cmd *cobra.Command, args []string) error {
	store, err := db.Open("")
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}

	// Get Jira URL from config
	jiraBaseURL := "https://jira.example.com"
	if cfg := Config(); cfg != nil && cfg.Jira.BaseURL != "" {
		jiraBaseURL = strings.TrimSuffix(cfg.Jira.BaseURL, "/")
	}

	mux := newDashboardMux(store, jiraBaseURL)

	addr := fmt.Sprintf(":%d", dashboardPort)
	url := fmt.Sprintf("http://localhost:%d", dashboardPort)

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

const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Dandori Analytics</title>
    <script src="https://cdn.jsdelivr.net/npm/chart.js"></script>
    <link rel="preconnect" href="https://fonts.googleapis.com">
    <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
    <link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&display=swap" rel="stylesheet">
    <style>
        :root {
            --bg-primary: #09090b;
            --bg-secondary: #18181b;
            --bg-tertiary: #27272a;
            --bg-hover: #3f3f46;
            --border: #27272a;
            --border-subtle: #1f1f23;
            --text-primary: #fafafa;
            --text-secondary: #a1a1aa;
            --text-muted: #71717a;
            --accent: #6366f1;
            --accent-hover: #818cf8;
            --success: #22c55e;
            --success-bg: rgba(34, 197, 94, 0.1);
            --warning: #eab308;
            --warning-bg: rgba(234, 179, 8, 0.1);
            --error: #ef4444;
            --error-bg: rgba(239, 68, 68, 0.1);
            --chart-1: #6366f1;
            --chart-2: #22c55e;
            --chart-3: #f59e0b;
            --chart-4: #ef4444;
            --chart-5: #ec4899;
            --chart-6: #8b5cf6;
            --radius: 8px;
            --radius-lg: 12px;
            --shadow: 0 1px 3px rgba(0,0,0,0.4), 0 1px 2px rgba(0,0,0,0.3);
            --shadow-lg: 0 10px 15px -3px rgba(0,0,0,0.4), 0 4px 6px -2px rgba(0,0,0,0.3);
            --transition: all 0.15s ease;
        }
        * { box-sizing: border-box; margin: 0; padding: 0; }
        body {
            font-family: 'Inter', -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            background: var(--bg-primary);
            color: var(--text-primary);
            line-height: 1.5;
            -webkit-font-smoothing: antialiased;
        }
        ::-webkit-scrollbar { width: 8px; height: 8px; }
        ::-webkit-scrollbar-track { background: var(--bg-primary); }
        ::-webkit-scrollbar-thumb { background: var(--bg-tertiary); border-radius: 4px; }
        ::-webkit-scrollbar-thumb:hover { background: var(--bg-hover); }
        .app { min-height: 100vh; }
        .sidebar {
            position: fixed; left: 0; top: 0; bottom: 0; width: 240px;
            background: var(--bg-secondary); border-right: 1px solid var(--border);
            padding: 24px 16px; display: flex; flex-direction: column; z-index: 100;
        }
        .logo { display: flex; align-items: center; gap: 10px; padding: 0 8px; margin-bottom: 32px; }
        .logo-icon {
            width: 32px; height: 32px;
            background: linear-gradient(135deg, var(--accent), var(--chart-5));
            border-radius: var(--radius); display: flex; align-items: center;
            justify-content: center; font-size: 16px;
        }
        .logo-text { font-size: 18px; font-weight: 700; color: var(--text-primary); letter-spacing: -0.5px; }
        .nav { flex: 1; }
        .nav-item {
            display: flex; align-items: center; gap: 10px; padding: 10px 12px;
            border-radius: var(--radius); color: var(--text-secondary); text-decoration: none;
            font-size: 14px; font-weight: 500; transition: var(--transition); cursor: pointer; margin-bottom: 4px;
        }
        .nav-item:hover { background: var(--bg-tertiary); color: var(--text-primary); }
        .nav-item.active { background: var(--bg-tertiary); color: var(--text-primary); }
        .nav-icon { width: 18px; height: 18px; opacity: 0.7; }
        .main { margin-left: 240px; padding: 32px 40px; max-width: 1400px; }
        .header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 32px; }
        .header-left h1 { font-size: 24px; font-weight: 700; color: var(--text-primary); letter-spacing: -0.5px; }
        .header-left p { font-size: 14px; color: var(--text-muted); margin-top: 4px; }
        .header-actions { display: flex; gap: 12px; align-items: center; }
        .btn {
            display: inline-flex; align-items: center; gap: 8px; padding: 8px 16px;
            border-radius: var(--radius); font-size: 14px; font-weight: 500;
            border: none; cursor: pointer; transition: var(--transition);
        }
        .btn-ghost { background: transparent; color: var(--text-secondary); border: 1px solid var(--border); }
        .btn-ghost:hover { background: var(--bg-tertiary); color: var(--text-primary); }
        .btn-primary { background: var(--accent); color: white; }
        .btn-primary:hover { background: var(--accent-hover); }
        .btn svg { width: 16px; height: 16px; }
        .last-updated { font-size: 12px; color: var(--text-muted); }
        .stats-grid { display: grid; grid-template-columns: repeat(4, 1fr); gap: 16px; margin-bottom: 24px; }
        .stat-card {
            background: var(--bg-secondary); border: 1px solid var(--border);
            border-radius: var(--radius-lg); padding: 20px 24px; transition: var(--transition);
        }
        .stat-card:hover { border-color: var(--bg-hover); box-shadow: var(--shadow); }
        .stat-label { font-size: 13px; font-weight: 500; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.5px; margin-bottom: 8px; }
        .stat-value { font-size: 32px; font-weight: 700; color: var(--text-primary); letter-spacing: -1px; line-height: 1.2; }
        .stat-value.accent { color: var(--accent); }
        .stat-value.success { color: var(--success); }
        .stat-value.warning { color: var(--warning); }
        .stat-change { display: inline-flex; align-items: center; gap: 4px; font-size: 12px; font-weight: 500; margin-top: 8px; padding: 2px 8px; border-radius: 4px; }
        .stat-change.up { color: var(--success); background: var(--success-bg); }
        .stat-change.down { color: var(--error); background: var(--error-bg); }
        .card { background: var(--bg-secondary); border: 1px solid var(--border); border-radius: var(--radius-lg); overflow: hidden; }
        .card-header { display: flex; align-items: center; justify-content: space-between; padding: 16px 20px; border-bottom: 1px solid var(--border); }
        .card-title { font-size: 14px; font-weight: 600; color: var(--text-primary); }
        .card-body { padding: 20px; }
        .card-body.no-padding { padding: 0; }
        .grid-2 { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; margin-bottom: 24px; }
        .grid-3-1 { display: grid; grid-template-columns: 2fr 1fr; gap: 16px; margin-bottom: 24px; }
        .chart-container { height: 280px; position: relative; }
        .table-wrapper { overflow-x: auto; }
        table { width: 100%; border-collapse: collapse; }
        th { text-align: left; padding: 12px 16px; font-size: 12px; font-weight: 500; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.5px; border-bottom: 1px solid var(--border); background: var(--bg-primary); position: sticky; top: 0; }
        td { padding: 14px 16px; font-size: 14px; color: var(--text-secondary); border-bottom: 1px solid var(--border-subtle); }
        tr { transition: var(--transition); }
        tr:hover { background: var(--bg-tertiary); }
        tr:last-child td { border-bottom: none; }
        .badge { display: inline-flex; align-items: center; gap: 6px; padding: 4px 10px; border-radius: 9999px; font-size: 12px; font-weight: 500; }
        .badge-success { background: var(--success-bg); color: var(--success); }
        .badge-error { background: var(--error-bg); color: var(--error); }
        .badge-warning { background: var(--warning-bg); color: var(--warning); }
        .badge-dot { width: 6px; height: 6px; border-radius: 50%; background: currentColor; }
        .link { color: var(--accent); text-decoration: none; font-weight: 500; transition: var(--transition); }
        .link:hover { color: var(--accent-hover); text-decoration: underline; }
        .task-link { display: inline-flex; align-items: center; gap: 6px; color: var(--accent); text-decoration: none; font-weight: 500; font-size: 13px; padding: 4px 8px; border-radius: var(--radius); background: rgba(99, 102, 241, 0.1); transition: var(--transition); }
        .task-link:hover { background: rgba(99, 102, 241, 0.2); color: var(--accent-hover); }
        .task-link svg { width: 12px; height: 12px; opacity: 0; transition: var(--transition); }
        .task-link:hover svg { opacity: 1; }
        .agent-cell { display: flex; align-items: center; gap: 10px; }
        .agent-avatar { width: 28px; height: 28px; border-radius: var(--radius); background: linear-gradient(135deg, var(--chart-1), var(--chart-5)); display: flex; align-items: center; justify-content: center; font-size: 12px; font-weight: 600; color: white; }
        .agent-name { color: var(--text-primary); font-weight: 500; }
        .progress-bar { height: 6px; background: var(--bg-tertiary); border-radius: 3px; overflow: hidden; width: 80px; }
        .progress-fill { height: 100%; border-radius: 3px; transition: width 0.3s ease; }
        .progress-fill.success { background: var(--success); }
        .progress-fill.warning { background: var(--warning); }
        .progress-fill.error { background: var(--error); }
        .duration { font-family: 'SF Mono', 'Monaco', 'Inconsolata', monospace; font-size: 13px; color: var(--text-muted); }
        .cost { font-family: 'SF Mono', 'Monaco', 'Inconsolata', monospace; font-weight: 500; }
        .timestamp { font-size: 13px; color: var(--text-muted); }
        .empty-state { padding: 60px 20px; text-align: center; color: var(--text-muted); }
        .empty-state svg { width: 48px; height: 48px; margin-bottom: 16px; opacity: 0.3; }
        .loading { display: flex; align-items: center; justify-content: center; padding: 40px; }
        .spinner { width: 24px; height: 24px; border: 2px solid var(--border); border-top-color: var(--accent); border-radius: 50%; animation: spin 0.8s linear infinite; }
        @keyframes spin { to { transform: rotate(360deg); } }
        @keyframes fadeIn { from { opacity: 0; transform: translateY(8px); } to { opacity: 1; transform: translateY(0); } }
        .fade-in { animation: fadeIn 0.3s ease forwards; }
        .tab-group { display: flex; gap: 4px; padding: 4px; background: var(--bg-primary); border-radius: var(--radius); }
        .tab-btn { padding: 6px 12px; font-size: 13px; font-weight: 500; color: var(--text-muted); background: transparent; border: none; border-radius: 6px; cursor: pointer; transition: var(--transition); }
        .tab-btn:hover { color: var(--text-secondary); }
        .tab-btn.active { background: var(--bg-tertiary); color: var(--text-primary); }
        @media (max-width: 1200px) { .stats-grid { grid-template-columns: repeat(2, 1fr); } .grid-2, .grid-3-1 { grid-template-columns: 1fr; } }
        @media (max-width: 768px) { .sidebar { transform: translateX(-100%); } .main { margin-left: 0; padding: 20px; } .stats-grid { grid-template-columns: 1fr; } }
        .live-dot { width: 8px; height: 8px; background: var(--success); border-radius: 50%; animation: pulse 2s ease-in-out infinite; }
        @keyframes pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.5; } }
        .dim-selector { background: var(--bg-tertiary); border: 1px solid var(--border); color: var(--text-primary); padding: 6px 10px; font-size: 13px; font-family: inherit; border-radius: var(--radius); cursor: pointer; transition: var(--transition); }
        .dim-selector:hover { border-color: var(--bg-hover); }
        .dim-selector:focus { outline: none; border-color: var(--accent); }
        .clean-badge { display: inline-flex; align-items: center; padding: 2px 8px; border-radius: 9999px; font-size: 11px; font-weight: 500; }
        .clean-badge.yes { background: var(--success-bg); color: var(--success); }
        .clean-badge.no  { background: var(--error-bg);   color: var(--error); }

        /* <!-- G9-STYLES: role switcher + hero panels --> */
        .g9-banner {
            display: flex; align-items: center; gap: 8px; padding: 10px 16px;
            background: rgba(99,102,241,0.08); border: 1px solid rgba(99,102,241,0.25);
            border-radius: var(--radius); margin-bottom: 20px; font-size: 13px; color: var(--accent);
        }
        .role-switcher { display: flex; align-items: center; gap: 10px; }
        .role-switcher label { font-size: 13px; color: var(--text-muted); }
        .role-select {
            background: var(--bg-tertiary); border: 1px solid var(--border); color: var(--text-primary);
            padding: 6px 12px; font-size: 13px; font-family: inherit;
            border-radius: var(--radius); cursor: pointer; transition: var(--transition);
        }
        .role-select:focus { outline: none; border-color: var(--accent); }
        .g9-panels { margin-bottom: 28px; }
        .g9-hero-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; margin-bottom: 16px; }
        .dora-grid { display: grid; grid-template-columns: repeat(4, 1fr); gap: 12px; }
        .dora-metric {
            background: var(--bg-primary); border: 1px solid var(--border);
            border-radius: var(--radius); padding: 16px; text-align: center;
        }
        .dora-label { font-size: 11px; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.5px; margin-bottom: 6px; }
        .dora-value { font-size: 28px; font-weight: 700; line-height: 1; }
        .dora-unit { font-size: 11px; color: var(--text-muted); margin-top: 4px; }
        .dora-rating { display: inline-block; padding: 2px 8px; border-radius: 9999px; font-size: 11px; font-weight: 600; margin-top: 6px; }
        .rating-elite { background: rgba(34,197,94,0.15); color: var(--success); }
        .rating-high  { background: rgba(99,102,241,0.15); color: var(--accent); }
        .rating-medium { background: rgba(234,179,8,0.15); color: var(--warning); }
        .rating-low   { background: rgba(239,68,68,0.15); color: var(--error); }
        .stale-banner {
            display: flex; align-items: center; gap: 10px; padding: 12px 16px;
            background: var(--warning-bg); border: 1px solid rgba(234,179,8,0.3);
            border-radius: var(--radius); font-size: 13px; color: var(--warning);
            margin-bottom: 16px;
        }
        .attribution-tile {
            display: flex; flex-direction: column; justify-content: center;
            align-items: flex-start; gap: 8px; height: 100%;
        }
        .attr-headline { font-size: 16px; font-weight: 600; color: var(--text-primary); }
        .attr-sub { font-size: 13px; color: var(--text-muted); }
        .attr-sparkline { display: flex; align-items: flex-end; gap: 4px; height: 40px; margin-top: 4px; }
        .spark-bar { background: var(--accent); border-radius: 2px 2px 0 0; min-width: 20px; transition: height 0.3s ease; }
        .intent-feed { max-height: 400px; overflow-y: auto; }
        .intent-row {
            border-bottom: 1px solid var(--border-subtle); cursor: pointer;
            transition: var(--transition);
        }
        .intent-row:hover { background: var(--bg-tertiary); }
        .intent-row:last-child { border-bottom: none; }
        .intent-row-header { display: flex; align-items: center; gap: 12px; padding: 12px 16px; }
        .intent-ts { font-size: 12px; color: var(--text-muted); min-width: 100px; }
        .intent-type { font-size: 11px; padding: 2px 8px; background: var(--bg-tertiary); border-radius: 9999px; color: var(--text-muted); }
        .intent-summary { flex: 1; font-size: 13px; color: var(--text-primary); }
        .intent-engineer { font-size: 12px; color: var(--text-muted); }
        .intent-expand { display: none; padding: 12px 16px; background: var(--bg-primary); border-top: 1px solid var(--border-subtle); }
        .intent-expand pre { font-family: 'SF Mono', monospace; font-size: 12px; color: var(--text-secondary); white-space: pre-wrap; word-break: break-word; }
        .intent-row.expanded .intent-expand { display: block; }
        .engineer-filter { display: flex; align-items: center; gap: 8px; }
        .engineer-input {
            background: var(--bg-tertiary); border: 1px solid var(--border); color: var(--text-primary);
            padding: 5px 10px; font-size: 13px; font-family: inherit; border-radius: var(--radius);
            width: 160px; transition: var(--transition);
        }
        .engineer-input:focus { outline: none; border-color: var(--accent); }

        /* G9-P2-STYLES: period selector, compare toggle, filter pills, project view */
        .period-selector {
            background: var(--bg-tertiary); border: 1px solid var(--border); color: var(--text-primary);
            padding: 6px 12px; font-size: 13px; font-family: inherit;
            border-radius: var(--radius); cursor: pointer; transition: var(--transition);
        }
        .period-selector:focus { outline: none; border-color: var(--accent); }
        .custom-date-range {
            display: none; align-items: center; gap: 6px; font-size: 13px; color: var(--text-muted);
        }
        .custom-date-range.visible { display: flex; }
        .date-input {
            background: var(--bg-tertiary); border: 1px solid var(--border); color: var(--text-primary);
            padding: 5px 8px; font-size: 12px; font-family: inherit; border-radius: var(--radius);
            cursor: pointer; transition: var(--transition);
        }
        .date-input:focus { outline: none; border-color: var(--accent); }
        .compare-label {
            display: flex; align-items: center; gap: 6px;
            font-size: 13px; color: var(--text-muted); cursor: pointer; white-space: nowrap;
        }
        .compare-label input[type="checkbox"] { accent-color: var(--accent); cursor: pointer; }
        /* Filter pills */
        .filter-pill-bar {
            display: flex; flex-wrap: wrap; align-items: center;
            gap: 6px; padding: 8px 0; min-height: 40px;
        }
        .filter-pill {
            display: inline-flex; align-items: center; gap: 6px;
            padding: 4px 10px; background: rgba(99,102,241,0.12);
            border: 1px solid rgba(99,102,241,0.3); border-radius: 9999px;
            font-size: 12px; font-weight: 500; color: var(--accent);
        }
        .filter-pill-remove {
            background: none; border: none; color: var(--accent); cursor: pointer;
            padding: 0; font-size: 14px; line-height: 1; opacity: 0.7;
        }
        .filter-pill-remove:hover { opacity: 1; }
        .filter-add-btn {
            display: inline-flex; align-items: center; gap: 4px;
            padding: 4px 10px; background: transparent;
            border: 1px dashed var(--border); border-radius: 9999px;
            font-size: 12px; color: var(--text-muted); cursor: pointer; transition: var(--transition);
        }
        .filter-add-btn:hover { border-color: var(--accent); color: var(--accent); }
        /* Project selector inline */
        .project-selector-wrap {
            display: none; align-items: center; gap: 8px;
            padding: 12px 16px; background: rgba(99,102,241,0.05);
            border: 1px solid rgba(99,102,241,0.2); border-radius: var(--radius);
            margin-bottom: 16px; font-size: 13px;
        }
        .project-selector-wrap.visible { display: flex; }
        .project-select {
            background: var(--bg-tertiary); border: 1px solid var(--border); color: var(--text-primary);
            padding: 6px 12px; font-size: 13px; font-family: inherit;
            border-radius: var(--radius); cursor: pointer; flex: 1; max-width: 300px;
        }
        .project-select:focus { outline: none; border-color: var(--accent); }
        /* Project view panels */
        #project-view { display: none; }
        #project-view.visible { display: block; }
        /* G9-P4a engineer detail view */
        #engineer-detail-view { display: none; margin-top: 16px; }
        #engineer-detail-view.visible { display: block; }
        #engineer-detail-view .eng-detail-header { display: flex; gap: 16px; align-items: baseline; margin-bottom: 16px; }
        #engineer-detail-view .eng-detail-header h2 { margin: 0; font-size: 18px; color: var(--text-primary); }
        #engineer-detail-view .eng-detail-header .eng-back { font-size: 12px; color: var(--accent); cursor: pointer; }
        #engineer-detail-view .eng-detail-header .eng-back:hover { text-decoration: underline; }
        #engineer-runs-table { width: 100%; border-collapse: collapse; font-size: 12px; }
        #engineer-runs-table th { text-align: left; padding: 8px; color: var(--text-muted); border-bottom: 1px solid var(--border); }
        #engineer-runs-table td { padding: 6px 8px; border-bottom: 1px solid var(--border-subtle, #1a1a1a); }
        /* engineer-retention-spark sized by parent wrapper */
        /* Engineer name links — clickable in any panel */
        .engineer-link { color: var(--accent, #6366f1); cursor: pointer; text-decoration: underline; text-decoration-style: dotted; text-underline-offset: 2px; }
        .engineer-link:hover { color: var(--text-primary); }
        /* G9-P4a Run-row inline expand */
        #runs-table tr.run-row { cursor: pointer; }
        #runs-table tr.run-row:hover { background: var(--bg-secondary); }
        #runs-table tr.run-expand td { padding: 0; background: var(--bg-secondary); border-bottom: 1px solid var(--border); }
        #runs-table tr.run-expand .expand-inner { padding: 12px 16px; display: grid; grid-template-columns: 1fr 1fr; gap: 16px; }
        #runs-table tr.run-expand .expand-inner h4 { margin: 0 0 8px 0; font-size: 11px; text-transform: uppercase; color: var(--text-muted); letter-spacing: 0.5px; }
        #runs-table tr.run-expand .expand-list { font-size: 12px; color: var(--text-primary); }
        #runs-table tr.run-expand .expand-list li { padding: 3px 0; list-style: none; }
        #runs-table tr.run-expand .expand-empty { font-size: 12px; color: var(--text-muted); font-style: italic; }
        .project-hero-grid { display: grid; grid-template-columns: repeat(4,1fr); gap: 16px; margin-bottom: 20px; }
        .hero-tile {
            background: var(--bg-secondary); border: 1px solid var(--border);
            border-radius: var(--radius-lg); padding: 20px 24px;
        }
        .hero-tile .stat-label { font-size: 12px; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.5px; margin-bottom: 8px; }
        .hero-tile .stat-value { font-size: 28px; font-weight: 700; color: var(--text-primary); line-height: 1.2; }
        .hero-delta { font-size: 12px; font-weight: 600; margin-top: 6px; display: inline-flex; align-items: center; gap: 3px; }
        .hero-delta.good { color: var(--success); }
        .hero-delta.bad  { color: var(--error); }
        .hero-delta.neutral { color: var(--text-muted); }
        /* G9-P4b hero sparklines */
        .hero-spark { height: 32px; margin-top: 8px; position: relative; }
        .hero-spark canvas { width: 100% !important; height: 100% !important; }
        /* G9-P4b mobile responsive — viewport ≤ 768px */
        @media (max-width: 768px) {
            .project-hero-grid { grid-template-columns: 1fr; gap: 12px; }
            .hero-tile { padding: 14px 16px; }
            .hero-tile .stat-value { font-size: 22px; }
            .pill-bar { flex-wrap: wrap; }
            #project-burn-chart-wrap .chart-container { height: 180px; }
            #iteration-histogram-wrap { height: 160px; }
            .insights-grid { grid-template-columns: 1fr; }
            #runs-table tr.run-expand .expand-inner { grid-template-columns: 1fr; }
            #engineer-runs-table { font-size: 11px; }
            /* Wrap top-nav controls so they fit narrow screens */
            .header-actions { flex-wrap: wrap; row-gap: 8px; }
            /* Tables that don't fit get horizontal scroll */
            .table-wrapper { overflow-x: auto; }
            #runs-table, #project-tasks-table { min-width: 0; }
        }
        /* G9-P4b mobile small — viewport ≤ 375px */
        @media (max-width: 375px) {
            .top-nav { padding: 10px 12px; gap: 8px; flex-wrap: wrap; }
            #role-select, #period-selector { font-size: 12px; padding: 4px 6px; }
            .card { padding: 12px; }
            .hero-tile .stat-value { font-size: 20px; }
        }
        /* cost burn chart area */
        #project-burn-chart-wrap .chart-container { height: 240px; }
        /* tasks table */
        #project-tasks-table th, #project-tasks-table td { font-size: 13px; }
        /* iteration histogram placeholder */
        .tbd-placeholder {
            padding: 40px; text-align: center; color: var(--text-muted);
            font-size: 13px; border: 1px dashed var(--border);
            border-radius: var(--radius); margin-top: 8px;
        }
        #iteration-histogram-wrap { height: 220px; position: relative; }
        /* Insight cards */
        .insights-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(280px, 1fr)); gap: 12px; }
        .insight-card {
            background: var(--bg-secondary); border: 1px solid var(--border);
            border-left: 4px solid var(--text-muted);
            border-radius: var(--radius); padding: 12px 14px;
        }
        .insight-card.severity-low    { border-left-color: var(--success); }
        .insight-card.severity-medium { border-left-color: #f59e0b; }
        .insight-card.severity-high   { border-left-color: var(--error); }
        .insight-title { font-size: 13px; font-weight: 600; color: var(--text-primary); margin-bottom: 4px; }
        .insight-body  { font-size: 12px; color: var(--text-muted); line-height: 1.5; }
        .insight-empty { padding: 20px; text-align: center; color: var(--text-muted); font-size: 13px; }
    </style>
</head>
<body>
    <div class="app">
        <aside class="sidebar">
            <div class="logo">
                <div class="logo-icon">D</div>
                <span class="logo-text">Dandori</span>
            </div>
            <nav class="nav">
                <a class="nav-item active" href="#">
                    <svg class="nav-icon" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2V6zM14 6a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2V6zM4 16a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2v-2zM14 16a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2v-2z"/>
                    </svg>
                    Overview
                </a>
                <a class="nav-item" href="#g9-section">
                    <svg class="nav-icon" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 19v-6a2 2 0 00-2-2H5a2 2 0 00-2 2v6a2 2 0 002 2h2a2 2 0 002-2zm0 0V9a2 2 0 012-2h2a2 2 0 012 2v10m-6 0a2 2 0 002 2h2a2 2 0 002-2m0 0V5a2 2 0 012-2h2a2 2 0 012 2v14a2 2 0 01-2 2h-2a2 2 0 01-2-2z"/>
                    </svg>
                    DORA + G9
                </a>
                <a class="nav-item" href="#agents">
                    <svg class="nav-icon" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9.75 17L9 20l-1 1h8l-1-1-.75-3M3 13h18M5 17h14a2 2 0 002-2V5a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z"/>
                    </svg>
                    Agents
                </a>
                <a class="nav-item" href="#runs">
                    <svg class="nav-icon" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 10V3L4 14h7v7l9-11h-7z"/>
                    </svg>
                    Runs
                </a>
                <a class="nav-item" href="#costs">
                    <svg class="nav-icon" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8c-1.657 0-3 .895-3 2s1.343 2 3 2 3 .895 3 2-1.343 2-3 2m0-8c1.11 0 2.08.402 2.599 1M12 8V7m0 1v8m0 0v1m0-1c-1.11 0-2.08-.402-2.599-1M21 12a9 9 0 11-18 0 9 9 0 0118 0z"/>
                    </svg>
                    Costs
                </a>
                <a class="nav-item" href="#quality">
                    <svg class="nav-icon" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z"/>
                    </svg>
                    Quality KPI
                </a>
            </nav>
            <div style="padding: 16px 12px; border-top: 1px solid var(--border);">
                <div style="display: flex; align-items: center; gap: 8px;">
                    <div class="live-dot"></div>
                    <span style="font-size: 12px; color: var(--text-muted);">Auto-refresh: 30s</span>
                </div>
                <div style="margin-top: 8px; font-size: 11px; color: rgba(99,102,241,0.7); font-weight: 500;">G9 Analytics</div>
            </div>
        </aside>

        <main class="main">
            <header class="header">
                <div class="header-left">
                    <h1>Analytics Dashboard</h1>
                    <p id="last-updated">Last updated: --</p>
                </div>
                <div class="header-actions">
                    <!-- G9-ROLE-SWITCHER (P2: added project option) -->
                    <div class="role-switcher">
                        <label for="role-select">View:</label>
                        <select id="role-select" class="role-select" onchange="updateState({role: this.value, id: ''})">
                            <option value="org">Organization</option>
                            <option value="engineer">Engineer</option>
                            <option value="project">Project</option>
                        </select>
                    </div>
                    <!-- G9-PERIOD-SELECTOR -->
                    <select id="period-selector" class="period-selector" onchange="onPeriodChange(this.value)">
                        <option value="">Default</option>
                        <option value="7d">7 days</option>
                        <option value="28d">28 days</option>
                        <option value="90d">90 days</option>
                        <option value="custom">Custom...</option>
                    </select>
                    <div class="custom-date-range" id="custom-date-range">
                        <input type="date" id="custom-from" class="date-input" onchange="onCustomDateChange()">
                        <span>to</span>
                        <input type="date" id="custom-to" class="date-input" onchange="onCustomDateChange()">
                        <span id="custom-date-error" style="color:var(--error);font-size:12px;display:none;">Invalid range</span>
                    </div>
                    <!-- G9-COMPARE-TOGGLE -->
                    <label class="compare-label">
                        <input type="checkbox" id="compare-toggle" onchange="updateState({compare: this.checked})">
                        vs prior period
                    </label>
                    <button class="btn btn-ghost" onclick="loadAll()">
                        <svg fill="none" viewBox="0 0 24 24" stroke="currentColor">
                            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"/>
                        </svg>
                        Refresh
                    </button>
                </div>
            </header>

            <!-- G9-P2: Filter pill bar (persisted in URL as &filter=engineer:NAME&filter=project:KEY) -->
            <div id="filter-pill-bar" class="filter-pill-bar" style="margin-bottom:8px;"></div>

            <!-- G9-P2: Project selector (shown when role=project and no ?id= set) -->
            <div id="project-selector-wrap" class="project-selector-wrap">
                <label for="project-select" style="color:var(--text-muted);">Select project:</label>
                <select id="project-select" class="project-select" onchange="onProjectSelect(this.value)">
                    <option value="">-- choose project --</option>
                </select>
            </div>

            <!-- G9-P2: Project view panels (visible only when role=project) -->
            <section id="project-view">
                <!-- Project hero tiles -->
                <div class="project-hero-grid" id="project-hero-grid">
                    <div class="hero-tile">
                        <div class="stat-label">Project Cost</div>
                        <div class="stat-value" id="proj-cost">--</div>
                        <div class="hero-delta neutral" id="proj-cost-delta"></div>
                        <div class="hero-spark"><canvas id="spark-proj-cost"></canvas></div>
                    </div>
                    <div class="hero-tile">
                        <div class="stat-label">Tasks Completed</div>
                        <div class="stat-value" id="proj-tasks">--</div>
                        <div class="hero-delta neutral" id="proj-tasks-delta"></div>
                        <div class="hero-spark"><canvas id="spark-proj-tasks"></canvas></div>
                    </div>
                    <div class="hero-tile">
                        <div class="stat-label">Avg Cost / Task</div>
                        <div class="stat-value" id="proj-avg-cost">--</div>
                        <div class="hero-delta neutral" id="proj-avg-delta"></div>
                        <div class="hero-spark"><canvas id="spark-proj-avg"></canvas></div>
                    </div>
                    <div class="hero-tile">
                        <div class="stat-label">DORA (mini)</div>
                        <div class="stat-value" id="proj-dora-light" style="font-size:16px;">--</div>
                    </div>
                </div>

                <!-- Project DORA scorecard -->
                <div class="card fade-in" style="margin-bottom:16px;" id="project-dora-card">
                    <div class="card-header">
                        <span class="card-title">DORA Scorecard — Project</span>
                        <span style="font-size:12px;color:var(--text-muted);" id="proj-dora-age"></span>
                    </div>
                    <div class="card-body">
                        <div class="dora-grid" id="proj-dora-grid">
                            <div class="loading"><div class="spinner"></div></div>
                        </div>
                    </div>
                </div>

                <!-- Project cost burn line chart -->
                <div class="card fade-in" style="margin-bottom:16px;" id="project-burn-chart-wrap">
                    <div class="card-header">
                        <span class="card-title">Cost Burn</span>
                    </div>
                    <div class="card-body">
                        <div class="chart-container"><canvas id="project-burn-chart"></canvas></div>
                    </div>
                </div>

                <!-- Project tasks table -->
                <div class="card fade-in" style="margin-bottom:16px;">
                    <div class="card-header"><span class="card-title">Tasks</span></div>
                    <div class="card-body no-padding">
                        <div class="table-wrapper" style="max-height:400px;overflow:auto;">
                            <table id="project-tasks-table">
                                <thead><tr>
                                    <th>Issue</th><th>Cost</th><th>Runs</th><th>Iterations</th><th>Status</th><th>Engineer</th>
                                </tr></thead>
                                <tbody></tbody>
                            </table>
                        </div>
                    </div>
                </div>

                <!-- Iteration distribution histogram -->
                <div class="card fade-in" style="margin-bottom:16px;">
                    <div class="card-header"><span class="card-title">Iteration Distribution</span></div>
                    <div class="card-body">
                        <div id="iteration-histogram-wrap">
                            <canvas id="iteration-histogram-chart"></canvas>
                        </div>
                    </div>
                </div>

                <!-- Insights (project scope) -->
                <div class="card fade-in" style="margin-bottom:16px;">
                    <div class="card-header"><span class="card-title">Insights</span></div>
                    <div class="card-body">
                        <div class="insights-grid" id="project-insights-grid">
                            <div class="loading"><div class="spinner"></div></div>
                        </div>
                    </div>
                </div>
            </section>

            <!-- G9-P4a: Engineer detail drilldown (visible only when role=engineer) -->
            <section id="engineer-detail-view">
                <div class="card g9-card">
                    <div class="eng-detail-header">
                        <h2 id="eng-detail-name">Engineer</h2>
                        <span class="eng-back" onclick="drillToOrg()">← back to org</span>
                    </div>
                    <div class="card-body">
                        <h4 style="margin:0 0 8px 0;font-size:11px;text-transform:uppercase;color:var(--text-muted);letter-spacing:0.5px;">Retention sparkline (4 weekly buckets)</h4>
                        <div style="height:80px;position:relative;"><canvas id="engineer-retention-spark"></canvas></div>
                        <h4 style="margin:16px 0 8px 0;font-size:11px;text-transform:uppercase;color:var(--text-muted);letter-spacing:0.5px;">Last 50 runs</h4>
                        <div style="overflow-x:auto;">
                            <table id="engineer-runs-table">
                                <thead><tr><th>ID</th><th>Issue</th><th>Agent</th><th>Status</th><th>Cost</th><th>Started</th></tr></thead>
                                <tbody><tr><td colspan="6" class="empty-state">Loading…</td></tr></tbody>
                            </table>
                        </div>
                    </div>
                </div>
            </section>

            <!-- G9-SECTION: DORA + Attribution + Intent Feed -->
            <section id="g9-section" class="g9-panels fade-in">
                <div id="g9-stale-banner" class="stale-banner" style="display:none;">
                    <svg width="16" height="16" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01M10.29 3.86L1.82 18a2 2 0 001.71 3h16.94a2 2 0 001.71-3L13.71 3.86a2 2 0 00-3.42 0z"/>
                    </svg>
                    <span id="g9-stale-msg">Metric data is stale. Run: dandori metric export --include-attribution</span>
                </div>

                <!-- DORA Scorecard (org scope only) -->
                <div id="g9-dora-section" class="card fade-in" style="margin-bottom: 16px;">
                    <div class="card-header">
                        <span class="card-title">DORA Scorecard — Last 28 days</span>
                        <span style="font-size: 12px; color: var(--text-muted);" id="dora-age"></span>
                    </div>
                    <div class="card-body">
                        <div class="dora-grid" id="dora-grid">
                            <div class="loading"><div class="spinner"></div></div>
                        </div>
                    </div>
                </div>

                <!-- G9-HERO-GRID: Attribution + Intent -->
                <div class="g9-hero-grid">
                    <!-- Attribution Tile -->
                    <div class="card fade-in">
                        <div class="card-header">
                            <span class="card-title">Attribution — 28d</span>
                            <div class="engineer-filter" id="attr-engineer-filter" style="display:none;">
                                <input type="text" class="engineer-input" id="attr-engineer-input"
                                    placeholder="engineer name" oninput="loadG9Attribution()">
                            </div>
                        </div>
                        <div class="card-body">
                            <div id="attribution-tile" class="attribution-tile">
                                <div class="loading"><div class="spinner"></div></div>
                            </div>
                        </div>
                    </div>

                    <!-- Intent Feed -->
                    <div class="card fade-in">
                        <div class="card-header">
                            <span class="card-title">Intent Feed — Recent Decisions</span>
                            <div class="engineer-filter" id="intent-engineer-filter" style="display:none;">
                                <input type="text" class="engineer-input" id="intent-engineer-input"
                                    placeholder="engineer name" oninput="loadG9Intent()">
                            </div>
                        </div>
                        <div class="card-body no-padding">
                            <div class="intent-feed" id="intent-feed">
                                <div class="loading"><div class="spinner"></div></div>
                            </div>
                        </div>
                    </div>
                </div>

                <!-- Insights (org/engineer scope) -->
                <div class="card fade-in" style="margin-top:16px;">
                    <div class="card-header"><span class="card-title">Insights</span></div>
                    <div class="card-body">
                        <div class="insights-grid" id="org-insights-grid">
                            <div class="loading"><div class="spinner"></div></div>
                        </div>
                    </div>
                </div>
            </section>

            <!-- Existing legacy panels below G9 section -->
            <div class="stats-grid">
                <div class="stat-card fade-in">
                    <div class="stat-label">Total Runs</div>
                    <div class="stat-value" id="total-runs">--</div>
                </div>
                <div class="stat-card fade-in" style="animation-delay: 0.05s;">
                    <div class="stat-label">Total Cost</div>
                    <div class="stat-value success" id="total-cost">--</div>
                </div>
                <div class="stat-card fade-in" style="animation-delay: 0.1s;">
                    <div class="stat-label">Total Tokens</div>
                    <div class="stat-value warning" id="total-tokens">--</div>
                </div>
                <div class="stat-card fade-in" style="animation-delay: 0.15s;">
                    <div class="stat-label">Avg Cost/Run</div>
                    <div class="stat-value accent" id="avg-cost">--</div>
                </div>
            </div>

            <div class="grid-3-1">
                <div class="card fade-in" style="animation-delay: 0.2s;">
                    <div class="card-header">
                        <span class="card-title">Cost Trend (Last 7 Days)</span>
                        <div class="tab-group">
                            <button class="tab-btn active" data-chart="cost">Cost</button>
                            <button class="tab-btn" data-chart="runs">Runs</button>
                        </div>
                    </div>
                    <div class="card-body">
                        <div class="chart-container"><canvas id="trend-chart"></canvas></div>
                    </div>
                </div>
                <div class="card fade-in" style="animation-delay: 0.25s;">
                    <div class="card-header"><span class="card-title">Cost by Agent</span></div>
                    <div class="card-body">
                        <div class="chart-container"><canvas id="cost-chart"></canvas></div>
                    </div>
                </div>
            </div>

            <div class="card fade-in" style="animation-delay: 0.3s; margin-bottom: 24px;" id="agents">
                <div class="card-header"><span class="card-title">Agent Performance</span></div>
                <div class="card-body no-padding">
                    <div class="table-wrapper">
                        <table id="agents-table">
                            <thead><tr>
                                <th>Agent</th><th>Runs</th><th>Success Rate</th>
                                <th>Total Cost</th><th>Avg Cost</th><th>Avg Duration</th><th>Tokens</th>
                            </tr></thead>
                            <tbody></tbody>
                        </table>
                    </div>
                </div>
            </div>

            <div id="quality" style="margin-bottom: 24px;">
                <div class="card fade-in" style="animation-delay: 0.35s; margin-bottom: 16px;">
                    <div class="card-header">
                        <span class="card-title">Quality KPI — Regression Rate</span>
                        <select class="dim-selector" data-kpi="regression">
                            <option value="agent">By Agent</option>
                            <option value="engineer">By Engineer</option>
                            <option value="sprint">By Sprint</option>
                        </select>
                    </div>
                    <div class="card-body no-padding">
                        <div class="table-wrapper" style="max-height: 400px; overflow-y: auto;">
                            <table id="quality-regression-table">
                                <thead><tr>
                                    <th class="dim-header">Agent</th><th>Tasks</th>
                                    <th>Regressed</th><th>Regression %</th>
                                </tr></thead>
                                <tbody></tbody>
                            </table>
                        </div>
                    </div>
                </div>
                <div class="card fade-in" style="animation-delay: 0.4s; margin-bottom: 16px;">
                    <div class="card-header">
                        <span class="card-title">Quality KPI — Bug Rate</span>
                        <select class="dim-selector" data-kpi="bugs">
                            <option value="agent">By Agent</option>
                            <option value="engineer">By Engineer</option>
                            <option value="sprint">By Sprint</option>
                        </select>
                    </div>
                    <div class="card-body no-padding">
                        <div class="table-wrapper" style="max-height: 400px; overflow-y: auto;">
                            <table id="quality-bugs-table">
                                <thead><tr>
                                    <th class="dim-header">Agent</th><th>Runs</th>
                                    <th>Bugs</th><th>Bugs / Run</th>
                                </tr></thead>
                                <tbody></tbody>
                            </table>
                        </div>
                    </div>
                </div>
                <div class="card fade-in" style="animation-delay: 0.45s;">
                    <div class="card-header">
                        <span class="card-title">Quality KPI — Quality-Adjusted Cost</span>
                        <select class="dim-selector" data-kpi="cost">
                            <option value="agent">By Agent</option>
                            <option value="engineer">By Engineer</option>
                            <option value="sprint">By Sprint</option>
                        </select>
                    </div>
                    <div class="card-body no-padding">
                        <div class="table-wrapper" style="max-height: 400px; overflow-y: auto;">
                            <table id="quality-cost-table">
                                <thead><tr>
                                    <th>Task</th><th class="dim-header">Agent</th><th>Cost</th>
                                    <th>Runs</th><th>Iterations</th><th>Bugs</th><th>Clean</th>
                                </tr></thead>
                                <tbody></tbody>
                            </table>
                        </div>
                    </div>
                </div>
            </div>

            <div class="card fade-in" style="animation-delay: 0.5s;" id="runs">
                <div class="card-header"><span class="card-title">Recent Runs</span></div>
                <div class="card-body no-padding">
                    <div class="table-wrapper" style="max-height: 500px; overflow-y: auto;">
                        <table id="runs-table">
                            <thead><tr>
                                <th style="width: 100px;">Run ID</th>
                                <th style="width: 120px;">Task</th>
                                <th>Agent</th>
                                <th style="width: 100px;">Status</th>
                                <th style="width: 100px;">Duration</th>
                                <th style="width: 100px;">Cost</th>
                                <th style="width: 100px;">Tokens</th>
                                <th style="width: 160px;">Started</th>
                            </tr></thead>
                            <tbody></tbody>
                        </table>
                    </div>
                </div>
            </div>
        </main>
    </div>

    <script>
        const JIRA_BASE_URL = '{{JIRA_BASE_URL}}';
        const REFRESH_INTERVAL = 30000;
        let costChart = null;
        let trendChart = null;
        let currentTrendMode = 'cost';
        const chartColors = ['#6366f1', '#22c55e', '#f59e0b', '#ef4444', '#ec4899', '#8b5cf6', '#14b8a6', '#f97316'];

        // ---- G9-P2: URL state machine ----
        // State keys: role, id, period, from, to, compare, filter[] (array of "type:value" strings)
        // Single source of truth: window.location.search
        // All controls call updateState(partial) → merges → writes URL → calls loadAll().

        function readState() {
            const p = new URLSearchParams(window.location.search);
            return {
                role:    p.get('role')    || '',
                id:      p.get('id')      || '',
                period:  p.get('period')  || '',
                from:    p.get('from')    || '',
                to:      p.get('to')      || '',
                compare: p.get('compare') === 'true',
                filters: p.getAll('filter'), // array of "engineer:NAME" or "project:KEY"
            };
        }

        function writeState(s) {
            const p = new URLSearchParams();
            if (s.role)    p.set('role', s.role);
            if (s.id)      p.set('id', s.id);
            if (s.period)  p.set('period', s.period);
            if (s.from)    p.set('from', s.from);
            if (s.to)      p.set('to', s.to);
            if (s.compare) p.set('compare', 'true');
            (s.filters || []).forEach(f => p.append('filter', f));
            history.replaceState(null, '', '?' + p.toString());
        }

        // Merge partial into current state, write URL, re-render + reload.
        function updateState(partial) {
            const cur = readState();
            // When switching roles, clear filters (engineer view has its own scope).
            if (partial.role && partial.role !== cur.role) {
                partial.filters = [];
                partial.id = partial.id !== undefined ? partial.id : '';
            }
            const next = Object.assign({}, cur, partial);
            writeState(next);
            syncUIToState(next);
            loadAll();
        }

        // Build API query string from current state (appends period, compare, scope params).
        function buildAPIQuery(extra) {
            const s = readState();
            const p = new URLSearchParams();
            if (s.period) p.set('period', s.period);
            if (s.period === 'custom') {
                if (s.from) p.set('from', s.from);
                if (s.to)   p.set('to', s.to);
            }
            if (s.compare) p.set('compare', 'true');
            // Scope from role + id
            if (s.role === 'engineer' && s.id) p.set('engineer', s.id);
            if (s.role === 'project'  && s.id) p.set('project', s.id);
            // Filter pills append additional scopes
            (s.filters || []).forEach(f => {
                const [type, val] = f.split(':');
                if (type && val) p.append(type, val);
            });
            if (extra) Object.entries(extra).forEach(([k,v]) => p.set(k, v));
            const qs = p.toString();
            return qs ? '?' + qs : '';
        }

        // Build query string for the PRIOR comparison window (mirrors current window backwards).
        // Returns '' if no period basis. Always emits ?period=custom&from=YYYY-MM-DD&to=YYYY-MM-DD.
        function priorWindowQuery(s) {
            s = s || readState();
            const fmt = d => d.toISOString().slice(0, 10);
            let curFrom, curTo;
            const now = new Date();
            if (s.period === 'custom' && s.from && s.to) {
                curFrom = new Date(s.from + 'T00:00:00Z');
                curTo   = new Date(s.to   + 'T00:00:00Z');
            } else {
                const days = s.period === '90d' ? 90 : s.period === '28d' ? 28 : s.period === '7d' ? 7 : 0;
                if (!days) return '';
                curTo   = now;
                curFrom = new Date(now.getTime() - days * 86400000);
            }
            const durMs    = curTo.getTime() - curFrom.getTime();
            const priorTo   = new Date(curFrom.getTime());
            const priorFrom = new Date(curFrom.getTime() - durMs);
            const p = new URLSearchParams();
            p.set('period', 'custom');
            p.set('from', fmt(priorFrom));
            p.set('to',   fmt(priorTo));
            if (s.role === 'engineer' && s.id) p.set('engineer', s.id);
            if (s.role === 'project'  && s.id) p.set('project', s.id);
            (s.filters || []).forEach(f => {
                const [type, val] = f.split(':');
                if (type && val) p.append(type, val);
            });
            return '?' + p.toString();
        }

        // Sync all UI controls to match state (called after URL changes).
        function syncUIToState(s) {
            const role = s.role || 'org';
            // Role select
            const roleSel = document.getElementById('role-select');
            if (roleSel) roleSel.value = role;
            // Period selector
            const periodSel = document.getElementById('period-selector');
            if (periodSel) periodSel.value = s.period || '';
            // Custom date range visibility
            const cdr = document.getElementById('custom-date-range');
            if (cdr) {
                cdr.classList.toggle('visible', s.period === 'custom');
                if (s.from) document.getElementById('custom-from').value = s.from;
                if (s.to)   document.getElementById('custom-to').value   = s.to;
            }
            // Compare toggle
            const ct = document.getElementById('compare-toggle');
            if (ct) ct.checked = s.compare;
            // Role-based panel visibility
            applyRoleVisibility(role, s.id);
            // Filter pills
            renderFilterPills(s.filters || []);
        }

        // ---- Role/panel visibility ----
        function getCurrentRole() {
            return readState().role || 'org';
        }

        function applyRoleVisibility(role, id) {
            // Org-only DORA card: shown only at org. Project has its own DORA grid;
            // engineer view doesn't show DORA at all.
            const doraSection = document.getElementById('g9-dora-section');
            doraSection.style.display = role === 'org' ? '' : 'none';
            // Engineer-name input filter only visible in engineer scope.
            document.getElementById('attr-engineer-filter').style.display  = role === 'engineer' ? '' : 'none';
            document.getElementById('intent-engineer-filter').style.display = role === 'engineer' ? '' : 'none';
            // G9 hero grid (attribution + intent): always visible. The intent
            // feed scopes by project when role=project (backend reads ?project=).
            // Attribution tile is org/engineer scoped; we hide just the
            // attribution card for project to avoid showing org numbers there.
            const g9Section = document.getElementById('g9-section');
            g9Section.style.display = '';
            const attrTileCard = document.getElementById('attribution-tile')?.closest('.card');
            if (attrTileCard) attrTileCard.style.display = role === 'project' ? 'none' : '';
            // Project view section
            const projView = document.getElementById('project-view');
            projView.classList.toggle('visible', role === 'project');
            // Project selector: show when project role but no id
            const pswrap = document.getElementById('project-selector-wrap');
            pswrap.classList.toggle('visible', role === 'project' && !id);
            // G9-P4a: Engineer detail panel — visible when role=engineer with id.
            const engView = document.getElementById('engineer-detail-view');
            if (engView) engView.classList.toggle('visible', role === 'engineer' && !!id);
        }

        // ---- Period selector ----
        function onPeriodChange(val) {
            if (val === 'custom') {
                // Show custom inputs; don't fire loadAll yet (wait for dates).
                const cdr = document.getElementById('custom-date-range');
                if (cdr) cdr.classList.add('visible');
                writeState(Object.assign(readState(), {period: 'custom'}));
                const periodSel = document.getElementById('period-selector');
                if (periodSel) periodSel.value = 'custom';
            } else {
                updateState({period: val, from: '', to: ''});
            }
        }

        function onCustomDateChange() {
            const from = document.getElementById('custom-from').value;
            const to   = document.getElementById('custom-to').value;
            const errEl = document.getElementById('custom-date-error');
            if (!from || !to) return;
            if (from > to) {
                if (errEl) errEl.style.display = '';
                return;
            }
            if (errEl) errEl.style.display = 'none';
            updateState({period: 'custom', from, to});
        }

        // ---- Filter pills ----
        const MAX_PILLS = 4;

        function renderFilterPills(filters) {
            const bar = document.getElementById('filter-pill-bar');
            if (!bar) return;
            let html = (filters || []).map((f, i) =>
                ` + "`" + `<span class="filter-pill">${f}<button class="filter-pill-remove" onclick="removeFilterPill(${i})" title="Remove">&times;</button></span>` + "`" + `
            ).join('');
            html += ` + "`" + `<button class="filter-add-btn" onclick="addFilterPill()" title="Add filter">+ Add filter</button>` + "`" + `;
            bar.innerHTML = html;
        }

        function addFilterPill() {
            const s = readState();
            if ((s.filters || []).length >= MAX_PILLS) {
                alert('Maximum ' + MAX_PILLS + ' filter pills allowed.');
                return;
            }
            const input = prompt('Add filter (e.g. engineer:alice or project:CLITEST):');
            if (!input || !input.includes(':')) return;
            const parts = input.trim().split(':');
            const type = parts[0].toLowerCase();
            const val  = parts.slice(1).join(':').trim();
            if (!['engineer','project'].includes(type) || !val) {
                alert('Format must be engineer:NAME or project:KEY');
                return;
            }
            const newFilters = [...(s.filters || []), type + ':' + val];
            updateState({filters: newFilters});
        }

        function removeFilterPill(idx) {
            const s = readState();
            const newFilters = (s.filters || []).filter((_, i) => i !== idx);
            updateState({filters: newFilters});
        }

        // ---- Project selector ----
        async function loadProjectOptions() {
            try {
                const res = await fetch('/api/cost/task');
                const data = await res.json();
                if (!data || !data.length) return;
                // Derive distinct project prefixes from task groups.
                const keys = [...new Set(data.map(d => {
                    const idx = (d.Group || '').indexOf('-');
                    return idx > 0 ? d.Group.substring(0, idx).toUpperCase() : '';
                }).filter(Boolean))].sort();
                const sel = document.getElementById('project-select');
                if (!sel) return;
                sel.innerHTML = '<option value="">-- choose project --</option>' +
                    keys.map(k => ` + "`" + `<option value="${k}">${k}</option>` + "`" + `).join('');
                // If current id already matches a key, select it.
                const s = readState();
                if (s.id && keys.includes(s.id)) sel.value = s.id;
            } catch (e) { console.error('loadProjectOptions:', e); }
        }

        function onProjectSelect(key) {
            if (!key) return;
            updateState({id: key});
        }

        // ---- Utility functions (same as v1) ----
        function formatCost(cost) { return '$' + (cost || 0).toFixed(2); }
        function formatNumber(num) { return (num || 0).toLocaleString(); }
        function formatDuration(seconds) {
            if (seconds < 60) return Math.round(seconds) + 's';
            if (seconds < 3600) return Math.round(seconds / 60) + 'm ' + Math.round(seconds % 60) + 's';
            return Math.round(seconds / 3600) + 'h ' + Math.round((seconds % 3600) / 60) + 'm';
        }
        function formatTime(dateStr) {
            const date = new Date(dateStr);
            const now = new Date();
            const diff = now - date;
            if (diff < 60000) return 'Just now';
            if (diff < 3600000) return Math.floor(diff / 60000) + 'm ago';
            if (diff < 86400000) return Math.floor(diff / 3600000) + 'h ago';
            return date.toLocaleDateString('en-US', { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
        }
        function getAgentInitials(name) { return name.split(/[-_\s]/).map(w => w[0]).join('').toUpperCase().slice(0, 2); }
        function getAgentColor(name) {
            let hash = 0;
            for (let i = 0; i < name.length; i++) hash = name.charCodeAt(i) + ((hash << 5) - hash);
            return chartColors[Math.abs(hash) % chartColors.length];
        }
        function createJiraLink(issueKey) {
            if (!issueKey || issueKey === '-') return '<span style="color: var(--text-muted);">-</span>';
            const url = JIRA_BASE_URL ? JIRA_BASE_URL + '/browse/' + issueKey : '#';
            return ` + "`" + `<a href="${url}" target="_blank" rel="noopener" class="task-link">
                ${issueKey}
                <svg fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10 6H6a2 2 0 00-2 2v10a2 2 0 002 2h10a2 2 0 002-2v-4M14 4h6m0 0v6m0-6L10 14"/>
                </svg>
            </a>` + "`" + `;
        }
        function createStatusBadge(status) {
            const isDone = status === 'done' || status === 'success' || status === 'completed';
            const badgeClass = isDone ? 'badge-success' : 'badge-error';
            const label = isDone ? 'Done' : status.charAt(0).toUpperCase() + status.slice(1);
            return ` + "`" + `<span class="badge ${badgeClass}"><span class="badge-dot"></span>${label}</span>` + "`" + `;
        }
        function createProgressBar(rate) {
            const fillClass = rate >= 80 ? 'success' : rate >= 50 ? 'warning' : 'error';
            return ` + "`" + `<div style="display: flex; align-items: center; gap: 8px;">
                <div class="progress-bar"><div class="progress-fill ${fillClass}" style="width: ${rate}%"></div></div>
                <span style="color: var(--text-${fillClass}); font-weight: 500; font-size: 13px;">${rate.toFixed(1)}%</span>
            </div>` + "`" + `;
        }

        // ---- G9 Panels ----

        async function loadG9DORA() {
            try {
                const res = await fetch('/api/g9/dora' + buildAPIQuery());
                const data = await res.json();
                const banner = document.getElementById('g9-stale-banner');
                const ageEl = document.getElementById('dora-age');

                if (data.stale) {
                    banner.style.display = 'flex';
                    document.getElementById('g9-stale-msg').textContent = data.message || 'Data is stale. Run: dandori metric export';
                } else {
                    banner.style.display = 'none';
                }

                if (data.age_hours != null) {
                    ageEl.textContent = data.age_hours < 1
                        ? '< 1h ago'
                        : Math.round(data.age_hours) + 'h ago';
                }

                const grid = document.getElementById('dora-grid');
                if (!data.metrics) {
                    grid.innerHTML = '<div class="empty-state" style="padding: 20px; text-align: center; color: var(--text-muted); grid-column: 1/-1;">No DORA data. Run: dandori metric export --include-attribution</div>';
                    return;
                }

                const m = data.metrics;
                const doraFields = [
                    { key: 'deploy_frequency', label: 'Deploy Freq', fmt: v => v?.value?.toFixed(1) ?? '--', unit: v => v?.unit ?? '' },
                    { key: 'lead_time',         label: 'Lead Time',   fmt: v => v?.value?.toFixed(1) ?? '--', unit: v => v?.unit ?? '' },
                    { key: 'change_failure_rate', label: 'Change Fail',fmt: v => v?.value != null ? (v.value * 100).toFixed(1) + '%' : '--', unit: _ => '' },
                    { key: 'mttr',              label: 'MTTR',        fmt: v => v?.value?.toFixed(1) ?? '--', unit: v => v?.unit ?? '' },
                ];
                grid.innerHTML = doraFields.map(f => {
                    const val = m[f.key];
                    const rating = val?.rating || '';
                    const ratingClass = 'rating-' + (rating || 'medium');
                    return ` + "`" + `<div class="dora-metric">
                        <div class="dora-label">${f.label}</div>
                        <div class="dora-value">${f.fmt(val)}</div>
                        <div class="dora-unit">${f.unit(val)}</div>
                        ${rating ? ` + "`" + `<span class="dora-rating ${ratingClass}">${rating}</span>` + "`" + ` : ''}
                    </div>` + "`" + `;
                }).join('');
            } catch (e) {
                console.error('loadG9DORA:', e);
            }
        }

        async function loadG9Attribution() {
            const role = getCurrentRole();
            // Engineer name from inline input overrides URL state for legacy compatibility.
            const engineerOverride = role === 'engineer'
                ? (document.getElementById('attr-engineer-input')?.value || '')
                : '';
            try {
                const extra = engineerOverride ? {engineer: engineerOverride} : {};
                const url = '/api/g9/attribution' + buildAPIQuery(extra);
                const res = await fetch(url);
                const engineer = engineerOverride;
                const data = await res.json();

                const tile = document.getElementById('attribution-tile');
                if (data.insufficient_data) {
                    tile.innerHTML = '<div class="empty-state" style="padding: 20px; color: var(--text-muted);">Insufficient attribution data (need task_attribution rows)</div>';
                    return;
                }

                const authoredPct = ((data.authored_pct || 0) * 100).toFixed(1);
                const retainedPct = ((data.retained_pct || 0) * 100).toFixed(1);
                const sparkline = data.sparkline || [0, 0, 0, 0];
                const maxSpark = Math.max(...sparkline, 0.01);

                tile.innerHTML = ` + "`" + `
                    <div class="attr-headline">AI Authored ${authoredPct}% · Retained ${retainedPct}%</div>
                    <div class="attr-sub">28-day window${engineer ? ' · ' + engineer : ''}</div>
                    <div class="attr-sparkline">
                        ${sparkline.map(v => {
                            const h = Math.max(4, Math.round((v / maxSpark) * 36));
                            return ` + "`" + `<div class="spark-bar" style="height: ${h}px;" title="${(v*100).toFixed(1)}%"></div>` + "`" + `;
                        }).join('')}
                    </div>
                ` + "`" + `;
            } catch (e) {
                console.error('loadG9Attribution:', e);
            }
        }

        async function loadG9Intent() {
            const role = getCurrentRole();
            const engineerOverride = role === 'engineer'
                ? (document.getElementById('intent-engineer-input')?.value || '')
                : '';
            try {
                const extra = engineerOverride ? {engineer: engineerOverride} : {};
                const url = '/api/g9/intent' + buildAPIQuery(extra);
                const res = await fetch(url);
                const engineer = engineerOverride;
                const events = await res.json();

                const feed = document.getElementById('intent-feed');
                if (!events || events.length === 0) {
                    feed.innerHTML = '<div class="empty-state" style="padding: 30px; text-align: center; color: var(--text-muted);">No intent decisions captured yet for this scope.</div>';
                    return;
                }

                feed.innerHTML = events.map((ev, idx) => {
                    const parsed = (() => { try { return ev.data || {}; } catch { return {}; } })();
                    const summary = parsed.chosen
                        ? ('Chose: ' + parsed.chosen)
                        : (parsed.summary || parsed.first_user_msg || ev.event_type);
                    const expanded = JSON.stringify(ev.data, null, 2);
                    return ` + "`" + `<div class="intent-row" id="irow-${idx}" onclick="toggleIntentRow(${idx})">
                        <div class="intent-row-header">
                            <span class="intent-ts">${formatTime(ev.ts)}</span>
                            <span class="intent-type">${ev.event_type}</span>
                            <span class="intent-summary">${summary}</span>
                            ${ev.engineer_name ? ` + "`" + `<span class="intent-engineer engineer-link" onclick="event.stopPropagation(); drillToEngineer('${ev.engineer_name}')">${ev.engineer_name}</span>` + "`" + ` : ''}
                        </div>
                        <div class="intent-expand">
                            <pre>${expanded}</pre>
                        </div>
                    </div>` + "`" + `;
                }).join('');
            } catch (e) {
                console.error('loadG9Intent:', e);
            }
        }

        function toggleIntentRow(idx) {
            const row = document.getElementById('irow-' + idx);
            if (row) row.classList.toggle('expanded');
        }

        // ---- G9-P4a: Engineer drilldown + run-row inline expand ----

        // Click engineer name → drill to engineer view (URL state).
        function drillToEngineer(name) {
            if (!name) return;
            updateState({role: 'engineer', id: name});
        }

        // Back link from engineer view → org view.
        function drillToOrg() {
            updateState({role: 'org', id: ''});
        }

        // Click any row in Recent Runs → toggle inline expand row below.
        // First click fetches /api/g9/run/{id}/expand; subsequent clicks just toggle.
        async function toggleRunExpand(runID, rowEl) {
            const expRow = document.getElementById('rexp-' + runID);
            if (!expRow) return;
            if (expRow.style.display === 'table-row') {
                expRow.style.display = 'none';
                return;
            }
            expRow.style.display = 'table-row';
            // Lazy-load only once.
            const inner = expRow.querySelector('.expand-inner');
            if (inner.dataset.loaded === '1') return;
            inner.innerHTML = '<div class="expand-empty">Loading…</div>';
            try {
                const res = await fetch('/api/g9/run/' + encodeURIComponent(runID) + '/expand');
                if (!res.ok) throw new Error('http ' + res.status);
                const data = await res.json();
                const iters = (data.iterations || []).map(it =>
                    '<li>Round ' + escapeHTML(String(it.round)) +
                    ' — ' + escapeHTML(it.issue_key || '') +
                    ' <span style="color:var(--text-muted);">' + escapeHTML(it.transitioned_at || '') + '</span></li>'
                ).join('');
                const events = (data.intent_events || []).map(ev => {
                    let summary = '';
                    try {
                        const parsed = ev.data || {};
                        summary = parsed.chosen ? ('chose: ' + parsed.chosen)
                                : (parsed.summary || parsed.first_user_msg || parsed.goal || '');
                    } catch (_) {}
                    return '<li><strong>' + escapeHTML(ev.event_type) + '</strong> ' +
                        '<span style="color:var(--text-muted);">' + escapeHTML(ev.ts) + '</span>' +
                        (summary ? ' — ' + escapeHTML(summary) : '') + '</li>';
                }).join('');
                inner.innerHTML =
                    '<div><h4>Iterations (' + (data.iterations || []).length + ')</h4>' +
                    (iters ? '<ul class="expand-list">' + iters + '</ul>' : '<div class="expand-empty">No iterations recorded</div>') +
                    '</div>' +
                    '<div><h4>Intent events (' + (data.intent_events || []).length + ')</h4>' +
                    (events ? '<ul class="expand-list">' + events + '</ul>' : '<div class="expand-empty">No layer-4 events</div>') +
                    '</div>';
                inner.dataset.loaded = '1';
            } catch (e) {
                inner.innerHTML = '<div class="expand-empty">Failed to load: ' + escapeHTML(String(e)) + '</div>';
            }
        }

        // Load engineer detail panel (50 runs + retention sparkline).
        let engineerRetentionChart = null;
        async function loadEngineerDetail(name) {
            const titleEl = document.getElementById('eng-detail-name');
            if (titleEl) titleEl.textContent = 'Engineer: ' + name;
            try {
                const res = await fetch('/api/g9/engineer/' + encodeURIComponent(name));
                if (!res.ok) throw new Error('http ' + res.status);
                const data = await res.json();

                // Runs table.
                const tbody = document.querySelector('#engineer-runs-table tbody');
                if (!tbody) return;
                if (!data.runs || !data.runs.length) {
                    tbody.innerHTML = '<tr><td colspan="6" class="empty-state">No runs for ' + escapeHTML(name) + '</td></tr>';
                } else {
                    tbody.innerHTML = data.runs.map(r =>
                        '<tr>' +
                        '<td style="font-family:monospace;color:var(--text-muted);">' + escapeHTML((r.id || '').substring(0, 8)) + '</td>' +
                        '<td>' + (r.jira_issue_key ? createJiraLink(r.jira_issue_key) : '<span style="color:var(--text-muted);">—</span>') + '</td>' +
                        '<td>' + escapeHTML(r.agent_name || '') + '</td>' +
                        '<td>' + (r.status ? createStatusBadge(r.status) : '') + '</td>' +
                        '<td class="cost">' + formatCost(r.cost_usd || 0) + '</td>' +
                        '<td style="color:var(--text-muted);">' + formatTime(r.started_at) + '</td>' +
                        '</tr>'
                    ).join('');
                }

                // Retention sparkline.
                const canvas = document.getElementById('engineer-retention-spark');
                if (canvas && window.Chart) {
                    if (engineerRetentionChart) { engineerRetentionChart.destroy(); engineerRetentionChart = null; }
                    const buckets = data.retention_sparkline || [0,0,0,0];
                    engineerRetentionChart = new Chart(canvas, {
                        type: 'line',
                        data: {
                            labels: ['4w ago', '3w ago', '2w ago', '1w ago'],
                            datasets: [{
                                label: 'retention',
                                data: buckets,
                                borderColor: '#22c55e',
                                backgroundColor: 'rgba(34,197,94,0.15)',
                                fill: true, tension: 0.4, pointRadius: 3,
                            }]
                        },
                        options: {
                            responsive: true, maintainAspectRatio: false,
                            scales: { y: { beginAtZero: true, max: 1, ticks: { color: '#71717a', font: { size: 10 } } },
                                      x: { ticks: { color: '#71717a', font: { size: 10 } } } },
                            plugins: { legend: { display: false } }
                        }
                    });
                }
            } catch (e) {
                console.error('loadEngineerDetail:', e);
            }
        }

        // ---- Project view ----
        let projectBurnChart = null;

        // "Good direction" rules for delta arrows:
        // cost↓ good, autonomy/retention↑ good, intervention↓ good. Default: show neutral %.
        function deltaArrow(metric, pct) {
            // metric: 'cost', 'tasks', 'avg_cost'
            const goodIfDown = ['cost', 'avg_cost'];
            if (pct === null || pct === undefined || isNaN(pct)) return '';
            const isGoodDir = goodIfDown.includes(metric) ? pct < 0 : pct > 0;
            const cls  = isGoodDir ? 'good' : 'bad';
            const sign = pct >= 0 ? '↑' : '↓';
            return ` + "`" + `<span class="hero-delta ${cls}">${sign}${Math.abs(pct).toFixed(1)}%</span>` + "`" + `;
        }

        async function loadProjectView() {
            const s = readState();
            if (!s.id) {
                // No project selected — load options and wait.
                loadProjectOptions();
                return;
            }
            // Ensure project selector shows correct value.
            const psel = document.getElementById('project-select');
            if (psel && psel.value !== s.id) {
                await loadProjectOptions();
                if (psel) psel.value = s.id;
            }

            // Load all project sub-panels concurrently.
            loadProjectHero(s);
            loadProjectDORA(s);
            loadProjectBurn(s);
            loadProjectTasks(s);
            loadIterationHistogram(s);
            loadInsights('project-insights-grid', s);
        }

        // Hero tiles: aggregate cost/tasks from /api/cost/task. Server-side ?project= filter.
        async function loadProjectHero(s) {
            try {
                const qs = buildAPIQuery();
                const taskRes = await fetch('/api/cost/task' + qs);
                const projTasks = await taskRes.json() || [];

                const totalCost   = projTasks.reduce((a, t) => a + (t.Cost || 0), 0);
                const totalTasks  = projTasks.length;
                const avgCost     = totalTasks > 0 ? totalCost / totalTasks : 0;

                document.getElementById('proj-cost').textContent     = formatCost(totalCost);
                document.getElementById('proj-tasks').textContent    = String(totalTasks);
                document.getElementById('proj-avg-cost').textContent = formatCost(avgCost);

                // Compare deltas: re-fetch prior period when compare=true.
                document.getElementById('proj-cost-delta').innerHTML  = '';
                document.getElementById('proj-tasks-delta').innerHTML = '';
                document.getElementById('proj-avg-delta').innerHTML   = '';
                if (s.compare) {
                    try {
                        const prior = priorWindowQuery(s);
                        if (prior) {
                            const priorRes  = await fetch('/api/cost/task' + prior);
                            const priorTasks = await priorRes.json() || [];
                            const priorCost  = priorTasks.reduce((a, t) => a + (t.Cost || 0), 0);
                            const priorN     = priorTasks.length;
                            const priorAvg   = priorN > 0 ? priorCost / priorN : 0;
                            document.getElementById('proj-cost-delta').innerHTML  = renderDelta(totalCost,  priorCost,  /*lowerBetter=*/true);
                            document.getElementById('proj-tasks-delta').innerHTML = renderDelta(totalTasks, priorN,     /*lowerBetter=*/false);
                            document.getElementById('proj-avg-delta').innerHTML   = renderDelta(avgCost,    priorAvg,   /*lowerBetter=*/true);
                        }
                    } catch(_) { /* prior optional */ }
                }

                // DORA mini-light: fetch dora and show overall rating.
                const doraRes  = await fetch('/api/g9/dora' + qs);
                const doraData = await doraRes.json();
                const doraEl   = document.getElementById('proj-dora-light');
                if (doraData.metrics) {
                    const ratings = Object.values(doraData.metrics).map(m => m?.rating || '').filter(Boolean);
                    const top = ratings.includes('elite') ? 'Elite'
                              : ratings.includes('high')  ? 'High'
                              : ratings.includes('medium')? 'Med'  : 'Low';
                    doraEl.textContent = top;
                    doraEl.className   = 'stat-value rating-' + top.toLowerCase();
                } else {
                    doraEl.textContent = 'N/A';
                }
            } catch (e) { console.error('loadProjectHero:', e); }
        }

        async function loadProjectDORA(s) {
            try {
                const res  = await fetch('/api/g9/dora' + buildAPIQuery());
                const data = await res.json();
                const ageEl = document.getElementById('proj-dora-age');
                if (data.age_hours != null && ageEl) {
                    ageEl.textContent = data.age_hours < 1 ? '< 1h ago' : Math.round(data.age_hours) + 'h ago';
                }
                const grid = document.getElementById('proj-dora-grid');
                if (!data.metrics) {
                    grid.innerHTML = '<div class="empty-state" style="grid-column:1/-1;padding:20px;text-align:center;color:var(--text-muted);">No DORA data. Run: dandori metric export</div>';
                    return;
                }
                const m = data.metrics;
                const doraFields = [
                    { key: 'deploy_frequency',  label: 'Deploy Freq',  fmt: v => v?.value?.toFixed(1) ?? '--', unit: v => v?.unit ?? '' },
                    { key: 'lead_time',          label: 'Lead Time',    fmt: v => v?.value?.toFixed(1) ?? '--', unit: v => v?.unit ?? '' },
                    { key: 'change_failure_rate',label: 'Change Fail',  fmt: v => v?.value != null ? (v.value*100).toFixed(1)+'%' : '--', unit: _ => '' },
                    { key: 'mttr',               label: 'MTTR',         fmt: v => v?.value?.toFixed(1) ?? '--', unit: v => v?.unit ?? '' },
                ];
                grid.innerHTML = doraFields.map(f => {
                    const val = m[f.key]; const rating = val?.rating || '';
                    return ` + "`" + `<div class="dora-metric">
                        <div class="dora-label">${f.label}</div>
                        <div class="dora-value">${f.fmt(val)}</div>
                        <div class="dora-unit">${f.unit(val)}</div>
                        ${rating ? ` + "`" + `<span class="dora-rating rating-${rating}">${rating}</span>` + "`" + ` : ''}
                    </div>` + "`" + `;
                }).join('');
            } catch (e) { console.error('loadProjectDORA:', e); }
        }

        async function loadProjectBurn(s) {
            try {
                const res  = await fetch('/api/cost/day' + buildAPIQuery());
                const data = await res.json();
                if (projectBurnChart) { projectBurnChart.destroy(); projectBurnChart = null; }
                // Server-side ?project= filter applied in P3; data already scoped.
                const sorted = (data || []).sort((a,b) => new Date(a.Group) - new Date(b.Group));
                if (!sorted.length) {
                    document.getElementById('project-burn-chart').parentElement.innerHTML = '<div class="empty-state" style="padding:20px;text-align:center;color:var(--text-muted);">No cost data yet</div>';
                    return;
                }
                const labels   = sorted.map(d => new Date(d.Group).toLocaleDateString('en-US', {month:'short',day:'numeric'}));
                const datasets = [{
                    label: 'Cost', data: sorted.map(d => d.Cost),
                    borderColor: '#6366f1', backgroundColor: 'rgba(99,102,241,0.1)',
                    fill: true, tension: 0.4, pointRadius: 3, pointHoverRadius: 5,
                    pointBackgroundColor: '#6366f1', pointBorderColor: '#09090b', pointBorderWidth: 2,
                }];
                // If compare=true, re-fetch prior window data and append faded dataset.
                if (s.compare) {
                    try {
                        const prior = priorWindowQuery(s);
                        if (!prior) throw new Error('no prior window');
                        const priorRes  = await fetch('/api/cost/day' + prior);
                        const priorData = await priorRes.json();
                        if (priorData && priorData.length) {
                            const priorSorted = (priorData || []).sort((a,b) => new Date(a.Group) - new Date(b.Group));
                            datasets.push({
                                label: 'Prior period', data: priorSorted.map(d => d.Cost),
                                borderColor: 'rgba(99,102,241,0.3)', backgroundColor: 'rgba(99,102,241,0.04)',
                                fill: true, tension: 0.4, pointRadius: 2, borderDash: [4,4],
                            });
                        }
                    } catch(_) { /* prior data optional */ }
                }
                const canvas = document.getElementById('project-burn-chart');
                if (!canvas) return;
                projectBurnChart = new Chart(canvas, {
                    type: 'line', data: {labels, datasets},
                    options: {
                        responsive: true, maintainAspectRatio: false,
                        interaction: {intersect: false, mode: 'index'},
                        scales: {
                            x: {grid:{color:'#27272a',drawBorder:false}, ticks:{color:'#71717a',font:{family:'Inter',size:11}}},
                            y: {beginAtZero:true, grid:{color:'#27272a',drawBorder:false}, ticks:{color:'#71717a',font:{family:'Inter',size:11},callback: v => '$'+v.toFixed(0)}},
                        },
                        plugins: {legend:{display:false}, tooltip:{backgroundColor:'#27272a',titleColor:'#fafafa',bodyColor:'#a1a1aa',borderColor:'#3f3f46',borderWidth:1,padding:10,cornerRadius:8,callbacks:{label:ctx=>' $'+ctx.raw.toFixed(2)}}},
                    },
                });
                // G9-P4b: populate hero sparklines from same data — no extra fetch.
                populateProjectSparklines(sorted);
            } catch (e) { console.error('loadProjectBurn:', e); }
        }

        // ---- G9-P4b: hero tile sparklines ----
        // Stores chart instances so we can destroy on re-render.
        const heroSparkCharts = {};

        // Render a tiny line chart in canvasID with axes/legend hidden.
        function renderHeroSparkline(canvasID, values, color) {
            const canvas = document.getElementById(canvasID);
            if (!canvas || !window.Chart) return;
            if (heroSparkCharts[canvasID]) {
                heroSparkCharts[canvasID].destroy();
                delete heroSparkCharts[canvasID];
            }
            heroSparkCharts[canvasID] = new Chart(canvas, {
                type: 'line',
                data: {
                    labels: values.map((_, i) => i),
                    datasets: [{
                        data: values,
                        borderColor: color,
                        backgroundColor: color + '22',
                        fill: true, tension: 0.4,
                        pointRadius: 0, borderWidth: 1.5,
                    }],
                },
                options: {
                    responsive: true, maintainAspectRatio: false,
                    scales: { x: { display: false }, y: { display: false, beginAtZero: true } },
                    plugins: { legend: { display: false }, tooltip: { enabled: false } },
                    elements: { line: { borderJoinStyle: 'round' } },
                    animation: false,
                },
            });
        }

        // Populate the 3 project hero sparklines from already-fetched cost/day data.
        function populateProjectSparklines(sortedDayData) {
            if (!sortedDayData || !sortedDayData.length) return;
            const costSeries  = sortedDayData.map(d => d.Cost || 0);
            const tasksSeries = sortedDayData.map(d => d.RunCount || 0);
            const avgSeries   = sortedDayData.map(d => (d.RunCount > 0) ? (d.Cost || 0) / d.RunCount : 0);
            renderHeroSparkline('spark-proj-cost',  costSeries,  '#6366f1');
            renderHeroSparkline('spark-proj-tasks', tasksSeries, '#22c55e');
            renderHeroSparkline('spark-proj-avg',   avgSeries,   '#eab308');
        }

        async function loadProjectTasks(s) {
            try {
                const res  = await fetch('/api/cost/task' + buildAPIQuery());
                const data = await res.json();
                // Server-side ?project= filter applied in P3.
                const projTasks = data || [];
                const tbody = document.querySelector('#project-tasks-table tbody');
                if (!projTasks.length) {
                    tbody.innerHTML = ` + "`" + `<tr><td colspan="6" class="empty-state">No tasks for project ${s.id}</td></tr>` + "`" + `;
                    return;
                }
                tbody.innerHTML = projTasks.map(t => ` + "`" + `<tr>
                    <td>${createJiraLink(t.Group)}</td>
                    <td class="cost">${formatCost(t.Cost)}</td>
                    <td>${t.RunCount || 0}</td>
                    <td>${t.IterationCount || 0}</td>
                    <td>${createStatusBadge(t.Status || 'done')}</td>
                    <td style="color:var(--text-muted);">${t.Engineer || '--'}</td>
                </tr>` + "`" + `).join('');
            } catch (e) { console.error('loadProjectTasks:', e); }
        }

        function loadG9Panels(role) {
            role = role || getCurrentRole();
            if (role === 'project') {
                loadProjectView();
                return;
            }
            if (role === 'engineer') {
                const s = readState();
                if (s.id) loadEngineerDetail(s.id);
                // Still load attribution/intent (filtered to this engineer via buildAPIQuery).
                loadG9Attribution();
                loadG9Intent();
                return;
            }
            if (role === 'org') {
                loadG9DORA();
            }
            loadG9Attribution();
            loadG9Intent();
            loadInsights('org-insights-grid', readState());
        }

        // ---- Insights ----
        async function loadInsights(targetID, s) {
            const grid = document.getElementById(targetID);
            if (!grid) return;
            try {
                const res  = await fetch('/api/g9/insights' + buildAPIQuery());
                const data = await res.json();
                if (!Array.isArray(data) || !data.length) {
                    grid.innerHTML = '<div class="insight-empty">No insights right now — your team is humming.</div>';
                    return;
                }
                grid.innerHTML = data.map(c => ` + "`" + `<div class="insight-card severity-${c.severity || 'medium'}">
                    <div class="insight-title">${escapeHTML(c.title || '')}</div>
                    <div class="insight-body">${escapeHTML(c.body || '')}</div>
                </div>` + "`" + `).join('');
            } catch (e) {
                console.error('loadInsights:', e);
                grid.innerHTML = '<div class="insight-empty">Failed to load insights.</div>';
            }
        }

        // ---- Iteration histogram ----
        let iterationHistogramChart = null;
        async function loadIterationHistogram(s) {
            const canvas = document.getElementById('iteration-histogram-chart');
            if (!canvas) return;
            try {
                const res  = await fetch('/api/g9/iterations' + buildAPIQuery());
                const data = await res.json();
                const buckets = (data && data.buckets) || [];
                if (iterationHistogramChart) { iterationHistogramChart.destroy(); iterationHistogramChart = null; }
                if (!buckets.length || (data.total || 0) === 0) {
                    canvas.parentElement.innerHTML = '<div class="empty-state" style="padding:20px;text-align:center;color:var(--text-muted);">No iteration data yet</div>';
                    return;
                }
                iterationHistogramChart = new Chart(canvas, {
                    type: 'bar',
                    data: {
                        labels: buckets.map(b => b.label),
                        datasets: [{
                            label: 'Runs', data: buckets.map(b => b.count),
                            backgroundColor: 'rgba(99,102,241,0.6)', borderColor: '#6366f1', borderWidth: 1,
                        }],
                    },
                    options: {
                        responsive: true, maintainAspectRatio: false,
                        scales: {
                            x: {grid:{color:'#27272a'}, ticks:{color:'#71717a',font:{family:'Inter',size:11}}},
                            y: {beginAtZero:true, grid:{color:'#27272a'}, ticks:{color:'#71717a',precision:0,font:{family:'Inter',size:11}}},
                        },
                        plugins: {legend:{display:false}, tooltip:{backgroundColor:'#27272a',titleColor:'#fafafa',bodyColor:'#a1a1aa',borderColor:'#3f3f46',borderWidth:1,padding:10,cornerRadius:8}},
                    },
                });
            } catch (e) { console.error('loadIterationHistogram:', e); }
        }

        // ---- Tiny HTML escape (used by insight cards) ----
        function escapeHTML(s) {
            return String(s).replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
        }

        // ---- Compare delta renderer (used by hero tiles) ----
        function renderDelta(current, prior, lowerBetter) {
            if (prior == null || prior === 0) return '';
            const diff = current - prior;
            const pct  = (diff / prior) * 100;
            const arrow = diff > 0 ? '▲' : (diff < 0 ? '▼' : '→');
            const isGood = lowerBetter ? diff < 0 : diff > 0;
            const cls = diff === 0 ? 'neutral' : (isGood ? 'good' : 'bad');
            return ` + "`" + `<span class="hero-delta ${cls}">${arrow} ${Math.abs(pct).toFixed(0)}%</span>` + "`" + `;
        }

        // ---- Legacy load functions (same as v1) ----
        async function loadOverview() {
            try {
                const res = await fetch('/api/overview');
                const data = await res.json();
                document.getElementById('total-runs').textContent = formatNumber(data.runs);
                document.getElementById('total-cost').textContent = formatCost(data.cost);
                document.getElementById('total-tokens').textContent = formatNumber(data.tokens);
                const avgCost = data.runs > 0 ? data.cost / data.runs : 0;
                document.getElementById('avg-cost').textContent = formatCost(avgCost);
                document.getElementById('last-updated').textContent = 'Last updated: ' + new Date().toLocaleTimeString();
            } catch (e) { console.error('loadOverview:', e); }
        }
        async function loadAgents() {
            try {
                const res = await fetch('/api/agents');
                const data = await res.json();
                const tbody = document.querySelector('#agents-table tbody');
                if (!data || data.length === 0) {
                    tbody.innerHTML = '<tr><td colspan="7" class="empty-state">No agent data available</td></tr>';
                    return;
                }
                tbody.innerHTML = data.map(a => ` + "`" + `<tr>
                    <td><div class="agent-cell">
                        <div class="agent-avatar" style="background: ${getAgentColor(a.AgentName)}">${getAgentInitials(a.AgentName)}</div>
                        <span class="agent-name">${a.AgentName}</span>
                    </div></td>
                    <td>${formatNumber(a.RunCount)}</td>
                    <td>${createProgressBar(a.SuccessRate)}</td>
                    <td class="cost">${formatCost(a.TotalCost)}</td>
                    <td class="cost" style="color: var(--text-muted);">${formatCost(a.AvgCost)}</td>
                    <td class="duration">${formatDuration(a.AvgDuration)}</td>
                    <td style="color: var(--text-muted);">${formatNumber(a.TotalTokens)}</td>
                </tr>` + "`" + `).join('');
            } catch (e) { console.error('loadAgents:', e); }
        }
        async function loadCostChart() {
            try {
                const res = await fetch('/api/cost/agent');
                const data = await res.json();
                if (costChart) costChart.destroy();
                if (!data || data.length === 0) {
                    document.getElementById('cost-chart').parentElement.innerHTML = '<div class="empty-state">No cost data</div>';
                    return;
                }
                costChart = new Chart(document.getElementById('cost-chart'), {
                    type: 'doughnut',
                    data: { labels: data.map(d => d.Group), datasets: [{ data: data.map(d => d.Cost), backgroundColor: chartColors.slice(0, data.length), borderWidth: 0, hoverOffset: 4 }] },
                    options: { responsive: true, maintainAspectRatio: false, cutout: '65%', plugins: { legend: { position: 'bottom', labels: { color: '#a1a1aa', font: { family: 'Inter', size: 12 }, padding: 16, usePointStyle: true, pointStyle: 'circle' } }, tooltip: { backgroundColor: '#27272a', titleColor: '#fafafa', bodyColor: '#a1a1aa', borderColor: '#3f3f46', borderWidth: 1, padding: 12, cornerRadius: 8, callbacks: { label: ctx => ' $' + ctx.raw.toFixed(2) } } } }
                });
            } catch (e) { console.error('loadCostChart:', e); }
        }
        async function loadTrendChart() {
            try {
                const res = await fetch('/api/cost/day');
                const data = await res.json();
                if (trendChart) trendChart.destroy();
                if (!data || data.length === 0) {
                    document.getElementById('trend-chart').parentElement.innerHTML = '<div class="empty-state">No trend data</div>';
                    return;
                }
                const sortedData = data.sort((a, b) => new Date(a.Group) - new Date(b.Group)).slice(-7);
                const labels = sortedData.map(d => new Date(d.Group).toLocaleDateString('en-US', { month: 'short', day: 'numeric' }));
                const datasets = currentTrendMode === 'cost' ? [{ label: 'Cost', data: sortedData.map(d => d.Cost), borderColor: '#6366f1', backgroundColor: 'rgba(99,102,241,0.1)', fill: true, tension: 0.4, pointRadius: 4, pointHoverRadius: 6, pointBackgroundColor: '#6366f1', pointBorderColor: '#09090b', pointBorderWidth: 2 }] : [{ label: 'Runs', data: sortedData.map(d => d.RunCount), borderColor: '#22c55e', backgroundColor: 'rgba(34,197,94,0.1)', fill: true, tension: 0.4, pointRadius: 4, pointHoverRadius: 6, pointBackgroundColor: '#22c55e', pointBorderColor: '#09090b', pointBorderWidth: 2 }];
                trendChart = new Chart(document.getElementById('trend-chart'), { type: 'line', data: { labels, datasets }, options: { responsive: true, maintainAspectRatio: false, interaction: { intersect: false, mode: 'index' }, scales: { x: { grid: { color: '#27272a', drawBorder: false }, ticks: { color: '#71717a', font: { family: 'Inter', size: 11 } } }, y: { beginAtZero: true, grid: { color: '#27272a', drawBorder: false }, ticks: { color: '#71717a', font: { family: 'Inter', size: 11 }, callback: val => currentTrendMode === 'cost' ? '$' + val.toFixed(0) : val } } }, plugins: { legend: { display: false }, tooltip: { backgroundColor: '#27272a', titleColor: '#fafafa', bodyColor: '#a1a1aa', borderColor: '#3f3f46', borderWidth: 1, padding: 12, cornerRadius: 8, callbacks: { label: ctx => currentTrendMode === 'cost' ? ' $' + ctx.raw.toFixed(2) : ' ' + ctx.raw + ' runs' } } } } });
            } catch (e) { console.error('loadTrendChart:', e); }
        }
        async function loadRuns() {
            try {
                const res = await fetch('/api/runs');
                const data = await res.json();
                const tbody = document.querySelector('#runs-table tbody');
                if (!data || data.length === 0) {
                    tbody.innerHTML = '<tr><td colspan="8" class="empty-state">No runs recorded yet</td></tr>';
                    return;
                }
                // G9-P4a: each run renders as a clickable row + a hidden expand row below.
                tbody.innerHTML = data.slice(0, 30).map(r => {
                    const safeID = r.ID;
                    return ` + "`" + `<tr class="run-row" onclick="toggleRunExpand('${safeID}', this)">
                        <td style="font-family: monospace; font-size: 12px; color: var(--text-muted);">${r.ID.substring(0, 8)}</td>
                        <td>${createJiraLink(r.JiraIssueKey)}</td>
                        <td><div class="agent-cell">
                            <div class="agent-avatar" style="background: ${getAgentColor(r.AgentName)}; width: 24px; height: 24px; font-size: 10px;">${getAgentInitials(r.AgentName)}</div>
                            <span style="color: var(--text-primary);">${r.AgentName}</span>
                        </div></td>
                        <td>${createStatusBadge(r.Status)}</td>
                        <td class="duration">${formatDuration(r.Duration)}</td>
                        <td class="cost">${formatCost(r.Cost)}</td>
                        <td style="color: var(--text-muted);">${formatNumber(r.Tokens)}</td>
                        <td class="timestamp">${formatTime(r.StartedAt)}</td>
                    </tr>
                    <tr class="run-expand" id="rexp-${safeID}" style="display:none;">
                        <td colspan="8"><div class="expand-inner"></div></td>
                    </tr>` + "`" + `;
                }).join('');
            } catch (e) { console.error('loadRuns:', e); }
        }
        async function loadQualityRegression(by) {
            by = by || document.querySelector('.dim-selector[data-kpi="regression"]').value;
            try {
                const res = await fetch('/api/quality/regression?by=' + encodeURIComponent(by));
                const rows = await res.json();
                const table = document.querySelector('#quality-regression-table');
                table.querySelector('.dim-header').textContent = by.charAt(0).toUpperCase() + by.slice(1);
                const tbody = table.querySelector('tbody');
                if (!rows || rows.length === 0) { tbody.innerHTML = '<tr><td colspan="4" class="empty-state">No quality KPI data yet</td></tr>'; return; }
                tbody.innerHTML = rows.map(r => ` + "`" + `<tr>
                    <td style="color: var(--text-primary); font-weight: 500;">${r.group_key || '(unassigned)'}</td>
                    <td>${r.total_tasks}</td><td>${r.regressed_tasks}</td><td>${r.regression_pct.toFixed(1)}%</td>
                </tr>` + "`" + `).join('');
            } catch (e) { console.error('loadQualityRegression:', e); }
        }
        async function loadQualityBugs(by) {
            by = by || document.querySelector('.dim-selector[data-kpi="bugs"]').value;
            try {
                const res = await fetch('/api/quality/bugs?by=' + encodeURIComponent(by));
                const rows = await res.json();
                const table = document.querySelector('#quality-bugs-table');
                table.querySelector('.dim-header').textContent = by.charAt(0).toUpperCase() + by.slice(1);
                const tbody = table.querySelector('tbody');
                if (!rows || rows.length === 0) { tbody.innerHTML = '<tr><td colspan="4" class="empty-state">No quality KPI data yet</td></tr>'; return; }
                tbody.innerHTML = rows.map(r => ` + "`" + `<tr>
                    <td style="color: var(--text-primary); font-weight: 500;">${r.group_key || '(unassigned)'}</td>
                    <td>${r.runs}</td><td>${r.bugs}</td><td>${r.bugs_per_run.toFixed(2)}</td>
                </tr>` + "`" + `).join('');
            } catch (e) { console.error('loadQualityBugs:', e); }
        }
        async function loadQualityCost(by) {
            by = by || document.querySelector('.dim-selector[data-kpi="cost"]').value;
            try {
                const res = await fetch('/api/quality/cost?by=' + encodeURIComponent(by));
                const rows = await res.json();
                const table = document.querySelector('#quality-cost-table');
                table.querySelector('.dim-header').textContent = by.charAt(0).toUpperCase() + by.slice(1);
                const tbody = table.querySelector('tbody');
                if (!rows || rows.length === 0) { tbody.innerHTML = '<tr><td colspan="7" class="empty-state">No quality KPI data yet</td></tr>'; return; }
                tbody.innerHTML = rows.map(r => ` + "`" + `<tr>
                    <td style="font-family: monospace; font-size: 12px;">${r.issue_key}</td>
                    <td style="color: var(--text-primary); font-weight: 500;">${r.group_key || '(unassigned)'}</td>
                    <td class="cost">$${r.total_cost_usd.toFixed(4)}</td>
                    <td>${r.run_count}</td><td>${r.iteration_count}</td><td>${r.bug_count}</td>
                    <td><span class="clean-badge ${r.is_clean ? 'yes' : 'no'}">${r.is_clean ? 'Yes' : 'No'}</span></td>
                </tr>` + "`" + `).join('');
            } catch (e) { console.error('loadQualityCost:', e); }
        }

        document.querySelectorAll('.tab-btn').forEach(btn => {
            btn.addEventListener('click', function() {
                document.querySelectorAll('.tab-btn').forEach(b => b.classList.remove('active'));
                this.classList.add('active');
                currentTrendMode = this.dataset.chart;
                loadTrendChart();
            });
        });
        document.querySelectorAll('.dim-selector').forEach(sel => {
            sel.addEventListener('change', function() {
                const kpi = this.dataset.kpi;
                if (kpi === 'regression') loadQualityRegression(this.value);
                if (kpi === 'bugs')       loadQualityBugs(this.value);
                if (kpi === 'cost')       loadQualityCost(this.value);
            });
        });

        function loadAll() {
            const role = getCurrentRole();
            loadG9Panels(role);
            loadOverview();
            loadAgents();
            loadCostChart();
            loadTrendChart();
            loadRuns();
            loadQualityRegression();
            loadQualityBugs();
            loadQualityCost();
        }

        // Init: if no ?role= in URL, fetch /api/g9/landing once → adopt result → load.
        // Otherwise sync all UI controls from URL and load.
        (async function init() {
            let s = readState();
            if (!s.role) {
                // No role in URL: try landing detection (CWD-aware default).
                try {
                    const res = await fetch('/api/g9/landing');
                    const landing = await res.json();
                    if (landing && landing.role) {
                        s = Object.assign(s, {role: landing.role, id: landing.id || ''});
                        writeState(s);
                    }
                } catch(_) { /* fallback: org */ }
                if (!s.role) {
                    s.role = 'org';
                    writeState(s);
                }
            }
            syncUIToState(s);
            loadAll();
        })();

        setInterval(loadAll, REFRESH_INTERVAL);

        document.querySelectorAll('.nav-item').forEach(item => {
            item.addEventListener('click', function(e) {
                const href = this.getAttribute('href');
                if (href && href.startsWith('#') && href.length > 1) {
                    e.preventDefault();
                    const target = document.querySelector(href);
                    if (target) target.scrollIntoView({ behavior: 'smooth', block: 'start' });
                }
                document.querySelectorAll('.nav-item').forEach(n => n.classList.remove('active'));
                this.classList.add('active');
            });
        });
    </script>
</body>
</html>`
