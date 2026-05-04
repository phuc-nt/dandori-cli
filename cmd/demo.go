package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/phuc-nt/dandori-cli/internal/config"
	"github.com/phuc-nt/dandori-cli/internal/db"
	"github.com/phuc-nt/dandori-cli/internal/demo"
	"github.com/spf13/cobra"
)

var (
	demoReset   bool
	demoSeed    bool
	demoUse     bool
	demoRestore bool
	demoVariant string
)

var demoCmd = &cobra.Command{
	Use:   "demo",
	Short: "Manage the demo database (hackday blog scenario)",
	Long: `Tools for running the hackday demo against a separate ~/.dandori/demo.db
without polluting the real local database.

Flags can be combined, e.g.:
  dandori demo --reset --seed --use
  dandori demo --restore`,
	RunE: runDemo,
}

func init() {
	demoCmd.Flags().BoolVar(&demoReset, "reset", false, "Wipe demo.db contents")
	demoCmd.Flags().BoolVar(&demoSeed, "seed", false, "Insert blog scenario rows")
	demoCmd.Flags().BoolVar(&demoUse, "use", false, "Point dandori at demo.db for subsequent commands")
	demoCmd.Flags().BoolVar(&demoRestore, "restore", false, "Stop using demo.db (restore real DB)")
	demoCmd.Flags().StringVar(&demoVariant, "variant", "blog",
		"Seed variant: 'blog' (default, single team) or 'cross-project' (3 projects × 3 sprints)")
	rootCmd.AddCommand(demoCmd)
}

func demoDBPath() (string, error) {
	if env := os.Getenv("DANDORI_DB"); env != "" {
		return env, nil
	}
	dir, err := config.ConfigDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", dir, err)
	}
	return filepath.Join(dir, "demo.db"), nil
}

func activeDBPointerPath() (string, error) {
	dir, err := config.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "active_db"), nil
}

func runDemo(cmd *cobra.Command, args []string) error {
	if !demoReset && !demoSeed && !demoUse && !demoRestore {
		return fmt.Errorf("no action specified. use --reset, --seed, --use, or --restore")
	}

	dbPath, err := demoDBPath()
	if err != nil {
		return err
	}

	if demoRestore {
		p, err := activeDBPointerPath()
		if err != nil {
			return err
		}
		_ = os.Remove(p)
		fmt.Printf("Restored. Active DB back to default (%s).\n", mustDefaultDBPath())
		return nil
	}

	// --reset / --seed need the demo DB open + migrated.
	if demoReset || demoSeed {
		d, err := db.Open(dbPath)
		if err != nil {
			return fmt.Errorf("open demo db: %w", err)
		}
		defer d.Close()
		if err := d.Migrate(); err != nil {
			return fmt.Errorf("migrate demo db: %w", err)
		}
		if demoReset {
			if err := demo.ResetDB(d); err != nil {
				return fmt.Errorf("reset: %w", err)
			}
			fmt.Println("Demo DB reset (runs/events/quality_metrics cleared).")
		}
		if demoSeed {
			switch demoVariant {
			case "blog", "":
				if err := demo.SeedBlogScenario(d); err != nil {
					return fmt.Errorf("seed: %w", err)
				}
				fmt.Println("Seeded blog scenario: Alice+alpha (12), Bob human-only (9), Carol+beta (7).")
			case "cross-project":
				if err := demo.SeedCrossProject(d); err != nil {
					return fmt.Errorf("seed cross-project: %w", err)
				}
				fmt.Println("Seeded cross-project scenario: 3 projects (CLITEST1/2/3) × 3 sprints × 4 runs = 36 runs.")
			default:
				return fmt.Errorf("unknown --variant %q (expected 'blog' or 'cross-project')", demoVariant)
			}
		}
	}

	if demoUse {
		p, err := activeDBPointerPath()
		if err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte(dbPath), 0644); err != nil {
			return fmt.Errorf("write active_db pointer: %w", err)
		}
		fmt.Printf("Active DB now: %s\n", dbPath)
	}

	return nil
}

func mustDefaultDBPath() string {
	p, _ := config.DBPath()
	return p
}
