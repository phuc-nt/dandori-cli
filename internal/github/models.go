// Package github provides a minimal GitHub REST v3 client for dandori-cli's
// pull-based event sync (PRs, reviews, commits). Scope is intentionally narrow:
// just the resources needed to compute true AI-CFR and PR Review Cycle Time.
package github

import "time"

// PR state constants accepted by GitHub's /pulls endpoint.
const (
	StateOpen   = "open"
	StateClosed = "closed"
	StateAll    = "all"
)

// PR is the trimmed projection of a GitHub pull request used by sync.
// Only fields needed for AI-CFR (merged + revert detection) and PR
// Review Cycle Time (created → first approval) are kept.
type PR struct {
	Number         int       `json:"number"`
	Title          string    `json:"title"`
	State          string    `json:"state"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	MergedAt       *time.Time `json:"merged_at"`
	ClosedAt       *time.Time `json:"closed_at"`
	MergeCommitSHA string    `json:"merge_commit_sha"`
	User           User      `json:"user"`
	Base           Ref       `json:"base"`
	Head           Ref       `json:"head"`
}

// User is the minimum identification used in PRs and reviews.
type User struct {
	Login string `json:"login"`
}

// Ref names a branch endpoint on a PR.
type Ref struct {
	Ref string `json:"ref"`
	SHA string `json:"sha"`
}

// Review state constants returned by GitHub's review endpoint.
const (
	ReviewApproved        = "APPROVED"
	ReviewChangesRequested = "CHANGES_REQUESTED"
	ReviewCommented       = "COMMENTED"
	ReviewDismissed       = "DISMISSED"
)

// Review represents a single PR review event.
type Review struct {
	ID          int64     `json:"id"`
	State       string    `json:"state"`
	SubmittedAt time.Time `json:"submitted_at"`
	User        User      `json:"user"`
}

// Commit is the trimmed projection of a commit returned by /pulls/{n}/commits.
type Commit struct {
	SHA    string       `json:"sha"`
	Commit CommitDetail `json:"commit"`
}

// CommitDetail holds the inner commit fields we care about.
type CommitDetail struct {
	Message string       `json:"message"`
	Author  CommitAuthor `json:"author"`
}

// CommitAuthor records who authored the commit and when.
type CommitAuthor struct {
	Name string    `json:"name"`
	Date time.Time `json:"date"`
}
