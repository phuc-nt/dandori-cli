package cmd

import (
	"fmt"

	"github.com/phuc-nt/dandori-cli/internal/watchctl"
	"github.com/spf13/cobra"
)

var watchDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Stop and remove the dandori watch background daemon",
	Long: `Stop the running dandori watch daemon, unload its service definition, and
remove the plist / unit file. Idempotent — safe to run even if not currently enabled.`,
	RunE: runWatchDisable,
}

func init() {
	watchCmd.AddCommand(watchDisableCmd)
}

func runWatchDisable(_ *cobra.Command, _ []string) error {
	m := watchctl.New()

	if err := m.Disable(); err != nil {
		return fmt.Errorf("disable daemon: %w", err)
	}

	fmt.Println("✓ Daemon disabled")
	return nil
}
