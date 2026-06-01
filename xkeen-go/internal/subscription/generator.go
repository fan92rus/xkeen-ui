package subscription

import (
	"encoding/json"
	"fmt"
	"sort"
)

// CollectFilteredProxies returns the union of proxies needed by all enabled profiles.
// For the default profile, its filter is applied to determine which proxies belong to it.
// For non-default profiles, each profile's filter is applied independently.
// The result is the union of all filtered sets, ensuring every profile's balancer
// can find its proxy tags in the outbounds.
func CollectFilteredProxies(allProxies []*ProxyEntry, profiles []Profile) []*ProxyEntry {
	if len(allProxies) == 0 || len(profiles) == 0 {
		return allProxies
	}

	// Check if any profile has non-trivial filters
	hasFilters := false
	for _, p := range profiles {
		if !p.Enabled {
			continue
		}
		f := p.Filter
		if len(f.ExcludeMarkers) > 0 || len(f.IncludeMarkers) > 0 ||
			len(f.ExcludeCountries) > 0 || len(f.IncludeCountries) > 0 ||
			len(f.IncludeRegexes) > 0 || len(f.ExcludeRegexes) > 0 ||
			f.MaxProxies > 0 {
			hasFilters = true
			break
		}
	}

	// No filters active — return all proxies
	if !hasFilters {
		return allProxies
	}

	// Collect union of tags from all enabled profiles' filtered sets
	neededTags := make(map[string]bool)
	for _, p := range profiles {
		if !p.Enabled {
			continue
		}
		filtered := ApplyFilter(allProxies, &p.Filter)
		for _, fp := range filtered {
			neededTags[fp.Tag] = true
		}
	}

	// Build result preserving order of allProxies
	result := make([]*ProxyEntry, 0, len(neededTags))
	for _, p := range allProxies {
		if neededTags[p.Tag] {
			result = append(result, p)
		}
	}

	return result
}

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
// Each enabled profile generates its own balancer entry. The default profile
// uses selector ["proxy-"] (regex matching all outbound tags); other profiles
// use a concrete list of matching proxy tags. Existing rules are preserved as
// raw JSON — only the balancers section is regenerated.
func GenerateRoutingJSON(proxies []*ProxyEntry, profiles []Profile, existingRouting json.RawMessage) ([]byte, error) {
	if len(proxies) == 0 {
		return nil, fmt.Errorf("no proxies to generate routing from")
	}

	routing := &routingConfig{
		DomainStrategy: "IPIfNonMatch",
	}

	// Find the default profile for replace_balancer_tag handling.
	var defaultProfile *Profile
	for i := range profiles {
		if profiles[i].IsDefault {
			defaultProfile = &profiles[i]
			break
		}
	}

	// Parse existing routing if provided — preserve rules as raw JSON.
	if len(existingRouting) > 0 {
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

			if rulesRaw, ok := existing["rules"]; ok {
				if defaultProfile != nil && defaultProfile.Strategy.ReplaceBalancerTag {
					routing.RulesRaw = replaceBalancerRules(rulesRaw)
				} else {
					routing.RulesRaw = rulesRaw
				}
			}
		}
	}

	// No existing rules at all — add defaults.
	if routing.RulesRaw == nil && len(routing.Rules) == 0 {
		routing.Rules = append(routing.Rules,
			map[string]interface{}{
				"type":        "field",
				"domain":      []string{"geosite:category-ads-all"},
				"outboundTag": "block",
			},
		)
		// Fallback rule for default profile
		if defaultProfile != nil {
			sType := defaultProfile.Strategy.Type
			if sType == "" || sType == "all" {
				routing.Rules = append(routing.Rules, map[string]interface{}{
					"type":        "field",
					"outboundTag": "proxy",
					"network":     "tcp,udp",
				})
			} else {
				routing.Rules = append(routing.Rules, map[string]interface{}{
					"type":        "field",
					"balancerTag": "default-balancer",
					"network":     "tcp,udp",
				})
			}
		}
	}

	// Generate balancers for each enabled profile.
	var balancers []map[string]interface{}
	for _, profile := range profiles {
		if !profile.Enabled {
			continue
		}
		sType := profile.Strategy.Type
		if sType == "" || sType == "all" {
			continue // no balancer needed for "all" strategy
		}

		var selector []string
		if profile.IsDefault {
			// Default profile uses filtered proxy tags for its balancer selector.
			filtered := ApplyFilter(proxies, &profile.Filter)
			for _, p := range filtered {
				selector = append(selector, p.Tag)
			}
			sort.Strings(selector)
			// Fallback: if filter removes all proxies, use regex to match any
			if len(selector) == 0 {
				selector = []string{"proxy-"}
			}
		} else {
			// Concrete tag list from filtered proxies.
			filtered := ApplyFilter(proxies, &profile.Filter)
			for _, p := range filtered {
				selector = append(selector, p.Tag)
			}
			sort.Strings(selector)
		}

		balancer := map[string]interface{}{
			"tag":      profile.ID + "-balancer",
			"selector": selector,
			"strategy": map[string]interface{}{"type": sType},
		}
		// Only default profile gets fallbackTag.
		if profile.IsDefault {
			fallback := "direct"
			if profile.Strategy.FallbackTag != "" {
				fallback = profile.Strategy.FallbackTag
			}
			balancer["fallbackTag"] = fallback
		}

		balancers = append(balancers, balancer)
	}
	routing.Balancers = balancers

	result := map[string]interface{}{
		"routing": routing,
	}

	return json.MarshalIndent(result, "", "  ")
}

// replaceBalancerRules parses the existing rules array, replaces rules with
// balancerTag with a new rule pointing to "default-balancer", and keeps all
// other rules as raw JSON bytes (no re-serialization).
func replaceBalancerRules(rulesRaw json.RawMessage) json.RawMessage {
	var rules []json.RawMessage
	if err := json.Unmarshal(rulesRaw, &rules); err != nil {
		return rulesRaw // if we can't parse, return as-is
	}

	newRule, _ := json.Marshal(map[string]interface{}{
		"type":        "field",
		"balancerTag": "default-balancer",
		"network":     "tcp,udp",
	})

	var result []json.RawMessage
	for _, raw := range rules {
		var m map[string]interface{}
		if json.Unmarshal(raw, &m) == nil && m["balancerTag"] != nil {
			// Replace this rule with our balancer rule
			result = append(result, newRule)
		} else {
			// Keep as-is (raw JSON bytes — preserves formatting, key order, types)
			result = append(result, raw)
		}
	}

	out, _ := json.Marshal(result)
	return out
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

// NeedsObservatory returns true if any enabled profile requires an observatory config.
func NeedsObservatory(profiles []Profile) bool {
	for _, p := range profiles {
		if p.Enabled && (p.Strategy.Type == "leastping" || p.Strategy.Type == "leastload") {
			return true
		}
	}
	return false
}

// routingConfig is an internal struct for building routing JSON.
type routingConfig struct {
	DomainStrategy string                   `json:"domainStrategy"`
	Balancers      []map[string]interface{} `json:"balancers,omitempty"`
	Rules          []map[string]interface{} `json:"-"`
	RulesRaw       json.RawMessage          `json:"-"`
}

// MarshalJSON serializes routingConfig, using RulesRaw (preserved raw JSON)
// if available, otherwise falling back to Rules (generated defaults).
func (r *routingConfig) MarshalJSON() ([]byte, error) {
	m := map[string]json.RawMessage{}

	dsRaw, err := json.Marshal(r.DomainStrategy)
	if err != nil {
		return nil, err
	}
	m["domainStrategy"] = dsRaw

	// Rules: use raw JSON if available, otherwise marshal generated rules
	if r.RulesRaw != nil {
		m["rules"] = r.RulesRaw
	} else if len(r.Rules) > 0 {
		raw, err := json.Marshal(r.Rules)
		if err != nil {
			return nil, err
		}
		m["rules"] = raw
	}

	// Balancers (always regenerated if present)
	if r.Balancers != nil {
		raw, err := json.Marshal(r.Balancers)
		if err != nil {
			return nil, err
		}
		m["balancers"] = raw
	}

	return json.Marshal(m)
}
