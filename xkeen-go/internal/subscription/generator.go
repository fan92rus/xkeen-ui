package subscription

import (
	"encoding/json"
	"fmt"
	"strings"
)

// GenerateOutboundsJSON generates the content for 04_outbounds.json.
// The first proxy gets tag "proxy" (the default outbound). All others keep
// their assigned tags. "direct" and "block" outbounds are appended at the end.
func GenerateOutboundsJSON(proxies []*ProxyEntry) ([]byte, error) {
	if len(proxies) == 0 {
		return nil, fmt.Errorf("no proxies to generate outbounds from")
	}

	outbounds := make([]json.RawMessage, 0, len(proxies)+2)

	for i, p := range proxies {
		var outbound map[string]interface{}
		if err := json.Unmarshal(p.Outbound, &outbound); err != nil {
			return nil, fmt.Errorf("failed to parse outbound JSON for proxy %d: %w", i, err)
		}

		// First proxy gets tag "proxy" (default outbound)
		if i == 0 {
			outbound["tag"] = "proxy"
		} else {
			outbound["tag"] = p.Tag
		}

		raw, err := json.Marshal(outbound)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal outbound for proxy %d: %w", i, err)
		}
		outbounds = append(outbounds, raw)
	}

	// Append direct outbound
	direct := map[string]interface{}{
		"tag":      "direct",
		"protocol": "freedom",
	}
	directRaw, _ := json.Marshal(direct)
	outbounds = append(outbounds, directRaw)

	// Append block outbound
	block := map[string]interface{}{
		"tag":      "block",
		"protocol": "blackhole",
		"settings": map[string]interface{}{
			"response": map[string]interface{}{
				"type": "http",
			},
		},
	}
	blockRaw, _ := json.Marshal(block)
	outbounds = append(outbounds, blockRaw)

	result := map[string]interface{}{
		"outbounds": outbounds,
	}

	return json.MarshalIndent(result, "", "  ")
}

// GenerateRoutingJSON generates or updates the content for 05_routing.json.
// If existingRouting is non-empty, it preserves existing routing rules and only
// updates the proxy-related rule and balancers section.
func GenerateRoutingJSON(proxies []*ProxyEntry, strategy RoutingStrategy, existingRouting json.RawMessage) ([]byte, error) {
	if len(proxies) == 0 {
		return nil, fmt.Errorf("no proxies to generate routing from")
	}

	routing := &routingConfig{
		DomainStrategy: "IPIfNonMatch",
	}

	// Parse existing routing if provided
	if len(existingRouting) > 0 {
		// The file format is {"routing": {"domainStrategy": ..., "rules": ...}}
		// We need to unwrap the "routing" key first.
		var wrapper map[string]json.RawMessage
		innerRouting := existingRouting
		if err := json.Unmarshal(existingRouting, &wrapper); err == nil {
			if inner, ok := wrapper["routing"]; ok {
				innerRouting = inner
			}
		}

		var existing map[string]json.RawMessage
		if err := json.Unmarshal(innerRouting, &existing); err == nil {
			if ds, ok := existing["domainStrategy"]; ok {
				var dsStr string
				if json.Unmarshal(ds, &dsStr) == nil {
					routing.DomainStrategy = dsStr
				}
			}
			if rules, ok := existing["rules"]; ok {
				var existingRules []map[string]interface{}
				if json.Unmarshal(rules, &existingRules) == nil {
					// Preserve non-proxy rules (block, direct, custom)
					for _, rule := range existingRules {
						tag, _ := rule["outboundTag"].(string)
						bTag, _ := rule["balancerTag"].(string)
						// Skip old proxy-related rules — we'll regenerate them
						if tag == "proxy" || strings.HasPrefix(bTag, "proxy-") {
							continue
						}
						routing.Rules = append(routing.Rules, rule)
					}
				}
			}
		}
	}

	// If no existing rules, add default ad-blocking rule
	if len(routing.Rules) == 0 {
		routing.Rules = append(routing.Rules, map[string]interface{}{
			"type":         "field",
			"domain":       []string{"geosite:category-ads-all"},
			"outboundTag":  "block",
		})
	}

	// Set strategy-specific configuration
	if strategy.Type == "all" || strategy.Type == "" {
		// Simple: all traffic through the first proxy
		routing.Rules = append(routing.Rules, map[string]interface{}{
			"type":        "field",
			"outboundTag": "proxy",
			"network":     "tcp,udp",
		})
	} else {
		// Balancer mode
		fallback := "direct"
		if strategy.FallbackTag != "" {
			fallback = strategy.FallbackTag
		}

		routing.Balancers = []map[string]interface{}{
			{
				"tag":         "proxy-balancer",
				"selector":    []string{"proxy-"},
				"strategy":    map[string]interface{}{"type": strategy.Type},
				"fallbackTag": fallback,
			},
		}

		routing.Rules = append(routing.Rules, map[string]interface{}{
			"type":         "field",
			"balancerTag":  "proxy-balancer",
			"network":      "tcp,udp",
		})
	}

	result := map[string]interface{}{
		"routing": routing,
	}

	return json.MarshalIndent(result, "", "  ")
}

// GenerateObservatoryJSON generates 07_observatory.json for leastping/leastload strategies.
func GenerateObservatoryJSON() ([]byte, error) {
	observatory := map[string]interface{}{
		"observatory": map[string]interface{}{
			"subjectSelector": []string{"proxy-"},
			"probeURL":        "https://www.google.com/generate_204",
			"probeInterval":   "30s",
		},
	}

	return json.MarshalIndent(observatory, "", "  ")
}

// NeedsObservatory returns true if the given strategy type requires an observatory config.
func NeedsObservatory(strategyType string) bool {
	return strategyType == "leastping" || strategyType == "leastload"
}

// routingConfig is an internal struct for building routing JSON.
type routingConfig struct {
	DomainStrategy string                     `json:"domainStrategy"`
	Balancers      []map[string]interface{}   `json:"balancers,omitempty"`
	Rules          []map[string]interface{}    `json:"rules"`
}
