package db

// migrateV12ToV13 captures per-PR size on pr_events:
//
//   - additions  — lines added in the PR diff (nullable; NULL until a sync
//                  with GetPRDetail enabled has touched the row)
//   - deletions  — lines removed (same nullability rules)
//
// SQLite supports `ALTER TABLE ... ADD COLUMN` without a rewrite but has
// no `IF NOT EXISTS` clause for it — so we probe pragma_table_info first.
// This keeps the migration safe when a test fixture applied the full
// (v13) schema then rolled schema_version back to ≤ 12: the columns
// already exist, and only the version row needs bumping.
//
// Surfaced on `GET /api/metrics/pr-cycle-time` as `median_lines_changed`
// (median of additions+deletions over rows where both are non-NULL).
// Diagnostic only — not a KR, not in Trust composite (framework §2 warns
// against LOC-as-quantity targets).
func migrateV12ToV13(l *LocalDB) error {
	for _, col := range []string{"additions", "deletions"} {
		var n int
		if err := l.QueryRow(
			`SELECT COUNT(*) FROM pragma_table_info('pr_events') WHERE name = ?`,
			col,
		).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		if _, err := l.Exec(`ALTER TABLE pr_events ADD COLUMN ` + col + ` INTEGER`); err != nil {
			return err
		}
	}
	if _, err := l.Exec(`INSERT OR REPLACE INTO schema_version (version) VALUES (13)`); err != nil {
		return err
	}
	return nil
}
