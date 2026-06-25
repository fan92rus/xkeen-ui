// Package subscription — Mihomo config generator.
//
// Generates Clash-compatible YAML config (config.yaml) from the same
// subscription proxy pool and profiles used for Xray generation.
// Supports:
//   - VLESS → Mihomo vless (with ws/grpc/tcp, tls, reality, xtls-vision flow)
//   - Trojan → Mihomo trojan (with ws/grpc/tcp, tls)
//   - Hysteria2 → Mihomo hysteria2
//   - Profile-based proxy-groups (select, url-test, load-balance, random)
//   - Existing config.yaml mix-in (preserves dns, tun, port, etc.)
//   - Optional Xray 05_routing.json → Mihomo rules conversion
package subscription

import (
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// ── Mihomo proxy type mapping ──────────────────────────────────────────────

// mihomoProxyType maps Xray protocol strings to Mihomo proxy types.
func mihomoProxyType(xrayProtocol string) string {
	switch xrayProtocol {
	case "vless":
		return "vless"
	case "trojan":
		return "trojan"
	case "hysteria":
		return "hysteria2"
	default:
		return xrayProtocol
	}
}

// ── Single proxy conversion ────────────────────────────────────────────────

// proxyToMihomo converts a ProxyEntry to a map suitable for YAML serialization
// as a Mihomo/Clash proxy entry. It re-parses the Xray outbound JSON to extract
// connection details and reformats them in Mihomo's flat structure.
func proxyToMihomo(entry *ProxyEntry) (map[string]interface{}, error) {
	if len(entry.Outbound) == 0 {
		return nil, fmt.Errorf("proxy %s has no outbound data", entry.Tag)
	}

	var outbound map[string]interface{}
	if err := json.Unmarshal(entry.Outbound, &outbound); err != nil {
		return nil, fmt.Errorf("failed to parse outbound for %s: %w", entry.Tag, err)
	}

	protocol, _ := outbound["protocol"].(string)
	if protocol == "" {
		return nil, fmt.Errorf("proxy %s has no protocol in outbound", entry.Tag)
	}

	mihomoType := mihomoProxyType(protocol)

	m := map[string]interface{}{
		"name":   entry.Tag,
		"type":   mihomoType,
		"server": "",
		"port":   0,
	}

	switch protocol {
	case "vless":
		fillVless(m, outbound)
	case "trojan":
		fillTrojan(m, outbound)
	case "hysteria":
		fillHysteria2(m, outbound)
	default:
		return nil, fmt.Errorf("unsupported protocol for Mihomo conversion: %s", protocol)
	}

	// Remove empty/nil values
	cleanMap(m)

	return m, nil
}

// ── Protocol-specific fillers ──────────────────────────────────────────────

// fillVless extracts VLESS connection details into m.
// Xray outbound: {protocol:"vless", settings:{vnext:[{address,port,users:[{id,encryption,flow}]}]},
//                 streamSettings:{network,security,wsSettings/tcpSettings/grpcSettings/realitySettings/tlsSettings}}
func fillVless(m map[string]interface{}, outbound map[string]interface{}) {
	settings, _ := outbound["settings"].(map[string]interface{})
	if settings != nil {
		if vnext, ok := settings["vnext"].([]interface{}); ok && len(vnext) > 0 {
			if first, ok := vnext[0].(map[string]interface{}); ok {
				m["server"], _ = first["address"].(string)
				m["port"] = toInt(first["port"])
				if users, ok := first["users"].([]interface{}); ok && len(users) > 0 {
					if user, ok := users[0].(map[string]interface{}); ok {
						m["uuid"], _ = user["id"].(string)
						if flow, ok := user["flow"].(string); ok && flow != "" {
							m["flow"] = flow
						}
					}
				}
			}
		}
	}

	// HACK: some subscriptions have a legacy xtls-vision flow whose
	// under-specification is incompatible with Mihomo expectation of exactly "xtls-rprx-vision"
	if flow, ok := m["flow"].(string); ok && flow != "" {
		// sanity check: not necessary, but keep clean
		_ = flow
	}

	fillStreamSettings(m, outbound)
}

// fillTrojan extracts Trojan connection details into m.
// Xray outbound: {protocol:"trojan", settings:{servers:[{address,port,password}]},
//                 streamSettings:{...}}
func fillTrojan(m map[string]interface{}, outbound map[string]interface{}) {
	settings, _ := outbound["settings"].(map[string]interface{})
	if settings != nil {
		if servers, ok := settings["servers"].([]interface{}); ok && len(servers) > 0 {
			if first, ok := servers[0].(map[string]interface{}); ok {
				m["server"], _ = first["address"].(string)
				m["port"] = toInt(first["port"])
				m["password"], _ = first["password"].(string)
			}
		}
	}

	// Trojan always uses TLS
	fillTLS(m, outbound)
	fillStreamNetwork(m, outbound)
}

// fillHysteria2 extracts Hysteria2 connection details into m.
// Xray outbound: {protocol:"hysteria", settings:{version:2,address,port},
//                 streamSettings:{network:"hysteria",security:"tls",hysteriaSettings:{version:2,auth},tlsSettings:{serverName,alpn,allowInsecure}}}
func fillHysteria2(m map[string]interface{}, outbound map[string]interface{}) {
	settings, _ := outbound["settings"].(map[string]interface{})
	if settings != nil {
		m["server"], _ = settings["address"].(string)
		m["port"] = toInt(settings["port"])
	}

	// Auth from streamSettings.hysteriaSettings
	ss, _ := outbound["streamSettings"].(map[string]interface{})
	if ss != nil {
		if hy, ok := ss["hysteriaSettings"].(map[string]interface{}); ok {
			if auth, ok := hy["auth"]; ok {
				m["password"] = fmt.Sprintf("%v", auth)
			}
		}
		// TLS settings from streamSettings.tlsSettings
		if tls, ok := ss["tlsSettings"].(map[string]interface{}); ok {
			if sni, ok := tls["serverName"].(string); ok && sni != "" {
				m["sni"] = sni
			}
			if alpn, ok := tls["alpn"]; ok {
				m["alpn"] = alpn
			}
			if insecure, ok := tls["allowInsecure"].(bool); ok && insecure {
				m["skip-verify"] = true
			}
		}
	}
	m["tls"] = true // hysteria2 always uses TLS
}

// ── Stream settings ────────────────────────────────────────────────────────

// fillStreamSettings handles streamSettings for VLESS (which supports all variants).
func fillStreamSettings(m map[string]interface{}, outbound map[string]interface{}) {
	ss, _ := outbound["streamSettings"].(map[string]interface{})
	if ss == nil {
		return
	}

	// Security (TLS / Reality)
	security, _ := ss["security"].(string)
	switch security {
	case "tls":
		m["tls"] = true
		fillTLSMap(m, ss, "tlsSettings")
	case "reality":
		m["tls"] = true
		fillTLSMap(m, ss, "realitySettings")
		// Reality-specific
		if reality, ok := ss["realitySettings"].(map[string]interface{}); ok {
			if fp, ok := reality["fingerprint"].(string); ok && fp != "" {
				m["fingerprint"] = fp
			}
		}
	case "none", "":
		m["tls"] = false
	}

	// Network
	fillStreamNetwork(m, outbound)
}

// fillTLS extracts TLS settings specifically for trojan.
func fillTLS(m map[string]interface{}, outbound map[string]interface{}) {
	ss, _ := outbound["streamSettings"].(map[string]interface{})
	if ss == nil {
		m["tls"] = false
		return
	}
	security, _ := ss["security"].(string)
	if security == "tls" || security == "" {
		m["tls"] = true
	} else {
		m["tls"] = false
	}
	fillTLSMap(m, ss, "tlsSettings")
}

// fillTLSMap reads tlsSettings or realitySettings into m.
func fillTLSMap(m map[string]interface{}, ss map[string]interface{}, key string) {
	if tls, ok := ss[key].(map[string]interface{}); ok {
		if sni, ok := tls["serverName"].(string); ok && sni != "" {
			m["servername"] = sni
		}
		if fp, ok := tls["fingerprint"].(string); ok && fp != "" {
			m["fingerprint"] = fp
		}
		if alpn, ok := tls["alpn"]; ok {
			m["alpn"] = alpn
		}
	}
}

// fillStreamNetwork reads network-specific transport settings.
// Network options: ws, grpc, tcp (default).
func fillStreamNetwork(m map[string]interface{}, outbound map[string]interface{}) {
	ss, _ := outbound["streamSettings"].(map[string]interface{})
	if ss == nil {
		return
	}

	network, _ := ss["network"].(string)
	if network == "" || network == "tcp" {
		// TCP is the default for Mihomo — no need to set explicitly.
		return
	}
	m["network"] = network

	switch network {
	case "ws":
		if ws, ok := ss["wsSettings"].(map[string]interface{}); ok {
			wsOpts := map[string]interface{}{}
			if path, ok := ws["path"].(string); ok && path != "" {
				wsOpts["path"] = path
			}
			if headers, ok := ws["headers"].(map[string]interface{}); ok {
				if host, ok := headers["Host"].(string); ok && host != "" {
					wsOpts["headers"] = map[string]interface{}{"Host": host}
				}
			}
			if len(wsOpts) > 0 {
				m["ws-opts"] = wsOpts
			}
		}
	case "grpc":
		if grpc, ok := ss["grpcSettings"].(map[string]interface{}); ok {
			if sn, ok := grpc["serviceName"].(string); ok && sn != "" {
				m["grpc-opts"] = map[string]interface{}{
					"grpc-service-name": sn,
				}
			}
		}
	case "hysteria":
		// Hysteria2 network — no extra transport opts needed.
	}
}

// ── Strategy mapping ───────────────────────────────────────────────────────

// mihomoStrategyType maps Xray strategy to Mihomo proxy-group type.
func mihomoStrategyType(xrayStrategy string) string {
	switch xrayStrategy {
	case "all":
		return "select"
	case "random":
		return "random"
	case "roundrobin":
		return "load-balance"
	case "leastping", "leastload":
		return "url-test"
	default:
		return "select"
	}
}

// groupTypeSupportsURL returns true if the Mihomo group type supports
// url/interval parameters (url-test, fallback, load-balance).
func groupTypeSupportsURL(groupType string) bool {
	switch groupType {
	case "url-test", "load-balance", "fallback":
		return true
	default:
		return false
	}
}

// ── Xray routing conversion ────────────────────────────────────────────────

// XrayRule represents a single rule from 05_routing.json.
type XrayRule map[string]interface{}

// mihomoRule represents a single Mihomo rule string like "DOMAIN-SUFFIX,example.com,Proxy".
// Mihomo rules are flat strings, not structured objects.
type mihomoRule string

// convertXrayRoutingRules converts Xray routing rules to Mihomo rules.
// Returns both rules and warning messages for unmappable elements.
func convertXrayRoutingRules(rules []XrayRule, proxyTags []string) ([]mihomoRule, []string) {
	var mRules []mihomoRule
	var warnings []string

	// Build a set of known proxy tags for DIRECT/REJECT detection.
	knownTags := make(map[string]bool)
	for _, tag := range proxyTags {
		knownTags[tag] = true
	}
	knownTags["direct"] = true
	knownTags["block"] = true

	for i, rule := range rules {
		rType, _ := rule["type"].(string)
		if rType == "" {
			rType = "field"
		}

		// Determine the target (outboundTag or balancerTag)
		var target string
		if ot, ok := rule["outboundTag"].(string); ok {
			target = ot
		} else if bt, ok := rule["balancerTag"].(string); ok {
			target = bt
		}

		// Map target to Mihomo policy
		policy := mapTargetToMihomo(target, knownTags)

		// Domain-based rules
		if domains, ok := getStringSlice(rule, "domain"); ok {
			for _, d := range domains {
				mRules = append(mRules, mihomoRule(fmt.Sprintf("DOMAIN,%s,%s", d, policy)))
			}
		}

		// Domain suffix rules
		if suffixes, ok := getStringSlice(rule, "domain_suffix"); ok {
			for _, d := range suffixes {
				// Handle geosite references
				if strings.HasPrefix(d, "geosite:") {
					mRules = append(mRules, mihomoRule(fmt.Sprintf("GEOSITE,%s,%s", strings.TrimPrefix(d, "geosite:"), policy)))
				} else {
					mRules = append(mRules, mihomoRule(fmt.Sprintf("DOMAIN-SUFFIX,%s,%s", d, policy)))
				}
			}
		}

		// Domain keyword rules
		if keywords, ok := getStringSlice(rule, "domain_keyword"); ok {
			for _, d := range keywords {
				mRules = append(mRules, mihomoRule(fmt.Sprintf("DOMAIN-KEYWORD,%s,%s", d, policy)))
			}
		}

		// Domain regex rules
		if regexes, ok := getStringSlice(rule, "domain_regex"); ok {
			for _, d := range regexes {
				mRules = append(mRules, mihomoRule(fmt.Sprintf("DOMAIN-REGEX,%s,%s", d, policy)))
			}
		}

		// IP-based rules
		if ips, ok := getStringSlice(rule, "ip"); ok {
			for _, ip := range ips {
				if strings.HasPrefix(ip, "geoip:") {
					mRules = append(mRules, mihomoRule(fmt.Sprintf("GEOIP,%s,%s", strings.TrimPrefix(ip, "geoip:"), policy)))
				} else if strings.Contains(ip, "/") {
					mRules = append(mRules, mihomoRule(fmt.Sprintf("IP-CIDR,%s,%s", ip, policy)))
				} else {
					mRules = append(mRules, mihomoRule(fmt.Sprintf("IP-CIDR,%s/32,%s", ip, policy)))
				}
			}
		}

		// Port rules
		if port, ok := rule["port"].(string); ok && port != "" {
			mRules = append(mRules, mihomoRule(fmt.Sprintf("DST-PORT,%s,%s", port, policy)))
		}
		if srcPort, ok := rule["source_port"].(string); ok && srcPort != "" {
			mRules = append(mRules, mihomoRule(fmt.Sprintf("SRC-PORT,%s,%s", srcPort, policy)))
		}

		// Network rules — Mihomo doesn't have direct network matching.
		// We skip these with a warning.
		if netVal, ok := rule["network"].(string); ok && netVal != "" {
			warnings = append(warnings, fmt.Sprintf("rule[%d]: network=%q not supported in Mihomo (skipped)", i, netVal))
		}

		// Inbound tag rules
		if inTag, ok := rule["inboundTag"].([]interface{}); ok {
			warnings = append(warnings, fmt.Sprintf("rule[%d]: inboundTag=%v not fully supported in Mihomo (skipped)", i, inTag))
		}

		// If no domain/ip/port conditions, this is likely a catch-all / fallback rule
		if len(mRules) == 0 && i < len(rules)-1 {
			// This rule has no domain/ip conditions we could map — it's probably
			// a network-only or inboundTag-only rule. Skip silently.
			continue
		}
	}

	return mRules, warnings
}

// mapTargetToMihomo converts Xray outboundTag/balancerTag to Mihomo policy.
func mapTargetToMihomo(target string, knownTags map[string]bool) string {
	if target == "" {
		return "DIRECT"
	}
	if target == "direct" || target == "freedom" {
		return "DIRECT"
	}
	if target == "block" || target == "blackhole" {
		return "REJECT"
	}
	if knownTags[target] {
		return target
	}
	// For balancer tags like "default-balancer", map to "Proxy" or return as-is
	if strings.HasSuffix(target, "-balancer") {
		return strings.TrimSuffix(target, "-balancer")
	}
	// Unknown target — use DIRECT to avoid broken rules
	return "DIRECT"
}

// ── Config generation ──────────────────────────────────────────────────────

// MihomoGenerateOptions controls Mihomo config generation.
type MihomoGenerateOptions struct {
	// ConvertXrayRouting enables conversion of Xray 05_routing.json rules to Mihomo rules.
	// If false, only a default MATCH rule is generated.
	ConvertXrayRouting bool
	// XrayRoutingJSON is the raw content of 05_routing.json for routing conversion.
	XrayRoutingJSON []byte
	// ExistingMihomoConfig is the existing config.yaml content for mix-in.
	// Sections managed by subscriptions (proxies, proxy-groups, rules) are replaced.
	ExistingMihomoConfig []byte
}

// GenerateMihomoConfig generates a complete Mihomo config.yaml from subscription data.
//
// It produces three managed sections: proxies, proxy-groups, and rules.
// When existingMihomoConfig is provided, it merges the managed sections into it,
// preserving all other sections (dns, tun, port, etc.).
func GenerateMihomoConfig(proxies []*ProxyEntry, profiles []Profile, opts MihomoGenerateOptions) ([]byte, error) {
	if len(proxies) == 0 {
		return nil, fmt.Errorf("no proxies to generate Mihomo config from")
	}

	var cfg map[string]interface{}

	// Parse existing config if provided
	if len(opts.ExistingMihomoConfig) > 0 {
		if err := yaml.Unmarshal(opts.ExistingMihomoConfig, &cfg); err != nil {
			return nil, fmt.Errorf("failed to parse existing Mihomo config: %w", err)
		}
		// Remove managed sections — they'll be regenerated
		delete(cfg, "proxies")
		delete(cfg, "proxy-groups")
		delete(cfg, "rules")
	} else {
		cfg = make(map[string]interface{})
	}

	// 1. Generate proxies
	mihomoProxies, err := generateProxies(proxies)
	if err != nil {
		return nil, fmt.Errorf("failed to generate proxies: %w", err)
	}
	cfg["proxies"] = mihomoProxies

	// Collect all proxy names (tags) for proxy-group references
	proxyNames := make([]string, 0, len(proxies))
	for _, p := range proxies {
		proxyNames = append(proxyNames, p.Tag)
	}

	// 2. Generate proxy-groups from profiles
	proxyGroups := generateProxyGroups(proxies, profiles, proxyNames)
	cfg["proxy-groups"] = proxyGroups

	// 3. Generate rules
	rules := generateRules(profiles, proxyNames, opts)
	cfg["rules"] = rules

	// Serialize to YAML
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal Mihomo config: %w", err)
	}

	return out, nil
}

// generateProxies converts all ProxyEntry to Mihomo proxy maps.
func generateProxies(proxies []*ProxyEntry) ([]map[string]interface{}, error) {
	result := make([]map[string]interface{}, 0, len(proxies))
	for _, p := range proxies {
		m, err := proxyToMihomo(p)
		if err != nil {
			// Log a warning but don't fail the whole generation
			continue
		}
		result = append(result, m)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("no proxies could be converted to Mihomo format")
	}
	return result, nil
}

// generateProxyGroups creates Mihomo proxy-groups from subscription profiles.
// The default profile generates the main "Proxy" group.
// Additional profiles generate named groups. A "Select" group is always added
// with "Proxy", "DIRECT", and "REJECT" for manual selection.
func generateProxyGroups(proxies []*ProxyEntry, profiles []Profile, proxyNames []string) []map[string]interface{} {
	var groups []map[string]interface{}

	// Find the default profile
	var defaultProfile *Profile
	for i := range profiles {
		if profiles[i].IsDefault && profiles[i].Enabled {
			defaultProfile = &profiles[i]
			break
		}
	}

	// Generate the main "Proxy" group from the default profile
	if defaultProfile != nil {
		// Get filtered proxy names for this profile
		filtered := ApplyFilter(proxies, &defaultProfile.Filter)
		filteredProxies := make([]string, 0, len(filtered))
		for _, p := range filtered {
			filteredProxies = append(filteredProxies, p.Tag)
		}

		groupType := mihomoStrategyType(defaultProfile.Strategy.Type)
		group := map[string]interface{}{
			"name":     "Proxy",
			"type":     groupType,
			"proxies":  filteredProxies,
		}
		if groupTypeSupportsURL(groupType) {
			group["url"] = "http://www.gstatic.com/generate_204"
			group["interval"] = 300
		}
		groups = append(groups, group)
	} else {
		// Fallback: all proxies in a single select group
		groups = append(groups, map[string]interface{}{
			"name":    "Proxy",
			"type":    "select",
			"proxies": proxyNames,
		})
	}

	// Generate named groups for non-default enabled profiles
	for i := range profiles {
		p := &profiles[i]
		if p.IsDefault || !p.Enabled {
			continue
		}

		filtered := ApplyFilter(proxies, &p.Filter)
		names := make([]string, 0, len(filtered))
		for _, fp := range filtered {
			names = append(names, fp.Tag)
		}
		if len(names) == 0 {
			continue
		}

		groupType := mihomoStrategyType(p.Strategy.Type)
		group := map[string]interface{}{
			"name":    p.Name,
			"type":    groupType,
			"proxies": names,
		}
		if groupTypeSupportsURL(groupType) {
			group["url"] = "http://www.gstatic.com/generate_204"
			group["interval"] = 300
		}
		groups = append(groups, group)
	}

	// Add a "Select" group for manual override (if not already present)
	hasSelect := false
	groupNames := make([]string, 0, len(groups))
	for _, g := range groups {
		name, _ := g["name"].(string)
		if name == "Select" {
			hasSelect = true
		}
		groupNames = append(groupNames, name)
	}
	if !hasSelect {
		selectProxies := append([]string{"Proxy"}, "DIRECT", "REJECT")
		groups = append(groups, map[string]interface{}{
			"name":    "Select",
			"type":    "select",
			"proxies": selectProxies,
		})
	}

	return groups
}

// generateRules creates Mihomo rules from profiles and optional Xray routing conversion.
func generateRules(profiles []Profile, proxyNames []string, opts MihomoGenerateOptions) []string {
	var rules []string

	// Option A: Convert Xray routing rules
	if opts.ConvertXrayRouting && len(opts.XrayRoutingJSON) > 0 {
		xrayRules, warnings := parseXrayRoutingJSON(opts.XrayRoutingJSON)
		_ = warnings

		// Collect all proxy tag names for target mapping
		allTags := make([]string, 0, len(proxyNames))
		allTags = append(allTags, proxyNames...)
		allTags = append(allTags, "direct", "block")

		convertedRules, _ := convertXrayRoutingRules(xrayRules, allTags)
		for _, r := range convertedRules {
			rules = append(rules, string(r))
		}
	} else {
		// Option B: Default routing based on profiles
		// Block ads by default
		rules = append(rules, "GEOSITE,category-ads-all,REJECT")

		// GeoIP for LAN and CN
		rules = append(rules, "GEOIP,private,DIRECT,no-resolve")
		rules = append(rules, "GEOIP,CN,DIRECT")

		// GEOSITE rules — direct for common Chinese sites
		rules = append(rules, "GEOSITE,cn,DIRECT")
	}

	// Always end with MATCH rule
	rules = append(rules, "MATCH,Proxy")

	// Deduplicate rules while preserving order
	return dedupeRules(rules)
}

// dedupeRules removes duplicate rules while preserving order.
func dedupeRules(rules []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(rules))
	for _, r := range rules {
		if seen[r] {
			continue
		}
		seen[r] = true
		result = append(result, r)
	}
	return result
}

// ── Xray routing JSON parsing ──────────────────────────────────────────────

// parseXrayRoutingJSON parses 05_routing.json and returns the rules array.
func parseXrayRoutingJSON(data []byte) ([]XrayRule, []string) {
	var warnings []string

	// Try to parse as { "routing": { "rules": [...] } }
	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(data, &wrapper); err != nil {
		warnings = append(warnings, "cannot parse routing JSON")
		return nil, warnings
	}

	routingRaw, ok := wrapper["routing"]
	if !ok {
		// Maybe it's already the routing object
		routingRaw = data
	}

	var routingObj map[string]json.RawMessage
	if err := json.Unmarshal(routingRaw, &routingObj); err != nil {
		warnings = append(warnings, "cannot parse routing object")
		return nil, warnings
	}

	rulesRaw, ok := routingObj["rules"]
	if !ok {
		warnings = append(warnings, "no rules in routing JSON")
		return nil, warnings
	}

	var rules []XrayRule
	if err := json.Unmarshal(rulesRaw, &rules); err != nil {
		warnings = append(warnings, fmt.Sprintf("cannot parse rules array: %v", err))
		return nil, warnings
	}

	return rules, nil
}

// ── Helpers ────────────────────────────────────────────────────────────────

// toInt converts a JSON number (float64) or int to int.
func toInt(v interface{}) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	default:
		return 0
	}
}

// getStringSlice extracts a []string from a rule field that might be
// []interface{}, string, or absent.
func getStringSlice(rule XrayRule, key string) ([]string, bool) {
	v, ok := rule[key]
	if !ok || v == nil {
		return nil, false
	}
	switch val := v.(type) {
	case []interface{}:
		result := make([]string, 0, len(val))
		for _, item := range val {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		if len(result) == 0 {
			return nil, false
		}
		return result, true
	case string:
		if val == "" {
			return nil, false
		}
		return []string{val}, true
	default:
		return nil, false
	}
}

// cleanMap removes nil and zero-value entries from a map.
// Used to keep Mihomo proxy entries compact.
func cleanMap(m map[string]interface{}) {
	for k, v := range m {
		if v == nil {
			delete(m, k)
			continue
		}
		switch val := v.(type) {
		case string:
			if val == "" {
				delete(m, k)
			}
		case int:
			if val == 0 {
				delete(m, k)
			}
		case bool:
			if !val {
				delete(m, k)
			}
		}
	}
}
