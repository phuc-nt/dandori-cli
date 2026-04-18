package wrapper

import (
	"testing"
)

func TestExtractJiraKey(t *testing.T) {
	tests := []struct {
		branch   string
		expected string
	}{
		{"feature/PROJ-123-login-page", "PROJ-123"},
		{"bugfix/BUG-456-fix-crash", "BUG-456"},
		{"hotfix/HOT-789-urgent", "HOT-789"},
		{"fix/FIX-111-typo", "FIX-111"},
		{"task/TASK-222-refactor", "TASK-222"},
		{"PROJ-123-some-feature", "PROJ-123"},
		{"main", ""},
		{"develop", ""},
		{"feature/no-ticket-here", ""},
		{"AB-1", "AB-1"},
		{"feature/ABC-12345-long-number", "ABC-12345"},
	}

	for _, tt := range tests {
		t.Run(tt.branch, func(t *testing.T) {
			result := ExtractJiraKey(tt.branch)
			if result != tt.expected {
				t.Errorf("ExtractJiraKey(%q) = %q, want %q", tt.branch, result, tt.expected)
			}
		})
	}
}

func TestParseLineForTokens(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		expected TokenUsage
	}{
		{
			name: "assistant message with usage",
			line: `{"type":"assistant","message":{"model":"claude-sonnet-4-6","usage":{"input_tokens":100,"output_tokens":50}}}`,
			expected: TokenUsage{
				Input:  100,
				Output: 50,
				Model:  "claude-sonnet-4-6",
			},
		},
		{
			name: "with cache tokens",
			line: `{"type":"assistant","message":{"model":"claude-opus-4-6","usage":{"input_tokens":200,"output_tokens":100,"cache_creation_input_tokens":50,"cache_read_input_tokens":150}}}`,
			expected: TokenUsage{
				Input:      200,
				Output:     100,
				CacheWrite: 50,
				CacheRead:  150,
				Model:      "claude-opus-4-6",
			},
		},
		{
			name:     "non-assistant message",
			line:     `{"type":"user","content":"hello"}`,
			expected: TokenUsage{},
		},
		{
			name:     "invalid json",
			line:     `invalid json`,
			expected: TokenUsage{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseLineForTokens([]byte(tt.line))
			if result.Input != tt.expected.Input {
				t.Errorf("Input = %d, want %d", result.Input, tt.expected.Input)
			}
			if result.Output != tt.expected.Output {
				t.Errorf("Output = %d, want %d", result.Output, tt.expected.Output)
			}
			if result.CacheRead != tt.expected.CacheRead {
				t.Errorf("CacheRead = %d, want %d", result.CacheRead, tt.expected.CacheRead)
			}
			if result.CacheWrite != tt.expected.CacheWrite {
				t.Errorf("CacheWrite = %d, want %d", result.CacheWrite, tt.expected.CacheWrite)
			}
			if result.Model != tt.expected.Model {
				t.Errorf("Model = %q, want %q", result.Model, tt.expected.Model)
			}
		})
	}
}

func TestComputeCost(t *testing.T) {
	usage := TokenUsage{
		Input:      1_000_000,
		Output:     1_000_000,
		CacheRead:  1_000_000,
		CacheWrite: 1_000_000,
		Model:      "claude-sonnet-4-6",
	}

	cost := ComputeCost(usage)

	expectedCost := 3.00 + 15.00 + 0.30 + 3.75
	if cost != expectedCost {
		t.Errorf("ComputeCost = %f, want %f", cost, expectedCost)
	}
}

func TestMergeUsage(t *testing.T) {
	a := TokenUsage{Input: 100, Output: 50, Model: "model-a"}
	b := TokenUsage{Input: 200, Output: 100, CacheRead: 50, Model: "model-b"}

	result := mergeUsage(a, b)

	if result.Input != 300 {
		t.Errorf("Input = %d, want 300", result.Input)
	}
	if result.Output != 150 {
		t.Errorf("Output = %d, want 150", result.Output)
	}
	if result.CacheRead != 50 {
		t.Errorf("CacheRead = %d, want 50", result.CacheRead)
	}
	if result.Model != "model-b" {
		t.Errorf("Model = %q, want model-b", result.Model)
	}
}
