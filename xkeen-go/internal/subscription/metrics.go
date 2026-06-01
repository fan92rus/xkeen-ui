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
