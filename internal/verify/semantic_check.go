// Package verify provides pre-sync gates that detect fake-completion failures
// before the auto-sync transitions a Jira ticket to Done.
//
// See plans/260428-0812-bug-3-fake-completion-spike/plan.md for the design.
package verify

import (
	"path/filepath"
	"regexp"
	"strings"
)

// Result of a semantic check.
//
// Pass=false means the spec referenced something the diff did not touch — likely
// a fake-completion. The caller should warn (post Jira comment, leave ticket
// In Progress) per Bug #3 plan Q1+Q5.
//
// Inconclusive means the spec produced no extractable paths/keywords. Per Q5
// the gate flags for review (warn) instead of blocking.
type Result struct {
	Pass         bool
	Inconclusive bool
	Reason       string
	Matched      []string // spec tokens that overlapped with the diff
	Missing      []string // spec tokens that did NOT overlap (the smoking gun)
}

// pathTokenRe matches file/dir-looking tokens in a spec: contains a slash or a
// dot followed by 1-5 alphanumeric chars (extension). Examples it catches:
//
//	hello.go, src/auth, internal/db/runs.go, README.md, demo-workspace/x.txt
//
// Examples it ignores: "the agent", "fix bug", "v1.0" (version, no slash).
var pathTokenRe = regexp.MustCompile(`[A-Za-z0-9_\-./]*[/.][A-Za-z0-9_\-./]*[A-Za-z0-9]`)

// docExtensions are file extensions treated as documentation. A diff containing
// only these extensions is "doc-only" and skips the quality gate (Q4).
var docExtensions = map[string]bool{
	".md":  true,
	".txt": true,
	".rst": true,
}

// ExtractSpecPaths pulls out file/dir-looking tokens from a free-form task
// description. It is intentionally lossy — the goal is high-recall path-match,
// not full NLP. Returns deduped, sorted-by-input-order tokens.
func ExtractSpecPaths(description string) []string {
	if description == "" {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, m := range pathTokenRe.FindAllString(description, -1) {
		t := strings.Trim(m, ".")
		if t == "" || seen[t] {
			continue
		}
		// Skip URL-ish noise. Source string may contain "://", but the
		// token captured by the regex starts at the first `//` because `:`
		// is outside the char class — so check both forms.
		if strings.Contains(t, "://") || strings.HasPrefix(t, "//") {
			continue
		}
		// Skip version-y noise: "1.0", "2.4.1", "v1.21.0".
		ver := t
		if strings.HasPrefix(ver, "v") || strings.HasPrefix(ver, "V") {
			ver = ver[1:]
		}
		if isVersionLike(ver) {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

func isVersionLike(s string) bool {
	if s == "" || !(s[0] >= '0' && s[0] <= '9') {
		return false
	}
	for _, r := range s {
		if !((r >= '0' && r <= '9') || r == '.') {
			return false
		}
	}
	return true
}

// CheckResult runs the semantic check given the task spec and the list of
// files changed in the diff. workspaceDir is the per-task workspace root
// (e.g. "demo-workspace/260427-CLITEST2-2"). Both relative and absolute paths
// in changedFiles are supported.
//
// Algorithm (per plan Section 11):
//  1. Extract path tokens from spec.
//  2. If none extracted → Inconclusive (Q5: flag for review).
//  3. For each spec token, check if any changed file path contains it
//     (basename match OR substring match). Anchor matches under workspaceDir
//     first; fall back to bare match for compatibility.
//  4. If at least one spec token matched → Pass.
//  5. If zero matched → Fail with the missing tokens listed.
func CheckResult(description string, changedFiles []string, workspaceDir string) Result {
	tokens := ExtractSpecPaths(description)
	if len(tokens) == 0 {
		return Result{
			Inconclusive: true,
			Reason:       "no file/path tokens extracted from spec — manual review recommended",
		}
	}
	if len(changedFiles) == 0 {
		return Result{
			Pass:    false,
			Reason:  "spec references files but diff is empty",
			Missing: tokens,
		}
	}

	var matched, missing []string
	for _, tok := range tokens {
		if anyFileMatches(tok, changedFiles, workspaceDir) {
			matched = append(matched, tok)
		} else {
			missing = append(missing, tok)
		}
	}

	if len(matched) == 0 {
		return Result{
			Pass:    false,
			Reason:  "diff did not touch any file/path referenced in the task spec",
			Missing: missing,
		}
	}
	return Result{
		Pass:    true,
		Reason:  "diff touches spec-referenced paths",
		Matched: matched,
		Missing: missing,
	}
}

// anyFileMatches returns true when any path in changedFiles matches the spec
// token. Match rules (any one wins):
//  1. Basename equality: spec "hello.go" vs changed "demo-workspace/x/hello.go" → match.
//  2. Substring: spec "internal/db" vs changed "internal/db/runs.go" → match.
//  3. Workspace-anchored substring: spec "hello.go" + workspace "demo-workspace/x"
//     → check if "demo-workspace/x/hello.go" appears in changedFiles.
func anyFileMatches(token string, changedFiles []string, workspaceDir string) bool {
	tokBase := filepath.Base(token)
	tokLower := strings.ToLower(token)
	anchored := ""
	if workspaceDir != "" {
		anchored = strings.ToLower(filepath.ToSlash(filepath.Join(workspaceDir, token)))
	}
	for _, f := range changedFiles {
		fLower := strings.ToLower(filepath.ToSlash(f))
		if filepath.Base(f) == tokBase {
			return true
		}
		if strings.Contains(fLower, tokLower) {
			return true
		}
		if anchored != "" && strings.Contains(fLower, anchored) {
			return true
		}
	}
	return false
}

// IsDocOnly returns true when every changed file has a documentation
// extension (.md, .txt, .rst). Empty input returns false. Used by the quality
// gate (Q4) to skip lint/test on doc-only diffs.
func IsDocOnly(changedFiles []string) bool {
	if len(changedFiles) == 0 {
		return false
	}
	for _, f := range changedFiles {
		ext := strings.ToLower(filepath.Ext(f))
		if !docExtensions[ext] {
			return false
		}
	}
	return true
}
