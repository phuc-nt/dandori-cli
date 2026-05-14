package github

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// TestConnection verifies the PAT is valid and can access the repo. Calls
// GET /user to extract login, then GET /repos/{repo} for access check.
// Returns a human-readable message on success: "<login> can access <repo>".
func TestConnection(repo, token string) (msg string, err error) {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return "", fmt.Errorf("GitHub repo is required (format: owner/name)")
	}
	if !strings.Contains(repo, "/") {
		return "", fmt.Errorf("GitHub repo must be owner/name (got %q)", repo)
	}
	if token == "" {
		return "", fmt.Errorf("GitHub token is required")
	}

	httpClient := &http.Client{Timeout: 10 * time.Second}

	login, err := fetchLogin(httpClient, token)
	if err != nil {
		return "", err
	}

	if err := verifyRepoAccess(httpClient, token, repo); err != nil {
		return "", err
	}

	return fmt.Sprintf("%s can access %s", login, repo), nil
}

// fetchLogin calls /user and returns the authenticated account's login.
// Error messages map GitHub's status codes to actionable guidance.
func fetchLogin(client *http.Client, token string) (string, error) {
	req, _ := http.NewRequest(http.MethodGet, "https://api.github.com/user", nil)
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("connection failed (timeout or network error): %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return "", fmt.Errorf("401 unauthorized — check PAT at https://github.com/settings/tokens (needs `repo` scope for private repos)")
	case http.StatusForbidden:
		return "", fmt.Errorf("403 forbidden — PAT valid but insufficient scope")
	default:
		return "", fmt.Errorf("unexpected /user status %d: %s", resp.StatusCode, excerpt(body))
	}

	var u struct {
		Login string `json:"login"`
	}
	if err := json.Unmarshal(body, &u); err != nil {
		return "", fmt.Errorf("parse /user response: %w", err)
	}
	if u.Login == "" {
		return "", fmt.Errorf("GitHub returned empty login")
	}
	return u.Login, nil
}

// verifyRepoAccess calls /repos/{repo} and converts non-2xx responses into
// user-friendly errors.
func verifyRepoAccess(client *http.Client, token, repo string) error {
	req, _ := http.NewRequest(http.MethodGet, "https://api.github.com/repos/"+repo, nil)
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("repo check failed: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusNotFound:
		return fmt.Errorf("404 not found — repo %q does not exist or PAT cannot see it (needs `repo` scope for private)", repo)
	case http.StatusForbidden:
		return fmt.Errorf("403 forbidden — PAT lacks access to %s", repo)
	default:
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected /repos status %d: %s", resp.StatusCode, excerpt(body))
	}
}

// excerpt trims response bodies for error messages.
func excerpt(b []byte) string {
	s := string(b)
	if len(s) > 200 {
		return s[:200] + "..."
	}
	return s
}
