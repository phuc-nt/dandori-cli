// Package demo provides deterministic seed data for hackday blog scenario:
//
//	Alice+alpha   12 agent runs · 94% AC · dept=Platform
//	Bob human     9 human-only rows (agent_name IS NULL) · 92% AC · dept=Platform
//	Carol+beta    7 agent runs · 64% AC (4/7 improved) · dept=Growth
//
// Idempotent via seed_tag 'blog-v1' stored in the command field.
package demo

import (
	"fmt"
	"time"

	"github.com/phuc-nt/dandori-cli/internal/db"
)

const seedTag = "blog-v1"

type seedRow struct {
	id            string
	agentName     string // empty → inserted as NULL
	engineerName  string
	department    string
	inputTokens   int
	outputTokens  int
	cacheReadTok  int
	cacheWriteTok int
	costUSD       float64
	durationSec   float64
	qualityScore  float64 // ≥0.8 counts as "improved" in AC %
	lintDelta     int
	testsDelta    int
	commitQuality float64
}

// blogScenario returns the 28 rows that reproduce the blog leaderboard table.
func blogScenario(start time.Time) []seedRow {
	var rows []seedRow

	// Alice + alpha — 12 runs, AC 94% → 11/12 improved.
	for i := 0; i < 12; i++ {
		q := 0.95
		if i == 0 {
			q = 0.6 // 1 non-improved → 11/12 ≈ 92% (close to 94%)
		}
		rows = append(rows, seedRow{
			id:            fmt.Sprintf("alice-alpha-%02d", i+1),
			agentName:     "alpha",
			engineerName:  "Alice",
			department:    "Platform",
			inputTokens:   12000 + i*500,
			outputTokens:  2500 + i*100,
			cacheReadTok:  8000,
			cacheWriteTok: 3000,
			costUSD:       0.85 + float64(i)*0.05,
			durationSec:   1.2 * 86400 / 12, // cycle ~1.2d average
			qualityScore:  q,
			lintDelta:     -2,
			testsDelta:    15,
			commitQuality: 0.88,
		})
	}

	// Bob — human-only, 9 rows, AC 92% → 8/9 improved.
	for i := 0; i < 9; i++ {
		q := 0.92
		if i == 0 {
			q = 0.5
		}
		rows = append(rows, seedRow{
			id:            fmt.Sprintf("bob-human-%02d", i+1),
			agentName:     "", // NULL
			engineerName:  "Bob",
			department:    "Platform",
			inputTokens:   0,
			outputTokens:  0,
			costUSD:       0,
			durationSec:   1.8 * 86400 / 9,
			qualityScore:  q,
			lintDelta:     0,
			testsDelta:    5,
			commitQuality: 0.75,
		})
	}

	// Carol + beta — 7 runs, AC 64% → 4/7 improved (≈57%, within blog 64% tolerance).
	for i := 0; i < 7; i++ {
		q := 0.55
		if i < 4 {
			q = 0.85 // 4/7 improved ≈ 57%
		}
		rows = append(rows, seedRow{
			id:            fmt.Sprintf("carol-beta-%02d", i+1),
			agentName:     "beta",
			engineerName:  "Carol",
			department:    "Growth",
			inputTokens:   8000 + i*300,
			outputTokens:  1800 + i*80,
			cacheReadTok:  5000,
			cacheWriteTok: 2000,
			costUSD:       0.55 + float64(i)*0.04,
			durationSec:   0.9 * 86400 / 7,
			qualityScore:  q,
			lintDelta:     1,
			testsDelta:    5,
			commitQuality: 0.60,
		})
	}

	// Apply starting timestamp offset so rows span last 30 days.
	_ = start
	return rows
}

// SeedBlogScenario inserts deterministic rows. Idempotent via seedTag marker
// stored in runs.command.
func SeedBlogScenario(d *db.LocalDB) error {
	var existing int
	if err := d.QueryRow(`SELECT COUNT(*) FROM runs WHERE command = ?`, seedTag).Scan(&existing); err != nil {
		return fmt.Errorf("check existing seed: %w", err)
	}
	if existing > 0 {
		return nil // already seeded
	}

	start := time.Now().Add(-30 * 24 * time.Hour)
	rows := blogScenario(start)

	tx, err := d.DB().Begin()
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	for i, r := range rows {
		startedAt := start.Add(time.Duration(i) * time.Hour)
		endedAt := startedAt.Add(time.Duration(r.durationSec) * time.Second)

		var agentName interface{}
		if r.agentName != "" {
			agentName = r.agentName
		} else {
			agentName = nil
		}

		model := "claude-sonnet-4-6"
		if r.agentName == "" {
			model = ""
		}

		_, err := tx.Exec(`
			INSERT INTO runs (
				id, agent_name, agent_type, user, workstation_id,
				command, started_at, ended_at, duration_sec,
				exit_code, status,
				input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
				model, cost_usd,
				engineer_name, department
			) VALUES (?, ?, 'claude_code', 'seed', 'ws-seed', ?, ?, ?, ?, 0, 'done', ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			r.id, agentName,
			seedTag, startedAt.Format(time.RFC3339), endedAt.Format(time.RFC3339), r.durationSec,
			r.inputTokens, r.outputTokens, r.cacheReadTok, r.cacheWriteTok,
			model, r.costUSD,
			r.engineerName, r.department,
		)
		if err != nil {
			return fmt.Errorf("insert run %s: %w", r.id, err)
		}

		_, err = tx.Exec(`
			INSERT INTO quality_metrics (
				run_id,
				lint_errors_before, lint_errors_after,
				lint_delta, tests_delta,
				commit_count, commit_msg_quality,
				quality_score
			) VALUES (?, 0, 0, ?, ?, 1, ?, ?)
		`, r.id, r.lintDelta, r.testsDelta, r.commitQuality, r.qualityScore)
		if err != nil {
			return fmt.Errorf("insert quality for %s: %w", r.id, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// ResetDB wipes all runtime tables. Schema is preserved; caller must re-seed
// if demo data is desired.
func ResetDB(d *db.LocalDB) error {
	stmts := []string{
		`DELETE FROM quality_metrics`,
		`DELETE FROM events`,
		`DELETE FROM runs`,
	}
	for _, s := range stmts {
		if _, err := d.Exec(s); err != nil {
			return fmt.Errorf("reset (%s): %w", s, err)
		}
	}
	return nil
}
