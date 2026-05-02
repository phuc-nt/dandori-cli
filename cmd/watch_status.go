package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/phuc-nt/dandori-cli/internal/watchctl"
	"github.com/spf13/cobra"
)

var watchStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show dandori watch daemon status",
	Long:  `Report whether the watch daemon is installed, running, and when it last polled.`,
	RunE:  runWatchStatus,
}

func init() {
	watchCmd.AddCommand(watchStatusCmd)
}

func runWatchStatus(_ *cobra.Command, _ []string) error {
	m := watchctl.New()

	loaded, running, since, err := m.Status()
	if err != nil {
		return fmt.Errorf("daemon status: %w", err)
	}

	lastPoll := "never"
	if !since.IsZero() {
		lastPoll = since.Local().Format(time.DateTime)
	}

	if !loaded {
		fmt.Printf("● Stopped (not installed)\n")
		fmt.Printf("  enabled=false running=false last_poll=%s\n", lastPoll)
		return nil
	}

	runningStr := "false"
	indicator := "○"
	if running {
		runningStr = "true"
		indicator = "●"
	}

	fmt.Printf("%s %s\n", indicator, runningStatus(running, since))
	fmt.Printf("  enabled=true running=%s last_poll=%s\n", runningStr, lastPoll)
	return nil
}

func runningStatus(running bool, since time.Time) string {
	if running {
		if !since.IsZero() {
			return fmt.Sprintf("Running (since %s)", since.Local().Format(time.DateTime))
		}
		return "Running"
	}
	return "Stopped (installed but not running)"
}

// homeDir returns the user's home directory or empty string on error.
func homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Clean(home)
}
