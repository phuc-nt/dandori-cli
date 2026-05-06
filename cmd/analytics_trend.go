// cmd/analytics_trend.go — dandori analytics trend subcommand (v0.11 Phase 03).
//
// Usage:
//
//	dandori analytics trend --metric <name> [--window 7d] [--since 90d] [--format table|json]
//
// Metrics: success-rate | cost | rework-rate
// Prints a Unicode block sparkline + slope label in table mode.
// `--format json` returns []TrendPoint.
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/phuc-nt/dandori-cli/internal/db"
	"github.com/spf13/cobra"
)

var analyticsTrendCmd = &cobra.Command{
	Use:   "trend",
	Short: "Week-over-week trend sparkline for key metrics",
	Long: `Show week-over-week trend for a metric with slope direction label.

Metrics:
  success-rate   % of runs that exited with code 0
  cost           average cost per run (USD)
  rework-rate    % of runs whose task had any rework event

Examples:
  dandori analytics trend --metric success-rate
  dandori analytics trend --metric cost --since 90d
  dandori analytics trend --metric rework-rate --window 7d --since 90d --format json`,
	RunE: runAnalyticsTrend,
}

var (
	analyticsTrendMetric string
	analyticsTrendWindow int
	analyticsTrendSince  int
)

func init() {
	analyticsCmd.AddCommand(analyticsTrendCmd)
	analyticsTrendCmd.Flags().StringVar(&analyticsTrendMetric, "metric", "success-rate", "Metric: success-rate, cost, rework-rate")
	analyticsTrendCmd.Flags().IntVar(&analyticsTrendWindow, "window", 7, "Bucket width in days (default 7)")
	analyticsTrendCmd.Flags().IntVar(&analyticsTrendSince, "since", 90, "Lookback in days (default 90)")
	analyticsTrendCmd.Flags().StringVar(&analyticsFormat, "format", "table", "Output format: table, json")
}

func runAnalyticsTrend(_ *cobra.Command, _ []string) error {
	store, err := getLocalDB()
	if err != nil {
		return err
	}
	defer store.Close()

	since := time.Now().AddDate(0, 0, -analyticsTrendSince)
	pts, err := store.GetTrend(analyticsTrendMetric, since, analyticsTrendWindow)
	if err != nil {
		return fmt.Errorf("trend: %w", err)
	}

	if analyticsFormat == "json" {
		return json.NewEncoder(os.Stdout).Encode(pts)
	}

	// Count data points for reporting.
	dataPts := 0
	for _, p := range pts {
		if p.HasData {
			dataPts++
		}
	}

	if dataPts == 0 {
		fmt.Printf("No %s data in last %d days. Use 'dandori run' to record runs.\n",
			analyticsTrendMetric, analyticsTrendSince)
		return nil
	}

	slope := db.Slope(pts)
	label := db.SlopeLabel(slope, dataPts)
	unit := metricUnit(analyticsTrendMetric)

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "Trend: %s (last %d days, %d-day buckets)\n\n", analyticsTrendMetric, analyticsTrendSince, analyticsTrendWindow)
	fmt.Fprintf(w, "WEEK\t%s\tRUNS\n", unit)
	fmt.Fprintf(w, "----\t%s\t----\n", dashes(len(unit)))
	for _, p := range pts {
		if !p.HasData {
			fmt.Fprintf(w, "%s\t—\t—\n", p.WeekStart)
			continue
		}
		bar := sparkBar(p.Value, pts)
		fmt.Fprintf(w, "%s\t%s %.1f\t%d\n", p.WeekStart, bar, p.Value, p.RunCount)
	}
	if err := w.Flush(); err != nil {
		return err
	}

	fmt.Printf("\nSlope: %s  (%d data points)\n", label, dataPts)
	return nil
}

// sparkBar returns a single Unicode block character representing the relative
// value of v within the non-nil points of pts (▁▂▃▄▅▆▇█).
func sparkBar(v float64, pts []db.TrendPoint) string {
	blocks := []string{"▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"}
	minV, maxV := v, v
	for _, p := range pts {
		if !p.HasData {
			continue
		}
		if p.Value < minV {
			minV = p.Value
		}
		if p.Value > maxV {
			maxV = p.Value
		}
	}
	if maxV == minV {
		return blocks[3] // mid-block when all equal
	}
	idx := int((v-minV)/(maxV-minV)*float64(len(blocks)-1) + 0.5)
	if idx < 0 {
		idx = 0
	}
	if idx >= len(blocks) {
		idx = len(blocks) - 1
	}
	return blocks[idx]
}

// metricUnit returns the column header label for the metric value column.
func metricUnit(metric string) string {
	switch metric {
	case "cost":
		return "AVG COST/RUN"
	case "rework-rate":
		return "REWORK RATE"
	default:
		return "SUCCESS RATE"
	}
}

func dashes(n int) string {
	if n <= 0 {
		n = 4
	}
	b := make([]byte, n)
	for i := range b {
		b[i] = '-'
	}
	return string(b)
}
