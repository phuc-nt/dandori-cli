package wrapper

import (
	"context"
	"database/sql"
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
	"github.com/phuc-nt/dandori-cli/internal/model"
	"github.com/phuc-nt/dandori-cli/internal/util"
)

type Options struct {
	Command      []string
	JiraIssueKey string
	AutoTask     bool
	NoTailer     bool
	DryRun       bool
	AgentName    string
	AgentType    string
}

type Result struct {
	RunID       string
	ExitCode    int
	Duration    time.Duration
	SessionID   string
	TokenUsage  TokenUsage
	CostUSD     float64
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

	runID := util.GenerateRunID()
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
		go func() {
			usage := TailSessionLog(tailerCtx, cwd, sessionSnapshot)
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

	slog.Debug("run completed", "run_id", runID, "exit_code", exitCode, "duration", duration)

	return &Result{
		RunID:      runID,
		ExitCode:   exitCode,
		Duration:   duration,
		SessionID:  sessionID,
		TokenUsage: tokenUsage,
		CostUSD:    costUSD,
	}, nil
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
	_, err := localDB.Exec(`
		INSERT INTO runs (
			id, jira_issue_key, jira_sprint_id, agent_name, agent_type,
			user, workstation_id, cwd, git_remote, git_head_before,
			command, started_at, status
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		run.ID, run.JiraIssueKey, run.JiraSprintID, run.AgentName, run.AgentType,
		run.User, run.WorkstationID, run.CWD, run.GitRemote, run.GitHeadBefore,
		run.Command, run.StartedAt.Format(time.RFC3339), run.Status,
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
