package cmd

import (
	"errors"
	"fmt"

	"github.com/phuc-nt/dandori-cli/internal/watchctl"
	"github.com/spf13/cobra"
)

var watchEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Install and start the dandori watch background daemon",
	Long: `Write a launchd plist (macOS) or systemd user unit (Linux) and load it so
dandori watch runs in the background and auto-starts on login.

The daemon logs to ~/.dandori/logs/watch.{out,err}.log.`,
	RunE: runWatchEnable,
}

func init() {
	watchCmd.AddCommand(watchEnableCmd)
}

func runWatchEnable(_ *cobra.Command, _ []string) error {
	m := watchctl.New()

	if err := m.Enable(); err != nil {
		if errors.Is(err, watchctl.ErrAlreadyEnabled) {
			fmt.Println("watch daemon is already enabled")
			return nil
		}
		return fmt.Errorf("enable daemon: %w", err)
	}

	path, _ := m.Path()
	home := homeDir()
	fmt.Println("✓ Background watch daemon enabled (auto-start on login)")
	if path != "" {
		fmt.Printf("  Service file: %s\n", path)
	}
	fmt.Printf("  Logs:         %s/.dandori/logs/watch.out.log\n", home)
	return nil
}
