package server_test

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/phuc-nt/dandori-cli/internal/db"
	"github.com/phuc-nt/dandori-cli/internal/server"
)

func newEngMux(t *testing.T) (*http.ServeMux, *db.LocalDB) {
	t.Helper()
	tmpDir := t.TempDir()
	store, err := db.Open(filepath.Join(tmpDir, "eng.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := store.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	mux := http.NewServeMux()
	server.RegisterEngRoutes(mux, store)
	server.RegisterAdminRoutes(mux, store)
	return mux, store
}

func seedEngRunHTTP(t *testing.T, d *db.LocalDB, id, agent, model, ws, repo string, started time.Time, cost float64) {
	t.Helper()
	_, err := d.Exec(`
		INSERT INTO runs (id, jira_issue_key, agent_name, agent_type,
			user, workstation_id, cwd, git_remote, started_at, duration_sec, status,
			cost_usd, engineer_name, department,
			input_tokens, cache_read_tokens, model, session_end_reason, human_approval_count)
		VALUES (?, 'PROJ-1', ?, 'claude_code', 'u', ?, '/tmp/r', ?, ?, 600, 'done', ?, 'a', 'eng',
			1000, 500, ?, 'stop', 0)
	`, id, agent, ws, repo, started.Format(time.RFC3339), cost, model)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func TestEng_AgentsCompare_RequiresBoth(t *testing.T) {
	mux, _ := newEngMux(t)
	w := doGET(t, mux, "/api/agents/compare?a=alpha")
	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestEng_AgentsCompare_ReturnsTwoPacks(t *testing.T) {
	mux, store := newEngMux(t)
	now := time.Now().UTC()
	seedEngRunHTTP(t, store, "r1", "alpha", "claude-sonnet-4", "ws1", "git@x:r.git", now, 1.0)
	seedEngRunHTTP(t, store, "r2", "beta", "claude-haiku", "ws2", "git@x:r.git", now, 0.5)

	w := doGET(t, mux, "/api/agents/compare?a=alpha&b=beta")
	if w.Code != 200 {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	var got struct {
		A db.AgentMetricPack `json:"a"`
		B db.AgentMetricPack `json:"b"`
	}
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.A.AgentName != "alpha" || got.B.AgentName != "beta" {
		t.Errorf("names = %s/%s, want alpha/beta", got.A.AgentName, got.B.AgentName)
	}
}

func TestEng_Autonomy_EmptyReturnsArray(t *testing.T) {
	mux, _ := newEngMux(t)
	w := doGET(t, mux, "/api/autonomy")
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	if w.Body.String() == "null\n" || w.Body.String() == "null" {
		t.Errorf("want [] not null, got %q", w.Body.String())
	}
}

func TestEng_ModelMix_GroupsByModel(t *testing.T) {
	mux, store := newEngMux(t)
	now := time.Now().UTC()
	seedEngRunHTTP(t, store, "r1", "alpha", "claude-sonnet-4", "ws", "r", now, 2.0)
	seedEngRunHTTP(t, store, "r2", "alpha", "claude-haiku", "ws", "r", now, 0.5)

	w := doGET(t, mux, "/api/model-mix")
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	var got []db.ModelMixRow
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 models, got %d", len(got))
	}
}

func TestEng_DurationHistogram_ReturnsBuckets(t *testing.T) {
	mux, store := newEngMux(t)
	now := time.Now().UTC()
	for i := 0; i < 6; i++ {
		seedEngRunHTTP(t, store, "h"+string(rune('a'+i)), "alpha", "m", "ws", "r",
			now.Add(time.Duration(i)*time.Minute), 0.1)
	}

	w := doGET(t, mux, "/api/duration-histogram")
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	var got []db.DurationBucket
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 6 {
		t.Errorf("want 6 buckets, got %d", len(got))
	}
}

func TestAdmin_Workstations_ReturnsRows(t *testing.T) {
	mux, store := newEngMux(t)
	now := time.Now().UTC()
	seedEngRunHTTP(t, store, "w1", "alpha", "m", "ws-A", "r", now, 0.1)
	seedEngRunHTTP(t, store, "w2", "alpha", "m", "ws-B", "r", now, 0.1)

	w := doGET(t, mux, "/api/workstations")
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	var got []db.WorkstationRow
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("want 2 ws rows, got %d", len(got))
	}
}

func TestAdmin_Repos_RanksByCost(t *testing.T) {
	mux, store := newEngMux(t)
	now := time.Now().UTC()
	seedEngRunHTTP(t, store, "r1", "alpha", "m", "ws", "git@x:hi.git", now, 5.0)
	seedEngRunHTTP(t, store, "r2", "alpha", "m", "ws", "git@x:lo.git", now, 1.0)

	w := doGET(t, mux, "/api/repos")
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	var got []db.RepoLeaderboardRow
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 repos, got %d", len(got))
	}
	if got[0].Cost < got[1].Cost {
		t.Errorf("not sorted desc: %+v", got)
	}
	if len(got[0].Spark) != 14 {
		t.Errorf("spark len = %d, want 14", len(got[0].Spark))
	}
}
