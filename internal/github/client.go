package github

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Client talks to GitHub REST v3. Single-repo scope; multi-repo deferred.
type Client struct {
	repo       string // "owner/name"
	token      string
	baseURL    string
	httpClient *http.Client
}

// ClientConfig configures Client construction. Token is required; BaseURL
// defaults to api.github.com so tests can swap in httptest.
type ClientConfig struct {
	Repo    string
	Token   string
	BaseURL string
	Timeout time.Duration
}

// NewClient builds a client with sensible defaults. Empty BaseURL → api.github.com.
func NewClient(cfg ClientConfig) *Client {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	base := cfg.BaseURL
	if base == "" {
		base = "https://api.github.com"
	}
	return &Client{
		repo:       cfg.Repo,
		token:      cfg.Token,
		baseURL:    strings.TrimSuffix(base, "/"),
		httpClient: &http.Client{Timeout: timeout},
	}
}

// do issues an authenticated request with retry on 429/503 and the secondary
// rate-limit signal (403 + X-RateLimit-Remaining: 0). Honors Retry-After and
// X-RateLimit-Reset. Caller owns the returned response body.
func (c *Client) do(method, url string) (*http.Response, error) {
	var resp *http.Response
	var lastErr error

	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequest(method, url, nil)
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		if c.token != "" {
			req.Header.Set("Authorization", "token "+c.token)
		}

		resp, lastErr = c.httpClient.Do(req)
		if lastErr != nil {
			slog.Debug("github request failed", "attempt", attempt, "error", lastErr)
			time.Sleep(time.Duration(attempt+1) * time.Second)
			continue
		}

		if isTransient(resp) {
			wait := backoff(resp, attempt)
			slog.Debug("github rate limited, retrying",
				"status", resp.StatusCode,
				"remaining", resp.Header.Get("X-RateLimit-Remaining"),
				"wait", wait)
			resp.Body.Close()
			time.Sleep(wait)
			continue
		}
		return resp, nil
	}

	if lastErr != nil {
		return nil, fmt.Errorf("github request failed after retries: %w", lastErr)
	}
	return resp, nil
}

// isTransient returns true if the response is a rate-limit / overload signal
// that warrants a retry. 403 with X-RateLimit-Remaining: 0 is GitHub's
// secondary rate limit / abuse detection.
func isTransient(resp *http.Response) bool {
	if resp.StatusCode == 429 || resp.StatusCode == 503 {
		return true
	}
	if resp.StatusCode == 403 && resp.Header.Get("X-RateLimit-Remaining") == "0" {
		return true
	}
	return false
}

// backoff returns the wait duration between retries. Prefers Retry-After,
// then X-RateLimit-Reset, then exponential fallback.
func backoff(resp *http.Response, attempt int) time.Duration {
	if v := resp.Header.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
	}
	if v := resp.Header.Get("X-RateLimit-Reset"); v != "" {
		if ts, err := strconv.ParseInt(v, 10, 64); err == nil {
			wait := time.Until(time.Unix(ts, 0))
			if wait > 0 && wait < 5*time.Minute {
				return wait
			}
		}
	}
	return time.Duration(attempt+1) * 2 * time.Second
}

// get fetches a URL and decodes JSON into result. Returns the response so
// callers can inspect headers (e.g. Link for pagination).
func (c *Client) get(url string, result any) (*http.Response, error) {
	resp, err := c.do(http.MethodGet, url)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("github API error: %d - %s", resp.StatusCode, string(body))
	}
	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("decode response: %w", err)
		}
	}
	resp.Body.Close()
	return resp, nil
}

// linkNextRegex matches the next-page URL in a GitHub Link header.
//   `<https://api.github.com/...&page=2>; rel="next", <...>; rel="last"`
var linkNextRegex = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)

// parseNextLink extracts the next-page URL from a Link header. Returns ""
// when there is no next page.
func parseNextLink(linkHeader string) string {
	m := linkNextRegex.FindStringSubmatch(linkHeader)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

// ListPRs returns PRs in the configured repo updated at or after `since`,
// filtered by state ("open"|"closed"|"all"). Iterates the cursor-style
// pagination via Link header. Stops early once the oldest PR on a page is
// older than `since` — pagination is sorted by updated desc.
func (c *Client) ListPRs(since time.Time, state string) ([]PR, error) {
	if state == "" {
		state = StateAll
	}
	url := fmt.Sprintf("%s/repos/%s/pulls?state=%s&sort=updated&direction=desc&per_page=100",
		c.baseURL, c.repo, state)

	var out []PR
	for url != "" {
		var page []PR
		resp, err := c.get(url, &page)
		if err != nil {
			return nil, err
		}

		stop := false
		for _, p := range page {
			if !since.IsZero() && p.UpdatedAt.Before(since) {
				stop = true
				break
			}
			out = append(out, p)
		}
		if stop {
			break
		}
		url = parseNextLink(resp.Header.Get("Link"))
	}
	return out, nil
}

// GetPRDetail fetches a single PR with the full schema, including
// additions/deletions which the list endpoint omits.
func (c *Client) GetPRDetail(prNumber int) (*PR, error) {
	url := fmt.Sprintf("%s/repos/%s/pulls/%d", c.baseURL, c.repo, prNumber)
	var out PR
	if _, err := c.get(url, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetPRReviews returns all reviews on a single PR.
func (c *Client) GetPRReviews(prNumber int) ([]Review, error) {
	url := fmt.Sprintf("%s/repos/%s/pulls/%d/reviews?per_page=100",
		c.baseURL, c.repo, prNumber)
	var out []Review
	if _, err := c.get(url, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetPRCommits returns all commits on a single PR (capped at 250 by GitHub).
func (c *Client) GetPRCommits(prNumber int) ([]Commit, error) {
	url := fmt.Sprintf("%s/repos/%s/pulls/%d/commits?per_page=100",
		c.baseURL, c.repo, prNumber)
	var out []Commit
	if _, err := c.get(url, &out); err != nil {
		return nil, err
	}
	return out, nil
}
