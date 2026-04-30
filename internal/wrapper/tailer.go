package wrapper

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/phuc-nt/dandori-cli/internal/event"
	"github.com/phuc-nt/dandori-cli/internal/model"
)

// DefaultPostExitTimeout is how long the tailer keeps polling after the
// wrapped process exits before giving up on the session log appearing.
// Claude Code typically flushes JSONL 1-4s after exit; 10s gives headroom.
const DefaultPostExitTimeout = 10 * time.Second

// TailSessionLog preserves the original API — delegates to the configurable
// version with DefaultPostExitTimeout.
func TailSessionLog(ctx context.Context, cwd string, snapshot *SessionSnapshot) TokenUsage {
	return TailSessionLogWithTimeout(ctx, cwd, snapshot, DefaultPostExitTimeout)
}

// TailSessionLogWithRecorder is the phase-01 entry point: same behaviour as
// TailSessionLogWithTimeout, plus emits Layer-3 semantic events (tool.use /
// tool.result / skill.invoke) into the events table for runID. recorder may
// be nil — in that case, behaviour is identical to the timeout-only variant.
func TailSessionLogWithRecorder(ctx context.Context, cwd string, snapshot *SessionSnapshot, postExitTimeout time.Duration, recorder *event.Recorder, runID string) TokenUsage {
	return tailSessionLog(ctx, cwd, snapshot, postExitTimeout, recorder, runID)
}

// TailSessionLogWithTimeout watches the Claude session JSONL in two phases:
//
//	Phase A (ctx alive): standard ticker loop — captures tokens as they're written.
//	Phase B (ctx done):  post-exit wait up to postExitTimeout. Session JSONL often
//	                     appears 1-4s AFTER the child process exits, so we keep
//	                     polling GetSessionLogPath at 500ms intervals. Once the
//	                     file appears, we drain it until no new bytes for 500ms.
//
// Pass postExitTimeout=0 to skip Phase B entirely (--no-wait behaviour).
func TailSessionLogWithTimeout(ctx context.Context, cwd string, snapshot *SessionSnapshot, postExitTimeout time.Duration) TokenUsage {
	return tailSessionLog(ctx, cwd, snapshot, postExitTimeout, nil, "")
}

// tailSessionLog is the unified implementation. recorder/runID are optional;
// when both are set, every parsed line also emits Layer-3 events.
func tailSessionLog(ctx context.Context, cwd string, snapshot *SessionSnapshot, postExitTimeout time.Duration, recorder *event.Recorder, runID string) TokenUsage {
	usage := TokenUsage{}

	if snapshot == nil || snapshot.Dir == "" {
		return usage
	}

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var lastOffset int64
	var logPath string

	// Phase A: child still running.
	for {
		select {
		case <-ctx.Done():
			if postExitTimeout <= 0 {
				// --no-wait: one last read if we already found the file.
				if logPath != "" {
					newUsage, newOffset := parseLogFromOffsetWithRecorder(logPath, lastOffset, recorder, runID)
					usage = mergeUsage(usage, newUsage)
					lastOffset = newOffset
				}
				return usage
			}
			return tailPostExit(cwd, snapshot, logPath, lastOffset, usage, postExitTimeout, recorder, runID)
		case <-ticker.C:
			if logPath == "" {
				logPath = GetSessionLogPath(cwd, snapshot)
				if logPath == "" {
					continue
				}
			}
			newUsage, newOffset := parseLogFromOffsetWithRecorder(logPath, lastOffset, recorder, runID)
			usage = mergeUsage(usage, newUsage)
			lastOffset = newOffset
		}
	}
}

// tailPostExit polls for the session log after child exit, drains it, and
// returns when either:
//   - no new bytes for idleGrace (session flushed and stable), OR
//   - postExitTimeout expires.
func tailPostExit(cwd string, snapshot *SessionSnapshot, logPath string, lastOffset int64, usage TokenUsage, postExitTimeout time.Duration, recorder *event.Recorder, runID string) TokenUsage {
	const (
		pollInterval = 250 * time.Millisecond
		idleGrace    = 750 * time.Millisecond
	)

	deadline := time.Now().Add(postExitTimeout)
	idleTicker := time.NewTicker(pollInterval)
	defer idleTicker.Stop()

	var lastGrowth time.Time
	hadBytes := lastOffset > 0

	for {
		if time.Now().After(deadline) {
			slog.Debug("tailer post-exit timeout", "elapsed", postExitTimeout, "found_log", logPath != "")
			return usage
		}

		if logPath == "" {
			logPath = GetSessionLogPath(cwd, snapshot)
		}

		if logPath != "" {
			newUsage, newOffset := parseLogFromOffsetWithRecorder(logPath, lastOffset, recorder, runID)
			if newOffset > lastOffset {
				usage = mergeUsage(usage, newUsage)
				lastOffset = newOffset
				lastGrowth = time.Now()
				hadBytes = true
			} else if hadBytes && !lastGrowth.IsZero() && time.Since(lastGrowth) >= idleGrace {
				// File is stable — session flushed.
				return usage
			}
		}

		// Sleep until next poll or deadline, whichever is sooner.
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return usage
		}
		wait := pollInterval
		if remaining < wait {
			wait = remaining
		}
		time.Sleep(wait)
	}
}

func parseLogFromOffset(path string, offset int64) (TokenUsage, int64) {
	return parseLogFromOffsetWithRecorder(path, offset, nil, "")
}

// parseLogFromOffsetWithRecorder reads new bytes from the JSONL session log,
// extracts token usage (Layer 2) and emits Layer-3 events when recorder is
// non-nil. Recorder failures are logged and swallowed — tracking must never
// break a live run.
func parseLogFromOffsetWithRecorder(path string, offset int64, recorder *event.Recorder, runID string) (TokenUsage, int64) {
	usage := TokenUsage{}

	f, err := os.Open(path)
	if err != nil {
		return usage, offset
	}
	defer f.Close()

	if offset > 0 {
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			return usage, offset
		}
	}

	reader := bufio.NewReader(f)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			break
		}

		lineUsage := parseLineForTokens(line)
		usage = mergeUsage(usage, lineUsage)

		if recorder != nil && runID != "" {
			for _, ev := range parseLineForEvents(line) {
				if recErr := recorder.RecordEvent(runID, model.LayerSemantic, ev.Type, ev.Payload); recErr != nil {
					slog.Warn("record session event failed", "type", ev.Type, "error", recErr)
				}
			}
		}
	}

	newOffset, _ := f.Seek(0, io.SeekCurrent)
	return usage, newOffset
}

type sessionMessage struct {
	Type    string `json:"type"`
	Message struct {
		Model string `json:"model"`
		Usage struct {
			InputTokens              int `json:"input_tokens"`
			OutputTokens             int `json:"output_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

func parseLineForTokens(line []byte) TokenUsage {
	usage := TokenUsage{}

	var msg sessionMessage
	if err := json.Unmarshal(line, &msg); err != nil {
		return usage
	}

	if msg.Type != "assistant" {
		return usage
	}

	if msg.Message.Model != "" {
		usage.Model = msg.Message.Model
	}

	usage.Input = msg.Message.Usage.InputTokens
	usage.Output = msg.Message.Usage.OutputTokens
	usage.CacheWrite = msg.Message.Usage.CacheCreationInputTokens
	usage.CacheRead = msg.Message.Usage.CacheReadInputTokens

	return usage
}

func mergeUsage(a, b TokenUsage) TokenUsage {
	result := TokenUsage{
		Input:      a.Input + b.Input,
		Output:     a.Output + b.Output,
		CacheRead:  a.CacheRead + b.CacheRead,
		CacheWrite: a.CacheWrite + b.CacheWrite,
	}

	if b.Model != "" {
		result.Model = b.Model
	} else {
		result.Model = a.Model
	}

	return result
}

func ComputeCost(usage TokenUsage) float64 {
	prices := GetModelPrices(usage.Model)

	cost := float64(usage.Input)*prices.Input/1_000_000 +
		float64(usage.Output)*prices.Output/1_000_000 +
		float64(usage.CacheWrite)*prices.CacheWrite/1_000_000 +
		float64(usage.CacheRead)*prices.CacheRead/1_000_000

	return cost
}

type ModelPrices struct {
	Input      float64
	Output     float64
	CacheWrite float64
	CacheRead  float64
}

var defaultPrices = map[string]ModelPrices{
	"claude-sonnet-4-6": {
		Input:      3.00,
		Output:     15.00,
		CacheWrite: 3.75,
		CacheRead:  0.30,
	},
	"claude-opus-4-6": {
		Input:      15.00,
		Output:     75.00,
		CacheWrite: 18.75,
		CacheRead:  1.50,
	},
	"claude-opus-4-7": {
		Input:      15.00,
		Output:     75.00,
		CacheWrite: 18.75,
		CacheRead:  1.50,
	},
	"claude-opus-4-5-20251101": {
		Input:      15.00,
		Output:     75.00,
		CacheWrite: 18.75,
		CacheRead:  1.50,
	},
	"claude-haiku-4-5": {
		Input:      0.80,
		Output:     4.00,
		CacheWrite: 1.00,
		CacheRead:  0.08,
	},
}

func GetModelPrices(model string) ModelPrices {
	if prices, ok := defaultPrices[model]; ok {
		return prices
	}

	slog.Debug("unknown model, using sonnet prices", "model", model)
	return defaultPrices["claude-sonnet-4-6"]
}
