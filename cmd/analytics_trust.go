// cmd/analytics_trust.go — dandori analytics trust subcommand (v0.12).
//
// Usage:
//
//	dandori analytics trust [--days 28] [--format table|json]
//
// Computes the Trust Index composite (Acceptance + AI-CFR + Intervention)
// over a rolling window and prints the value + band + 3 components.
// `--format json` returns the full TrustResult struct.
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var analyticsTrustCmd = &cobra.Command{
	Use:   "trust",
	Short: "Composite Trust Index (Acceptance + AI-CFR + Intervention)",
	Long: `Compute the Trust Index — a 0-100 composite KR that bands the agent's
autonomy posture into copilot (<60), co-own (60-79), or autonomous (≥80).

Formula:
  Trust = 0.40 * acceptance
        + 0.35 * (1 - ai_cfr)
        + 0.25 * (1 - clamp(intervention, 0..1))

Examples:
  dandori analytics trust
  dandori analytics trust --days 56
  dandori analytics trust --format json`,
	RunE: runAnalyticsTrust,
}

var analyticsTrustDays int

func init() {
	analyticsCmd.AddCommand(analyticsTrustCmd)
	analyticsTrustCmd.Flags().IntVar(&analyticsTrustDays, "days", 28, "Lookback window in days (default 28)")
}

func runAnalyticsTrust(_ *cobra.Command, _ []string) error {
	store, err := getLocalDB()
	if err != nil {
		return err
	}
	defer store.Close()

	res, err := store.GetTrustIndex(analyticsTrustDays)
	if err != nil {
		return fmt.Errorf("trust: %w", err)
	}

	if analyticsFormat == "json" {
		return json.NewEncoder(os.Stdout).Encode(res)
	}

	if !res.HasData {
		fmt.Printf("Trust Index: no data in last %d days.\n", res.WindowDays)
		fmt.Println("Need ≥1 task with line attribution AND ≥1 run in the window.")
		return nil
	}

	fmt.Printf("Trust Index: %d / 100   [%s]   (last %d days)\n\n",
		res.Value, res.Band, res.WindowDays)

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "COMPONENT\tVALUE\tWEIGHT")
	fmt.Fprintln(w, "---------\t-----\t------")
	fmt.Fprintf(w, "Code Acceptance\t%.1f%%\t40%%\n", res.Components.Acceptance*100)
	fmt.Fprintf(w, "AI CFR (proxy)\t%.1f%%\t35%%\n", res.Components.AICFR*100)
	fmt.Fprintf(w, "Human Intervention\t%.2f / run\t25%%\n", res.Components.InterventionRate)
	if err := w.Flush(); err != nil {
		return err
	}

	fmt.Println()
	switch res.Band {
	case "autonomous":
		fmt.Println("Posture: agent owns complex features; human reviews at PR stage.")
	case "co-own":
		fmt.Println("Posture: pair design review; human validates approach before agent runs.")
	case "copilot":
		fmt.Println("Posture: human leads; agent assists.")
	}
	return nil
}
