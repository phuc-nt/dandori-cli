package quality

import (
	"encoding/json"
	"strings"
)

// GolangciLintOutput represents golangci-lint JSON output
type GolangciLintOutput struct {
	Issues []GolangciIssue `json:"Issues"`
}

// GolangciIssue represents a single lint issue
type GolangciIssue struct {
	FromLinter  string   `json:"FromLinter"`
	Text        string   `json:"Text"`
	Severity    string   `json:"Severity"`
	SourceLines []string `json:"SourceLines"`
	Pos         struct {
		Filename string `json:"Filename"`
		Line     int    `json:"Line"`
		Column   int    `json:"Column"`
	} `json:"Pos"`
}

// ParseGolangciLint parses golangci-lint JSON output
// Returns (errors, warnings, error)
func ParseGolangciLint(output []byte) (int, int, error) {
	// Handle empty output
	if len(output) == 0 {
		return 0, 0, nil
	}

	// Try parsing as JSON
	var result GolangciLintOutput
	if err := json.Unmarshal(output, &result); err != nil {
		// Fallback: count lines with error/warning patterns
		return parseLineCounts(string(output))
	}

	// Handle null Issues array
	if result.Issues == nil {
		return 0, 0, nil
	}

	errors := 0
	warnings := 0

	for _, issue := range result.Issues {
		severity := strings.ToLower(issue.Severity)
		switch severity {
		case "error", "":
			// Empty severity defaults to error
			errors++
		case "warning":
			warnings++
		default:
			// Unknown severity treated as warning
			warnings++
		}
	}

	return errors, warnings, nil
}

// parseLineCounts fallback: count error/warning patterns in text output
func parseLineCounts(output string) (int, int, error) {
	lines := strings.Split(output, "\n")
	errors := 0
	warnings := 0

	for _, line := range lines {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "error:") || strings.Contains(lower, ": error") {
			errors++
		} else if strings.Contains(lower, "warning:") || strings.Contains(lower, ": warning") {
			warnings++
		}
	}

	return errors, warnings, nil
}
