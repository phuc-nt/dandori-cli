package analytics

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"
)

func TestExportCSVAgentStats(t *testing.T) {
	stats := []AgentStat{
		{AgentName: "alpha", RunCount: 10, SuccessRate: 90.0, TotalCost: 50.0, AvgCost: 5.0, AvgDuration: 120.5},
		{AgentName: "beta", RunCount: 5, SuccessRate: 80.0, TotalCost: 25.0, AvgCost: 5.0, AvgDuration: 60.0},
	}

	var buf bytes.Buffer
	err := ExportCSV(stats, &buf)
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}

	reader := csv.NewReader(strings.NewReader(buf.String()))
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("csv parse failed: %v", err)
	}

	// Header + 2 data rows
	if len(records) != 3 {
		t.Errorf("expected 3 rows (header + 2 data), got %d", len(records))
	}

	// Check header
	header := records[0]
	if header[0] != "agent_name" {
		t.Errorf("first header = %s, want agent_name", header[0])
	}

	// Check data
	if records[1][0] != "alpha" {
		t.Errorf("first row agent = %s, want alpha", records[1][0])
	}
}

func TestExportCSVCostGroups(t *testing.T) {
	groups := []CostGroup{
		{Group: "alpha", Cost: 100.50, RunCount: 10, Tokens: 5000},
		{Group: "beta", Cost: 50.25, RunCount: 5, Tokens: 2500},
	}

	var buf bytes.Buffer
	err := ExportCSV(groups, &buf)
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}

	if !strings.Contains(buf.String(), "100.5") {
		t.Error("CSV should contain cost value")
	}
	if !strings.Contains(buf.String(), "group") {
		t.Error("CSV should contain 'group' header")
	}
}

func TestExportCSVEmpty(t *testing.T) {
	var stats []AgentStat

	var buf bytes.Buffer
	err := ExportCSV(stats, &buf)
	if err != nil {
		t.Fatalf("export empty should not fail: %v", err)
	}

	// Should have header only or be empty
	if buf.Len() > 0 && !strings.Contains(buf.String(), "agent_name") {
		t.Error("empty export should have header or be empty")
	}
}

func TestExportJSONAgentStats(t *testing.T) {
	stats := []AgentStat{
		{AgentName: "alpha", RunCount: 10, SuccessRate: 90.0},
	}

	var buf bytes.Buffer
	err := ExportJSON(stats, &buf)
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}

	var result []AgentStat
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("json unmarshal failed: %v", err)
	}

	if len(result) != 1 {
		t.Errorf("expected 1 stat, got %d", len(result))
	}
	if result[0].AgentName != "alpha" {
		t.Errorf("agent = %s, want alpha", result[0].AgentName)
	}
}

func TestExportJSONEmpty(t *testing.T) {
	var stats []AgentStat

	var buf bytes.Buffer
	err := ExportJSON(stats, &buf)
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}

	// Should be valid JSON (empty array or null)
	var result []AgentStat
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("empty export should be valid json: %v", err)
	}
}

func TestExportJSONCostGroups(t *testing.T) {
	groups := []CostGroup{
		{Group: "sprint-1", Cost: 100.0, RunCount: 5, Tokens: 10000},
	}

	var buf bytes.Buffer
	err := ExportJSON(groups, &buf)
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}

	var result []CostGroup
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("json unmarshal failed: %v", err)
	}

	if result[0].Cost != 100.0 {
		t.Errorf("cost = %f, want 100.0", result[0].Cost)
	}
}

func TestExportCSVSpecialChars(t *testing.T) {
	stats := []AgentStat{
		{AgentName: "alpha,beta", RunCount: 1},  // comma in name
		{AgentName: "test\"quote", RunCount: 2}, // quote in name
	}

	var buf bytes.Buffer
	err := ExportCSV(stats, &buf)
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}

	// Should be parseable
	reader := csv.NewReader(strings.NewReader(buf.String()))
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("csv with special chars should be parseable: %v", err)
	}
	if len(records) != 3 {
		t.Errorf("expected 3 rows, got %d", len(records))
	}
}
