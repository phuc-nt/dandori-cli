package jira

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/phuc-nt/dandori-cli/internal/db"
	"github.com/phuc-nt/dandori-cli/internal/event"
)

// stubBugServer responds to Jira /search/jql with one Bug issue linked
// to TASK-9. /board/1/sprint and /sprint/X/issue return empty so the
// main poll cycle is a no-op.
func stubBugServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/board/") && strings.HasSuffix(r.URL.Path, "/sprint"):
			_, _ = w.Write([]byte(`{"values":[]}`))
		case strings.Contains(r.URL.Path, "/sprint/") && strings.HasSuffix(r.URL.Path, "/issue"):
			_, _ = w.Write([]byte(`{"issues":[]}`))
		case strings.Contains(r.URL.Path, "/search/jql"):
			_, _ = w.Write([]byte(`{
				"issues": [{
					"key": "BUG-1",
					"fields": {
						"summary": "save crashes",
						"description": "",
						"issuetype": {"name": "Bug"},
						"status": {"name": "Open", "statusCategory": {"key": "new"}},
						"issuelinks": [
							{"type": {"name": "is caused by"}, "inwardIssue": {"key": "TASK-9"}}
						]
					}
				}]
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
}

func setupBugTestDB(t *testing.T) *db.LocalDB {
	t.Helper()
	tmp := t.TempDir() + "/test.db"
	d, err := db.Open(tmp)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := d.Exec(`
		INSERT INTO runs (id, jira_issue_key, agent_type, agent_name, user, workstation_id, started_at, ended_at, status)
		VALUES ('run-task9', 'TASK-9', 'claude_code', 'alpha', 'tester', 'ws', ?, ?, 'done')
	`, time.Now().Add(-time.Hour).Format(time.RFC3339), time.Now().Format(time.RFC3339)); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	return d
}

func TestBugLinkCycle_EmitsBugFiledEvent(t *testing.T) {
	srv := stubBugServer(t)
	defer srv.Close()
	d := setupBugTestDB(t)
	rec := event.NewRecorder(d)

	p := NewPoller(PollerConfig{
		Client:   NewClient(ClientConfig{BaseURL: srv.URL, User: "u", Token: "t", IsCloud: true}),
		BoardID:  1,
		LocalDB:  d,
		Recorder: rec,
	})

	if err := p.bugLinkCycle(context.Background()); err != nil {
		t.Fatalf("bugLinkCycle: %v", err)
	}

	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM events WHERE event_type='bug.filed' AND run_id='run-task9'`).Scan(&n); err != nil {
		t.Fatalf("query: %v", err)
	}
	if n != 1 {
		t.Errorf("got %d bug.filed events, want 1", n)
	}
}

func TestBugLinkCycle_DedupesAcrossCycles(t *testing.T) {
	srv := stubBugServer(t)
	defer srv.Close()
	d := setupBugTestDB(t)
	rec := event.NewRecorder(d)

	p := NewPoller(PollerConfig{
		Client:   NewClient(ClientConfig{BaseURL: srv.URL, User: "u", Token: "t", IsCloud: true}),
		BoardID:  1,
		LocalDB:  d,
		Recorder: rec,
	})

	for i := 0; i < 3; i++ {
		if err := p.bugLinkCycle(context.Background()); err != nil {
			t.Fatalf("cycle %d: %v", i, err)
		}
	}

	var n int
	_ = d.QueryRow(`SELECT COUNT(*) FROM events WHERE event_type='bug.filed'`).Scan(&n)
	if n != 1 {
		t.Errorf("got %d events after 3 cycles, want 1 (dedupe)", n)
	}
}

func TestBugLinkCycle_NoDB_NoOp(t *testing.T) {
	srv := stubBugServer(t)
	defer srv.Close()

	p := NewPoller(PollerConfig{
		Client:  NewClient(ClientConfig{BaseURL: srv.URL, User: "u", Token: "t", IsCloud: true}),
		BoardID: 1,
	})
	if err := p.bugLinkCycle(context.Background()); err != nil {
		t.Errorf("expected nil err when DB/Recorder absent, got %v", err)
	}
}
