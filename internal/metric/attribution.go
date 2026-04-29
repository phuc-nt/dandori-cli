package metric

import (
	"encoding/json"
	"sort"

	"github.com/phuc-nt/dandori-cli/internal/db"
)

// AttributionResult is the per-window aggregate over task_attribution rows.
// Mirrors the v0.5.0 metric-result style (Window + InsufficientData) so
// formatters can treat it uniformly.
type AttributionResult struct {
	TasksTotal             int            `json:"tasks_total"`
	TasksWithSession       int            `json:"tasks_with_session"`
	AgentAutonomyRate      float64        `json:"agent_autonomy_rate"`
	RetentionP50           float64        `json:"agent_code_retention_p50"`
	RetentionP90           float64        `json:"agent_code_retention_p90"`
	InterventionRateP50    float64        `json:"intervention_rate_p50"`
	IterationsP50          float64        `json:"iterations_p50"`
	IterationsP90          float64        `json:"iterations_p90"`
	CostPerRetainedLineP50 float64        `json:"cost_per_retained_line_usd_p50"`
	SessionOutcomes        map[string]int `json:"session_outcomes"`
	InsufficientData       bool           `json:"insufficient_data"`
	Window                 MetricWindow   `json:"window"`
}

// AggregateAttribution loads task_attribution rows whose jira_done_at falls
// in [w.Start, w.End) and computes the percentile/ratio summary. Empty
// window → InsufficientData flag set, all numeric fields zero.
func AggregateAttribution(d *db.LocalDB, w MetricWindow) (AttributionResult, error) {
	res := AttributionResult{
		Window:          w,
		SessionOutcomes: map[string]int{},
	}

	rows, err := d.Query(`SELECT jira_issue_key, session_count,
		lines_attributed_agent, lines_attributed_human,
		total_iterations, intervention_rate, total_agent_cost_usd,
		COALESCE(session_outcomes, '{}')
		FROM task_attribution
		WHERE jira_done_at >= ? AND jira_done_at < ?`,
		w.Start.UTC().Format(rfc3339Format), w.End.UTC().Format(rfc3339Format))
	if err != nil {
		return res, err
	}
	defer rows.Close()

	var (
		retentions, interventionRates, iterations, costPerRetained []float64
		autonomousCount                                            int
	)
	for rows.Next() {
		var (
			key                                  string
			sessions, agentLines, humanLines, it int
			intRate, cost                        float64
			outcomesJSON                         string
		)
		if err := rows.Scan(&key, &sessions, &agentLines, &humanLines, &it, &intRate, &cost, &outcomesJSON); err != nil {
			return res, err
		}
		res.TasksTotal++
		if sessions > 0 {
			res.TasksWithSession++
		}
		// Retention only meaningful when there are tracked lines.
		if total := agentLines + humanLines; total > 0 {
			retentions = append(retentions, float64(agentLines)/float64(total))
		}
		interventionRates = append(interventionRates, intRate)
		iterations = append(iterations, float64(it))
		// Cost-per-retained-line undefined when agent retained zero lines.
		if agentLines > 0 {
			costPerRetained = append(costPerRetained, cost/float64(agentLines))
		}
		// Autonomy: tasks with session AND intervention_rate < 0.2.
		if sessions > 0 && intRate < 0.2 {
			autonomousCount++
		}
		// Merge session_outcomes into the running histogram.
		var outcomes map[string]int
		if err := json.Unmarshal([]byte(outcomesJSON), &outcomes); err == nil {
			for k, v := range outcomes {
				res.SessionOutcomes[k] += v
			}
		}
	}
	if err := rows.Err(); err != nil {
		return res, err
	}

	if res.TasksTotal == 0 {
		res.InsufficientData = true
		return res, nil
	}
	if res.TasksWithSession > 0 {
		res.AgentAutonomyRate = float64(autonomousCount) / float64(res.TasksWithSession)
	}
	res.RetentionP50 = pctOrZero(retentions, 50)
	res.RetentionP90 = pctOrZero(retentions, 90)
	res.InterventionRateP50 = pctOrZero(interventionRates, 50)
	res.IterationsP50 = pctOrZero(iterations, 50)
	res.IterationsP90 = pctOrZero(iterations, 90)
	res.CostPerRetainedLineP50 = pctOrZero(costPerRetained, 50)
	return res, nil
}

// pctOrZero sorts in place and returns percentile p. Empty slice yields 0
// (caller already gates on InsufficientData for the "no data" path).
func pctOrZero(vals []float64, p float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	sort.Float64s(vals)
	return percentile(vals, p)
}

// rfc3339Format pinned here so SQL bindings match the Go format used to
// write jira_done_at on the upsert side. Using `time.RFC3339` directly keeps
// formatting consistent across compute + aggregate.
const rfc3339Format = "2006-01-02T15:04:05Z07:00"
