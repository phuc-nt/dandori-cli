package confluence

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// TestConnection verifies Confluence credentials by fetching the space with
// the given key. It tries the Cloud path (/wiki/rest/api/space/{key}) first;
// if that returns 404 it falls back to the Data Center path
// (/rest/api/space/{key}) to support self-hosted instances.
//
// Returns the space name on success. Errors include HTTP status context.
func TestConnection(baseURL, spaceKey, email, token string) (spaceName string, err error) {
	baseURL = strings.TrimSuffix(baseURL, "/")
	if baseURL == "" {
		return "", fmt.Errorf("Confluence base URL is required")
	}
	if spaceKey == "" {
		return "", fmt.Errorf("Confluence space key is required")
	}

	client := &http.Client{Timeout: 10 * time.Second}
	auth := base64.StdEncoding.EncodeToString([]byte(email + ":" + token))

	// Try Cloud path first.
	name, cloudErr := trySpacePath(client, auth, baseURL+"/wiki/rest/api/space/"+spaceKey)
	if cloudErr == nil {
		return name, nil
	}

	// If 404, fall back to Data Center path.
	if strings.HasPrefix(cloudErr.Error(), "404") {
		name, dcErr := trySpacePath(client, auth, baseURL+"/rest/api/space/"+spaceKey)
		if dcErr == nil {
			return name, nil
		}
		// Return DC error if DC also failed (more informative for DC users).
		return "", dcErr
	}

	return "", cloudErr
}

func trySpacePath(client *http.Client, auth, url string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Basic "+auth)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("connection failed (timeout or network error): %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	switch resp.StatusCode {
	case http.StatusOK:
		// Parse space name below.
	case http.StatusUnauthorized:
		return "", fmt.Errorf("401 unauthorized — check token at https://id.atlassian.com/manage-profile/security/api-tokens")
	case http.StatusForbidden:
		return "", fmt.Errorf("403 forbidden — token valid but account lacks space access")
	case http.StatusNotFound:
		return "", fmt.Errorf("404 not found — check Confluence base URL or space key")
	default:
		excerpt := string(body)
		if len(excerpt) > 200 {
			excerpt = excerpt[:200] + "..."
		}
		return "", fmt.Errorf("%d unexpected status: %s", resp.StatusCode, excerpt)
	}

	var result struct {
		Key  string `json:"key"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	name := result.Name
	if name == "" {
		name = result.Key
	}
	return name, nil
}
