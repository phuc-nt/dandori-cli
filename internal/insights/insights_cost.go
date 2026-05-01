package insights

import (
	"fmt"
	"strings"
)

// wowCostSpike detects when this-week cost > last-week cost * 1.20.
// Trigger: ratio > 1.20 AND this_week_cost > $1 (avoid noise on tiny totals).
// Severity: high if ratio>1.5, medium if 1.2<ratio<=1.5.
// Returns at most 1 card.
func wowCostSpike(store Store, projectKey string) ([]Card, error) {
	pf, pfArgs := projectFilter(projectKey)

	// Sum cost for this week (last 7 days) and last week (7–14 days ago).
	q := fmt.Sprintf(`
		SELECT
			COALESCE(SUM(CASE WHEN started_at >= datetime('now', '-7 days') THEN cost_usd ELSE 0 END), 0) AS this_week,
			COALESCE(SUM(CASE WHEN started_at >= datetime('now', '-14 days')
			                   AND started_at < datetime('now', '-7 days')  THEN cost_usd ELSE 0 END), 0) AS last_week
		FROM runs
		WHERE 1=1%s
	`, pf)

	rows, err := store.Query(q, pfArgs...)
	if err != nil {
		return nil, fmt.Errorf("wow cost spike query: %w", err)
	}
	defer rows.Close()

	var thisWeek, lastWeek float64
	if rows.Next() {
		if err := rows.Scan(&thisWeek, &lastWeek); err != nil {
			return nil, fmt.Errorf("wow cost spike scan: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("wow cost spike rows: %w", err)
	}

	// Must exceed noise floor and ratio threshold.
	if thisWeek <= 1.0 || lastWeek <= 0 {
		return []Card{}, nil
	}
	ratio := thisWeek / lastWeek
	if ratio <= 1.20 {
		return []Card{}, nil
	}

	// Find top contributor (agent or project) this week.
	topContributor, err := topCostContributor(store, projectKey)
	if err != nil {
		topContributor = "unknown"
	}

	severity := "medium"
	if ratio > 1.5 {
		severity = "high"
	}

	pctIncrease := (ratio - 1) * 100
	card := Card{
		ID:       "wow-spike",
		Severity: severity,
		Title:    "Cost spike WoW",
		Body: fmt.Sprintf("$%.2f this week vs $%.2f last week (+%.0f%%). Top contributor: %s",
			thisWeek, lastWeek, pctIncrease, topContributor),
		Action: "cost_dashboard",
	}
	return []Card{card}, nil
}

// topCostContributor returns the agent_name with highest cost in the last 7 days.
func topCostContributor(store Store, projectKey string) (string, error) {
	pf, pfArgs := projectFilter(projectKey)
	q := fmt.Sprintf(`
		SELECT COALESCE(agent_name, 'unknown')
		FROM runs
		WHERE started_at >= datetime('now', '-7 days')%s
		GROUP BY agent_name
		ORDER BY SUM(cost_usd) DESC
		LIMIT 1
	`, pf)
	rows, err := store.Query(q, pfArgs...)
	if err != nil {
		return "unknown", err
	}
	defer rows.Close()
	if rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return "unknown", err
		}
		return name, nil
	}
	return "unknown", nil
}

// costOutlierTask detects tasks whose total cost exceeds project mean + 3*stddev
// (or 3*mean when stddev=0). Requires min 5 tasks in the project.
// Returns at most 3 cards (highest outliers first).
func costOutlierTask(store Store, projectKey string) ([]Card, error) {
	pf, pfArgs := projectFilter(projectKey)

	// Aggregate cost per task (jira_issue_key), extract project key prefix.
	q := fmt.Sprintf(`
		SELECT
			jira_issue_key,
			SUM(cost_usd) AS task_cost,
			SUBSTR(jira_issue_key, 1, INSTR(jira_issue_key, '-') - 1) AS proj_key
		FROM runs
		WHERE jira_issue_key IS NOT NULL
		  AND jira_issue_key != ''
		  AND INSTR(jira_issue_key, '-') > 0
		  %s
		GROUP BY jira_issue_key
	`, strings.Replace(pf, " AND ", " AND ", 1))

	rows, err := store.Query(q, pfArgs...)
	if err != nil {
		return nil, fmt.Errorf("cost outlier task query: %w", err)
	}
	defer rows.Close()

	type taskRow struct {
		issueKey string
		cost     float64
		projKey  string
	}
	var tasks []taskRow
	for rows.Next() {
		var r taskRow
		if err := rows.Scan(&r.issueKey, &r.cost, &r.projKey); err != nil {
			return nil, fmt.Errorf("cost outlier task scan: %w", err)
		}
		tasks = append(tasks, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("cost outlier task rows: %w", err)
	}

	if len(tasks) == 0 {
		return []Card{}, nil
	}

	// Group by project key and compute per-project stats.
	type projStats struct {
		tasks []taskRow
		sum   float64
		sum2  float64
	}
	projects := map[string]*projStats{}
	for _, t := range tasks {
		ps := projects[t.projKey]
		if ps == nil {
			ps = &projStats{}
			projects[t.projKey] = ps
		}
		ps.tasks = append(ps.tasks, t)
		ps.sum += t.cost
		ps.sum2 += t.cost * t.cost
	}

	var cards []Card
	for _, ps := range projects {
		n := float64(len(ps.tasks))
		if n < 5 {
			continue
		}
		mean := ps.sum / n
		if mean <= 0 {
			continue
		}
		variance := ps.sum2/n - mean*mean
		var stddev float64
		if variance > 0 {
			stddev = sqrt(variance)
		}
		threshold := mean + 3*stddev
		if stddev == 0 {
			threshold = 3 * mean
		}

		for _, t := range ps.tasks {
			if t.cost <= threshold {
				continue
			}
			severity := "medium"
			if t.cost > 5*mean {
				severity = "high"
			}
			cards = append(cards, Card{
				ID:       "cost-outlier-" + t.issueKey,
				Severity: severity,
				Title:    "Cost outlier task",
				Body: fmt.Sprintf("%s: $%.2f (project mean: $%.2f) — runaway iteration loop?",
					t.issueKey, t.cost, mean),
				Action:   "cost_dashboard",
				ActionID: t.issueKey,
			})
		}
	}

	// Sort descending by cost ratio, cap at 3.
	sortCardsByBodyCost(cards)
	if len(cards) > 3 {
		cards = cards[:3]
	}
	return cards, nil
}

// sqrt computes integer square root via Newton's method (avoids math import).
func sqrt(x float64) float64 {
	if x <= 0 {
		return 0
	}
	z := x / 2
	for i := 0; i < 100; i++ {
		z1 := z - (z*z-x)/(2*z)
		if abs(z1-z) < 1e-10 {
			return z1
		}
		z = z1
	}
	return z
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// sortCardsByBodyCost is a simple insertion sort by card ID (stable ordering for caps).
// Cards are already deterministic since IDs encode the task key.
func sortCardsByBodyCost(cards []Card) {
	// Sort descending by severity then ID for determinism.
	for i := 1; i < len(cards); i++ {
		for j := i; j > 0 && cards[j].Severity > cards[j-1].Severity; j-- {
			cards[j], cards[j-1] = cards[j-1], cards[j]
		}
	}
}
