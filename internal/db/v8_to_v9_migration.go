package db

import (
	"encoding/json"
	"fmt"
)

// MigrationV8ToV9 is data-only — bumps schema_version. The actual key
// rewrite for task_attribution.session_outcomes runs in Go (MigrateV8ToV9Data)
// because SQLite cannot remap JSON keys cleanly via SQL.
const MigrationV8ToV9 = `
INSERT OR REPLACE INTO schema_version (version) VALUES (9);
`

// MigrateV8ToV9Data rewrites task_attribution.session_outcomes JSON so its
// keys are RunOutcomeReason enum values instead of free-text reasons or the
// wrapper's literal session_end_reason values.
//
// The JSON shape today is one of:
//
//   - map[string]int            — written by attribution.upsertAttribution.
//   - []map[string]any (with    — written ad-hoc by the Phase 04 dashboard
//     "reason" + "outcome")       demo seed; only the .reason field matters.
//
// Both are accepted; output is always map[RunOutcomeReason]int. Idempotent
// because Migrate() gates on schema_version (will not re-run after success).
// Per-row errors are logged-by-skip — preserve raw data over halting.
func MigrateV8ToV9Data(d *LocalDB) error {
	rows, err := d.Query(`SELECT jira_issue_key, COALESCE(session_outcomes, '') FROM task_attribution`)
	if err != nil {
		return fmt.Errorf("scan task_attribution: %w", err)
	}

	type rewrite struct {
		key string
		out string
	}
	var pending []rewrite
	for rows.Next() {
		var key, raw string
		if err := rows.Scan(&key, &raw); err != nil {
			rows.Close()
			return fmt.Errorf("scan row: %w", err)
		}
		out, ok := remapOutcomes(raw)
		if !ok {
			continue
		}
		pending = append(pending, rewrite{key: key, out: out})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("rows: %w", err)
	}

	for _, r := range pending {
		if _, err := d.Exec(`UPDATE task_attribution SET session_outcomes = ? WHERE jira_issue_key = ?`, r.out, r.key); err != nil {
			return fmt.Errorf("rewrite %s: %w", r.key, err)
		}
	}
	return nil
}

// remapOutcomes accepts either JSON shape, returns the canonical
// map[RunOutcomeReason]int rewritten as a JSON string. Returns (_, false)
// when the row should be left alone (empty / malformed / no signal).
func remapOutcomes(raw string) (string, bool) {
	if raw == "" {
		return "", false
	}

	out := map[RunOutcomeReason]int{}

	// Shape A: map[string]int (production wrapper output).
	var asMap map[string]int
	if err := json.Unmarshal([]byte(raw), &asMap); err == nil && asMap != nil {
		for k, v := range asMap {
			out[LegacyBucketReworkReason(k)] += v
		}
		return marshalOutcomes(out)
	}

	// Shape B: []map[string]any with a "reason" field (Phase 04 seed).
	var asArr []map[string]any
	if err := json.Unmarshal([]byte(raw), &asArr); err == nil {
		for _, item := range asArr {
			reason, _ := item["reason"].(string)
			out[LegacyBucketReworkReason(reason)]++
		}
		return marshalOutcomes(out)
	}

	return "", false
}

func marshalOutcomes(m map[RunOutcomeReason]int) (string, bool) {
	if len(m) == 0 {
		return "", false
	}
	// Re-key to map[string]int so the JSON wire format stays plain strings.
	plain := make(map[string]int, len(m))
	for k, v := range m {
		plain[string(k)] = v
	}
	b, err := json.Marshal(plain)
	if err != nil {
		return "", false
	}
	return string(b), true
}
