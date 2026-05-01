package insights

import (
	"fmt"
	"strings"
)

// retentionDecay detects engineers whose 7-day intervention rate worsened by
// more than 10pp vs their 28-day baseline.
// Trigger: rate_7d > rate_28d + 0.10 AND runs_7d >= 5 (min sample).
// Severity: high if delta>0.20, medium otherwise.
// Returns at most 3 cards (worst deltas first).
func retentionDecay(store Store, projectKey string) ([]Card, error) {
	pf, pfArgs := projectFilter(projectKey)

	// One pass: compute both windows per engineer.
	// 28d baseline includes the 7d window (just longer window).
	q := fmt.Sprintf(`
		SELECT
			engineer_name,
			SUM(CASE WHEN started_at >= datetime('now', '-7 days')
			         THEN 1 ELSE 0 END)                                         AS runs_7d,
			SUM(CASE WHEN started_at >= datetime('now', '-7 days')
			         THEN human_intervention_count ELSE 0 END)                  AS inter_7d,
			SUM(CASE WHEN started_at >= datetime('now', '-28 days')
			         THEN 1 ELSE 0 END)                                         AS runs_28d,
			SUM(CASE WHEN started_at >= datetime('now', '-28 days')
			         THEN human_intervention_count ELSE 0 END)                  AS inter_28d
		FROM runs
		WHERE engineer_name IS NOT NULL
		  AND engineer_name != ''
		  AND started_at >= datetime('now', '-28 days')
		  %s
		GROUP BY engineer_name
	`, pf)

	rows, err := store.Query(q, pfArgs...)
	if err != nil {
		return nil, fmt.Errorf("retention decay query: %w", err)
	}
	defer rows.Close()

	type decayResult struct {
		engineer string
		delta    float64
		rate7d   float64
		rate28d  float64
	}
	var results []decayResult

	for rows.Next() {
		var engineer string
		var runs7d, inter7d, runs28d, inter28d int
		if err := rows.Scan(&engineer, &runs7d, &inter7d, &runs28d, &inter28d); err != nil {
			return nil, fmt.Errorf("retention decay scan: %w", err)
		}
		// Skip below min sample for 7d window.
		if runs7d < 5 {
			continue
		}
		rate7d := float64(inter7d) / float64(runs7d)
		var rate28d float64
		if runs28d > 0 {
			rate28d = float64(inter28d) / float64(runs28d)
		}
		delta := rate7d - rate28d
		if delta > 0.10 {
			results = append(results, decayResult{engineer, delta, rate7d, rate28d})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("retention decay rows: %w", err)
	}

	// Sort descending by delta (worst first).
	for i := 1; i < len(results); i++ {
		for j := i; j > 0 && results[j].delta > results[j-1].delta; j-- {
			results[j], results[j-1] = results[j-1], results[j]
		}
	}
	if len(results) > 3 {
		results = results[:3]
	}

	cards := make([]Card, 0, len(results))
	for _, r := range results {
		severity := "medium"
		if r.delta > 0.20 {
			severity = "high"
		}
		cards = append(cards, Card{
			ID:       "retention-decay-" + slugify(r.engineer),
			Severity: severity,
			Title:    "Retention decay: " + r.engineer,
			Body: fmt.Sprintf("%.2f interventions/run last 7d vs %.2f over 28d",
				r.rate7d, r.rate28d),
		})
	}
	return cards, nil
}

// interventionCluster detects (engineer, agent_name) pairs with intervention
// rate > 0.5 over 28 days with at least 5 runs — likely model/skill mismatch.
// Returns at most 3 cards.
func interventionCluster(store Store, projectKey string) ([]Card, error) {
	pf, pfArgs := projectFilter(projectKey)

	q := fmt.Sprintf(`
		SELECT
			engineer_name,
			agent_name,
			COUNT(*)                                                     AS run_count,
			CAST(SUM(human_intervention_count) AS REAL) / COUNT(*)      AS rate
		FROM runs
		WHERE engineer_name IS NOT NULL
		  AND engineer_name != ''
		  AND agent_name IS NOT NULL
		  AND agent_name != ''
		  AND started_at >= datetime('now', '-28 days')
		  %s
		GROUP BY engineer_name, agent_name
		HAVING COUNT(*) >= 5
		   AND CAST(SUM(human_intervention_count) AS REAL) / COUNT(*) > 0.5
		ORDER BY rate DESC
		LIMIT 3
	`, pf)

	rows, err := store.Query(q, pfArgs...)
	if err != nil {
		return nil, fmt.Errorf("intervention cluster query: %w", err)
	}
	defer rows.Close()

	var cards []Card
	for rows.Next() {
		var engineer, agent string
		var runCount int
		var rate float64
		if err := rows.Scan(&engineer, &agent, &runCount, &rate); err != nil {
			return nil, fmt.Errorf("intervention cluster scan: %w", err)
		}
		cards = append(cards, Card{
			ID:       "intervention-cluster-" + slugify(engineer) + "-" + slugify(agent),
			Severity: "high",
			Title:    fmt.Sprintf("Intervention cluster: %s + %s", engineer, agent),
			Body: fmt.Sprintf("%s + %s: %.2f interventions/run over %d runs — likely model/skill mismatch",
				engineer, agent, rate, runCount),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("intervention cluster rows: %w", err)
	}

	if cards == nil {
		return []Card{}, nil
	}
	return cards, nil
}

// slugify converts a name to a URL-safe lowercase slug for use in Card IDs.
// Replaces spaces and special chars with hyphens.
func slugify(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}
