package subscription

import (
	"encoding/json"
	"fmt"
)

// GenerateMetricsJSON generates metrics + policy.system for Xray.
// If port is <= 0, returns nil (metrics disabled).
//
// Xray deep-merges config files, so policy.system here merges with
// policy.levels from 06_policy.json. No need to modify 06_policy.json.
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
