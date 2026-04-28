package confluence

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var (
	ErrPageNotFound = errors.New("page not found")
	ErrEmptyBaseURL = errors.New("empty base URL")
)

// FlexID handles IDs that can be string or number in JSON
type FlexID string

func (f *FlexID) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*f = FlexID(s)
		return nil
	}
	var n int64
	if err := json.Unmarshal(b, &n); err == nil {
		*f = FlexID(fmt.Sprintf("%d", n))
		return nil
	}
	return fmt.Errorf("cannot unmarshal %s into FlexID", string(b))
}

type Page struct {
	ID      string      `json:"id"`
	Title   string      `json:"title"`
	Type    string      `json:"type"`
	Status  string      `json:"status"`
	Body    PageBody    `json:"body"`
	Version PageVersion `json:"version"`
	Space   *Space      `json:"space,omitempty"`
	Links   PageLinks   `json:"_links,omitempty"`
}

type PageBody struct {
	Storage StorageBody `json:"storage"`
}

type StorageBody struct {
	Value          string `json:"value"`
	Representation string `json:"representation"`
}

type PageVersion struct {
	Number int `json:"number"`
}

type PageLinks struct {
	WebUI string `json:"webui"`
	Base  string `json:"base"`
}

type Space struct {
	ID   FlexID `json:"id"`
	Key  string `json:"key"`
	Name string `json:"name"`
}

type PageSearchResult struct {
	Results []Page `json:"results"`
	Size    int    `json:"size"`
}

type CreatePageRequest struct {
	SpaceKey string `json:"-"`
	Title    string `json:"title"`
	Body     string `json:"-"`
	ParentID string `json:"-"`
}

type createPageAPIRequest struct {
	Type  string `json:"type"`
	Title string `json:"title"`
	Space struct {
		Key string `json:"key"`
	} `json:"space"`
	Body struct {
		Storage struct {
			Value          string `json:"value"`
			Representation string `json:"representation"`
		} `json:"storage"`
	} `json:"body"`
	Ancestors []struct {
		ID string `json:"id"`
	} `json:"ancestors,omitempty"`
}

type UpdatePageRequest struct {
	Title   string      `json:"title"`
	Body    string      `json:"-"`
	Version PageVersion `json:"version"`
}

type updatePageAPIRequest struct {
	Type    string      `json:"type"`
	Title   string      `json:"title"`
	Version PageVersion `json:"version"`
	Body    struct {
		Storage struct {
			Value          string `json:"value"`
			Representation string `json:"representation"`
		} `json:"storage"`
	} `json:"body"`
}

type RunReport struct {
	RunID         string
	IssueKey      string
	AgentName     string
	Status        string
	Duration      time.Duration
	CostUSD       float64
	InputTokens   int
	OutputTokens  int
	Model         string
	GitHeadBefore string
	GitHeadAfter  string
	FilesChanged  []string
	Decisions     []string
	GitDiff       string
	Summary       string
	StartedAt     time.Time
	EndedAt       time.Time
}

func (r RunReport) Validate() error {
	if r.RunID == "" {
		return errors.New("run ID is required")
	}
	// IssueKey is optional: runs may be ad-hoc agent invocations not tied
	// to a Jira ticket; the report still posts to Confluence.
	return nil
}
