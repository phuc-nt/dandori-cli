package db

import (
	"encoding/json"
	"fmt"
	"time"
)

// IntentData holds the G8 intent.extracted event payload for a run.
type IntentData struct {
	FirstUserMsg string          `json:"first_user_msg"`
	Summary      string          `json:"summary"`
	SpecLinks    IntentSpecLinks `json:"spec_links"`
}

// IntentSpecLinks mirrors the spec_links sub-object stored in the event payload.
type IntentSpecLinks struct {
	JiraKey        string   `json:"jira_key"`
	ConfluenceURLs []string `json:"confluence_urls"`
	SourcePaths    []string `json:"source_paths"`
}

// IntentDecision holds one decision.point event payload for a run.
type IntentDecision struct {
	Chosen    string   `json:"chosen"`
	Rejected  []string `json:"rejected,omitempty"`
	Rationale string   `json:"rationale,omitempty"`
}

// RunIntentEvents aggregates the G8 Layer-4 intent events for a single run.
// Intent is nil when no intent.extracted event exists for the run.
type RunIntentEvents struct {
	Intent    *IntentData
	Decisions []IntentDecision
}

// RecentIntentEvent is one row in the intent feed returned by GetRecentIntentEvents.
// It joins the events table with the runs table to surface engineer attribution.
type RecentIntentEvent struct {
	ID           int64           `json:"id"`
	RunID        string          `json:"run_id"`
	EventType    string          `json:"event_type"`
	Data         json.RawMessage `json:"data"`
	TS           time.Time       `json:"ts"`
	EngineerName string          `json:"engineer_name,omitempty"`
	JiraIssueKey string          `json:"jira_issue_key,omitempty"`
}

// GetRecentIntentEvents returns the most recent layer-4 events, newest first,
// up to limit rows. Pass engineer="" to skip engineer filter, project="" to
// skip project filter. Only event_type rows 'intent.extracted' and
// 'decision.point' are included (same set as the existing GetIntentEvents
// function). Project filter matches runs whose jira_issue_key starts with
// "<project>-" (e.g. project="CLITEST" matches "CLITEST-123").
func (l *LocalDB) GetRecentIntentEvents(limit int, engineer, project string) ([]RecentIntentEvent, error) {
	if limit <= 0 {
		limit = 20
	}
	projLike := ""
	if project != "" {
		projLike = project + "-%"
	}
	query := `
		SELECT e.id, e.run_id, e.event_type, e.data, e.ts,
		       COALESCE(r.engineer_name, '') AS engineer_name,
		       COALESCE(r.jira_issue_key, '') AS jira_issue_key
		FROM events e
		JOIN runs r ON r.id = e.run_id
		WHERE e.layer = 4
		  AND e.event_type IN ('intent.extracted', 'decision.point')
		  AND (? = '' OR COALESCE(r.engineer_name, '') = ?)
		  AND (? = '' OR COALESCE(r.jira_issue_key, '') LIKE ?)
		ORDER BY e.ts DESC, e.id DESC
		LIMIT ?
	`
	rows, err := l.db.Query(query, engineer, engineer, projLike, projLike, limit)
	if err != nil {
		return nil, fmt.Errorf("query recent intent events: %w", err)
	}
	defer rows.Close()

	var out []RecentIntentEvent
	for rows.Next() {
		var ev RecentIntentEvent
		var tsStr string
		var rawData string
		if err := rows.Scan(&ev.ID, &ev.RunID, &ev.EventType, &rawData, &tsStr, &ev.EngineerName, &ev.JiraIssueKey); err != nil {
			return nil, fmt.Errorf("scan recent intent event: %w", err)
		}
		ev.Data = json.RawMessage(rawData)
		if t, err := parseTime(tsStr); err == nil {
			ev.TS = t
		}
		out = append(out, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recent intent events: %w", err)
	}
	return out, nil
}

// GetIntentEvents queries the events table for layer-4 G8 events belonging to
// runID and returns the parsed results. Returns nil Intent when the run has no
// intent.extracted event (e.g. legacy run or extraction was disabled).
//
// This function is fail-soft: any single event's JSON parse error is silently
// skipped so a malformed event does not block the Jira comment sync.
func (l *LocalDB) GetIntentEvents(runID string) (*RunIntentEvents, error) {
	rows, err := l.db.Query(`
		SELECT event_type, data
		FROM events
		WHERE run_id = ?
		  AND layer = 4
		  AND event_type IN ('intent.extracted', 'decision.point')
		ORDER BY id ASC
	`, runID)
	if err != nil {
		return nil, fmt.Errorf("query intent events: %w", err)
	}
	defer rows.Close()

	result := &RunIntentEvents{}

	for rows.Next() {
		var eventType, data string
		if err := rows.Scan(&eventType, &data); err != nil {
			// Skip malformed row — fail-soft.
			continue
		}

		switch eventType {
		case "intent.extracted":
			var d IntentData
			if err := json.Unmarshal([]byte(data), &d); err != nil {
				// Malformed payload — skip gracefully.
				continue
			}
			result.Intent = &d

		case "decision.point":
			var d IntentDecision
			if err := json.Unmarshal([]byte(data), &d); err != nil {
				continue
			}
			if d.Chosen != "" {
				result.Decisions = append(result.Decisions, d)
			}
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate intent events: %w", err)
	}

	return result, nil
}
