//go:build server

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// Scenario 45: Empty JSON body
func TestIngestEmptyJSON(t *testing.T) {
	s := &Server{
		router: nil,
		sse:    NewSSEHub(),
	}

	req := httptest.NewRequest(http.MethodPost, "/api/events", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.handleIngestEvents(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("empty json should succeed, got %d", w.Code)
	}

	var resp IngestResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Accepted != 0 || resp.Errors != 0 {
		t.Error("empty request should have 0 accepted and 0 errors")
	}
}

// Scenario 46: Malformed JSON
func TestIngestMalformedJSON(t *testing.T) {
	s := &Server{
		router: nil,
		sse:    NewSSEHub(),
	}

	req := httptest.NewRequest(http.MethodPost, "/api/events", strings.NewReader("{invalid json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.handleIngestEvents(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("malformed json should return 400, got %d", w.Code)
	}
}

// Scenario 48: SSE client disconnect
func TestSSEClientDisconnect(t *testing.T) {
	hub := NewSSEHub()

	ch := hub.AddClient()
	if hub.ClientCount() != 1 {
		t.Error("should have 1 client")
	}

	hub.RemoveClient(ch)
	if hub.ClientCount() != 0 {
		t.Error("should have 0 clients after remove")
	}

	// Verify channel is closed
	_, ok := <-ch
	if ok {
		t.Error("channel should be closed")
	}
}

// Scenario 49: Many SSE clients
func TestSSEManyClients(t *testing.T) {
	hub := NewSSEHub()

	const numClients = 100
	channels := make([]chan string, numClients)

	for i := 0; i < numClients; i++ {
		channels[i] = hub.AddClient()
	}

	if hub.ClientCount() != numClients {
		t.Errorf("expected %d clients, got %d", numClients, hub.ClientCount())
	}

	// Broadcast to all
	hub.Broadcast(map[string]int{"count": 42})

	// Verify all received
	for i, ch := range channels {
		select {
		case msg := <-ch:
			if !strings.Contains(msg, "42") {
				t.Errorf("client %d got wrong message: %s", i, msg)
			}
		case <-time.After(100 * time.Millisecond):
			t.Errorf("client %d timed out", i)
		}
	}

	// Cleanup
	for _, ch := range channels {
		hub.RemoveClient(ch)
	}
}

// Scenario 48 extended: SSE concurrent add/remove
func TestSSEConcurrentAccess(t *testing.T) {
	hub := NewSSEHub()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch := hub.AddClient()
			time.Sleep(10 * time.Millisecond)
			hub.RemoveClient(ch)
		}()
	}

	// Concurrent broadcasts
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			hub.Broadcast(map[string]int{"n": n})
		}(i)
	}

	wg.Wait()

	if hub.ClientCount() != 0 {
		t.Errorf("all clients should be removed, got %d", hub.ClientCount())
	}
}

// Scenario: SSE buffer overflow (client not reading)
func TestSSEBufferOverflow(t *testing.T) {
	hub := NewSSEHub()
	ch := hub.AddClient()
	defer hub.RemoveClient(ch)

	// Send more messages than buffer size (10)
	for i := 0; i < 20; i++ {
		hub.Broadcast(map[string]int{"i": i})
	}

	// Should not block or panic - messages are dropped
	count := 0
	for {
		select {
		case <-ch:
			count++
		default:
			goto done
		}
	}
done:
	if count > 10 {
		t.Errorf("buffer should be limited, got %d messages", count)
	}
}

// Scenario: Health endpoint edge
func TestHealthEndpointEdge(t *testing.T) {
	s := &Server{sse: NewSSEHub()}

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	w := httptest.NewRecorder()

	s.handleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("health should return 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "ok") {
		t.Error("health should contain 'ok'")
	}
}

// Scenario: Dashboard HTML edge
func TestDashboardEndpointEdge(t *testing.T) {
	s := &Server{sse: NewSSEHub()}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	s.handleDashboard(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("dashboard should return 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "EventSource") {
		t.Error("dashboard should contain SSE script")
	}
}

// Scenario: Ingest JSON structure validation
func TestIngestJSONStructure(t *testing.T) {
	// Test JSON parsing structure
	body := `{
		"workstation_id": "ws-1",
		"runs": [{
			"id": "run-1",
			"agent_name": "test",
			"agent_type": "claude_code",
			"user": "phuc",
			"started_at": "2026-04-18T10:00:00Z",
			"status": "done",
			"input_tokens": 1000,
			"output_tokens": 500,
			"cost_usd": 0.05
		}],
		"events": []
	}`

	var req IngestRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("JSON parse failed: %v", err)
	}

	if req.WorkstationID != "ws-1" {
		t.Error("workstation_id mismatch")
	}
	if len(req.Runs) != 1 {
		t.Fatal("expected 1 run")
	}
	if req.Runs[0].InputTokens != 1000 {
		t.Error("input_tokens mismatch")
	}
}

// Scenario: SSE broadcast timing
func TestSSEBroadcastTiming(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := NewSSEHub()
	ch := hub.AddClient()
	defer hub.RemoveClient(ch)

	// Simulate broadcast loop
	go func() {
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		count := 0
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				hub.Broadcast(map[string]int{"tick": count})
				count++
				if count >= 3 {
					cancel()
					return
				}
			}
		}
	}()

	received := 0
	timeout := time.After(500 * time.Millisecond)
loop:
	for {
		select {
		case <-ch:
			received++
		case <-timeout:
			break loop
		case <-ctx.Done():
			// Drain remaining
			for {
				select {
				case <-ch:
					received++
				default:
					break loop
				}
			}
		}
	}

	if received < 3 {
		t.Errorf("should receive at least 3 broadcasts, got %d", received)
	}
}
