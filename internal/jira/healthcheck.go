package jira

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// TestConnection calls GET /rest/api/3/myself and returns the account's
// displayName on success. Errors include HTTP status context so the caller
// can present actionable messages to the user.
func TestConnection(baseURL, email, token string) (displayName string, err error) {
	baseURL = strings.TrimSuffix(baseURL, "/")
	if baseURL == "" {
		return "", fmt.Errorf("Jira base URL is required")
	}

	url := baseURL + "/rest/api/3/myself"

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	auth := base64.StdEncoding.EncodeToString([]byte(email + ":" + token))
	req.Header.Set("Authorization", "Basic "+auth)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("connection failed (timeout or network error): %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	switch resp.StatusCode {
	case http.StatusOK:
		// OK — parse displayName
	case http.StatusUnauthorized:
		return "", fmt.Errorf("401 unauthorized — check token at https://id.atlassian.com/manage-profile/security/api-tokens")
	case http.StatusForbidden:
		return "", fmt.Errorf("403 forbidden — token valid but account lacks API access")
	case http.StatusNotFound:
		return "", fmt.Errorf("404 not found — check Jira base URL (got %s)", baseURL)
	default:
		excerpt := string(body)
		if len(excerpt) > 200 {
			excerpt = excerpt[:200] + "..."
		}
		return "", fmt.Errorf("unexpected status %d: %s", resp.StatusCode, excerpt)
	}

	var result struct {
		DisplayName string `json:"displayName"`
		AccountID   string `json:"accountId"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	if result.DisplayName == "" {
		result.DisplayName = result.AccountID
	}
	return result.DisplayName, nil
}
