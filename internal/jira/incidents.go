package jira

import "time"

// Incident is the slim row shape needed for CFR + MTTR. Defined in jira pkg
// (not metric) so the JSON decode path stays close to the wire format. Only
// CreatedAt and ResolvedAt are required; Key + Summary are kept for logging
// and incident lists.
//
// ResolvedAt is a pointer because Jira returns null for unresolved incidents,
// and MTTR must be able to distinguish "ongoing" from a zero duration.
type Incident struct {
	Key        string
	Summary    string
	IssueType  string
	Labels     []string
	CreatedAt  time.Time
	ResolvedAt *time.Time
}

// incidentSearchResponse mirrors /rest/api/3/search/jql but only decodes the
// fields needed for incident metrics. Separate from issuesResponse so the
// hot path (sprint sync, etc.) doesn't pay for resolutiondate.
type incidentSearchResponse struct {
	Issues []struct {
		Key    string `json:"key"`
		Fields struct {
			Summary   string `json:"summary"`
			IssueType struct {
				Name string `json:"name"`
			} `json:"issuetype"`
			Labels         []string `json:"labels"`
			Created        JiraTime `json:"created"`
			ResolutionDate JiraTime `json:"resolutiondate"`
		} `json:"fields"`
	} `json:"issues"`
}

// SearchIncidents runs JQL and decodes incidents with resolutiondate. Caller
// passes the full JQL (incident type / label filters built upstream so this
// stays generic). MaxResults=0 falls back to 50.
func (c *Client) SearchIncidents(jql string, maxResults int) ([]Incident, error) {
	if maxResults == 0 {
		maxResults = 50
	}
	body := map[string]any{
		"jql":        jql,
		"maxResults": maxResults,
		"fields":     []string{"summary", "issuetype", "labels", "created", "resolutiondate"},
	}
	var resp incidentSearchResponse
	if err := c.post("/rest/api/3/search/jql", body, &resp); err != nil {
		return nil, err
	}

	out := make([]Incident, 0, len(resp.Issues))
	for _, ir := range resp.Issues {
		inc := Incident{
			Key:       ir.Key,
			Summary:   ir.Fields.Summary,
			IssueType: ir.Fields.IssueType.Name,
			Labels:    ir.Fields.Labels,
			CreatedAt: ir.Fields.Created.Time,
		}
		if !ir.Fields.ResolutionDate.IsZero() {
			t := ir.Fields.ResolutionDate.Time
			inc.ResolvedAt = &t
		}
		out = append(out, inc)
	}
	return out, nil
}
