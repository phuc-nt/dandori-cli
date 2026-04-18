package sync

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewUploader(t *testing.T) {
	u := NewUploader("http://localhost:8080", "test-key", "ws-test")

	if u.serverURL != "http://localhost:8080" {
		t.Errorf("serverURL = %s", u.serverURL)
	}
	if u.apiKey != "test-key" {
		t.Errorf("apiKey = %s", u.apiKey)
	}
	if u.workstationID != "ws-test" {
		t.Errorf("workstationID = %s", u.workstationID)
	}
}

func TestUploadRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/events" {
			t.Errorf("path = %s, want /api/events", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("auth header = %s", r.Header.Get("Authorization"))
		}

		var req UploadRequest
		json.NewDecoder(r.Body).Decode(&req)

		if req.WorkstationID != "ws-test" {
			t.Errorf("workstationID = %s", req.WorkstationID)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(UploadResponse{
			Accepted: len(req.Runs),
			Errors:   0,
		})
	}))
	defer server.Close()

	u := NewUploader(server.URL, "test-key", "ws-test")

	req := UploadRequest{
		WorkstationID: "ws-test",
		Runs: []RunData{
			{
				ID:        "run-1",
				AgentName: "alpha",
				AgentType: "claude_code",
				User:      "phuc",
				StartedAt: "2026-04-18T10:00:00Z",
				Status:    "done",
			},
		},
	}

	resp, err := u.upload(req)
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}

	if resp.Accepted != 1 {
		t.Errorf("accepted = %d, want 1", resp.Accepted)
	}
}

func TestUploadServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	u := NewUploader(server.URL, "", "ws-test")

	req := UploadRequest{WorkstationID: "ws-test"}
	_, err := u.upload(req)

	if err == nil {
		t.Error("expected error for 500 response")
	}
}
