package db

import (
	"database/sql"
	"fmt"
	"time"
)

// MetricSnapshot caches a single DORA metric export result so re-export is
// fast and historical metric values are auditable.
type MetricSnapshot struct {
	ID          string    `json:"id"`
	Team        string    `json:"team,omitempty"`
	Format      string    `json:"format"`
	WindowStart time.Time `json:"window_start"`
	WindowEnd   time.Time `json:"window_end"`
	Payload     string    `json:"payload"`
	CreatedAt   time.Time `json:"created_at"`
}

func (l *LocalDB) InsertSnapshot(s MetricSnapshot) error {
	_, err := l.Exec(`
		INSERT INTO metric_snapshots
			(id, team, format, window_start, window_end, payload)
		VALUES (?, ?, ?, ?, ?, ?)
	`,
		s.ID,
		nullableString(s.Team),
		s.Format,
		s.WindowStart.UTC().Format(time.RFC3339),
		s.WindowEnd.UTC().Format(time.RFC3339),
		s.Payload,
	)
	if err != nil {
		return fmt.Errorf("insert metric snapshot: %w", err)
	}
	return nil
}

// LatestSnapshot returns the most recent snapshot matching team+format.
// Pass team="" to match snapshots that have no team filter.
func (l *LocalDB) LatestSnapshot(team, format string) (*MetricSnapshot, error) {
	row := l.QueryRow(`
		SELECT id, COALESCE(team, ''), format, window_start, window_end, payload, created_at
		FROM metric_snapshots
		WHERE COALESCE(team, '') = ? AND format = ?
		ORDER BY created_at DESC
		LIMIT 1
	`, team, format)

	s, err := scanSnapshot(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query latest snapshot: %w", err)
	}
	return s, nil
}

// ListSnapshots returns snapshots filtered by team+format (both optional).
// Pass empty strings to skip the corresponding filter.
func (l *LocalDB) ListSnapshots(team, format string, limit int) ([]MetricSnapshot, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := l.Query(`
		SELECT id, COALESCE(team, ''), format, window_start, window_end, payload, created_at
		FROM metric_snapshots
		WHERE (? = '' OR COALESCE(team, '') = ?)
		  AND (? = '' OR format = ?)
		ORDER BY created_at DESC
		LIMIT ?
	`, team, team, format, format, limit)
	if err != nil {
		return nil, fmt.Errorf("list snapshots: %w", err)
	}
	defer rows.Close()

	var out []MetricSnapshot
	for rows.Next() {
		s, err := scanSnapshot(rows)
		if err != nil {
			return nil, fmt.Errorf("scan snapshot: %w", err)
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanSnapshot(r rowScanner) (*MetricSnapshot, error) {
	var (
		s           MetricSnapshot
		windowStart string
		windowEnd   string
		createdAt   string
	)
	if err := r.Scan(&s.ID, &s.Team, &s.Format, &windowStart, &windowEnd, &s.Payload, &createdAt); err != nil {
		return nil, err
	}
	var err error
	if s.WindowStart, err = parseTime(windowStart); err != nil {
		return nil, fmt.Errorf("parse window_start: %w", err)
	}
	if s.WindowEnd, err = parseTime(windowEnd); err != nil {
		return nil, fmt.Errorf("parse window_end: %w", err)
	}
	if s.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, fmt.Errorf("parse created_at: %w", err)
	}
	return &s, nil
}

func parseTime(v string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, nil
	}
	return time.Parse("2006-01-02 15:04:05", v)
}

func nullableString(v string) any {
	if v == "" {
		return nil
	}
	return v
}
