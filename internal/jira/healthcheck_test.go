package jira

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJiraTestConnection(t *testing.T) {
	tests := []struct {
		name        string
		handler     http.HandlerFunc
		wantDisplay string
		wantErrSub  string
	}{
		{
			name: "200 returns displayName",
			handler: func(w http.ResponseWriter, r *http.Request) {
				// Verify Basic auth header is present.
				auth := r.Header.Get("Authorization")
				if !strings.HasPrefix(auth, "Basic ") {
					http.Error(w, "no auth", http.StatusUnauthorized)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"accountId":"abc123","displayName":"Phuc Nguyen"}`))
			},
			wantDisplay: "Phuc Nguyen",
		},
		{
			name: "200 falls back to accountId when displayName empty",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"accountId":"abc123","displayName":""}`))
			},
			wantDisplay: "abc123",
		},
		{
			name: "401 returns token hint",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
			},
			wantErrSub: "401 unauthorized",
		},
		{
			name: "403 returns forbidden message",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "Forbidden", http.StatusForbidden)
			},
			wantErrSub: "403 forbidden",
		},
		{
			name: "404 returns URL hint",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "Not Found", http.StatusNotFound)
			},
			wantErrSub: "404 not found",
		},
		{
			name: "500 returns status excerpt",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			},
			wantErrSub: "unexpected status 500",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			display, err := TestConnection(srv.URL, "user@example.com", "token123")

			if tc.wantErrSub != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErrSub)
				}
				if !strings.Contains(err.Error(), tc.wantErrSub) {
					t.Errorf("error %q does not contain %q", err.Error(), tc.wantErrSub)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if display != tc.wantDisplay {
				t.Errorf("displayName = %q, want %q", display, tc.wantDisplay)
			}
		})
	}
}

func TestJiraTestConnectionEmptyURL(t *testing.T) {
	_, err := TestConnection("", "user@example.com", "token")
	if err == nil || !strings.Contains(err.Error(), "required") {
		t.Errorf("expected 'required' error for empty URL, got %v", err)
	}
}
