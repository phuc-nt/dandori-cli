package attribution

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// SessionDiff describes the git range covered by a single agent session: the
// HEAD before the wrapper started, and the HEAD when it exited. Either may be
// empty when a session ended without committing — callers filter those out
// before calling ComputeRetention.
type SessionDiff struct {
	HeadBefore string
	HeadAfter  string
}

// RetentionResult is what ComputeRetention returns: how many of the lines
// present in the final tree (across all files any session touched) trace back
// to commits inside any session's range, vs. lines introduced after.
type RetentionResult struct {
	TotalLinesFinal      int
	LinesAttributedAgent int
	LinesAttributedHuman int
	FilesTouched         []string
}

// ComputeRetention walks the union of files touched by any session, runs git
// blame at finalHead, and attributes each line to either an agent session
// (if its introducing commit is in the union of session-reachable commits)
// or to a human (otherwise). Files deleted between any session and finalHead
// contribute 0 lines — their content is gone, so blame has nothing to score.
func ComputeRetention(repoPath string, sessions []SessionDiff, finalHead string) (RetentionResult, error) {
	var res RetentionResult

	if finalHead == "" || !revExists(repoPath, finalHead) {
		// finalHead is the anchor for blame. Without it we cannot attribute
		// anything — return empty result rather than error so callers persist
		// a "no tracked lines" row instead of aborting the Jira flow.
		return res, nil
	}

	agentShas := map[string]struct{}{}
	humanShas := map[string]struct{}{}
	filesTouched := map[string]struct{}{}

	for _, s := range sessions {
		if s.HeadBefore == "" || s.HeadAfter == "" {
			continue
		}
		if s.HeadBefore == s.HeadAfter {
			continue
		}
		// Cross-repo / orphaned-sha guard. The wrapper records git_head_* from
		// the session's CWD repo; if those shas aren't reachable here, the
		// session committed to a different repo (sibling, dogfood workspace,
		// pruned branch). Skip the session rather than aborting the whole
		// task — the row should still reflect any sessions that DID land
		// commits in this repo.
		if !revExists(repoPath, s.HeadBefore) || !revExists(repoPath, s.HeadAfter) {
			continue
		}
		shas, err := revList(repoPath, s.HeadBefore, s.HeadAfter)
		if err != nil {
			return res, err
		}
		for _, sha := range shas {
			agentShas[sha] = struct{}{}
		}
		// Commits between this session's end and finalHead = post-session
		// human work. We do this per-session (rather than once for the union)
		// so that a later session's commits don't get double-counted as human
		// when they're really agent work from a subsequent session.
		postShas, err := revList(repoPath, s.HeadAfter, finalHead)
		if err != nil {
			return res, err
		}
		for _, sha := range postShas {
			humanShas[sha] = struct{}{}
		}
		files, err := diffNameOnly(repoPath, s.HeadBefore, s.HeadAfter)
		if err != nil {
			return res, err
		}
		for _, f := range files {
			filesTouched[f] = struct{}{}
		}
	}
	// A commit that's an agent commit in any session must not also count as
	// human just because it's reachable from a later session's HeadAfter.
	for sha := range agentShas {
		delete(humanShas, sha)
	}

	for file := range filesTouched {
		res.FilesTouched = append(res.FilesTouched, file)

		exists, err := fileExistsAt(repoPath, finalHead, file)
		if err != nil {
			return res, err
		}
		if !exists {
			continue
		}
		lines, err := blameLines(repoPath, finalHead, file)
		if err != nil {
			return res, err
		}
		for _, l := range lines {
			if _, ok := agentShas[l.sha]; ok {
				res.LinesAttributedAgent++
				res.TotalLinesFinal++
				continue
			}
			if _, ok := humanShas[l.sha]; ok {
				res.LinesAttributedHuman++
				res.TotalLinesFinal++
				continue
			}
			// Pre-session baseline line — neither agent nor post-session
			// human edited it. Excluded from totals.
		}
	}

	return res, nil
}

// revList enumerates the commits reachable from headAfter but not from
// headBefore — i.e. the commits introduced by the session. Empty range
// returns no commits without error.
func revList(repoPath, headBefore, headAfter string) ([]string, error) {
	out, err := runGit(repoPath, "rev-list", headBefore+".."+headAfter)
	if err != nil {
		return nil, err
	}
	var shas []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line != "" {
			shas = append(shas, line)
		}
	}
	return shas, nil
}

// diffNameOnly returns the file paths changed between two commits. Used to
// build the union of files any session touched.
func diffNameOnly(repoPath, headBefore, headAfter string) ([]string, error) {
	out, err := runGit(repoPath, "diff", "--name-only", headBefore, headAfter)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

// fileExistsAt reports whether file is present in the tree at rev. Used to
// skip blame for files a human deleted after the session committed them.
func fileExistsAt(repoPath, rev, file string) (bool, error) {
	cmd := exec.Command("git", "cat-file", "-e", rev+":"+file)
	cmd.Dir = repoPath
	if err := cmd.Run(); err != nil {
		// Non-zero exit = missing. cat-file -e doesn't write to stderr on miss.
		if _, ok := err.(*exec.ExitError); ok {
			return false, nil
		}
		return false, fmt.Errorf("cat-file -e %s:%s: %w", rev, file, err)
	}
	return true, nil
}

// blameLine is one entry from `git blame -p`: the introducing commit's SHA
// and the line content (used only for counting; we never persist content).
type blameLine struct {
	sha string
}

// blameLines runs `git blame -p` on file at rev and returns one entry per
// final-tree line. The porcelain format starts each line group with
// "<sha> <orig-line> <final-line> [n-lines]" and ends with a tab-prefixed
// content line; we only need the SHA.
func blameLines(repoPath, rev, file string) ([]blameLine, error) {
	out, err := runGit(repoPath, "blame", "-p", "--", file)
	if err != nil {
		// Fallback: blame at finalHead specifically.
		out, err = runGit(repoPath, "blame", "-p", rev, "--", file)
		if err != nil {
			return nil, err
		}
	}
	var lines []blameLine
	for _, raw := range strings.Split(out, "\n") {
		if len(raw) == 0 || raw[0] == '\t' {
			continue
		}
		// Header lines we want look like "<40-hex-sha> <orig> <final> [n]".
		// Skip metadata lines like "author X", "committer Y", "summary ...".
		fields := strings.Fields(raw)
		if len(fields) < 3 {
			continue
		}
		if !looksLikeSha(fields[0]) {
			continue
		}
		lines = append(lines, blameLine{sha: fields[0]})
	}
	return lines, nil
}

// revExists reports whether rev is a resolvable commit-ish in repoPath. Used
// to drop sessions whose recorded heads point at a sibling repo, a pruned
// branch, or another machine's workspace — those would otherwise crash
// rev-list with "Invalid revision range".
func revExists(repoPath, rev string) bool {
	if rev == "" {
		return false
	}
	cmd := exec.Command("git", "cat-file", "-e", rev+"^{commit}")
	cmd.Dir = repoPath
	return cmd.Run() == nil
}

func looksLikeSha(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// runGit executes a git subcommand in repoPath and returns combined stdout.
// stderr is folded into the returned error so callers see what failed.
func runGit(repoPath string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = repoPath
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w (stderr: %s)", strings.Join(args, " "), err, stderr.String())
	}
	return stdout.String(), nil
}
