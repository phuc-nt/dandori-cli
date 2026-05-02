//go:build server

package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSSEHub(t *testing.T) {
	hub := NewSSEHub()

	if hub.ClientCount() != 0 {
		t.Error("initial client count should be 0")
	}

	client1 := hub.AddClient()
	client2 := hub.AddClient()

	if hub.ClientCount() != 2 {
		t.Errorf("client count = %d, want 2", hub.ClientCount())
	}

	hub.Broadcast(map[string]string{"test": "data"})

	select {
	case msg := <-client1:
		if !strings.Contains(msg, "test") {
			t.Error("broadcast should contain test data")
		}
	default:
		t.Error("client1 should receive broadcast")
	}

	hub.RemoveClient(client1)
	if hub.ClientCount() != 1 {
		t.Errorf("client count after remove = %d, want 1", hub.ClientCount())
	}

	hub.RemoveClient(client2)
}

func TestHealthEndpoint(t *testing.T) {
	srv := &Server{
		router: nil,
	}

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	w := httptest.NewRecorder()

	srv.handleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	if !strings.Contains(w.Body.String(), "ok") {
		t.Error("body should contain ok")
	}
}

func TestDashboardEndpoint(t *testing.T) {
	srv := &Server{}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	srv.handleDashboard(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	if !strings.Contains(w.Body.String(), "Dandori") {
		t.Error("body should contain Dandori")
	}
}
