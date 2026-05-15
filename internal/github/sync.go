// Package github — sync.go: orchestrates the pull-based PR event flow used
// by `dandori sync` when GitHub integration is enabled. Reads watermark
// from sync_state, walks ListPRs since watermark (or now-backfillDays on
// first run), fetches reviews to compute first_approval_at, upserts
// pr_events, then detects reverts + reopens. Idempotent: re-running mints
// no duplicates.
package github

import (
	"fmt"
	"regexp"
	"time"

	"github.com/phuc-nt/dandori-cli/internal/db"
)

// SyncStateKey is the single key v0.13 reads/writes in db.sync_state.
const SyncStateKey = "github.last_pr_sync_at"

// DefaultBackfillDays is the look-back window when no watermark exists.
const DefaultBackfillDays = 90

// OverlapWindow is how far back to re-pull on incremental runs, to catch
// reviews submitted after a PR's last `updated_at` bump.
const OverlapWindow = 1 * time.Hour

// revertTitleRe captures the inner title of GitHub's default revert PR
// title format: `Revert "feat: add login"` → `feat: add login`.
var revertTitleRe = regexp.MustCompile(`^Revert "(.+)"$`)

// Summary is what PullPREvents returns for the sync command to print.
type Summary struct {
	PRsPulled        int
	ReviewsFetched   int
	RevertsDetected  int
	ReopensDetected  int
	DetailsFetched   int
	WatermarkUpdated string
	Duration         time.Duration
}

// PullOptions controls optional per-PR detail fetches.
type PullOptions struct {
	BackfillDays int
	// FetchDetail triggers an extra `GET /pulls/{n}` per PR to capture
	// additions/deletions (v0.14). Best-effort — a per-PR failure logs
	// and continues without size data rather than aborting the sync.
	FetchDetail bool
}

// PullPREvents walks PRs updated since the stored watermark (or
// now-backfillDays on first run) and persists them via the supplied
// LocalDB. Failure modes are aggregated into the returned error; partial
// progress is preserved (each PR is committed in its own statement).
func PullPREvents(client *Client, store *db.LocalDB, opts PullOptions) (Summary, error) {
	backfillDays := opts.BackfillDays
	start := time.Now()
	var s Summary

	since, err := resolveWatermark(store, backfillDays)
	if err != nil {
		return s, fmt.Errorf("resolve watermark: %w", err)
	}

	prs, err := client.ListPRs(since, StateAll)
	if err != nil {
		return s, fmt.Errorf("list prs: %w", err)
	}

	// Pass 1: upsert every PR + detect reopen transitions. We can't yet
	// resolve reverts because a revert PR may arrive before its original
	// in the same batch (GitHub returns updated-desc; the revert PR is
	// newer than what it reverts).
	for _, pr := range prs {
		prior, err := store.GetPRByNumber(client.repo, pr.Number)
		if err != nil {
			return s, fmt.Errorf("lookup prior pr#%d: %w", pr.Number, err)
		}

		firstApprovalAt, reviewCount, err := computeFirstApproval(client, pr.Number)
		if err != nil {
			return s, fmt.Errorf("reviews for pr#%d: %w", pr.Number, err)
		}
		s.ReviewsFetched += reviewCount

		event := buildPREvent(client.repo, pr, firstApprovalAt)

		if opts.FetchDetail {
			if detail, derr := client.GetPRDetail(pr.Number); derr == nil {
				add, del := detail.Additions, detail.Deletions
				event.Additions = &add
				event.Deletions = &del
				s.DetailsFetched++
			}
			// Best-effort: a 404/rate-limit here leaves size NULL and
			// the next sync will retry. No need to fail the run.
		}

		if err := store.UpsertPR(event); err != nil {
			return s, fmt.Errorf("upsert pr#%d: %w", pr.Number, err)
		}
		s.PRsPulled++

		if detectReopen(prior, event) {
			ts := time.Now().UTC().Format(time.RFC3339)
			if err := store.MarkReopened(client.repo, pr.Number, ts); err != nil {
				return s, fmt.Errorf("mark reopen pr#%d: %w", pr.Number, err)
			}
			s.ReopensDetected++
		}
	}

	// Pass 2: resolve reverts now that all originals in this batch are
	// persisted. The revert target may also have been seeded by a prior
	// sync — GetPRByTitle hits the DB either way.
	for _, pr := range prs {
		origTitle, ok := matchRevertTitle(pr.Title)
		if !ok {
			continue
		}
		orig, err := store.GetPRByTitle(client.repo, origTitle, 30)
		if err != nil {
			return s, fmt.Errorf("lookup revert target for pr#%d: %w", pr.Number, err)
		}
		if orig == nil || orig.IsReverted {
			continue
		}
		if err := store.MarkReverted(client.repo, orig.PRNumber, pr.Number); err != nil {
			return s, fmt.Errorf("mark revert of pr#%d: %w", orig.PRNumber, err)
		}
		s.RevertsDetected++
	}

	watermark := time.Now().UTC().Format(time.RFC3339)
	if err := store.SetSyncState(SyncStateKey, watermark); err != nil {
		return s, fmt.Errorf("set watermark: %w", err)
	}
	s.WatermarkUpdated = watermark
	s.Duration = time.Since(start)
	return s, nil
}

func resolveWatermark(store *db.LocalDB, backfillDays int) (time.Time, error) {
	if backfillDays <= 0 {
		backfillDays = DefaultBackfillDays
	}
	v, ok, err := store.GetSyncState(SyncStateKey)
	if err != nil {
		return time.Time{}, err
	}
	if !ok {
		return time.Now().UTC().AddDate(0, 0, -backfillDays), nil
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		// Corrupt watermark — fall back to full backfill rather than skip work.
		return time.Now().UTC().AddDate(0, 0, -backfillDays), nil
	}
	return t.Add(-OverlapWindow), nil
}

func computeFirstApproval(client *Client, prNumber int) (*string, int, error) {
	reviews, err := client.GetPRReviews(prNumber)
	if err != nil {
		return nil, 0, err
	}
	var firstApproval *time.Time
	for i := range reviews {
		r := reviews[i]
		if r.State != ReviewApproved {
			continue
		}
		if r.SubmittedAt.IsZero() {
			continue
		}
		if firstApproval == nil || r.SubmittedAt.Before(*firstApproval) {
			t := r.SubmittedAt
			firstApproval = &t
		}
	}
	if firstApproval == nil {
		return nil, len(reviews), nil
	}
	formatted := firstApproval.UTC().Format(time.RFC3339)
	return &formatted, len(reviews), nil
}

func buildPREvent(repo string, pr PR, firstApprovalAt *string) db.PREvent {
	event := db.PREvent{
		Repo:            repo,
		PRNumber:        pr.Number,
		Title:           pr.Title,
		State:           normalizeState(pr),
		Author:          pr.User.Login,
		CreatedAt:       pr.CreatedAt.UTC().Format(time.RFC3339),
		SubmittedAt:     pr.CreatedAt.UTC().Format(time.RFC3339),
		MergeCommitSHA:  pr.MergeCommitSHA,
		FirstApprovalAt: firstApprovalAt,
	}
	if pr.MergedAt != nil && !pr.MergedAt.IsZero() {
		ts := pr.MergedAt.UTC().Format(time.RFC3339)
		event.MergedAt = &ts
	}
	if pr.ClosedAt != nil && !pr.ClosedAt.IsZero() {
		ts := pr.ClosedAt.UTC().Format(time.RFC3339)
		event.ClosedAt = &ts
	}
	return event
}

// normalizeState collapses GitHub's open/closed plus the merged_at signal
// into a single state token used by metric queries.
func normalizeState(pr PR) string {
	if pr.MergedAt != nil && !pr.MergedAt.IsZero() {
		return "merged"
	}
	return pr.State
}

// detectReopen returns true when the prior row was closed/merged and the
// current PR is open again. Only triggers when no reopened_at has been
// stamped yet — MarkReopened is itself idempotent but we save a write.
func detectReopen(prior *db.PREvent, current db.PREvent) bool {
	if prior == nil {
		return false
	}
	if prior.ReopenedAt != nil {
		return false
	}
	wasInactive := prior.State == "closed" || prior.State == "merged"
	return wasInactive && current.State == "open"
}

// matchRevertTitle returns the inner title and true when the PR title
// matches GitHub's default revert format.
func matchRevertTitle(title string) (string, bool) {
	m := revertTitleRe.FindStringSubmatch(title)
	if m == nil {
		return "", false
	}
	return m[1], true
}
