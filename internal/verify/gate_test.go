package verify

import (
	"strings"
	"testing"
)

type fakeQuality struct {
	lint, tests int
}

func (f fakeQuality) CountFailures() (int, int) { return f.lint, f.tests }

func TestPreSync_SkipLabel_BypassesAllGates(t *testing.T) {
	in := PreSyncInput{
		TaskDescription: "edit auth.go",
		JiraLabels:      []string{"skip-verify"},
		ChangedFiles:    []string{"unrelated.md"},
	}
	r := PreSync(DefaultGateConfig(), in)
	if !r.Pass || !r.Skipped {
		t.Fatalf("expected skipped pass, got %+v", r)
	}
}

func TestPreSync_SemanticPass_QualityClean(t *testing.T) {
	in := PreSyncInput{
		TaskDescription: "edit auth.go",
		ChangedFiles:    []string{"internal/auth.go"},
		Quality:         fakeQuality{0, 0},
	}
	r := PreSync(DefaultGateConfig(), in)
	if !r.Pass {
		t.Fatalf("expected pass, got %s", r.Reason)
	}
}

func TestPreSync_SemanticFail_BlocksRegardlessOfQuality(t *testing.T) {
	in := PreSyncInput{
		TaskDescription: "fix login.ts",
		ChangedFiles:    []string{"random.go"},
		Quality:         fakeQuality{0, 0},
	}
	r := PreSync(DefaultGateConfig(), in)
	if r.Pass {
		t.Fatalf("expected fail")
	}
	if !strings.Contains(r.Reason, "semantic check failed") {
		t.Errorf("reason missing semantic note: %s", r.Reason)
	}
	if !strings.Contains(r.Reason, "login.ts") {
		t.Errorf("reason should list missing token: %s", r.Reason)
	}
}

func TestPreSync_QualityFail_BlocksWhenSemanticPasses(t *testing.T) {
	in := PreSyncInput{
		TaskDescription: "edit auth.go",
		ChangedFiles:    []string{"auth.go"},
		Quality:         fakeQuality{2, 0},
	}
	r := PreSync(DefaultGateConfig(), in)
	if r.Pass {
		t.Fatalf("expected fail")
	}
	if !strings.Contains(r.Reason, "lint errors") {
		t.Errorf("reason missing lint note: %s", r.Reason)
	}
}

func TestPreSync_DocOnly_SkipsQualityButRunsSemantic(t *testing.T) {
	// Q4: doc-only diffs skip quality gate. Semantic still runs.
	in := PreSyncInput{
		TaskDescription: "update README.md",
		ChangedFiles:    []string{"README.md"},
		Quality:         fakeQuality{99, 99}, // would fail if it ran
	}
	r := PreSync(DefaultGateConfig(), in)
	if !r.Pass {
		t.Fatalf("expected pass (quality should be skipped), got %s", r.Reason)
	}
	if r.LintErrors != 0 || r.TestFailures != 0 {
		t.Errorf("quality should not have been consulted, got lint=%d test=%d", r.LintErrors, r.TestFailures)
	}
}

func TestPreSync_InconclusiveSemantic_FlagsForReview(t *testing.T) {
	// Q5: spec has no extractable paths → flag for review (warn, not pass).
	in := PreSyncInput{
		TaskDescription: "improve overall performance",
		ChangedFiles:    []string{"main.go"},
		Quality:         fakeQuality{0, 0},
	}
	r := PreSync(DefaultGateConfig(), in)
	if r.Pass {
		t.Fatalf("expected fail (inconclusive should warn), got pass")
	}
	if !strings.Contains(r.Reason, "inconclusive") {
		t.Errorf("reason should say inconclusive: %s", r.Reason)
	}
}

func TestPreSync_DisabledChecks_AllPass(t *testing.T) {
	cfg := GateConfig{SemanticCheck: false, QualityGate: false}
	in := PreSyncInput{
		TaskDescription: "fix login.ts",
		ChangedFiles:    []string{"random.go"},
		Quality:         fakeQuality{99, 99},
	}
	r := PreSync(cfg, in)
	if !r.Pass {
		t.Fatalf("expected pass when all gates disabled, got %s", r.Reason)
	}
}

func TestPreSync_SkipLabel_CaseInsensitive(t *testing.T) {
	in := PreSyncInput{
		JiraLabels: []string{"Skip-Verify"},
	}
	r := PreSync(DefaultGateConfig(), in)
	if !r.Skipped {
		t.Fatalf("expected case-insensitive label match")
	}
}
