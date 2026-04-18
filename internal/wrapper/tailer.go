package wrapper

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"time"
)

func TailSessionLog(ctx context.Context, cwd string, snapshot *SessionSnapshot) TokenUsage {
	usage := TokenUsage{}

	if snapshot == nil || snapshot.Dir == "" {
		return usage
	}

	time.Sleep(2 * time.Second)

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	var lastOffset int64
	var logPath string

	for {
		select {
		case <-ctx.Done():
			if logPath != "" {
				usage = readTokensFromOffset(logPath, lastOffset, usage)
			}
			return usage
		case <-ticker.C:
			if logPath == "" {
				logPath = GetSessionLogPath(cwd, snapshot)
				if logPath == "" {
					continue
				}
			}

			newUsage, newOffset := parseLogFromOffset(logPath, lastOffset)
			usage = mergeUsage(usage, newUsage)
			lastOffset = newOffset
		}
	}
}

func readTokensFromOffset(path string, offset int64, current TokenUsage) TokenUsage {
	newUsage, _ := parseLogFromOffset(path, offset)
	return mergeUsage(current, newUsage)
}

func parseLogFromOffset(path string, offset int64) (TokenUsage, int64) {
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
