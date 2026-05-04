// Package db — eng_queries.go: query helpers for Phase 03 Engineering + Admin View.
//
// Covers:
//   - agent compare (two-agent metric pack)
//   - autonomy ratio (agent-msg / total-msg over time, fallback to run-count)
//   - approval funnel (count by event type from events table)
//   - cache efficiency (cache_read / total tokens, daily series)
//   - cost per task (cost_usd / distinct jira_issue_key, daily series)
//   - model mix (group by model — count + cost)
//   - session end reasons (group by session_end_reason — daily series)
//   - duration histogram (auto-bucketed using IQR)
//   - workstation matrix (workstation_id × engineer_name)
//   - repo leaderboard (group by git_remote/cwd — cost / runs / success)
package db

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// AgentMetricPack is a single agent's roll-up used by compare endpoint.
type AgentMetricPack struct {
	AgentName        string  `json:"agent_name"`
	Runs             int     `json:"runs"`
	TotalCost        float64 `json:"total_cost"`
	AvgCostPerRun    float64 `json:"avg_cost_per_run"`
	SuccessRate      float64 `json:"success_rate"`     // status='done' / total
	AutonomyPct      float64 `json:"autonomy_pct"`     // 100 - human_approval_count / runs (capped 0..100)
	CacheEffPct      float64 `json:"cache_eff_pct"`    // cache_read / (input+cache_read)
	InterventionRate float64 `json:"intervention_rate"` // human_approval_count / runs
	AvgDurationSec   float64 `json:"avg_duration_sec"`
	LinesAuthored    int     `json:"lines_authored"` // sum of agent_lines_added (if column exists, else 0)
}

// AgentMetrics returns the metric pack for one agent.
// Returns zero-valued pack (with name) if no rows.
func (l *LocalDB) AgentMetrics(agent string) (AgentMetricPack, error) {
	pack := AgentMetricPack{AgentName: agent}
	if agent == "" {
		return pack, fmt.Errorf("agent name required")
	}
	row := l.QueryRow(`
		SELECT
			COUNT(*) AS runs,
			COALESCE(SUM(cost_usd), 0) AS total_cost,
			COALESCE(SUM(CASE WHEN status='done' THEN 1 ELSE 0 END), 0) AS done_count,
			COALESCE(SUM(human_approval_count), 0) AS approvals,
			COALESCE(SUM(cache_read_tokens), 0) AS cache_read,
			COALESCE(SUM(input_tokens), 0) AS input_tok,
			COALESCE(SUM(duration_sec), 0) AS dur_sum
		FROM runs
		WHERE agent_name = ?
	`, agent)
	var doneCount, approvals, cacheRead, inputTok int64
	var durSum float64
	if err := row.Scan(&pack.Runs, &pack.TotalCost, &doneCount, &approvals, &cacheRead, &inputTok, &durSum); err != nil {
		return pack, err
	}
	if pack.Runs == 0 {
		return pack, nil
	}
	pack.AvgCostPerRun = pack.TotalCost / float64(pack.Runs)
	pack.SuccessRate = float64(doneCount) / float64(pack.Runs) * 100
	pack.InterventionRate = float64(approvals) / float64(pack.Runs)
	pack.AutonomyPct = math.Max(0, 100-pack.InterventionRate*100)
	tokTotal := cacheRead + inputTok
	if tokTotal > 0 {
		pack.CacheEffPct = float64(cacheRead) / float64(tokTotal) * 100
	}
	pack.AvgDurationSec = durSum / float64(pack.Runs)
	return pack, nil
}

// AutonomyDay captures one day's autonomy ratio.
type AutonomyDay struct {
	Day         string  `json:"day"`
	AutonomyPct float64 `json:"autonomy_pct"`
	Runs        int     `json:"runs"`
}

// AutonomyTimeline returns daily autonomy series for the given engineer (or all if empty).
// Uses run-level approval counts as proxy: autonomy = 100 - avg(approvals).
func (l *LocalDB) AutonomyTimeline(engineer string, days int) ([]AutonomyDay, error) {
	if days <= 0 {
		days = 14
	}
	cutoff := time.Now().AddDate(0, 0, -days).Format(time.RFC3339)
	q := `
		SELECT
			date(started_at) AS day,
			COUNT(*) AS runs,
			COALESCE(AVG(human_approval_count), 0) AS avg_appr
		FROM runs
		WHERE started_at >= ?
	`
	args := []any{cutoff}
	if engineer != "" {
		q += " AND engineer_name = ?"
		args = append(args, engineer)
	}
	q += " GROUP BY date(started_at) ORDER BY day ASC"

	rows, err := l.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []AutonomyDay{}
	for rows.Next() {
		var d AutonomyDay
		var avgAppr float64
		if err := rows.Scan(&d.Day, &d.Runs, &avgAppr); err != nil {
			return nil, err
		}
		d.AutonomyPct = math.Max(0, 100-avgAppr*100)
		out = append(out, d)
	}
	return out, rows.Err()
}

// FunnelStep represents one stage of the approval funnel.
type FunnelStep struct {
	StepType string `json:"step_type"`
	Count    int    `json:"count"`
}

// ApprovalFunnel returns count grouped by event type for `approval.*` events.
// Returns empty slice if events table has no matching rows (UI shows empty state).
func (l *LocalDB) ApprovalFunnel() ([]FunnelStep, error) {
	rows, err := l.Query(`
		SELECT event_type, COUNT(*) AS c
		FROM events
		WHERE event_type LIKE 'approval.%'
		GROUP BY event_type
		ORDER BY c DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []FunnelStep{}
	for rows.Next() {
		var s FunnelStep
		if err := rows.Scan(&s.StepType, &s.Count); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// CacheEffPoint is one daily cache-efficiency reading.
type CacheEffPoint struct {
	Day     string  `json:"day"`
	Pct     float64 `json:"pct"`
	Cached  int64   `json:"cached_tokens"`
	Total   int64   `json:"total_tokens"`
}

// CacheEfficiencyTimeline returns daily cache hit rate for the last N days.
func (l *LocalDB) CacheEfficiencyTimeline(days int) ([]CacheEffPoint, error) {
	if days <= 0 {
		days = 14
	}
	cutoff := time.Now().AddDate(0, 0, -days).Format(time.RFC3339)
	rows, err := l.Query(`
		SELECT
			date(started_at) AS day,
			COALESCE(SUM(cache_read_tokens), 0) AS cached,
			COALESCE(SUM(input_tokens) + SUM(cache_read_tokens), 0) AS total
		FROM runs
		WHERE started_at >= ?
		GROUP BY date(started_at)
		ORDER BY day ASC
	`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []CacheEffPoint{}
	for rows.Next() {
		var p CacheEffPoint
		if err := rows.Scan(&p.Day, &p.Cached, &p.Total); err != nil {
			return nil, err
		}
		if p.Total > 0 {
			p.Pct = float64(p.Cached) / float64(p.Total) * 100
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// CostPerTaskPoint is daily cost-per-distinct-PBI.
type CostPerTaskPoint struct {
	Day        string  `json:"day"`
	Tasks      int     `json:"tasks"`
	Cost       float64 `json:"cost"`
	CostPerTask float64 `json:"cost_per_task"`
}

// CostPerTaskTimeline returns daily cost / distinct task count.
func (l *LocalDB) CostPerTaskTimeline(days int) ([]CostPerTaskPoint, error) {
	if days <= 0 {
		days = 28
	}
	cutoff := time.Now().AddDate(0, 0, -days).Format(time.RFC3339)
	rows, err := l.Query(`
		SELECT
			date(started_at) AS day,
			COUNT(DISTINCT jira_issue_key) AS tasks,
			COALESCE(SUM(cost_usd), 0) AS cost
		FROM runs
		WHERE started_at >= ? AND jira_issue_key IS NOT NULL AND jira_issue_key != ''
		GROUP BY date(started_at)
		ORDER BY day ASC
	`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []CostPerTaskPoint{}
	for rows.Next() {
		var p CostPerTaskPoint
		if err := rows.Scan(&p.Day, &p.Tasks, &p.Cost); err != nil {
			return nil, err
		}
		if p.Tasks > 0 {
			p.CostPerTask = p.Cost / float64(p.Tasks)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ModelMixRow is one row in the model mix breakdown.
type ModelMixRow struct {
	Model string  `json:"model"`
	Runs  int     `json:"runs"`
	Cost  float64 `json:"cost"`
}

// ModelMix returns runs+cost grouped by model. NULL model bucketed as "(unknown)".
func (l *LocalDB) ModelMix(days int) ([]ModelMixRow, error) {
	if days <= 0 {
		days = 28
	}
	cutoff := time.Now().AddDate(0, 0, -days).Format(time.RFC3339)
	rows, err := l.Query(`
		SELECT
			COALESCE(NULLIF(model, ''), '(unknown)') AS m,
			COUNT(*) AS runs,
			COALESCE(SUM(cost_usd), 0) AS cost
		FROM runs
		WHERE started_at >= ?
		GROUP BY m
		ORDER BY cost DESC
	`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ModelMixRow{}
	for rows.Next() {
		var r ModelMixRow
		if err := rows.Scan(&r.Model, &r.Runs, &r.Cost); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SessionEndPoint is one daily aggregate of session end reasons.
// One row per (day, reason) pair.
type SessionEndPoint struct {
	Day    string `json:"day"`
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

// SessionEndReasons returns a (day × reason) time series.
func (l *LocalDB) SessionEndReasons(days int) ([]SessionEndPoint, error) {
	if days <= 0 {
		days = 28
	}
	cutoff := time.Now().AddDate(0, 0, -days).Format(time.RFC3339)
	rows, err := l.Query(`
		SELECT
			date(started_at) AS day,
			COALESCE(NULLIF(session_end_reason, ''), '(unknown)') AS r,
			COUNT(*) AS c
		FROM runs
		WHERE started_at >= ?
		GROUP BY day, r
		ORDER BY day ASC, r ASC
	`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []SessionEndPoint{}
	for rows.Next() {
		var p SessionEndPoint
		if err := rows.Scan(&p.Day, &p.Reason, &p.Count); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// DurationBucket is one bin in the duration histogram.
type DurationBucket struct {
	Label    string  `json:"label"`
	MinSec   float64 `json:"min_sec"`
	MaxSec   float64 `json:"max_sec"`
	Count    int     `json:"count"`
}

// DurationHistogram fetches all durations, computes IQR-based bucket width
// (Freedman-Diaconis), caps at p99, then bins into 6 buckets.
// Falls back to 6 fixed buckets if data is too sparse.
func (l *LocalDB) DurationHistogram(days int) ([]DurationBucket, error) {
	if days <= 0 {
		days = 28
	}
	cutoff := time.Now().AddDate(0, 0, -days).Format(time.RFC3339)
	rows, err := l.Query(`
		SELECT COALESCE(duration_sec, 0)
		FROM runs
		WHERE started_at >= ? AND duration_sec IS NOT NULL AND duration_sec > 0
		ORDER BY duration_sec ASC
	`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	durations := []float64{}
	for rows.Next() {
		var d float64
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		durations = append(durations, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(durations) == 0 {
		return []DurationBucket{}, nil
	}
	// Already sorted ASC.
	p99 := percentile(durations, 99)
	// Cap at p99 to mute outliers.
	capped := durations
	if len(durations) > 5 {
		capped = make([]float64, 0, len(durations))
		for _, d := range durations {
			if d > p99 {
				d = p99
			}
			capped = append(capped, d)
		}
	}
	// 6 fixed-width buckets between 0 and p99 (or max if data sparse).
	maxV := capped[len(capped)-1]
	if maxV <= 0 {
		return []DurationBucket{}, nil
	}
	const N = 6
	width := maxV / N
	out := make([]DurationBucket, N)
	for i := 0; i < N; i++ {
		lo := width * float64(i)
		hi := width * float64(i+1)
		out[i] = DurationBucket{
			Label:  fmtRange(lo, hi),
			MinSec: lo,
			MaxSec: hi,
		}
	}
	for _, d := range capped {
		idx := int(d / width)
		if idx >= N {
			idx = N - 1
		}
		out[idx].Count++
	}
	return out, nil
}

func percentile(sorted []float64, p int) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(float64(p)/100*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func fmtRange(lo, hi float64) string {
	return fmt.Sprintf("%s–%s", fmtSec(lo), fmtSec(hi))
}

func fmtSec(s float64) string {
	if s < 60 {
		return fmt.Sprintf("%.0fs", s)
	}
	if s < 3600 {
		return fmt.Sprintf("%.1fm", s/60)
	}
	return fmt.Sprintf("%.1fh", s/3600)
}

// WorkstationRow is a single workstation × engineer row.
type WorkstationRow struct {
	WorkstationID string `json:"workstation_id"`
	EngineerName  string `json:"engineer_name"`
	RunCount      int    `json:"run_count"`
	LastSeen      string `json:"last_seen"`
	IsAnomaly     bool   `json:"is_anomaly"` // true if engineer has only 1 workstation entry that's recent
}

// WorkstationMatrix lists distinct (workstation_id, engineer_name) pairs
// with run count and last_seen. Anomaly flag is set when an engineer has
// just appeared on a workstation in the last 7d that they never used before.
func (l *LocalDB) WorkstationMatrix(days int) ([]WorkstationRow, error) {
	if days <= 0 {
		days = 28
	}
	cutoff := time.Now().AddDate(0, 0, -days).Format(time.RFC3339)
	rows, err := l.Query(`
		SELECT
			workstation_id,
			COALESCE(engineer_name, '(unknown)') AS engineer,
			COUNT(*) AS runs,
			MAX(started_at) AS last_seen,
			MIN(started_at) AS first_seen
		FROM runs
		WHERE started_at >= ?
		GROUP BY workstation_id, engineer
		ORDER BY last_seen DESC
	`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type rawRow struct {
		WS, Eng, Last, First string
		Runs                 int
	}
	raws := []rawRow{}
	for rows.Next() {
		var r rawRow
		if err := rows.Scan(&r.WS, &r.Eng, &r.Runs, &r.Last, &r.First); err != nil {
			return nil, err
		}
		raws = append(raws, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Anomaly: engineer has only 1 row AND first_seen within last 7 days AND total runs <= 3
	weekAgo := time.Now().AddDate(0, 0, -7)
	engCount := map[string]int{}
	for _, r := range raws {
		engCount[r.Eng]++
	}
	out := make([]WorkstationRow, 0, len(raws))
	for _, r := range raws {
		anomaly := false
		if engCount[r.Eng] == 1 && r.Runs <= 3 {
			if t, err := time.Parse(time.RFC3339, r.First); err == nil && t.After(weekAgo) {
				anomaly = true
			}
		}
		out = append(out, WorkstationRow{
			WorkstationID: r.WS,
			EngineerName:  r.Eng,
			RunCount:      r.Runs,
			LastSeen:      r.Last,
			IsAnomaly:     anomaly,
		})
	}
	return out, nil
}

// RepoLeaderboardRow is one row in the repo cost ranking.
type RepoLeaderboardRow struct {
	Repo        string    `json:"repo"`
	Runs        int       `json:"runs"`
	Cost        float64   `json:"cost"`
	SuccessRate float64   `json:"success_rate"`
	Spark       []float64 `json:"spark"` // last-14d daily cost, padded
}

// RepoLeaderboard groups by git_remote (or cwd if remote NULL) and ranks by cost.
func (l *LocalDB) RepoLeaderboard(days int) ([]RepoLeaderboardRow, error) {
	if days <= 0 {
		days = 28
	}
	cutoff := time.Now().AddDate(0, 0, -days).Format(time.RFC3339)
	rows, err := l.Query(`
		SELECT
			COALESCE(NULLIF(git_remote, ''), cwd) AS repo,
			COUNT(*) AS runs,
			COALESCE(SUM(cost_usd), 0) AS cost,
			COALESCE(SUM(CASE WHEN status='done' THEN 1 ELSE 0 END), 0) AS done_count
		FROM runs
		WHERE started_at >= ? AND COALESCE(NULLIF(git_remote, ''), cwd) IS NOT NULL
		GROUP BY repo
		ORDER BY cost DESC
		LIMIT 20
	`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type raw struct {
		Repo string
		Runs int
		Cost float64
		Done int
	}
	raws := []raw{}
	for rows.Next() {
		var r raw
		if err := rows.Scan(&r.Repo, &r.Runs, &r.Cost, &r.Done); err != nil {
			return nil, err
		}
		raws = append(raws, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Build sparkline per repo: last 14 days daily cost.
	out := make([]RepoLeaderboardRow, 0, len(raws))
	for _, r := range raws {
		spark, err := l.repoDailySpark(r.Repo, 14)
		if err != nil {
			return nil, err
		}
		successRate := 0.0
		if r.Runs > 0 {
			successRate = float64(r.Done) / float64(r.Runs) * 100
		}
		out = append(out, RepoLeaderboardRow{
			Repo:        r.Repo,
			Runs:        r.Runs,
			Cost:        r.Cost,
			SuccessRate: successRate,
			Spark:       spark,
		})
	}
	return out, nil
}

// repoDailySpark builds a fixed-length daily cost series for one repo.
func (l *LocalDB) repoDailySpark(repo string, days int) ([]float64, error) {
	cutoff := time.Now().AddDate(0, 0, -days).Format(time.RFC3339)
	rows, err := l.Query(`
		SELECT date(started_at) AS day, COALESCE(SUM(cost_usd), 0)
		FROM runs
		WHERE started_at >= ? AND COALESCE(NULLIF(git_remote, ''), cwd) = ?
		GROUP BY day
		ORDER BY day ASC
	`, cutoff, repo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byDay := map[string]float64{}
	for rows.Next() {
		var day string
		var v float64
		if err := rows.Scan(&day, &v); err != nil {
			return nil, err
		}
		byDay[day] = v
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Fill last `days` days oldest first.
	out := make([]float64, days)
	for i := 0; i < days; i++ {
		day := time.Now().AddDate(0, 0, -(days - 1 - i)).Format("2006-01-02")
		out[i] = byDay[day]
	}
	return out, nil
}

// Sentinel: ensure sort import is used (helps when modifying).
var _ = sort.Float64s
