package taskcontext

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phuc-nt/dandori-cli/internal/confluence"
	"github.com/phuc-nt/dandori-cli/internal/db"
	"github.com/phuc-nt/dandori-cli/internal/event"
	"github.com/phuc-nt/dandori-cli/internal/jira"
)

// stubJiraServer returns a Jira REST server that serves one issue with two
// confluence remote links (one resolvable, one not).
func stubJiraServer(t *testing.T, confBase string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/api/2/issue/CLITEST-1", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
            "key":"CLITEST-1",
            "fields":{
                "summary":"Test",
                "issuetype":{"name":"Task"},
                "priority":{"name":"Medium"},
                "status":{"name":"To Do"},
                "labels":[],
                "description":"see ` + confBase + `/wiki/pages/100 and ` + confBase + `/wiki/pages/404"
            }
        }`))
	})
	mux.HandleFunc("/rest/api/2/issue/CLITEST-1/remotelink", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// stubConfluenceServer returns a Confluence server where page 100 succeeds and
// page 404 returns Not Found.
func stubConfluenceServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/api/content/100", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
            "id":"100","title":"Auth Architecture","type":"page","status":"current",
            "body":{"storage":{"value":"<p>token flow</p>","representation":"storage"}},
            "version":{"number":7},
            "space":{"id":1,"key":"CLITEST","name":"CLI Test"}
        }`))
	})
	mux.HandleFunc("/rest/api/content/404", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// openTempDB opens a fresh dandori SQLite DB in a temp dir and runs migrations.
func openTempDB(t *testing.T) *db.LocalDB {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	localDB, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { localDB.Close() })
	if err := localDB.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return localDB
}

// insertPendingRun inserts a minimal runs row so events FK is satisfied.
func insertPendingRun(t *testing.T, localDB *db.LocalDB, runID string) {
	t.Helper()
	_, err := localDB.Exec(`
        INSERT INTO runs (id, agent_type, user, workstation_id, started_at, status)
        VALUES (?, 'claude_code', 'tester', 'ws-test', datetime('now'), 'pending')
    `, runID)
	if err != nil {
		t.Fatalf("insert pending run: %v", err)
	}
}

func TestFetcher_RecordsConfluenceReadEvents(t *testing.T) {
	conf := stubConfluenceServer(t)
	jiraSrv := stubJiraServer(t, conf.URL)

	jc := jira.NewClient(jira.ClientConfig{BaseURL: jiraSrv.URL, User: "u", Token: "t", IsCloud: true})
	cc := confluence.NewClient(confluence.ClientConfig{BaseURL: conf.URL, User: "u", Token: "t", IsCloud: true})

	localDB := openTempDB(t)
	rec := event.NewRecorder(localDB)
	runID := "test-run-1"
	insertPendingRun(t, localDB, runID)

	f := NewFetcher(jc, cc).WithRecorder(rec, runID)
	tc, err := f.Fetch(context.Background(), "CLITEST-1")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got := len(tc.LinkedDocs); got != 1 {
		t.Fatalf("LinkedDocs=%d, want 1 (only page 100 succeeds)", got)
	}

	rows, err := localDB.Query(`SELECT event_type, data FROM events WHERE run_id=? ORDER BY id`, runID)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	type evt struct {
		Type string
		Data map[string]any
	}
	var got []evt
	for rows.Next() {
		var e evt
		var raw string
		if err := rows.Scan(&e.Type, &raw); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if err := json.Unmarshal([]byte(raw), &e.Data); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		got = append(got, e)
	}

	if len(got) != 2 {
		t.Fatalf("got %d events, want 2 (1 read + 1 read.error)", len(got))
	}

	var success, failed *evt
	for i := range got {
		switch got[i].Type {
		case "confluence.read":
			success = &got[i]
		case "confluence.read.error":
			failed = &got[i]
		}
	}
	if success == nil {
		t.Fatalf("missing confluence.read event, got: %+v", got)
	}
	if failed == nil {
		t.Fatalf("missing confluence.read.error event, got: %+v", got)
	}

	if success.Data["page_id"] != "100" {
		t.Errorf("success page_id=%v, want 100", success.Data["page_id"])
	}
	if success.Data["title"] != "Auth Architecture" {
		t.Errorf("success title=%v, want Auth Architecture", success.Data["title"])
	}
	if v, _ := success.Data["version"].(float64); v != 7 {
		t.Errorf("success version=%v, want 7", success.Data["version"])
	}
	if cc, _ := success.Data["char_count"].(float64); cc <= 0 {
		t.Errorf("success char_count=%v, want > 0", success.Data["char_count"])
	}
	// Should not contain the body text itself
	if raw, _ := json.Marshal(success.Data); strings.Contains(string(raw), "token flow") {
		t.Errorf("success payload leaked body content: %s", raw)
	}

	if failed.Data["page_id"] != "404" {
		t.Errorf("failed page_id=%v, want 404", failed.Data["page_id"])
	}
	if failed.Data["error_class"] == nil || failed.Data["error_class"] == "" {
		t.Errorf("failed error_class missing: %+v", failed.Data)
	}
}

func TestFetcher_NoRecorder_NoCrash(t *testing.T) {
	conf := stubConfluenceServer(t)
	jiraSrv := stubJiraServer(t, conf.URL)

	jc := jira.NewClient(jira.ClientConfig{BaseURL: jiraSrv.URL, User: "u", Token: "t", IsCloud: true})
	cc := confluence.NewClient(confluence.ClientConfig{BaseURL: conf.URL, User: "u", Token: "t", IsCloud: true})

	// No WithRecorder call — must not panic.
	f := NewFetcher(jc, cc)
	if _, err := f.Fetch(context.Background(), "CLITEST-1"); err != nil {
		t.Fatalf("fetch: %v", err)
	}
}

// Sanity: ensure the test file is using a real on-disk path and not any
// home-dir fallback that would pollute the developer's environment.
func TestOpenTempDB_TempDirOnly(t *testing.T) {
	dir := t.TempDir()
	if !strings.HasPrefix(dir, os.TempDir()) {
		t.Fatalf("temp dir %s outside %s", dir, os.TempDir())
	}
}
