package jira

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	baseURL    string
	user       string
	token      string
	isCloud    bool
	httpClient *http.Client
}

type ClientConfig struct {
	BaseURL string
	User    string
	Token   string
	IsCloud bool
	Timeout time.Duration
}

func NewClient(cfg ClientConfig) *Client {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	return &Client{
		baseURL: strings.TrimSuffix(cfg.BaseURL, "/"),
		user:    cfg.User,
		token:   cfg.Token,
		isCloud: cfg.IsCloud,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *Client) do(method, path string, body io.Reader) (*http.Response, error) {
	url := c.baseURL + path

	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	if c.isCloud {
		auth := base64.StdEncoding.EncodeToString([]byte(c.user + ":" + c.token))
		req.Header.Set("Authorization", "Basic "+auth)
	} else {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	var resp *http.Response
	var lastErr error

	for attempt := 0; attempt < 3; attempt++ {
		resp, lastErr = c.httpClient.Do(req)
		if lastErr != nil {
			slog.Debug("request failed", "attempt", attempt, "error", lastErr)
			time.Sleep(time.Duration(attempt+1) * time.Second)
			continue
		}

		if resp.StatusCode == 429 || resp.StatusCode == 503 {
			retryAfter := resp.Header.Get("Retry-After")
			wait := time.Duration(attempt+1) * 2 * time.Second
			if retryAfter != "" {
				if secs, err := time.ParseDuration(retryAfter + "s"); err == nil {
					wait = secs
				}
			}
			slog.Debug("rate limited, retrying", "status", resp.StatusCode, "wait", wait)
			resp.Body.Close()
			time.Sleep(wait)
			continue
		}

		return resp, nil
	}

	return nil, fmt.Errorf("request failed after retries: %w", lastErr)
}

func (c *Client) get(path string, result any) error {
	resp, err := c.do(http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("jira API error: %d - %s", resp.StatusCode, string(body))
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}

	return nil
}

func (c *Client) post(path string, body any, result any) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = strings.NewReader(string(data))
	}

	resp, err := c.do(http.MethodPost, path, bodyReader)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("jira API error: %d - %s", resp.StatusCode, string(respBody))
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}

	return nil
}

func (c *Client) put(path string, body any) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = strings.NewReader(string(data))
	}

	resp, err := c.do(http.MethodPut, path, bodyReader)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("jira API error: %d - %s", resp.StatusCode, string(respBody))
	}

	return nil
}

func (c *Client) GetIssue(issueKey string) (*Issue, error) {
	var resp issueResponse
	path := fmt.Sprintf("/rest/api/2/issue/%s?expand=names,transitions", issueKey)
	if err := c.get(path, &resp); err != nil {
		return nil, err
	}
	return parseIssue(&resp), nil
}

func (c *Client) GetBoards(projectKey string) ([]Board, error) {
	var resp boardsResponse
	path := fmt.Sprintf("/rest/agile/1.0/board?projectKeyOrId=%s", projectKey)
	if err := c.get(path, &resp); err != nil {
		return nil, err
	}
	return resp.Values, nil
}

func (c *Client) GetActiveSprint(boardID int) (*Sprint, error) {
	var resp sprintsResponse
	path := fmt.Sprintf("/rest/agile/1.0/board/%d/sprint?state=active", boardID)
	if err := c.get(path, &resp); err != nil {
		return nil, err
	}
	if len(resp.Values) == 0 {
		return nil, nil
	}
	return &resp.Values[0], nil
}

func (c *Client) GetSprintIssues(sprintID int) ([]Issue, error) {
	var resp issuesResponse
	path := fmt.Sprintf("/rest/agile/1.0/sprint/%d/issue?maxResults=100", sprintID)
	if err := c.get(path, &resp); err != nil {
		return nil, err
	}

	issues := make([]Issue, 0, len(resp.Issues))
	for _, ir := range resp.Issues {
		issues = append(issues, *parseIssue(&ir))
	}
	return issues, nil
}

func (c *Client) GetRemoteLinks(issueKey string) ([]RemoteLink, error) {
	var links []RemoteLink
	path := fmt.Sprintf("/rest/api/2/issue/%s/remotelink", issueKey)
	if err := c.get(path, &links); err != nil {
		return nil, err
	}
	return links, nil
}

func (c *Client) SearchIssues(jql string, maxResults int) ([]Issue, error) {
	if maxResults == 0 {
		maxResults = 50
	}

	body := map[string]any{
		"jql":        jql,
		"maxResults": maxResults,
		"fields":     []string{"summary", "description", "issuetype", "priority", "status", "labels", "assignee", "created", "updated"},
	}

	var resp issuesResponse
	if err := c.post("/rest/api/3/search/jql", body, &resp); err != nil {
		return nil, err
	}

	issues := make([]Issue, 0, len(resp.Issues))
	for _, ir := range resp.Issues {
		issues = append(issues, *parseIssue(&ir))
	}
	return issues, nil
}

func (c *Client) delete(path string) error {
	resp, err := c.do(http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("jira API error: %d - %s", resp.StatusCode, string(body))
	}
	return nil
}

func (c *Client) DeleteIssue(issueKey string) error {
	path := fmt.Sprintf("/rest/api/2/issue/%s", issueKey)
	return c.delete(path)
}

type CreateIssueRequest struct {
	ProjectKey  string
	Summary     string
	Description string
	IssueType   string
}

func (c *Client) CreateIssue(req CreateIssueRequest) (*Issue, error) {
	issueType := req.IssueType
	if issueType == "" {
		issueType = "Task"
	}

	body := map[string]any{
		"fields": map[string]any{
			"project": map[string]string{
				"key": req.ProjectKey,
			},
			"summary":     req.Summary,
			"description": req.Description,
			"issuetype": map[string]string{
				"name": issueType,
			},
		},
	}

	var resp struct {
		Key string `json:"key"`
	}

	if err := c.post("/rest/api/2/issue", body, &resp); err != nil {
		return nil, err
	}

	return c.GetIssue(resp.Key)
}
