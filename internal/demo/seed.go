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

const (
	seedTag      = "blog-v1"
	seedTagCross = "cross-v1"
)

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

// SeedCrossProject inserts deterministic rows spanning 3 Jira projects
// (CLITEST1/2/3) × 3 sprints × 4 engineers, so the dashboard's project /
// sprint / engineer filters all have data to slice. Idempotent via
// seedTagCross stored in runs.command.
//
// Layout: 3 projects × 3 sprints × ~4 runs per sprint = 36 runs over 6 weeks.
func SeedCrossProject(d *db.LocalDB) error {
	var existing int
	if err := d.QueryRow(`SELECT COUNT(*) FROM runs WHERE command = ?`, seedTagCross).Scan(&existing); err != nil {
		return fmt.Errorf("check existing cross seed: %w", err)
	}
	if existing > 0 {
		return nil
	}

	type proj struct {
		key, dept, remote string
	}
	projects := []proj{
		{"CLITEST1", "Platform", "github.com/acme/platform"},
		{"CLITEST2", "Growth", "github.com/acme/growth"},
		{"CLITEST3", "Quality", "github.com/acme/quality"},
	}
	engineers := []struct{ name, agent string }{
		{"Alice", "alpha"},
		{"Bob", "beta"},
		{"Carol", "gamma"},
		{"Dan", "alpha"},
	}
	sprints := []string{"S1", "S2", "S3"}
	start := time.Now().Add(-42 * 24 * time.Hour) // 6 weeks ago

	tx, err := d.DB().Begin()
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	row := 0
	for pIdx, p := range projects {
		for sIdx, sprint := range sprints {
			// Each (project, sprint) gets 4 runs distributed over 14 days.
			// Sprint window: weeks (2*sIdx, 2*sIdx+2) since start.
			sprintStart := start.Add(time.Duration(sIdx*14*24) * time.Hour)
			for rIdx := 0; rIdx < 4; rIdx++ {
				eng := engineers[(pIdx+rIdx)%len(engineers)]
				issueKey := fmt.Sprintf("%s-%d", p.key, sIdx*10+rIdx+1)
				sprintID := fmt.Sprintf("%s-%s", p.key, sprint)
				startedAt := sprintStart.Add(time.Duration(rIdx*84+pIdx*4) * time.Hour)
				durationSec := 1200.0 + float64(rIdx)*180.0
				endedAt := startedAt.Add(time.Duration(durationSec) * time.Second)
				cost := 0.4 + 0.18*float64(rIdx) + 0.05*float64(pIdx)
				inTok := 8000 + rIdx*600
				outTok := 1800 + rIdx*120

				id := fmt.Sprintf("cross-%s-%s-%02d", p.key, sprint, rIdx+1)
				_, err := tx.Exec(`
					INSERT INTO runs (
						id, jira_issue_key, jira_sprint_id,
						agent_name, agent_type, user, workstation_id,
						git_remote, command,
						started_at, ended_at, duration_sec, exit_code, status,
						input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
						model, cost_usd,
						engineer_name, department
					) VALUES (?, ?, ?, ?, 'claude_code', 'seed-cross', 'ws-seed-cross',
					          ?, ?, ?, ?, ?, 0, 'done',
					          ?, ?, 4000, 1500,
					          'claude-sonnet-4-6', ?,
					          ?, ?)
				`,
					id, issueKey, sprintID,
					eng.agent,
					p.remote, seedTagCross,
					startedAt.Format(time.RFC3339), endedAt.Format(time.RFC3339), durationSec,
					inTok, outTok,
					cost,
					eng.name, p.dept,
				)
				if err != nil {
					return fmt.Errorf("insert cross run %s: %w", id, err)
				}

				// 3 of every 4 runs improve quality; the 4th regresses (lint+test deltas zero).
				lintDelta, testsDelta, quality, commitQ := -1, 8, 0.88, 0.85
				if rIdx == 3 {
					lintDelta, testsDelta, quality, commitQ = 1, -2, 0.45, 0.55
				}
				_, err = tx.Exec(`
					INSERT INTO quality_metrics (
						run_id, lint_errors_before, lint_errors_after,
						lint_delta, tests_delta, commit_count, commit_msg_quality, quality_score
					) VALUES (?, 0, 0, ?, ?, 1, ?, ?)
				`, id, lintDelta, testsDelta, commitQ, quality)
				if err != nil {
					return fmt.Errorf("insert cross quality %s: %w", id, err)
				}
				row++
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit cross: %w", err)
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
