package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/phuc-nt/dandori-cli/internal/analytics"
)

func TestParseFiltersEmpty(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/analytics/agents", nil)
	f := parseFilters(req)

	if f.Agent != "" || f.SprintID != "" {
		t.Error("empty request should have empty filters")
	}
	if f.From != nil || f.To != nil {
		t.Error("empty request should have nil dates")
	}
}

func TestParseFiltersWithParams(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/analytics/agents?agent=alpha&sprint=42&from=2026-04-01&to=2026-04-18", nil)
	f := parseFilters(req)

	if f.Agent != "alpha" {
		t.Errorf("agent = %s, want alpha", f.Agent)
	}
	if f.SprintID != "42" {
		t.Errorf("sprint = %s, want 42", f.SprintID)
	}
	if f.From == nil {
		t.Error("from should be parsed")
	}
	if f.To == nil {
		t.Error("to should be parsed")
	}
}

func TestParseGroupBy(t *testing.T) {
	tests := []struct {
		query    string
		expected string
	}{
		{"", "agent"},
		{"group_by=agent", "agent"},
		{"group_by=sprint", "sprint"},
		{"group_by=task", "task"},
		{"group_by=day", "day"},
		{"group_by=invalid", "agent"},
	}

	for _, tt := range tests {
		req := httptest.NewRequest(http.MethodGet, "/api/analytics/cost?"+tt.query, nil)
		result := parseGroupBy(req)
		if result != tt.expected {
			t.Errorf("parseGroupBy(%q) = %s, want %s", tt.query, result, tt.expected)
		}
	}
}

func TestAnalyticsResponseFormat(t *testing.T) {
	stats := []analytics.AgentStat{
		{AgentName: "alpha", RunCount: 10, SuccessRate: 90.0, TotalCost: 50.0},
	}

	data, err := json.Marshal(map[string]any{"data": stats})
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	dataArr, ok := resp["data"].([]any)
	if !ok {
		t.Fatal("response should have data array")
	}
	if len(dataArr) != 1 {
		t.Errorf("expected 1 item, got %d", len(dataArr))
	}
}

func TestExportFormatParsing(t *testing.T) {
	tests := []struct {
		query  string
		format string
	}{
		{"", "json"},
		{"format=json", "json"},
		{"format=csv", "csv"},
		{"format=invalid", "json"},
	}

	for _, tt := range tests {
		req := httptest.NewRequest(http.MethodGet, "/api/analytics/export?"+tt.query, nil)
		result := parseExportFormat(req)
		if result != tt.format {
			t.Errorf("parseExportFormat(%q) = %s, want %s", tt.query, result, tt.format)
		}
	}
}

func TestParseAgentList(t *testing.T) {
	tests := []struct {
		query    string
		expected []string
	}{
		{"", nil},
		{"agents=alpha", []string{"alpha"}},
		{"agents=alpha,beta,gamma", []string{"alpha", "beta", "gamma"}},
	}

	for _, tt := range tests {
		req := httptest.NewRequest(http.MethodGet, "/api/analytics/agents/compare?"+tt.query, nil)
		result := parseAgentList(req)
		if len(result) != len(tt.expected) {
			t.Errorf("parseAgentList(%q) len = %d, want %d", tt.query, len(result), len(tt.expected))
		}
	}
}

func TestParsePeriodAndDepth(t *testing.T) {
	tests := []struct {
		query  string
		period string
		depth  int
	}{
		{"", "week", 8},
		{"period=day&depth=30", "day", 30},
		{"period=month&depth=12", "month", 12},
		{"period=invalid", "week", 8},
		{"depth=abc", "week", 8},
	}

	for _, tt := range tests {
		req := httptest.NewRequest(http.MethodGet, "/api/analytics/cost/trend?"+tt.query, nil)
		period, depth := parsePeriodAndDepth(req)
		if period != tt.period {
			t.Errorf("period(%q) = %s, want %s", tt.query, period, tt.period)
		}
		if depth != tt.depth {
			t.Errorf("depth(%q) = %d, want %d", tt.query, depth, tt.depth)
		}
	}
}

func TestContentTypeHeaders(t *testing.T) {
	tests := []struct {
		format      string
		contentType string
	}{
		{"json", "application/json"},
		{"csv", "text/csv"},
	}

	for _, tt := range tests {
		ct := contentTypeForFormat(tt.format)
		if ct != tt.contentType {
			t.Errorf("contentType(%s) = %s, want %s", tt.format, ct, tt.contentType)
		}
	}
}

func TestCSVFilename(t *testing.T) {
	filename := csvFilename("agents")
	if !strings.HasPrefix(filename, "agents-") {
		t.Errorf("filename should start with query type: %s", filename)
	}
	if !strings.HasSuffix(filename, ".csv") {
		t.Errorf("filename should end with .csv: %s", filename)
	}
}
