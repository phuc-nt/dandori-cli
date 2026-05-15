package cmd

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/phuc-nt/dandori-cli/internal/config"
	"github.com/phuc-nt/dandori-cli/internal/db"
	"github.com/phuc-nt/dandori-cli/internal/event"
	"github.com/phuc-nt/dandori-cli/internal/model"
	"github.com/spf13/cobra"
)

// interventionReasons is the closed set accepted by --reason. Currently
// the 3 v0.14 intervention buckets — wrapper-emitted reasons (test_fail,
// lint_fail, etc.) are detected from events automatically and aren't valid
// here.
var interventionReasons = map[string]db.RunOutcomeReason{
	string(db.ReasonWrongApproach):         db.ReasonWrongApproach,
	string(db.ReasonScopeMisunderstanding): db.ReasonScopeMisunderstanding,
	string(db.ReasonMissingContext):        db.ReasonMissingContext,
}

// sortedInterventionReasons returns reason strings in deterministic order
// for error messages.
func sortedInterventionReasons() []string {
	out := make([]string, 0, len(interventionReasons))
	for k := range interventionReasons {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

var eventCmd = &cobra.Command{
	Use:   "event",
	Short: "Record a Layer 3 event from agent execution",
	Long: `Records semantic events emitted by the agent during execution.
This command is called BY the agent (via Claude Code's bash tool).

Examples:
  dandori event --run abc123 --type decision --data '{"rationale":"chose pagination"}'
  dandori event --run abc123 --type files_touched --data '{"files":["src/auth.ts"]}'`,
	RunE: runEvent,
}

var (
	eventRunID  string
	eventType   string
	eventData   string
	eventReason string
)

func init() {
	eventCmd.Flags().StringVar(&eventRunID, "run", "", "Run ID to link event to (required)")
	eventCmd.Flags().StringVar(&eventType, "type", "", "Event type (decision, file_change, task_link, custom). Omit when --reason is set.")
	eventCmd.Flags().StringVar(&eventData, "data", "{}", "JSON payload")
	eventCmd.Flags().StringVar(&eventReason, "reason", "", "Intervention reason: "+strings.Join(sortedInterventionReasons(), ", ")+". Mutually exclusive with --type.")
	eventCmd.MarkFlagRequired("run")
	rootCmd.AddCommand(eventCmd)
}

func runEvent(cmd *cobra.Command, args []string) error {
	// Resolve --reason into a canonical event_type. Validate up-front so a
	// typo doesn't silently fall through as a free-text event.
	resolvedType := eventType
	switch {
	case eventReason != "" && eventType != "":
		return fmt.Errorf("--reason and --type are mutually exclusive")
	case eventReason != "":
		canon, ok := interventionReasons[eventReason]
		if !ok {
			return fmt.Errorf("invalid --reason %q (valid: %s)",
				eventReason, strings.Join(sortedInterventionReasons(), ", "))
		}
		resolvedType = "intervention." + string(canon)
	case eventType == "":
		return fmt.Errorf("either --type or --reason is required")
	}

	dbPath, err := config.DBPath()
	if err != nil {
		return err
	}

	localDB, err := db.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer localDB.Close()

	var exists int
	err = localDB.QueryRow(`SELECT COUNT(*) FROM runs WHERE id = ?`, eventRunID).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check run: %w", err)
	}
	if exists == 0 {
		return fmt.Errorf("run %s not found", eventRunID)
	}

	var data any
	if err := json.Unmarshal([]byte(eventData), &data); err != nil {
		return fmt.Errorf("invalid JSON data: %w", err)
	}

	recorder := event.NewRecorder(localDB)
	if err := recorder.RecordEvent(eventRunID, model.LayerSkill, resolvedType, data); err != nil {
		return fmt.Errorf("record event: %w", err)
	}

	fmt.Printf("Event recorded: run=%s type=%s\n", eventRunID, resolvedType)
	return nil
}
