package analytics

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/phuc-nt/dandori-cli/internal/db"
)

// QualityKPIBlock holds the three quality KPI slices for the snapshot.
// Only agent dimension is used in the snapshot (keeps output scannable).
type QualityKPIBlock struct {
	Regression []db.RegressionRow `json:"regression"`
	Bugs       []db.BugRateRow    `json:"bugs"`
	Cost       []db.TaskCostRow   `json:"cost"`
}

// Snapshot is the 4-block output of `analytics all`.
type Snapshot struct {
	WindowLabel    string              `json:"window"`
	GeneratedAt    time.Time           `json:"generated_at"`
	CostByProject  []db.LocalCostGroup `json:"cost_by_project"`
	Leaderboard    []db.MixRow         `json:"leaderboard"`
	QualityByAgent []db.QualityStats   `json:"quality_by_agent"`
	Alerts         []Alert             `json:"alerts"`
	QualityKPI     QualityKPIBlock     `json:"quality_kpi"`
}

// Window controls the time range. Zero Since → 30 days.
type Window struct {
	Since time.Duration
}

func (w Window) days() int {
	if w.Since <= 0 {
		return 30
	}
	return int(w.Since / (24 * time.Hour))
}

func (w Window) label() string {
	d := w.days()
	if d <= 0 {
		d = 30
	}
	return fmt.Sprintf("last %dd", d)
}

// BuildSnapshot aggregates the 4 blocks. DB errors propagate; missing
// data yields empty slices (not nil) where reasonable.
func BuildSnapshot(local *db.LocalDB, w Window, th Thresholds) (*Snapshot, error) {
	if th == (Thresholds{}) {
		th = DefaultThresholds()
	}

	cost, err := local.GetCostByEngineer()
	if err != nil {
		return nil, fmt.Errorf("cost by engineer: %w", err)
	}

	board, err := local.GetMixLeaderboard(w.days())
	if err != nil {
		return nil, fmt.Errorf("mix leaderboard: %w", err)
	}

	quality, err := local.GetQualityStatsByAgent()
	if err != nil {
		return nil, fmt.Errorf("quality stats: %w", err)
	}

	var stats []RunStat
	for _, b := range board {
		stats = append(stats, RunStat{
			Engineer: b.Engineer,
			Agent:    b.Agent,
			Cost:     b.TotalCost,
		})
	}

	// Quality KPI block — agent dimension only, top 10 to keep snapshot scannable.
	// Errors are non-fatal: missing events simply yield empty slices.
	var kpiBlock QualityKPIBlock
	if rr, err := local.RegressionRate("agent", w.days()); err == nil {
		kpiBlock.Regression = rr
	}
	if br, err := local.BugRate("agent", w.days()); err == nil {
		kpiBlock.Bugs = br
	}
	if tc, err := local.QualityAdjustedCost("agent", w.days(), 10); err == nil {
		kpiBlock.Cost = tc
	}

	return &Snapshot{
		WindowLabel:    w.label(),
		GeneratedAt:    time.Now().UTC(),
		CostByProject:  cost,
		Leaderboard:    board,
		QualityByAgent: quality,
		Alerts:         DetectAlerts(stats, th),
		QualityKPI:     kpiBlock,
	}, nil
}

// FormatTable renders a terminal-friendly 4-block summary.
func FormatTable(s *Snapshot) string {
	var b strings.Builder
	fmt.Fprintf(&b, "=== DANDORI — Sprint Snapshot (%s) ===\n\n", s.WindowLabel)

	fmt.Fprintln(&b, "[1] COST BY ENGINEER")
	if len(s.CostByProject) == 0 {
		fmt.Fprintln(&b, "  (no data)")
	} else {
		var max float64
		for _, g := range s.CostByProject {
			if g.Cost > max {
				max = g.Cost
			}
		}
		for _, g := range s.CostByProject {
			bar := ""
			if max > 0 {
				n := int(g.Cost / max * 10)
				bar = strings.Repeat("█", n)
			}
			fmt.Fprintf(&b, "  %-18s $%7.2f  %s\n", g.Group, g.Cost, bar)
		}
	}
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "[2] HUMAN + AGENT LEADERBOARD")
	if len(s.Leaderboard) == 0 {
		fmt.Fprintln(&b, "  (no data)")
	} else {
		fmt.Fprintf(&b, "  %-10s %-8s %5s %10s\n", "ENGINEER", "AGENT", "RUNS", "COST")
		for _, r := range s.Leaderboard {
			agent := r.Agent
			if agent == "" {
				agent = "(human)"
			}
			fmt.Fprintf(&b, "  %-10s %-8s %5d %9.2f\n", r.Engineer, agent, r.RunCount, r.TotalCost)
		}
	}
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "[3] QUALITY GATES")
	if len(s.QualityByAgent) == 0 {
		fmt.Fprintln(&b, "  (no data)")
	} else {
		fmt.Fprintf(&b, "  %-10s %10s %10s %10s\n", "AGENT", "ΔLINT", "ΔTEST", "IMPROVED%")
		for _, q := range s.QualityByAgent {
			fmt.Fprintf(&b, "  %-10s %+10.1f %+10.1f %9.0f%%\n",
				q.AgentName, q.AvgLintDelta, q.AvgTestsDelta, q.ImprovedPercent)
		}
	}
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "[4] ALERTS")
	if len(s.Alerts) == 0 {
		fmt.Fprintln(&b, "  (none)")
	} else {
		for _, a := range s.Alerts {
			fmt.Fprintf(&b, "  ⚠ %s\n", a.Message)
		}
	}
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "[5] QUALITY KPI (by agent)")
	kpi := s.QualityKPI
	if len(kpi.Regression) == 0 && len(kpi.Bugs) == 0 && len(kpi.Cost) == 0 {
		fmt.Fprintln(&b, "  (no quality KPI data yet)")
	} else {
		if len(kpi.Regression) > 0 {
			fmt.Fprintln(&b, "  Regression rate:")
			fmt.Fprintf(&b, "    %-18s %6s %10s %12s\n", "AGENT", "TASKS", "REGRESSED", "REGRESSION%")
			for _, r := range kpi.Regression {
				key := r.GroupKey
				if key == "" {
					key = "(none)"
				}
				fmt.Fprintf(&b, "    %-18s %6d %10d %11.1f%%\n", key, r.TotalTasks, r.RegressedTasks, r.RegressionPct)
			}
		}
		if len(kpi.Bugs) > 0 {
			fmt.Fprintln(&b, "  Bug rate:")
			fmt.Fprintf(&b, "    %-18s %6s %6s %10s\n", "AGENT", "RUNS", "BUGS", "BUGS/RUN")
			for _, r := range kpi.Bugs {
				key := r.GroupKey
				if key == "" {
					key = "(none)"
				}
				fmt.Fprintf(&b, "    %-18s %6d %6d %10.2f\n", key, r.Runs, r.Bugs, r.BugsPerRun)
			}
		}
		if len(kpi.Cost) > 0 {
			fmt.Fprintln(&b, "  Top cost tasks:")
			fmt.Fprintf(&b, "    %-14s %-18s %8s %5s %10s %5s %6s\n", "TASK", "AGENT", "COST", "RUNS", "ITERATIONS", "BUGS", "CLEAN")
			for _, r := range kpi.Cost {
				key := r.GroupKey
				if key == "" {
					key = "(none)"
				}
				clean := "no"
				if r.IsClean {
					clean = "yes"
				}
				fmt.Fprintf(&b, "    %-14s %-18s %8.4f %5d %10d %5d %6s\n",
					r.IssueKey, key, r.TotalCostUSD, r.RunCount, r.IterationCount, r.BugCount, clean)
			}
		}
	}

	return b.String()
}

// FormatJSON returns indented JSON.
func FormatJSON(s *Snapshot) string {
	out, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(out)
}
