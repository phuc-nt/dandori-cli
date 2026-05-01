package insights

import (
	"encoding/json"
	"fmt"
)

// doraTrafficLight reads the latest metric_snapshot and summarizes DORA ratings
// as a single card. Returns 0 cards if no snapshot exists.
func doraTrafficLight(store Store) ([]Card, error) {
	snap, err := store.LatestSnapshot("", "json")
	if err != nil {
		return nil, fmt.Errorf("dora traffic light snapshot: %w", err)
	}
	if snap == nil {
		return []Card{}, nil
	}

	var raw map[string]any
	if err := json.Unmarshal([]byte(snap.Payload), &raw); err != nil {
		// Unparseable snapshot — skip rather than error.
		return []Card{}, nil
	}

	// Support both canonical format (deploy_frequency key) and faros format (metrics key).
	metrics := extractDoraMetrics(raw)
	if len(metrics) == 0 {
		return []Card{}, nil
	}

	// Count ratings.
	counts := map[string]int{"elite": 0, "high": 0, "medium": 0, "low": 0}
	for metric, val := range metrics {
		r := doraRatingInsights(metric, val)
		if r != "" {
			counts[r]++
		}
	}

	// Determine card severity.
	severity := "low"
	if counts["low"] > 0 {
		severity = "high"
	} else if counts["medium"] > 0 {
		severity = "medium"
	}

	card := Card{
		ID:       "dora-traffic-light",
		Severity: severity,
		Title:    "DORA traffic light",
		Body: fmt.Sprintf("%d elite, %d high, %d medium, %d low",
			counts["elite"], counts["high"], counts["medium"], counts["low"]),
		Action: "dora_dashboard",
	}
	return []Card{card}, nil
}

// extractDoraMetrics normalizes a raw snapshot payload into a map of
// metric-name → float64 value suitable for doraRatingInsights.
// Handles both canonical format and faros format.
func extractDoraMetrics(raw map[string]any) map[string]float64 {
	out := map[string]float64{}

	// Canonical format: keys are deploy_frequency, lead_time, change_failure_rate, mttr.
	if _, ok := raw["deploy_frequency"]; ok {
		if df, ok := raw["deploy_frequency"].(map[string]any); ok {
			if v, ok := df["value"].(float64); ok {
				out["deploy_frequency"] = v
			}
		}
		if lt, ok := raw["lead_time"].(map[string]any); ok {
			if v, ok := lt["value"].(float64); ok {
				out["lead_time"] = v
			}
		}
		if cfr, ok := raw["change_failure_rate"].(map[string]any); ok {
			if v, ok := cfr["value"].(float64); ok {
				out["change_failure_rate"] = v
			}
		}
		if mttr, ok := raw["mttr"].(map[string]any); ok {
			if v, ok := mttr["value"].(float64); ok {
				out["mttr"] = v
			}
		}
		return out
	}

	// Faros format: has "metrics" sub-key.
	metricsRaw, ok := raw["metrics"].(map[string]any)
	if !ok {
		return out
	}

	if df, ok := metricsRaw["deployment_frequency"].(map[string]any); ok {
		if v, ok := df["value"].(float64); ok {
			out["deploy_frequency"] = v
		}
	}
	if lt, ok := metricsRaw["lead_time_for_changes"].(map[string]any); ok {
		if p50, ok := lt["p50_seconds"].(float64); ok {
			out["lead_time"] = p50 / 86400 // convert seconds → days
		}
	}
	if cfr, ok := metricsRaw["change_failure_rate"].(map[string]any); ok {
		if v, ok := cfr["value"].(float64); ok {
			out["change_failure_rate"] = v
		}
	}
	if mttr, ok := metricsRaw["time_to_restore_service"].(map[string]any); ok {
		if p50, ok := mttr["p50_seconds"].(float64); ok {
			out["mttr"] = p50 / 3600 // convert seconds → hours
		}
	}
	return out
}

// doraRatingInsights maps a DORA metric value to Elite/High/Medium/Low per DORA
// 2023 benchmark thresholds. Inlined here to avoid importing internal/server
// (would create an import cycle).
func doraRatingInsights(metric string, val float64) string {
	switch metric {
	case "deploy_frequency": // deployments per day
		if val >= 1 {
			return "elite"
		}
		if val >= 1.0/7 {
			return "high"
		}
		if val >= 1.0/30 {
			return "medium"
		}
		return "low"
	case "lead_time": // days
		if val < 1 {
			return "elite"
		}
		if val < 7 {
			return "high"
		}
		if val < 30 {
			return "medium"
		}
		return "low"
	case "change_failure_rate": // ratio 0–1
		if val <= 0.05 {
			return "elite"
		}
		if val <= 0.10 {
			return "high"
		}
		if val <= 0.15 {
			return "medium"
		}
		return "low"
	case "mttr": // hours
		if val < 1 {
			return "elite"
		}
		if val < 24 {
			return "high"
		}
		if val < 168 {
			return "medium"
		}
		return "low"
	}
	return ""
}
