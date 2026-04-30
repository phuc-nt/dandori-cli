package jira

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/phuc-nt/dandori-cli/internal/db"
	"github.com/phuc-nt/dandori-cli/internal/event"
	"github.com/phuc-nt/dandori-cli/internal/model"
)

// bugLinkScanWindow is the JQL "created >= -Nd" window. Conservative
// 30d default — bugs filed weeks after a run still get linked. Since
// detection dedupes on bug_key, re-scanning is idempotent.
const bugLinkScanWindow = "30d"

type Poller struct {
	client          *Client
	boardIDs        []int
	interval        time.Duration
	bugLinkInterval time.Duration
	lastIssueSet    map[string]bool
	pendingSuggests map[string]pendingSuggest
	onNewTask       func(Issue)
	onAssigned      func(Issue)
	onSuggestAgent  func(Issue) (agentName string, score int, reason string)
	reminderAfter   time.Duration

	localDB  *db.LocalDB
	recorder *event.Recorder
}

type pendingSuggest struct {
	suggestedAt  time.Time
	agentName    string
	reminderSent bool
}

type PollerConfig struct {
	Client *Client
	// BoardID is the legacy single-board configuration. New code should use
	// BoardIDs to watch multiple boards at once. NewPoller merges both.
	BoardID        int
	BoardIDs       []int
	Interval       time.Duration
	OnNewTask      func(Issue)
	OnAssigned     func(Issue)
	OnSuggestAgent func(Issue) (agentName string, score int, reason string)
	ReminderAfter  time.Duration

	// LocalDB + Recorder enable iteration detection + bug-link cycle.
	// When either is nil, the poller skips both tracking features.
	LocalDB  *db.LocalDB
	Recorder *event.Recorder

	// BugLinkInterval controls how often the bug-link cycle runs.
	// Default 1h — much rarer than the sprint poll because bug
	// detection is a heavier search and idempotent across cycles.
	BugLinkInterval time.Duration
}

func NewPoller(cfg PollerConfig) *Poller {
	interval := cfg.Interval
	if interval == 0 {
		interval = 30 * time.Second
	}

	reminderAfter := cfg.ReminderAfter
	if reminderAfter == 0 {
		reminderAfter = 2 * time.Hour
	}

	bugLinkInterval := cfg.BugLinkInterval
	if bugLinkInterval == 0 {
		bugLinkInterval = time.Hour
	}

	boardIDs := mergeBoardIDs(cfg.BoardID, cfg.BoardIDs)

	return &Poller{
		client:          cfg.Client,
		boardIDs:        boardIDs,
		interval:        interval,
		bugLinkInterval: bugLinkInterval,
		lastIssueSet:    make(map[string]bool),
		pendingSuggests: make(map[string]pendingSuggest),
		onNewTask:       cfg.OnNewTask,
		onAssigned:      cfg.OnAssigned,
		onSuggestAgent:  cfg.OnSuggestAgent,
		reminderAfter:   reminderAfter,
		localDB:         cfg.LocalDB,
		recorder:        cfg.Recorder,
	}
}

// mergeBoardIDs deduplicates and orders board IDs, with the legacy single
// BoardID field placed first when present.
func mergeBoardIDs(single int, list []int) []int {
	seen := make(map[int]bool)
	var out []int
	if single > 0 {
		seen[single] = true
		out = append(out, single)
	}
	for _, id := range list {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func (p *Poller) Run(ctx context.Context) error {
	slog.Info("jira poller started",
		"board_ids", p.boardIDs,
		"interval", p.interval,
		"bug_link_interval", p.bugLinkInterval,
	)

	if err := p.Poll(ctx); err != nil {
		slog.Error("initial poll failed", "error", err)
	}
	if err := p.bugLinkCycle(ctx); err != nil {
		slog.Error("initial bug link cycle failed", "error", err)
	}

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	bugTicker := time.NewTicker(p.bugLinkInterval)
	defer bugTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("jira poller stopped")
			return ctx.Err()
		case <-ticker.C:
			if err := p.Poll(ctx); err != nil {
				slog.Error("poll failed", "error", err)
			}
		case <-bugTicker.C:
			if err := p.bugLinkCycle(ctx); err != nil {
				slog.Error("bug link cycle failed", "error", err)
			}
		}
	}
}

func (p *Poller) Poll(ctx context.Context) error {
	if len(p.boardIDs) == 0 {
		return fmt.Errorf("no board IDs configured")
	}

	// Aggregate issues across every configured board so multi-project setups
	// don't leave any sprint invisible (Bug #4). Per-board failures must NOT
	// block the rest — log and continue.
	var issues []Issue
	for _, boardID := range p.boardIDs {
		sprint, err := p.client.GetActiveSprint(boardID)
		if err != nil {
			slog.Warn("get active sprint failed", "board_id", boardID, "error", err)
			continue
		}
		if sprint == nil {
			slog.Debug("no active sprint", "board_id", boardID)
			continue
		}
		boardIssues, err := p.client.GetSprintIssues(sprint.ID)
		if err != nil {
			slog.Warn("get sprint issues failed", "board_id", boardID, "sprint_id", sprint.ID, "error", err)
			continue
		}
		issues = append(issues, boardIssues...)
	}
	if len(issues) == 0 {
		return nil
	}

	currentSet := make(map[string]bool)
	for _, issue := range issues {
		currentSet[issue.Key] = true

		// New task detected
		if !p.lastIssueSet[issue.Key] {
			slog.Info("new task detected", "key", issue.Key, "summary", issue.Summary)

			links, err := p.client.GetRemoteLinks(issue.Key)
			if err != nil {
				slog.Warn("failed to get remote links", "key", issue.Key, "error", err)
			} else {
				issue.ConfluenceLinks = ExtractConfluenceLinks(links)
			}

			if p.onNewTask != nil {
				p.onNewTask(issue)
			}

			// Suggest agent for new task
			if p.onSuggestAgent != nil && !issue.IsAssigned() {
				agentName, score, reason := p.onSuggestAgent(issue)
				if agentName != "" {
					p.postSuggestionComment(issue.Key, agentName, score, reason)
					p.pendingSuggests[issue.Key] = pendingSuggest{
						suggestedAt: time.Now(),
						agentName:   agentName,
					}
				}
			}
		}

		// Check for confirmation (agent assigned)
		if issue.IsAssigned() && !issue.IsTracked() {
			slog.Info("task assigned", "key", issue.Key, "agent", issue.AgentName)

			// Remove from pending
			delete(p.pendingSuggests, issue.Key)

			if err := p.client.AddLabel(issue.Key, "dandori-tracked"); err != nil {
				slog.Warn("failed to add tracking label", "key", issue.Key, "error", err)
			}

			if p.onAssigned != nil {
				p.onAssigned(issue)
			}
		}

		// Check for reminder on pending suggestions
		if pending, ok := p.pendingSuggests[issue.Key]; ok {
			if !pending.reminderSent && time.Since(pending.suggestedAt) > p.reminderAfter {
				p.postReminderComment(issue.Key, pending.agentName)
				pending.reminderSent = true
				p.pendingSuggests[issue.Key] = pending
			}
		}
	}

	p.lastIssueSet = currentSet

	p.detectIterations(issues)
	return nil
}

// detectIterations walks current sprint issues and emits task.iteration.start
// events for any that have regressed from a prior completed run. Failures here
// must NEVER break the poll cycle — log and continue.
func (p *Poller) detectIterations(issues []Issue) {
	if p.localDB == nil || p.recorder == nil {
		return
	}
	for _, issue := range issues {
		lastRun, err := p.localDB.LatestRunForIssue(issue.Key)
		if err != nil {
			slog.Warn("iteration: latest run lookup failed", "key", issue.Key, "error", err)
			continue
		}
		if lastRun == nil {
			continue
		}
		existing, err := p.localDB.IterationEventsForIssue(issue.Key)
		if err != nil {
			slog.Warn("iteration: events lookup failed", "key", issue.Key, "error", err)
			continue
		}
		evt, err := DetectIteration(&issue,
			&PriorRun{
				RunID:                    lastRun.ID,
				Status:                   lastRun.Status,
				JiraStatusAtCompletion:   lastRun.JiraStatusAtCompletion,
				JiraCategoryAtCompletion: lastRun.JiraCategoryAtCompletion,
				EndedAt:                  lastRun.EndedAt,
			},
			toIterationEvents(existing),
		)
		if err != nil || evt == nil {
			continue
		}
		if err := p.recorder.RecordEvent(lastRun.ID, model.LayerSemantic, "task.iteration.start", evt.Payload()); err != nil {
			slog.Warn("iteration: record event failed", "key", issue.Key, "error", err)
			continue
		}
		slog.Info("iteration detected", "key", issue.Key, "round", evt.Round, "prev_run", evt.PrevRunID)
	}
}

// BugLinkCycleOnce runs the bug-link cycle exactly once. Public so the
// `dandori jira-poll --once` command can trigger a single pass without
// starting the long-running ticker loop.
func (p *Poller) BugLinkCycleOnce(ctx context.Context) error {
	return p.bugLinkCycle(ctx)
}

// bugLinkCycle searches Jira for recently-created Bug issues, runs
// DetectBugLinks on each, and emits bug.filed events for the matches.
// Designed for a separate ticker (~1h) — heavier than the sprint poll.
//
// Failures must NEVER break the cycle: each per-bug failure logs and
// continues so a single bad payload doesn't block the rest.
func (p *Poller) bugLinkCycle(ctx context.Context) error {
	if p.localDB == nil || p.recorder == nil {
		return nil
	}
	jql := fmt.Sprintf("issuetype=Bug AND created >= -%s ORDER BY created DESC", bugLinkScanWindow)
	bugs, err := p.client.SearchBugs(jql, 50)
	if err != nil {
		return fmt.Errorf("bug search: %w", err)
	}
	resolver := bugLinkDBResolver{db: p.localDB}
	for _, issue := range bugs {
		bug := &BugIssue{}
		bug.FromIssue(&issue)
		events, err := DetectBugLinks(bug, resolver)
		if err != nil {
			slog.Warn("bug link: detect failed", "key", bug.Key, "error", err)
			continue
		}
		for _, e := range events {
			if err := p.recorder.RecordEvent(e.RunID, model.LayerSemantic, "bug.filed", e.Payload); err != nil {
				slog.Warn("bug link: record event failed", "key", bug.Key, "run", e.RunID, "error", err)
				continue
			}
			slog.Info("bug linked", "bug_key", bug.Key, "run", e.RunID, "link_type", e.Payload["link_type"])
		}
	}
	return nil
}

// bugLinkDBResolver adapts LocalDB to BugLinkResolver. Lives in poller.go
// (not buglink.go) so the jira package's pure detection layer stays
// resolver-interface only.
type bugLinkDBResolver struct{ db *db.LocalDB }

func (r bugLinkDBResolver) LatestRunForIssue(issueKey string) (string, error) {
	return r.db.LatestRunIDForIssue(issueKey)
}
func (r bugLinkDBResolver) FindRunByPrefix(prefix string) (string, error) {
	return r.db.FindRunByPrefix(prefix)
}
func (r bugLinkDBResolver) BugEventExists(bugKey string) (bool, error) {
	return r.db.BugEventExists(bugKey)
}

func toIterationEvents(rows []db.IterationEventRow) []IterationEvent {
	out := make([]IterationEvent, 0, len(rows))
	for _, r := range rows {
		out = append(out, IterationEvent{
			Round:          r.Round,
			IssueKey:       r.IssueKey,
			TransitionedAt: r.TransitionedAt,
		})
	}
	return out
}

func (p *Poller) postSuggestionComment(issueKey, agentName string, score int, reason string) {
	comment := "🤖 *Agent Suggestion*\n\n" +
		"*Suggested agent:* " + agentName + " (" + itoa(score) + "%)\n" +
		"*Reason:* " + reason + "\n\n" +
		"To confirm: set `dandori-agent` field to `" + agentName + "`"

	if err := p.client.AddComment(issueKey, comment); err != nil {
		slog.Warn("failed to post suggestion comment", "key", issueKey, "error", err)
	} else {
		slog.Info("posted suggestion", "key", issueKey, "agent", agentName, "score", score)
	}
}

func (p *Poller) postReminderComment(issueKey, agentName string) {
	comment := "⏰ *Reminder*: Agent suggestion pending confirmation.\n\n" +
		"Suggested agent: *" + agentName + "*\n\n" +
		"Please set `dandori-agent` field to confirm or assign a different agent."

	if err := p.client.AddComment(issueKey, comment); err != nil {
		slog.Warn("failed to post reminder", "key", issueKey, "error", err)
	} else {
		slog.Info("posted reminder", "key", issueKey)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func (p *Poller) GetLastIssueSet() map[string]bool {
	return p.lastIssueSet
}
