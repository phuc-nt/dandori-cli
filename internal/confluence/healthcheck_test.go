package confluence

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConfluenceTestConnection(t *testing.T) {
	tests := []struct {
		name string
		// handler receives path so tests can differentiate Cloud vs DC paths.
		handler    http.HandlerFunc
		spaceKey   string
		wantSpace  string
		wantErrSub string
	}{
		{
			name:     "Cloud path returns space name",
			spaceKey: "ENG",
			handler: func(w http.ResponseWriter, r *http.Request) {
				// Cloud path: /wiki/rest/api/space/{key}
				if r.URL.Path == "/wiki/rest/api/space/ENG" {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(`{"key":"ENG","name":"Engineering"}`))
					return
				}
				http.NotFound(w, r)
			},
			wantSpace: "Engineering",
		},
		{
			name:     "DC fallback when Cloud 404",
			spaceKey: "OPS",
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/wiki/rest/api/space/OPS":
					http.NotFound(w, r)
				case "/rest/api/space/OPS":
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(`{"key":"OPS","name":"Operations"}`))
				default:
					http.NotFound(w, r)
				}
			},
			wantSpace: "Operations",
		},
		{
			name:     "falls back to key when name is empty",
			spaceKey: "NONAME",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "NONAME") && strings.Contains(r.URL.Path, "wiki") {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(`{"key":"NONAME","name":""}`))
					return
				}
				http.NotFound(w, r)
			},
			wantSpace: "NONAME",
		},
		{
			name:     "401 returns token hint",
			spaceKey: "ANY",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
			},
			wantErrSub: "401 unauthorized",
		},
		{
			name:     "403 returns forbidden message",
			spaceKey: "ANY",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "Forbidden", http.StatusForbidden)
			},
			wantErrSub: "403 forbidden",
		},
		{
			name:     "both Cloud and DC 404 returns error",
			spaceKey: "MISSING",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.NotFound(w, r)
			},
			wantErrSub: "404",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			space, err := TestConnection(srv.URL, tc.spaceKey, "user@example.com", "token123")

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
			if space != tc.wantSpace {
				t.Errorf("spaceName = %q, want %q", space, tc.wantSpace)
			}
		})
	}
}

func TestConfluenceTestConnectionEmptyURL(t *testing.T) {
	_, err := TestConnection("", "ENG", "user@example.com", "token")
	if err == nil || !strings.Contains(err.Error(), "required") {
		t.Errorf("expected 'required' error for empty URL, got %v", err)
	}
}

func TestConfluenceTestConnectionEmptySpaceKey(t *testing.T) {
	_, err := TestConnection("http://example.com", "", "user@example.com", "token")
	if err == nil || !strings.Contains(err.Error(), "required") {
		t.Errorf("expected 'required' error for empty space key, got %v", err)
	}
}
