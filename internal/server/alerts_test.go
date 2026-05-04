package server_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/phuc-nt/dandori-cli/internal/db"
	"github.com/phuc-nt/dandori-cli/internal/server"
)

func newAlertsMux(t *testing.T) (*http.ServeMux, *db.LocalDB) {
	t.Helper()
	tmpDir := t.TempDir()
	store, err := db.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := store.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	mux := http.NewServeMux()
	server.RegisterAlertRoutes(mux, store)
	return mux, store
}

func TestAlerts_GETReturnsEmptyArray(t *testing.T) {
	mux, _ := newAlertsMux(t)

	req := httptest.NewRequest(http.MethodGet, "/api/alerts", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Alerts []map[string]any `json:"alerts"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v; body=%s", err, w.Body.String())
	}
	if resp.Alerts == nil {
		t.Error("alerts must be [] not null")
	}
}

func TestAlerts_AckPersistsAndFilters(t *testing.T) {
	mux, store := newAlertsMux(t)

	// Manually ack a key, then verify it's stored.
	key := db.ComputeAlertKey("cost_multiple", "test: cost 4× baseline")

	body, _ := json.Marshal(map[string]string{"alert_key": key, "acked_by": "phuc"})
	req := httptest.NewRequest(http.MethodPost, "/api/alerts/ack", bytes.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("ack status = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	acked, err := store.IsAlertAcked(key)
	if err != nil {
		t.Fatalf("IsAlertAcked: %v", err)
	}
	if !acked {
		t.Error("alert should be acked after POST /api/alerts/ack")
	}
}

func TestAlerts_AckRejectsEmptyKey(t *testing.T) {
	mux, _ := newAlertsMux(t)

	body, _ := json.Marshal(map[string]string{"alert_key": ""})
	req := httptest.NewRequest(http.MethodPost, "/api/alerts/ack", bytes.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("empty key status = %d, want 400", w.Code)
	}
}

func TestAlerts_AckRejectsGET(t *testing.T) {
	mux, _ := newAlertsMux(t)

	req := httptest.NewRequest(http.MethodGet, "/api/alerts/ack", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET ack status = %d, want 405", w.Code)
	}
}
