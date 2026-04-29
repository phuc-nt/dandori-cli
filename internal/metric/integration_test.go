package metric

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/phuc-nt/dandori-cli/internal/db"
	"github.com/phuc-nt/dandori-cli/internal/jira"
)

// jiraMockState is the canned data the mock server replays. Tests construct
// it once, swap into a httptest server, then assert against export output.
type jiraMockState struct {
	deployIssues   []jiraSearchHit
	incidents      []jiraIncidentHit
	changelogs     map[string]jiraChangelog
	searchHits     int // counter to assert call shape
	changelogHits  map[string]int
	incidentHits   int
	failNextSearch bool
}

type jiraSearchHit struct {
	Key string `json:"key"`
}

type jiraIncidentHit struct {
	Key       string   `json:"key"`
	Summary   string   `json:"summary"`
	IssueType string   `json:"issueType"`
	Labels    []string `json:"labels"`
	Created   string   `json:"created"`
	Resolved  string   `json:"resolved"` // empty = ongoing
}

type jiraChangelog struct {
	histories []changelogEntry
}

type changelogEntry struct {
	from, to string
	when     time.Time
}

// startMockJira returns a server that recognises the 3 endpoints we hit:
// POST /rest/api/3/search/jql (returns deploy hits OR incident hits based on JQL),
// GET  /rest/api/2/issue/{key}?expand=changelog (returns canned changelog).
func startMockJira(t *testing.T, state *jiraMockState) *httptest.Server {
	t.Helper()
	if state.changelogHits == nil {
		state.changelogHits = map[string]int{}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/search/jql"):
			if state.failNextSearch {
				state.failNextSearch = false
				http.Error(w, `{"errorMessages":["mock failure"]}`, http.StatusInternalServerError)
				return
			}
			body := readJSON(r)
			jql, _ := body["jql"].(string)
			if strings.Contains(jql, "issuetype IN") || strings.Contains(jql, "labels IN") {
				state.incidentHits++
				writeIncidentResponse(w, state.incidents)
				return
			}
			state.searchHits++
			writeDeploySearchResponse(w, state.deployIssues)

		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/issue/"):
			key := extractIssueKey(r.URL.Path)
			state.changelogHits[key]++
			writeChangelogResponse(w, key, state.changelogs[key])

		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func readJSON(r *http.Request) map[string]any {
	var out map[string]any
	_ = json.NewDecoder(r.Body).Decode(&out)
	return out
}

func writeDeploySearchResponse(w http.ResponseWriter, hits []jiraSearchHit) {
	out := map[string]any{"issues": []map[string]any{}}
	issues := []map[string]any{}
	for _, h := range hits {
		issues = append(issues, map[string]any{
			"key":    h.Key,
			"fields": map[string]any{"summary": "deploy " + h.Key, "issuetype": map[string]any{"name": "Task"}, "status": map[string]any{"name": "Released", "statusCategory": map[string]any{"key": "done"}}},
		})
	}
	out["issues"] = issues
	_ = json.NewEncoder(w).Encode(out)
}

func writeIncidentResponse(w http.ResponseWriter, hits []jiraIncidentHit) {
	issues := []map[string]any{}
	for _, h := range hits {
		fields := map[string]any{
			"summary":   h.Summary,
			"issuetype": map[string]any{"name": h.IssueType},
			"labels":    h.Labels,
			"created":   h.Created,
		}
		if h.Resolved != "" {
			fields["resolutiondate"] = h.Resolved
		} else {
			fields["resolutiondate"] = nil
		}
		issues = append(issues, map[string]any{"key": h.Key, "fields": fields})
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"issues": issues})
}

func writeChangelogResponse(w http.ResponseWriter, key string, cl jiraChangelog) {
	histories := []map[string]any{}
	for _, h := range cl.histories {
		histories = append(histories, map[string]any{
			"created": h.when.UTC().Format("2006-01-02T15:04:05.000-0700"),
			"author":  map[string]any{"displayName": "tester"},
			"items": []map[string]any{
				{"field": "status", "fromString": h.from, "toString": h.to},
			},
		})
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"key":       key,
		"changelog": map[string]any{"histories": histories},
	})
}

func extractIssueKey(path string) string {
	// /rest/api/2/issue/KEY-123 → split gives [rest, api, 2, issue, KEY-123]
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) < 5 {
		return ""
	}
	return parts[4]
}

// setupIntegrationDB creates a fresh SQLite store with the migrated schema
// and seeds it with `total` runs (some with iteration events).
func setupIntegrationDB(t *testing.T, total, withRework int, windowEnd time.Time) *db.LocalDB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "e2e.db")
	store, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	if err := store.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	for i := 0; i < total; i++ {
		runID := fmt.Sprintf("run-%03d", i)
		startedAt := windowEnd.Add(-time.Duration(total-i) * time.Hour)
		_, err := store.Exec(`
			INSERT INTO runs (id, jira_issue_key, agent_type, user, workstation_id, started_at, status, department)
			VALUES (?, ?, 'claude_code', 'tester', 'ws-1', ?, ?, ?)
		`, runID, fmt.Sprintf("E2E-%d", i), startedAt.Format(time.RFC3339), "done", "payments")
		if err != nil {
			t.Fatalf("insert run: %v", err)
		}
		if i < withRework {
			data := fmt.Sprintf(`{"round":2,"issue_key":"E2E-%d","transitioned_at":"%s"}`,
				i, startedAt.Format(time.RFC3339))
			_, err := store.Exec(`
				INSERT INTO events (run_id, layer, event_type, data, ts)
				VALUES (?, 4, 'task.iteration.start', ?, ?)
			`, runID, data, startedAt.Format(time.RFC3339))
			if err != nil {
				t.Fatalf("insert event: %v", err)
			}
		}
	}
	return store
}

func makeJiraClient(t *testing.T, srv *httptest.Server) *jira.Client {
	t.Helper()
	return jira.NewClient(jira.ClientConfig{
		BaseURL: srv.URL, User: "u", Token: "t", IsCloud: true,
	})
}

func TestE2E_HappyPath(t *testing.T) {
	end := time.Date(2026, 4, 29, 0, 0, 0, 0, time.UTC)
	start := end.AddDate(0, 0, -28)
	mid := start.Add(14 * 24 * time.Hour)

	deploys := []jiraSearchHit{}
	changelogs := map[string]jiraChangelog{}
	for i := 0; i < 20; i++ {
		key := fmt.Sprintf("D-%d", i)
		deploys = append(deploys, jiraSearchHit{Key: key})
		changelogs[key] = jiraChangelog{
			histories: []changelogEntry{
				{from: "Backlog", to: "In Progress", when: mid.Add(-time.Duration(i+1) * time.Hour)},
				{from: "In Progress", to: "Released", when: mid.Add(time.Duration(i) * time.Hour)},
			},
		}
	}
	state := &jiraMockState{
		deployIssues: deploys,
		changelogs:   changelogs,
		incidents: []jiraIncidentHit{
			{Key: "I-1", Summary: "boom", IssueType: "Incident",
				Created:  mid.Add(time.Hour).Format("2006-01-02T15:04:05.000-0700"),
				Resolved: mid.Add(2 * time.Hour).Format("2006-01-02T15:04:05.000-0700")},
			{Key: "I-2", Summary: "kaboom", IssueType: "Incident",
				Created:  mid.Add(3 * time.Hour).Format("2006-01-02T15:04:05.000-0700"),
				Resolved: mid.Add(7 * time.Hour).Format("2006-01-02T15:04:05.000-0700")},
			{Key: "I-3", Summary: "ongoing", IssueType: "Incident",
				Created: mid.Add(5 * time.Hour).Format("2006-01-02T15:04:05.000-0700")},
		},
	}
	srv := startMockJira(t, state)
	store := setupIntegrationDB(t, 50, 5, end)

	cfg := ExportConfig{
		Window:     MetricWindow{Start: start, End: end},
		StatusCfg:  DefaultJiraStatusConfig(),
		IncidentCf: IncidentMatchConfig{IssueTypes: []string{"Incident"}},
		MaxResults: 100,
	}
	client := makeJiraClient(t, srv)
	rep, err := Run(ExportSources{Jira: NewJiraSource(client), Rework: store}, cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if rep.Deploy.Count != 20 {
		t.Errorf("deploys=%d want 20", rep.Deploy.Count)
	}
	if rep.LeadTime.SamplesUsed != 20 {
		t.Errorf("lead samples=%d want 20", rep.LeadTime.SamplesUsed)
	}
	if rep.CFR.IncidentCount != 3 || rep.CFR.DeployCount != 20 {
		t.Errorf("cfr counts=(%d/%d) want 3/20", rep.CFR.IncidentCount, rep.CFR.DeployCount)
	}
	if rep.CFR.Rate != 0.15 {
		t.Errorf("cfr=%v want 0.15", rep.CFR.Rate)
	}
	if rep.MTTR.SamplesUsed != 2 || rep.MTTR.OngoingIncidents != 1 {
		t.Errorf("mttr samples=%d ongoing=%d want 2/1", rep.MTTR.SamplesUsed, rep.MTTR.OngoingIncidents)
	}
	if rep.Rework.ReworkCount != 5 || rep.Rework.TotalCount != 50 {
		t.Errorf("rework=%d/%d want 5/50", rep.Rework.ReworkCount, rep.Rework.TotalCount)
	}
	if rep.Rework.Rate != 0.10 {
		t.Errorf("rework rate=%v want 0.10", rep.Rework.Rate)
	}
	if rep.Rework.ExceedsThreshold {
		t.Errorf("rework should NOT exceed threshold at exact boundary (strict >)")
	}

	// Format all 3 — assert valid JSON shape, not exact content (timestamps drift).
	for _, f := range []Format{FormatFaros, FormatOobeya, FormatRaw} {
		body, err := FormatReport(rep, f)
		if err != nil {
			t.Errorf("format %s: %v", f, err)
		}
		var got map[string]any
		if err := json.Unmarshal(body, &got); err != nil {
			t.Errorf("format %s invalid JSON: %v", f, err)
		}
	}
}

func TestE2E_EmptyEverything(t *testing.T) {
	end := time.Date(2026, 4, 29, 0, 0, 0, 0, time.UTC)
	start := end.AddDate(0, 0, -28)
	state := &jiraMockState{}
	srv := startMockJira(t, state)
	store := setupIntegrationDB(t, 0, 0, end)
	client := makeJiraClient(t, srv)

	cfg := ExportConfig{
		Window:     MetricWindow{Start: start, End: end},
		StatusCfg:  DefaultJiraStatusConfig(),
		IncidentCf: IncidentMatchConfig{IssueTypes: []string{"Incident"}},
	}
	rep, err := Run(ExportSources{Jira: NewJiraSource(client), Rework: store}, cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, want := range []string{"deployment_frequency", "lead_time_for_changes", "change_failure_rate", "time_to_restore_service", "rework_rate"} {
		if !contains(rep.InsufficientData, want) {
			t.Errorf("expected %s in insufficient_data, got %v", want, rep.InsufficientData)
		}
	}

	// Faros must encode null for value, not 0.
	body, _ := FormatReport(rep, FormatFaros)
	if !strings.Contains(string(body), `"value": null`) {
		t.Errorf("expected null values, got: %s", body)
	}
}

func TestE2E_JiraAPIError(t *testing.T) {
	end := time.Date(2026, 4, 29, 0, 0, 0, 0, time.UTC)
	start := end.AddDate(0, 0, -28)
	state := &jiraMockState{failNextSearch: true}
	srv := startMockJira(t, state)
	store := setupIntegrationDB(t, 0, 0, end)
	client := makeJiraClient(t, srv)

	cfg := ExportConfig{
		Window:     MetricWindow{Start: start, End: end},
		StatusCfg:  DefaultJiraStatusConfig(),
		IncidentCf: IncidentMatchConfig{IssueTypes: []string{"Incident"}},
	}
	_, err := Run(ExportSources{Jira: NewJiraSource(client), Rework: store}, cfg)
	if err == nil {
		t.Error("want err on Jira 500")
	}
}

func TestE2E_ReworkAtBoundary(t *testing.T) {
	// 10 of 100 rework runs = exactly 10% threshold → should NOT exceed (strict >).
	end := time.Date(2026, 4, 29, 0, 0, 0, 0, time.UTC)
	start := end.AddDate(0, 0, -28)
	state := &jiraMockState{}
	srv := startMockJira(t, state)
	store := setupIntegrationDB(t, 100, 10, end)
	client := makeJiraClient(t, srv)

	cfg := ExportConfig{
		Window:     MetricWindow{Start: start, End: end},
		StatusCfg:  DefaultJiraStatusConfig(),
		IncidentCf: IncidentMatchConfig{IssueTypes: []string{"Incident"}},
	}
	rep, _ := Run(ExportSources{Jira: NewJiraSource(client), Rework: store}, cfg)
	if rep.Rework.Rate != 0.10 {
		t.Errorf("rate=%v want 0.10", rep.Rework.Rate)
	}
	if rep.Rework.ExceedsThreshold {
		t.Errorf("rate exactly == threshold should NOT exceed (strict >)")
	}
}
