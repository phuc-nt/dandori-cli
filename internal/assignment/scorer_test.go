package assignment

import (
	"testing"
)

func TestScoreCapabilityOverlap(t *testing.T) {
	tests := []struct {
		name        string
		agentCaps   []string
		taskLabels  []string
		taskComps   []string
		expectedPct float64 // expected percentage (0-1)
	}{
		{
			name:        "full overlap",
			agentCaps:   []string{"backend", "api"},
			taskLabels:  []string{"backend", "api"},
			expectedPct: 1.0,
		},
		{
			name:        "partial overlap",
			agentCaps:   []string{"backend", "frontend"},
			taskLabels:  []string{"backend", "api"},
			expectedPct: 0.5,
		},
		{
			name:        "no overlap",
			agentCaps:   []string{"frontend", "mobile"},
			taskLabels:  []string{"backend", "api"},
			expectedPct: 0.0,
		},
		{
			name:        "empty task labels",
			agentCaps:   []string{"backend"},
			taskLabels:  []string{},
			expectedPct: 0.0,
		},
		{
			name:        "include components",
			agentCaps:   []string{"auth", "backend"},
			taskLabels:  []string{"urgent"},
			taskComps:   []string{"auth-service"},
			expectedPct: 0.33, // 1 match (auth) / 3 terms (urgent, auth, service)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := AgentConfig{Capabilities: tt.agentCaps}
			task := Task{Labels: tt.taskLabels, Components: tt.taskComps}
			score := scoreCapabilityOverlap(agent, task)
			if score < tt.expectedPct-0.1 || score > tt.expectedPct+0.1 {
				t.Errorf("scoreCapabilityOverlap() = %.2f, want ~%.2f", score, tt.expectedPct)
			}
		})
	}
}

func TestScoreIssueTypePreference(t *testing.T) {
	tests := []struct {
		name        string
		preferred   []string
		issueType   string
		expectedPct float64
	}{
		{
			name:        "exact match",
			preferred:   []string{"Bug", "Story"},
			issueType:   "Bug",
			expectedPct: 1.0,
		},
		{
			name:        "no preference",
			preferred:   []string{},
			issueType:   "Bug",
			expectedPct: 0.5,
		},
		{
			name:        "mismatch",
			preferred:   []string{"Story"},
			issueType:   "Bug",
			expectedPct: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := AgentConfig{PreferredIssueTypes: tt.preferred}
			task := Task{IssueType: tt.issueType}
			score := scoreIssueTypePreference(agent, task)
			if score != tt.expectedPct {
				t.Errorf("scoreIssueTypePreference() = %.2f, want %.2f", score, tt.expectedPct)
			}
		})
	}
}

func TestScoreLoadBalance(t *testing.T) {
	tests := []struct {
		name        string
		activeRuns  int
		maxConc     int
		expectedPct float64
	}{
		{
			name:        "no active runs",
			activeRuns:  0,
			maxConc:     3,
			expectedPct: 1.0,
		},
		{
			name:        "half loaded",
			activeRuns:  1,
			maxConc:     2,
			expectedPct: 0.5,
		},
		{
			name:        "fully loaded",
			activeRuns:  3,
			maxConc:     3,
			expectedPct: 0.0,
		},
		{
			name:        "over loaded",
			activeRuns:  5,
			maxConc:     3,
			expectedPct: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := AgentConfig{MaxConcurrent: tt.maxConc, ActiveRuns: tt.activeRuns}
			score := scoreLoadBalance(agent)
			if score != tt.expectedPct {
				t.Errorf("scoreLoadBalance() = %.2f, want %.2f", score, tt.expectedPct)
			}
		})
	}
}

func TestScore(t *testing.T) {
	agent := AgentConfig{
		Name:                "alpha",
		Capabilities:        []string{"backend", "api"},
		PreferredIssueTypes: []string{"Bug"},
		MaxConcurrent:       3,
		ActiveRuns:          0,
	}

	task := Task{
		IssueKey:   "TEST-1",
		IssueType:  "Bug",
		Labels:     []string{"backend", "api"},
		Components: []string{},
	}

	history := HistoryStats{SuccessRate: 1.0}

	score, reason := Score(agent, task, history)

	if score < 80 {
		t.Errorf("Score() = %d, expected >= 80 for perfect match", score)
	}
	if reason == "" {
		t.Error("Score() should return explanation")
	}
	t.Logf("Score: %d, Reason: %s", score, reason)
}

func TestScoreWeights(t *testing.T) {
	// Verify weights sum to 1.0
	total := WeightCapability + WeightIssueType + WeightHistory + WeightLoadBalance
	if total < 0.99 || total > 1.01 {
		t.Errorf("Weights sum to %.2f, should be 1.0", total)
	}
}
