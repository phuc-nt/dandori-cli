package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// cleanMinAge is how old a go-build* dir must be before we'll touch it.
// 60 minutes guards in-flight `go test` / `go build` runs from having their
// scratch dirs ripped out from underneath them.
const cleanMinAge = 60 * time.Minute

var (
	cleanForce bool
)

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Sweep stale go-build* scratch dirs from $TMPDIR",
	Long: `Removes go-build* directories from $TMPDIR that are older than 60 minutes.

These are Go toolchain scratch directories that should auto-delete on ` + "`go`" + `
command exit but survive when the process is killed before reaching its cleanup
path (e.g., SIGKILL on timeout). Older versions of dandori-cli could leave tens
of thousands of these behind, consuming hundreds of GB.

By default this runs in dry-run mode and only reports what it would delete.
Pass --force to actually remove them.

GOCACHE (long-lived build cache, default ~/Library/Caches/go-build on macOS) is
intentionally NOT touched — that's a valuable cache, not a leak.`,
	RunE: runClean,
}

func init() {
	cleanCmd.Flags().BoolVar(&cleanForce, "force", false, "Actually delete; without this, only reports what would be deleted")
	rootCmd.AddCommand(cleanCmd)
}

func runClean(cmd *cobra.Command, args []string) error {
	tmpDir := os.TempDir()
	cutoff := time.Now().Add(-cleanMinAge)

	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return fmt.Errorf("read tmpdir %s: %w", tmpDir, err)
	}

	var (
		matched      int
		eligible     int
		skippedYoung int
		skippedErr   int
		totalBytes   int64
		deletedDirs  int
	)

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if !strings.HasPrefix(e.Name(), "go-build") {
			continue
		}
		matched++

		full := filepath.Join(tmpDir, e.Name())
		info, err := e.Info()
		if err != nil {
			skippedErr++
			continue
		}
		if info.ModTime().After(cutoff) {
			skippedYoung++
			continue
		}

		size, err := dirSize(full)
		if err != nil {
			// Couldn't size it; still allow deletion if --force, just don't tally.
			skippedErr++
		}
		totalBytes += size
		eligible++

		if cleanForce {
			if err := os.RemoveAll(full); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "warn: remove %s: %v\n", full, err)
				continue
			}
			deletedDirs++
		}
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Scanned %s\n", tmpDir)
	fmt.Fprintf(out, "  go-build* dirs found:        %d\n", matched)
	fmt.Fprintf(out, "  eligible (older than 60m):   %d\n", eligible)
	fmt.Fprintf(out, "  skipped (in-flight, <60m):   %d\n", skippedYoung)
	if skippedErr > 0 {
		fmt.Fprintf(out, "  skipped (stat/size errors):  %d\n", skippedErr)
	}
	fmt.Fprintf(out, "  total reclaimable size:      %s\n", humanBytes(totalBytes))
	if cleanForce {
		fmt.Fprintf(out, "Deleted %d / %d eligible dirs.\n", deletedDirs, eligible)
	} else {
		fmt.Fprintln(out, "(dry run — pass --force to actually delete)")
	}
	return nil
}

// dirSize sums file sizes under root. Permission errors on individual entries
// are swallowed so a single unreadable subdir doesn't fail the whole sweep.
func dirSize(root string) (int64, error) {
	var total int64
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrPermission) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			if errors.Is(err, fs.ErrPermission) || errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		total += info.Size()
		return nil
	})
	return total, walkErr
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	suffix := []string{"KiB", "MiB", "GiB", "TiB"}[exp]
	return fmt.Sprintf("%.1f %s", float64(n)/float64(div), suffix)
}
