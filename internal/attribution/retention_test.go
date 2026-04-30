package attribution

import (
	"testing"
)

// TestComputeRetention_AllAgentLinesKept covers the simplest happy path:
// agent adds a line, no human follow-up. The added line must count as agent.
func TestComputeRetention_AllAgentLinesKept(t *testing.T) {
	repo := newTestRepo(t)
	repo.commit("initial.go", "package x\nfunc A() {}\n")
	headBefore := repo.head()
	repo.commit("initial.go", "package x\nfunc A() {}\nfunc B() {}\n")
	headAfter := repo.head()
	finalHead := headAfter

	res, err := ComputeRetention(repo.path, []SessionDiff{{HeadBefore: headBefore, HeadAfter: headAfter}}, finalHead)
	if err != nil {
		t.Fatalf("ComputeRetention: %v", err)
	}
	if res.LinesAttributedAgent != 1 {
		t.Errorf("LinesAttributedAgent = %d, want 1", res.LinesAttributedAgent)
	}
	if res.LinesAttributedHuman != 0 {
		t.Errorf("LinesAttributedHuman = %d, want 0", res.LinesAttributedHuman)
	}
}

// TestComputeRetention_HumanOverwritesAgentLine: agent's line is replaced by a
// later human commit. The agent's contribution drops to 0 because git blame
// at finalHead points to the human commit.
func TestComputeRetention_HumanOverwritesAgentLine(t *testing.T) {
	repo := newTestRepo(t)
	repo.commit("file.go", "package x\nfunc Old() {}\n")
	headBefore := repo.head()
	repo.commit("file.go", "package x\nfunc Agent() {}\n")
	headAfter := repo.head()
	repo.commit("file.go", "package x\nfunc Human() {}\n")
	finalHead := repo.head()

	res, err := ComputeRetention(repo.path, []SessionDiff{{HeadBefore: headBefore, HeadAfter: headAfter}}, finalHead)
	if err != nil {
		t.Fatalf("ComputeRetention: %v", err)
	}
	if res.LinesAttributedAgent != 0 {
		t.Errorf("LinesAttributedAgent = %d, want 0", res.LinesAttributedAgent)
	}
	if res.LinesAttributedHuman != 1 {
		t.Errorf("LinesAttributedHuman = %d, want 1", res.LinesAttributedHuman)
	}
}

// TestComputeRetention_HumanAppendsAfterAgent: agent line preserved + human
// line added afterward → split attribution.
func TestComputeRetention_HumanAppendsAfterAgent(t *testing.T) {
	repo := newTestRepo(t)
	repo.commit("file.go", "package x\n")
	h0 := repo.head()
	repo.commit("file.go", "package x\nfunc A() {}\n")
	h1 := repo.head()
	repo.commit("file.go", "package x\nfunc A() {}\nfunc B() {}\n")
	finalHead := repo.head()

	res, err := ComputeRetention(repo.path, []SessionDiff{{HeadBefore: h0, HeadAfter: h1}}, finalHead)
	if err != nil {
		t.Fatalf("ComputeRetention: %v", err)
	}
	if res.LinesAttributedAgent != 1 {
		t.Errorf("LinesAttributedAgent = %d, want 1", res.LinesAttributedAgent)
	}
	if res.LinesAttributedHuman != 1 {
		t.Errorf("LinesAttributedHuman = %d, want 1", res.LinesAttributedHuman)
	}
}

// TestComputeRetention_MultipleSessions: two consecutive agent sessions, no
// human edits. Each session's diff is in its own [HeadBefore..HeadAfter]
// range. All final lines must attribute to agent.
func TestComputeRetention_MultipleSessions(t *testing.T) {
	repo := newTestRepo(t)
	repo.commit("file.go", "package x\n")
	h0 := repo.head()
	repo.commit("file.go", "package x\nfunc X() {}\n")
	h1 := repo.head()
	// Session 2 starts where session 1 left off.
	repo.commit("file.go", "package x\nfunc X() {}\nfunc Y() {}\n")
	h2 := repo.head()
	finalHead := h2

	sessions := []SessionDiff{
		{HeadBefore: h0, HeadAfter: h1},
		{HeadBefore: h1, HeadAfter: h2},
	}
	res, err := ComputeRetention(repo.path, sessions, finalHead)
	if err != nil {
		t.Fatalf("ComputeRetention: %v", err)
	}
	if res.LinesAttributedAgent != 2 {
		t.Errorf("LinesAttributedAgent = %d, want 2", res.LinesAttributedAgent)
	}
	if res.LinesAttributedHuman != 0 {
		t.Errorf("LinesAttributedHuman = %d, want 0", res.LinesAttributedHuman)
	}
}

// TestComputeRetention_FileDeletedByHuman: agent created file, human removed
// it. Agent's lines no longer present in finalHead → 0 retained.
func TestComputeRetention_FileDeletedByHuman(t *testing.T) {
	repo := newTestRepo(t)
	repo.commit("seed.go", "package x\n")
	h0 := repo.head()
	repo.commit("agent.go", "package x\nfunc Agent() {}\n")
	h1 := repo.head()
	repo.remove("agent.go")
	finalHead := repo.head()

	res, err := ComputeRetention(repo.path, []SessionDiff{{HeadBefore: h0, HeadAfter: h1}}, finalHead)
	if err != nil {
		t.Fatalf("ComputeRetention: %v", err)
	}
	if res.LinesAttributedAgent != 0 {
		t.Errorf("LinesAttributedAgent = %d, want 0", res.LinesAttributedAgent)
	}
	// Human deletion of agent file leaves no human-attributed lines either.
	if res.LinesAttributedHuman != 0 {
		t.Errorf("LinesAttributedHuman = %d, want 0", res.LinesAttributedHuman)
	}
}

// TestComputeRetention_OrphanShas: session heads point at commits that
// don't exist in repoPath (cross-repo, pruned branch, or another machine).
// The function must skip those sessions silently rather than crash with
// "Invalid revision range" — the row should still record any sessions that
// did land commits in this repo.
func TestComputeRetention_OrphanShas(t *testing.T) {
	repo := newTestRepo(t)
	repo.commit("file.go", "package x\nfunc Init() {}\n")
	finalHead := repo.head()

	// Two sessions: one with shas that don't exist here, one valid.
	sessions := []SessionDiff{
		{HeadBefore: "deadbeef00000000000000000000000000000000",
			HeadAfter: "cafebabe00000000000000000000000000000000"},
	}
	res, err := ComputeRetention(repo.path, sessions, finalHead)
	if err != nil {
		t.Fatalf("ComputeRetention should skip orphan shas, got: %v", err)
	}
	if res.LinesAttributedAgent != 0 || res.LinesAttributedHuman != 0 {
		t.Errorf("expected zero attribution for orphan-only sessions, got agent=%d human=%d",
			res.LinesAttributedAgent, res.LinesAttributedHuman)
	}
}

// TestComputeRetention_OrphanFinalHead: finalHead itself is unreachable.
// Should return empty result without error so the Jira flow isn't blocked.
func TestComputeRetention_OrphanFinalHead(t *testing.T) {
	repo := newTestRepo(t)
	repo.commit("file.go", "package x\nfunc A() {}\n")
	headBefore := repo.head()
	repo.commit("file.go", "package x\nfunc A() {}\nfunc B() {}\n")
	headAfter := repo.head()

	res, err := ComputeRetention(repo.path,
		[]SessionDiff{{HeadBefore: headBefore, HeadAfter: headAfter}},
		"deadbeef00000000000000000000000000000000")
	if err != nil {
		t.Fatalf("orphan finalHead should not error: %v", err)
	}
	if res.TotalLinesFinal != 0 {
		t.Errorf("expected zero total lines for orphan finalHead, got %d", res.TotalLinesFinal)
	}
}

// TestComputeRetention_EmptyDiff: HeadBefore == HeadAfter (a session that
// committed nothing — e.g. read-only run). Must not error and must contribute
// 0 to agent counts.
func TestComputeRetention_EmptyDiff(t *testing.T) {
	repo := newTestRepo(t)
	repo.commit("file.go", "package x\nfunc A() {}\n")
	h := repo.head()

	res, err := ComputeRetention(repo.path, []SessionDiff{{HeadBefore: h, HeadAfter: h}}, h)
	if err != nil {
		t.Fatalf("ComputeRetention: %v", err)
	}
	if res.LinesAttributedAgent != 0 {
		t.Errorf("LinesAttributedAgent = %d, want 0", res.LinesAttributedAgent)
	}
	// File untouched by any "session" diff → not in filesTouched, so human=0 too.
	if res.LinesAttributedHuman != 0 {
		t.Errorf("LinesAttributedHuman = %d, want 0", res.LinesAttributedHuman)
	}
}
