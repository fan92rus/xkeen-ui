package subscription

import (
	"encoding/json"
	"fmt"
	"sort"
)

// Tags for the service outbounds that GenerateOutboundsJSON always appends.
// Balancer fallbackTag and routing rules reference these constants so that
// outbounds and routing never disagree on the direct/block tag names.
const (
	DirectTag = "direct" // freedom outbound
	BlockTag  = "block"  // blackhole outbound
)

// outboundTag returns the tag that will be assigned to proxy p in the outbounds JSON.
// The first proxy in the slice always gets tag "proxy" (the default outbound contract);
// all others keep their assigned ProxyEntry.Tag. Both the outbounds and routing generators
// MUST use this helper so balancer selectors never reference a tag that doesn't exist in outbounds.
// Pointer identity is used so that filtered subsets (via ApplyFilter) resolve correctly.
func outboundTag(proxies []*ProxyEntry, p *ProxyEntry) string {
	if len(proxies) > 0 && p == proxies[0] {
		return "proxy"
	}
	return p.Tag
}

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
		if len(f.ExcludeCountries) > 0 || len(f.IncludeCountries) > 0 ||
			len(f.IncludeProtocols) > 0 || len(f.ExcludeProtocols) > 0 ||
			len(f.IncludeFingerprints) > 0 || len(f.ExcludeFingerprints) > 0 ||
			len(f.IncludeNetwork) > 0 || len(f.ExcludeNetwork) > 0 ||
			len(f.IncludeTLS) > 0 || len(f.ExcludeTLS) > 0 ||
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
//
// If mark > 0, every outbound (proxies + direct + block) gets
// streamSettings.sockopt.mark set to the given value. This is required when
// routing Entware traffic through Xray: `xkeen -pr on` adds iptables OUTPUT
// rules that redirect router process traffic to Xray, and packets emitted
// by Xray must carry fwmark (e.g. 255) so the `-m mark --mark 255 -j RETURN`
// rule bypasses the redirect (otherwise traffic loops back into Xray).
func GenerateOutboundsJSON(proxies []*ProxyEntry, mark int) ([]byte, error) {
	if len(proxies) == 0 {
		return nil, fmt.Errorf("no proxies to generate outbounds from")
	}

	outbounds := make([]json.RawMessage, 0, len(proxies)+2)

	for i, p := range proxies {
		var outbound map[string]interface{}
		if err := json.Unmarshal(p.Outbound, &outbound); err != nil {
			return nil, fmt.Errorf("failed to parse outbound JSON for proxy %d: %w", i, err)
		}

		outbound["tag"] = outboundTag(proxies, p)
		applyMark(outbound, mark)

		raw, err := json.Marshal(outbound)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal outbound for proxy %d: %w", i, err)
		}
		outbounds = append(outbounds, raw)
	}

	// Append direct outbound
	direct := map[string]interface{}{
		"tag":      DirectTag,
		"protocol": "freedom",
	}
	applyMark(direct, mark)
	directRaw, _ := json.Marshal(direct)
	outbounds = append(outbounds, directRaw)

	// Append block outbound
	block := map[string]interface{}{
		"tag":      BlockTag,
		"protocol": "blackhole",
		"settings": map[string]interface{}{
			"response": map[string]interface{}{
				"type": "http",
			},
		},
	}
	applyMark(block, mark)
	blockRaw, _ := json.Marshal(block)
	outbounds = append(outbounds, blockRaw)

	result := map[string]interface{}{
		"outbounds": outbounds,
	}

	return json.MarshalIndent(result, "", "  ")
}

// applyMark merges streamSettings.sockopt.mark into an outbound map.
// If mark == 0, the outbound is left untouched.
// If mark > 0, the mark is set/overwritten while preserving all existing
// streamSettings and sockopt fields (tlsSettings, wsSettings, etc.).
func applyMark(outbound map[string]interface{}, mark int) {
	if mark == 0 {
		return
	}
	ss, _ := outbound["streamSettings"].(map[string]interface{})
	if ss == nil {
		ss = map[string]interface{}{}
	}
	sockopt, _ := ss["sockopt"].(map[string]interface{})
	if sockopt == nil {
		sockopt = map[string]interface{}{}
	}
	sockopt["mark"] = mark
	ss["sockopt"] = sockopt
	outbound["streamSettings"] = ss
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
	var balancers []map[string]interface{} //nolint:prealloc // nil slice required for JSON omitempty semantics
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
				selector = append(selector, outboundTag(proxies, p))
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
				selector = append(selector, outboundTag(proxies, p))
			}
			sort.Strings(selector)
		}

		balancer := map[string]interface{}{
			"tag":      profile.ID + "-balancer",
			"selector": selector,
			"strategy": buildStrategyConfig(profile.Strategy),
		}

		// fallbackTag: when all outbounds in the selector are unreachable (per
		// observatory/strategy), traffic is routed to this service outbound instead.
		switch profile.Strategy.Fallback {
		case "direct":
			balancer["fallbackTag"] = DirectTag
		case "block":
			balancer["fallbackTag"] = BlockTag
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

// xrayStrategyName maps internal lowercase strategy types to the camelCase
// names expected by Xray-core in the balancer config.
var xrayStrategyName = map[string]string{
	"leastping": "leastPing",
	"leastload": "leastLoad",
}

// buildStrategyConfig builds the "strategy" object for a balancer entry.
// Maps internal type names to Xray's camelCase (leastping → leastPing) and
// includes advanced settings for leastping/leastload when configured.
func buildStrategyConfig(s RoutingStrategy) map[string]interface{} {
	xrayType := s.Type
	if mapped, ok := xrayStrategyName[xrayType]; ok {
		xrayType = mapped
	}
	strategy := map[string]interface{}{"type": xrayType}

	// Settings are only meaningful for leastping/leastload.
	if (s.Type == "leastping" || s.Type == "leastload") && s.Settings != nil {
		settings := map[string]interface{}{}
		if s.Settings.Expected > 0 {
			settings["expected"] = s.Settings.Expected
		}
		if s.Settings.MaxRTT != "" {
			settings["maxRTT"] = s.Settings.MaxRTT
		}
		if s.Settings.Tolerance > 0 {
			settings["tolerance"] = s.Settings.Tolerance
		}
		if len(s.Settings.Baselines) > 0 {
			settings["baselines"] = s.Settings.Baselines
		}
		if len(settings) > 0 {
			strategy["settings"] = settings
		}
	}

	return strategy
}

// GenerateObservatoryJSON generates 07_observatory.json for leastping/leastload strategies.
// enableConcurrency=true probes all outbounds in parallel (one sleep after all finish);
// false (default) probes sequentially with probeInterval between each.
func GenerateObservatoryJSON(enableConcurrency bool) ([]byte, error) {
	obs := map[string]interface{}{
		"subjectSelector": []string{"proxy"},
		"probeURL":        "https://www.google.com/generate_204",
		"probeInterval":   "30s",
	}
	if enableConcurrency {
		obs["enableConcurrency"] = true
	}
	observatory := map[string]interface{}{
		"observatory": obs,
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
