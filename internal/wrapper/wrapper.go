package wrapper

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"strings"
	"syscall"
	"time"

	"github.com/phuc-nt/dandori-cli/internal/db"
	"github.com/phuc-nt/dandori-cli/internal/event"
	"github.com/phuc-nt/dandori-cli/internal/model"
	"github.com/phuc-nt/dandori-cli/internal/quality"
	"github.com/phuc-nt/dandori-cli/internal/util"
)

type Options struct {
	Command         []string
	JiraIssueKey    string
	AutoTask        bool
	NoTailer        bool
	DryRun          bool
	AgentName       string
	AgentType       string
	QualityConfig   quality.Config
	PostExitTimeout time.Duration // 0 = --no-wait, <0 = use DefaultPostExitTimeout
	// RunID lets the caller pre-create the runs row (status=pending) before the
	// wrapper starts so that earlier work (e.g. taskcontext fetch) can emit
	// events tagged with a real run_id. When empty, wrapper generates one.
	RunID string
}

type Result struct {
	RunID      string
	ExitCode   int
	Duration   time.Duration
	SessionID  string
	TokenUsage TokenUsage
	CostUSD    float64
	// QualityAfter is the post-run lint+test snapshot when QualityConfig is
	// enabled. nil otherwise. Exposed so callers (e.g. cmd/task_run.go) can
	// feed the verify gate without re-running the snapshot.
	QualityAfter *quality.Snapshot
}

type TokenUsage struct {
	Input      int
	Output     int
	CacheRead  int
	CacheWrite int
	Model      string
}

func Run(ctx context.Context, localDB *db.LocalDB, opts Options) (*Result, error) {
	if len(opts.Command) == 0 {
		return nil, fmt.Errorf("no command specified")
	}

	runID := opts.RunID
	if runID == "" {
		runID = util.GenerateRunID()
	}
	startedAt := time.Now()

	cwd, _ := os.Getwd()
	currentUser, _ := user.Current()
	hostname, _ := os.Hostname()
	workstationID := fmt.Sprintf("ws-%s", hostname)

	gitHeadBefore := getGitHead()

	jiraKey := opts.JiraIssueKey
	if jiraKey == "" && opts.AutoTask {
		jiraKey = ExtractJiraKeyFromBranch()
	}

	run := &model.Run{
		ID:            runID,
		AgentName:     opts.AgentName,
		AgentType:     opts.AgentType,
		User:          currentUser.Username,
		WorkstationID: workstationID,
		CWD:           sql.NullString{String: cwd, Valid: true},
		GitHeadBefore: sql.NullString{String: gitHeadBefore, Valid: gitHeadBefore != ""},
		Command:       sql.NullString{String: strings.Join(opts.Command, " "), Valid: true},
		StartedAt:     startedAt,
		Status:        model.StatusRunning,
	}

	if jiraKey != "" {
		run.JiraIssueKey = sql.NullString{String: jiraKey, Valid: true}
	}

	gitRemote := getGitRemote()
	if gitRemote != "" {
		run.GitRemote = sql.NullString{String: gitRemote, Valid: true}
	}

	if opts.DryRun {
		slog.Info("dry run", "run_id", runID, "command", opts.Command, "jira_key", jiraKey)
		return &Result{RunID: runID}, nil
	}

	if err := insertRun(localDB, run); err != nil {
		return nil, fmt.Errorf("insert run: %w", err)
	}
	slog.Debug("run started", "run_id", runID, "command", opts.Command)

	// Quality snapshot before (non-blocking)
	var qualityBefore *quality.Snapshot
	var qualityCollector *quality.Collector
	if opts.QualityConfig.Enabled {
		qualityCollector = quality.NewCollector(opts.QualityConfig)
		slog.Debug("capturing quality snapshot before run")
		qualityBefore = qualityCollector.SnapshotLintOnly(cwd) // Lint only for speed
		slog.Debug("quality before", "lint_errors", qualityBefore.LintErrors, "lint_warnings", qualityBefore.LintWarnings)
	}

	sessionSnapshot := SnapshotSessionDir(cwd)

	cmd := exec.CommandContext(ctx, opts.Command[0], opts.Command[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	doneChan := make(chan error, 1)
	go func() {
		doneChan <- cmd.Run()
	}()

	go func() {
		for sig := range sigChan {
			if cmd.Process != nil {
				cmd.Process.Signal(sig)
			}
		}
	}()

	tailerCtx, tailerCancel := context.WithCancel(context.Background())
	usageChan := make(chan TokenUsage, 1)

	if !opts.NoTailer {
		timeout := opts.PostExitTimeout
		if timeout < 0 {
			timeout = DefaultPostExitTimeout
		}
		recorder := event.NewRecorder(localDB)
		go func() {
			usage := TailSessionLogWithRecorder(tailerCtx, cwd, sessionSnapshot, timeout, recorder, runID)
			usageChan <- usage
		}()
	} else {
		usageChan <- TokenUsage{}
	}

	err := <-doneChan
	signal.Stop(sigChan)
	close(sigChan)

	endedAt := time.Now()
	duration := endedAt.Sub(startedAt)

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	gitHeadAfter := getGitHead()
	sessionID := DetectSessionID(cwd, sessionSnapshot)

	status := model.StatusDone
	if exitCode != 0 {
		status = model.StatusError
	}

	tailerCancel()
	tokenUsage := <-usageChan

	costUSD := ComputeCost(tokenUsage)

	if err := updateRunComplete(localDB, runID, endedAt, duration, exitCode, status, gitHeadAfter, sessionID, tokenUsage, costUSD); err != nil {
		slog.Error("update run", "error", err)
	}

	emitIterationEndIfApplicable(localDB, runID)

	// Quality snapshot after and store metrics
	var qualityAfter *quality.Snapshot
	if qualityCollector != nil && qualityBefore != nil {
		slog.Debug("capturing quality snapshot after run")
		qualityAfter = qualityCollector.Snapshot(cwd) // Full snapshot (lint + tests)
		slog.Debug("quality after", "lint_errors", qualityAfter.LintErrors, "tests_passed", qualityAfter.TestsPassed)

		metrics := quality.ComputeMetrics(runID, qualityBefore, qualityAfter)

		// Add git metrics (Phase 02)
		if gitHeadBefore != "" && gitHeadAfter != "" && gitHeadBefore != gitHeadAfter {
			analyzer := quality.NewGitAnalyzer(cwd)
			if stats, err := analyzer.DiffStats(gitHeadBefore, gitHeadAfter); err == nil {
				metrics.LinesAdded = stats.LinesAdded
				metrics.LinesRemoved = stats.LinesRemoved
				metrics.FilesChanged = stats.FilesChanged
				metrics.CommitCount = stats.CommitCount
				metrics.CommitMsgQuality = quality.ScoreCommitMessages(stats.CommitMsgs)
				slog.Debug("git metrics", "lines_added", stats.LinesAdded, "commits", stats.CommitCount, "msg_quality", metrics.CommitMsgQuality)
			}
		}

		if err := localDB.InsertQualityMetrics(metrics); err != nil {
			slog.Warn("failed to store quality metrics", "error", err)
		} else {
			slog.Debug("quality metrics stored", "lint_delta", metrics.LintDelta, "tests_delta", metrics.TestsDelta)
		}
	}

	slog.Debug("run completed", "run_id", runID, "exit_code", exitCode, "duration", duration)

	return &Result{
		RunID:        runID,
		ExitCode:     exitCode,
		Duration:     duration,
		SessionID:    sessionID,
		TokenUsage:   tokenUsage,
		CostUSD:      costUSD,
		QualityAfter: qualityAfter,
	}, nil
}

// emitIterationEndIfApplicable closes the loop on iteration tracking: if a
// task.iteration.start event was previously emitted against this run (set up
// by the poller when it detected the Done→Active transition), emit a matching
// task.iteration.end with the same round number. Failures are logged and
// swallowed — tracking must never break a finished run.
func emitIterationEndIfApplicable(localDB *db.LocalDB, runID string) {
	row := localDB.QueryRow(`
		SELECT data FROM events
		WHERE run_id = ? AND event_type = 'task.iteration.start'
		ORDER BY id DESC LIMIT 1
	`, runID)
	var data string
	if err := row.Scan(&data); err != nil {
		return // none — round 1 (implicit), nothing to close
	}

	recorder := event.NewRecorder(localDB)
	var payload map[string]any
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		slog.Warn("iteration end: parse start payload", "error", err)
		return
	}
	endPayload := map[string]any{
		"round":     payload["round"],
		"issue_key": payload["issue_key"],
		"ended_at":  time.Now().Format(time.RFC3339),
	}
	if err := recorder.RecordEvent(runID, model.LayerSemantic, "task.iteration.end", endPayload); err != nil {
		slog.Warn("iteration end: record event", "error", err)
	}
}

func getGitHead() string {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func getGitRemote() string {
	cmd := exec.Command("git", "remote", "get-url", "origin")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func insertRun(localDB *db.LocalDB, run *model.Run) error {
	// ON CONFLICT updates the row so callers can pre-create a pending run
	// (e.g. for taskcontext.Fetch event tagging) and the wrapper still owns
	// the canonical fields once execution starts.
	//
	// engineer_name uses COALESCE so that a pre-created value (set from the
	// Jira assignee in cmd/task_run.go) is not overwritten by NULL when the
	// wrapper fires. If the wrapper does have a value it wins normally.
	var engineerNameVal interface{}
	if run.EngineerName != "" {
		engineerNameVal = run.EngineerName
	}
	_, err := localDB.Exec(`
		INSERT INTO runs (
			id, jira_issue_key, jira_sprint_id, agent_name, agent_type,
			user, workstation_id, cwd, git_remote, git_head_before,
			command, started_at, status, engineer_name
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			jira_issue_key = excluded.jira_issue_key,
			jira_sprint_id = excluded.jira_sprint_id,
			agent_name     = excluded.agent_name,
			agent_type     = excluded.agent_type,
			user           = excluded.user,
			workstation_id = excluded.workstation_id,
			cwd            = excluded.cwd,
			git_remote     = excluded.git_remote,
			git_head_before= excluded.git_head_before,
			command        = excluded.command,
			started_at     = excluded.started_at,
			status         = excluded.status,
			engineer_name  = COALESCE(excluded.engineer_name, engineer_name)
	`,
		run.ID, run.JiraIssueKey, run.JiraSprintID, run.AgentName, run.AgentType,
		run.User, run.WorkstationID, run.CWD, run.GitRemote, run.GitHeadBefore,
		run.Command, run.StartedAt.Format(time.RFC3339), run.Status, engineerNameVal,
	)
	return err
}

func updateRunComplete(localDB *db.LocalDB, runID string, endedAt time.Time, duration time.Duration, exitCode int, status model.RunStatus, gitHeadAfter, sessionID string, tokens TokenUsage, costUSD float64) error {
	_, err := localDB.Exec(`
		UPDATE runs SET
			ended_at = ?,
			duration_sec = ?,
			exit_code = ?,
			status = ?,
			git_head_after = ?,
			session_id = ?,
			input_tokens = ?,
			output_tokens = ?,
			cache_read_tokens = ?,
			cache_write_tokens = ?,
			model = ?,
			cost_usd = ?
		WHERE id = ?
	`,
		endedAt.Format(time.RFC3339),
		duration.Seconds(),
		exitCode,
		status,
		gitHeadAfter,
		sessionID,
		tokens.Input,
		tokens.Output,
		tokens.CacheRead,
		tokens.CacheWrite,
		tokens.Model,
		costUSD,
		runID,
	)
	return err
}
