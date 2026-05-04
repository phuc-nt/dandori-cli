// Package server — alerts.go: unified Alert Center endpoints.
//
// Replaces the dashboard's two separate banner sources (g9/alerts +
// stale-banner) with one feed that supports per-alert dismiss persisted to
// the local DB.
//
//	GET  /api/alerts          — live alerts with alert_key + acked filter
//	POST /api/alerts/ack      — body {alert_key} marks alert dismissed
//
// /api/g9/alerts stays in place for backward-compat with anything that
// already calls it; new dashboard code uses /api/alerts.
package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/phuc-nt/dandori-cli/internal/analytics"
	"github.com/phuc-nt/dandori-cli/internal/db"
)

// AlertWithKey is the wire shape returned by /api/alerts.
// Embeds analytics.Alert and adds AlertKey so the frontend can post a
// dismiss request without needing to recompute the hash.
type AlertWithKey struct {
	AlertKey     string `json:"alert_key"`
	Kind         string `json:"kind"`
	Severity     string `json:"severity"`
	Message      string `json:"message"`
	DrilldownURL string `json:"drilldown_url,omitempty"`
}

// handleAlertsList serves GET /api/alerts.
// Returns {"alerts":[{alert_key,kind,severity,message,drilldown_url}]}
// with already-acked alerts filtered out.
func handleAlertsList(store *db.LocalDB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		days := 30
		if v := r.URL.Query().Get("since"); v != "" {
			if n, err := time.ParseDuration(v + "h"); err == nil && n > 0 {
				days = int(n.Hours() / 24)
			}
		}

		win := analytics.Window{Since: time.Duration(days) * 24 * time.Hour}
		snap, err := analytics.BuildSnapshot(store, win, analytics.DefaultThresholds())
		if err != nil {
			http.Error(w, `{"error":"alerts query failed"}`, http.StatusInternalServerError)
			return
		}

		acked, err := store.AckedAlertKeys()
		if err != nil {
			http.Error(w, `{"error":"acked lookup failed"}`, http.StatusInternalServerError)
			return
		}

		out := []AlertWithKey{}
		for _, a := range snap.Alerts {
			key := db.ComputeAlertKey(a.Kind, a.Message)
			if acked[key] {
				continue
			}
			out = append(out, AlertWithKey{
				AlertKey:     key,
				Kind:         a.Kind,
				Severity:     a.Severity,
				Message:      a.Message,
				DrilldownURL: a.DrilldownURL,
			})
		}

		_ = json.NewEncoder(w).Encode(map[string]any{"alerts": out})
	}
}

// handleAlertAck serves POST /api/alerts/ack.
// Body: {"alert_key":"<12hex>","acked_by":"<optional>"}.
// Idempotent — repeated acks update acked_at.
func handleAlertAck(store *db.LocalDB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}

		var body struct {
			AlertKey string `json:"alert_key"`
			AckedBy  string `json:"acked_by"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
			return
		}

		body.AlertKey = strings.TrimSpace(body.AlertKey)
		if body.AlertKey == "" {
			http.Error(w, `{"error":"alert_key required"}`, http.StatusBadRequest)
			return
		}

		if err := store.AckAlert(body.AlertKey, body.AckedBy); err != nil {
			http.Error(w, `{"error":"ack failed"}`, http.StatusInternalServerError)
			return
		}

		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}
}

// RegisterAlertRoutes mounts /api/alerts and /api/alerts/ack on mux.
func RegisterAlertRoutes(mux *http.ServeMux, store *db.LocalDB) {
	mux.HandleFunc("/api/alerts", handleAlertsList(store))
	mux.HandleFunc("/api/alerts/ack", handleAlertAck(store))
}
