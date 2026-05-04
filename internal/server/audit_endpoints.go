// Package server — audit_endpoints.go: Audit View backend endpoints (Phase 04).
//
// 3 endpoints:
//
//	GET  /api/events?run=&type=&limit=&offset=
//	GET  /api/audit-log?from=&to=&entity=&limit=&offset=
//	POST /api/audit-log/verify  (?limit=N, default 1000, max 100000)
//
// All return [] (never null) and parse pagination defensively.
package server

import (
	"net/http"
	"strconv"

	"github.com/phuc-nt/dandori-cli/internal/db"
)

// RegisterAuditRoutes mounts the 3 Audit endpoints on mux.
func RegisterAuditRoutes(mux *http.ServeMux, store *db.LocalDB) {
	mux.HandleFunc("/api/events", handleEventStream(store))
	mux.HandleFunc("/api/audit-log", handleAuditLog(store))
	mux.HandleFunc("/api/audit-log/verify", handleAuditVerify(store))
}

func parseLimitOffset(r *http.Request, defLimit, maxLimit int) (int, int) {
	limit := defLimit
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= maxLimit {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	return limit, offset
}

func handleEventStream(store *db.LocalDB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		runID := r.URL.Query().Get("run")
		typeFilter := r.URL.Query().Get("type")
		limit, offset := parseLimitOffset(r, 50, 500)

		rows, err := store.EventStream(runID, typeFilter, limit, offset)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "events query failed")
			return
		}
		if rows == nil {
			rows = []db.EventStreamRow{}
		}
		writeJSON(w, rows)
	}
}

func handleAuditLog(store *db.LocalDB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		entity := r.URL.Query().Get("entity")
		from := r.URL.Query().Get("from")
		to := r.URL.Query().Get("to")
		limit, offset := parseLimitOffset(r, 100, 500)

		rows, err := store.AuditLog(entity, from, to, limit, offset)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "audit-log query failed")
			return
		}
		if rows == nil {
			rows = []db.AuditLogRow{}
		}
		writeJSON(w, rows)
	}
}

func handleAuditVerify(store *db.LocalDB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Accept GET or POST (frontend may use POST per plan §1).
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "GET or POST only")
			return
		}
		limit := 1000
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100000 {
				limit = n
			}
		}
		// Plan §1: ?full=true → verify everything (cap at 100000 for safety).
		if r.URL.Query().Get("full") == "true" {
			limit = 100000
		}
		res, err := store.VerifyAuditChain(limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "verify failed")
			return
		}
		writeJSON(w, res)
	}
}
