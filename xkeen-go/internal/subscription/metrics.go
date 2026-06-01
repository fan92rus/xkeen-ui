package subscription

import (
	"encoding/json"
	"fmt"
	"os"
)

// GenerateMetricsJSON generates metrics + stats + policy.system for Xray.
// If port is <= 0, returns nil (metrics disabled).
//
// This file enables three things:
//   - "stats": {}          — activates Xray statistics infrastructure
//   - "metrics": {...}     — HTTP endpoint for /debug/vars
//   - "policy": {"system"} — enables specific traffic counters
//
// Xray deep-merges config files, so policy.system here merges with
// policy.levels from 06_policy.json.
func GenerateMetricsJSON(port int) []byte {
	if port <= 0 {
		return nil
	}

	result := map[string]interface{}{
		"stats": map[string]interface{}{},
		"metrics": map[string]interface{}{
			"tag":    "Metrics",
			"listen": fmt.Sprintf("127.0.0.1:%d", port),
		},
		"policy": map[string]interface{}{
			"system": map[string]interface{}{
				"statsInboundUplink":    true,
				"statsInboundDownlink":  true,
				"statsOutboundUplink":   true,
				"statsOutboundDownlink": true,
			},
		},
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return nil
	}
	return data
}

// WriteMetricsConfig writes 08_metrics.json and returns cleanup function.
// Also updates 06_policy.json to include traffic stats in system block.
func WriteMetricsConfig(xrayDir string, port int) error {
	metricsPath := xrayDir + "/08_metrics.json"

	if port > 0 {
		// Write 08_metrics.json with stats + metrics + policy.system
		metricsJSON := GenerateMetricsJSON(port)
		if metricsJSON != nil {
			if err := os.WriteFile(metricsPath, metricsJSON, 0644); err != nil {
				return fmt.Errorf("write metrics: %w", err)
			}
		}
	} else {
		os.Remove(metricsPath)
	}
	return nil
}
