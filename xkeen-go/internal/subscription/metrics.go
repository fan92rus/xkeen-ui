package subscription

import (
	"encoding/json"
	"fmt"
)

// GenerateMetricsJSON generates the metrics configuration block for Xray.
// If port is <= 0, returns nil (metrics disabled).
// Otherwise returns JSON like:
//
//	{
//	  "metrics": {
//	    "tag": "Metrics",
//	    "listen": "127.0.0.1:<port>"
//	  }
//	}
func GenerateMetricsJSON(port int) []byte {
	if port <= 0 {
		return nil
	}

	result := map[string]interface{}{
		"metrics": map[string]interface{}{
			"tag":    "Metrics",
			"listen": fmt.Sprintf("127.0.0.1:%d", port),
		},
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return nil
	}

	return data
}

// PolicyWithStats reads an existing policy JSON and injects traffic stats settings.
// If the input is empty/invalid, returns a minimal policy with stats enabled.
func PolicyWithStats(existing []byte) []byte {
	var raw map[string]interface{}
	if len(existing) > 0 {
		json.Unmarshal(existing, &raw)
	}
	if raw == nil {
		raw = map[string]interface{}{}
	}

	policy, _ := raw["policy"].(map[string]interface{})
	if policy == nil {
		policy = map[string]interface{}{}
	}

	policy["system"] = map[string]interface{}{
		"statsInboundUplink":    true,
		"statsInboundDownlink":  true,
		"statsOutboundUplink":   true,
		"statsOutboundDownlink": true,
	}

	raw["policy"] = policy

	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return nil
	}
	return data
}

// PolicyWithoutStats removes traffic stats settings from policy JSON.
// Returns nil if the policy has no other meaningful content.
func PolicyWithoutStats(existing []byte) []byte {
	if len(existing) == 0 {
		return nil
	}
	var raw map[string]interface{}
	if json.Unmarshal(existing, &raw) != nil {
		return nil
	}

	policy, _ := raw["policy"].(map[string]interface{})
	if policy == nil {
		return nil
	}
	delete(policy, "system")

	if len(policy) == 0 {
		return nil
	}

	raw["policy"] = policy
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return nil
	}
	return data
}
