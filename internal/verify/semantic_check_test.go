package verify

import (
	"reflect"
	"sort"
	"testing"
)

func TestExtractSpecPaths(t *testing.T) {
	tests := []struct {
		name string
		desc string
		want []string
	}{
		{"empty", "", nil},
		{"plain text only", "fix the bug in login flow", nil},
		{"single file", "Write a new file `hello.go` in the repo", []string{"hello.go"}},
		{"path with subdir", "Update internal/db/runs.go to add COALESCE", []string{"internal/db/runs.go"}},
		{"multiple paths", "Touch auth.go and login.ts", []string{"auth.go", "login.ts"}},
		{"ignore version", "Bump go to v1.21.0", nil},
		{"ignore url", "See https://example.com/foo for details", nil},
		{"dedup", "fix hello.go then test hello.go again", []string{"hello.go"}},
		{"workspace-relative", "edit demo-workspace/260427-CLITEST2-2/hello.go", []string{"demo-workspace/260427-CLITEST2-2/hello.go"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractSpecPaths(tt.desc)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCheckResult_Pass_DiffTouchesSpecFile(t *testing.T) {
	spec := "Write a new file hello.go in the workspace"
	changed := []string{"demo-workspace/260427-CLITEST2-2/hello.go"}
	r := CheckResult(spec, changed, "demo-workspace/260427-CLITEST2-2")
	if !r.Pass {
		t.Fatalf("expected pass, got fail: %s (missing=%v)", r.Reason, r.Missing)
	}
	if !contains(r.Matched, "hello.go") {
		t.Errorf("expected hello.go in matched, got %v", r.Matched)
	}
}

func TestCheckResult_Fail_FabricatedFileSameNameButPathMatchesAnyway(t *testing.T) {
	// Even when the agent fabricates hello.go in a new dir, basename match
	// still says "diff touched something named hello.go" → pass. This is a
	// known false-negative for category (i); the quality gate (phase 2)
	// catches the deeper issue (file doesn't compile / no tests). The
	// semantic check's job is to catch "diff touched NOTHING from spec".
	spec := "fix hello.go bug"
	changed := []string{"demo-workspace/260427-CLITEST2-2/hello.go"}
	r := CheckResult(spec, changed, "demo-workspace/260427-CLITEST2-2")
	if !r.Pass {
		t.Fatalf("expected pass on basename match, got fail: %s", r.Reason)
	}
}

func TestCheckResult_Fail_DiffTouchesNothingFromSpec(t *testing.T) {
	spec := "fix login.ts to handle null tokens"
	changed := []string{"README.md", "docs/changelog.md"}
	r := CheckResult(spec, changed, "")
	if r.Pass {
		t.Fatalf("expected fail, got pass")
	}
	if !contains(r.Missing, "login.ts") {
		t.Errorf("expected login.ts in missing, got %v", r.Missing)
	}
}

func TestCheckResult_Inconclusive_NoSpecPaths(t *testing.T) {
	spec := "improve performance of the system"
	changed := []string{"foo.go"}
	r := CheckResult(spec, changed, "")
	if !r.Inconclusive {
		t.Fatalf("expected inconclusive, got pass=%v fail=%v", r.Pass, !r.Pass)
	}
}

func TestCheckResult_Fail_EmptyDiff(t *testing.T) {
	spec := "edit auth.go"
	r := CheckResult(spec, nil, "")
	if r.Pass || r.Inconclusive {
		t.Fatalf("expected fail, got pass=%v inconclusive=%v", r.Pass, r.Inconclusive)
	}
}

func TestCheckResult_PartialMatch_StillPasses(t *testing.T) {
	// Spec mentions 2 files, agent touches 1. We treat any-match as pass and
	// surface the un-touched one in Missing for the human reviewer.
	spec := "update auth.go and session.go"
	changed := []string{"internal/auth/auth.go"}
	r := CheckResult(spec, changed, "")
	if !r.Pass {
		t.Fatalf("expected pass, got fail")
	}
	if !contains(r.Missing, "session.go") {
		t.Errorf("expected session.go in missing, got %v", r.Missing)
	}
}

func TestIsDocOnly(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want bool
	}{
		{"empty", nil, false},
		{"all md", []string{"README.md", "docs/x.md"}, true},
		{"mixed", []string{"README.md", "main.go"}, false},
		{"all txt", []string{"NOTES.txt"}, true},
		{"only go", []string{"main.go"}, false},
		{"upper case ext", []string{"README.MD"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsDocOnly(tt.in); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

// Stable sort helper used by debug-only assertions; keeping it tested.
func TestSortedExtractIsDeterministic(t *testing.T) {
	a := ExtractSpecPaths("touch a.go and b.go and c.go")
	b := ExtractSpecPaths("touch a.go and b.go and c.go")
	sort.Strings(a)
	sort.Strings(b)
	if !reflect.DeepEqual(a, b) {
		t.Errorf("nondeterministic: %v vs %v", a, b)
	}
}
