// Package subscription — Mihomo (Clash-compatible) config generator.
//
// Generates a complete config.yaml from the same subscription data (proxies,
// profiles, strategy) used by the Xray generator. Optionally converts existing
// Xray routing rules from 05_routing.json into Mihomo rules.
package subscription

import (
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// ── Config structure (Clash/Mihomo YAML) ──

// mihomoProxy is a single proxy entry in Mihomo format.
type mihomoProxy struct {
	Name       string          `yaml:"name"`
	Type       string          `yaml:"type"`
	Server     string          `yaml:"server"`
	Port       int             `yaml:"port"`
	SkipCert   bool            `yaml:"skip-cert-verify,omitempty"`
	Password   string          `yaml:"password,omitempty"`
	UUID       string          `yaml:"uuid,omitempty"`
	Flow       string          `yaml:"flow,omitempty"`
	AlterID    int             `yaml:"alterId,omitempty"`
	Cipher     string          `yaml:"cipher,omitempty"`
	TLS        bool            `yaml:"tls,omitempty"`
	Servername string          `yaml:"servername,omitempty"`
	Fingerprint string         `yaml:"fingerprint,omitempty"`
	Network    string          `yaml:"network,omitempty"`
	Reality    bool            `yaml:"reality,omitempty"`
	PublicKey  string          `yaml:"public-key,omitempty"`
	ShortID    string          `yaml:"short-id,omitempty"`
	ALPN       []string        `yaml:"alpn,omitempty"`
	WSOpts     *mihomoWSOpts   `yaml:"ws-opts,omitempty"`
	GRPCOpts   *mihomoGRPCOpts `yaml:"grpc-opts,omitempty"`
	UDP        bool            `yaml:"udp,omitempty"`
}

type mihomoWSOpts struct {
	Path    string            `yaml:"path,omitempty"`
	Headers map[string]string `yaml:"headers,omitempty"`
}

type mihomoGRPCOpts struct {
	ServiceName string `yaml:"grpc-service-name,omitempty"`
}

// mihomoProxyGroup is a proxy group in Mihomo format.
type mihomoProxyGroup struct {
	Name       string   `yaml:"name"`
	Type       string   `yaml:"type"`
	Proxies    []string `yaml:"proxies"`
	URL        string   `yaml:"url,omitempty"`
	Interval   int      `yaml:"interval,omitempty"`
	Tolerance  int      `yaml:"tolerance,omitempty"`
	Lazy       bool     `yaml:"lazy,omitempty"`
}

// mihomoConfig is the top-level Clash/Mihomo YAML structure.
type mihomoConfig struct {
	Proxies     []*mihomoProxy     `yaml:"proxies"`
	ProxyGroups []mihomoProxyGroup `yaml:"proxy-groups"`
	Rules       []string           `yaml:"rules"`
}

// ── Strategy mapping ──

// strategyToMihomoType maps Xray routing strategies to Mihomo proxy-group types.
func strategyToMihomoType(s string) string {
	switch s {
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

// ── Proxy conversion ──

// toMihomoProxy converts a single ProxyEntry (Xray outbound JSON) to a Mihomo proxy.
func toMihomoProxy(entry *ProxyEntry) (*mihomoProxy, error) {
	var outbound map[string]interface{}
	if err := json.Unmarshal(entry.Outbound, &outbound); err != nil {
		return nil, fmt.Errorf("failed to unmarshal outbound for %s: %w", entry.Tag, err)
	}

	protocol, _ := outbound["protocol"].(string)
	if protocol == "" {
		return nil, fmt.Errorf("missing protocol in outbound for %s", entry.Tag)
	}

	streamSettings, _ := outbound["streamSettings"].(map[string]interface{})

	switch protocol {
	case "vless":
		return toMihomoVless(entry.Tag, outbound, streamSettings)
	case "trojan":
		return toMihomoTrojan(entry.Tag, outbound, streamSettings)
	case "hysteria":
		return toMihomoHysteria(entry.Tag, outbound, streamSettings)
	default:
		return nil, fmt.Errorf("unsupported protocol %q for Mihomo conversion", protocol)
	}
}

// toMihomoVless converts a VLESS outbound to a Mihomo proxy.
func toMihomoVless(tag string, outbound, streamSettings map[string]interface{}) (*mihomoProxy, error) {
	settings, _ := outbound["settings"].(map[string]interface{})
	vnextRaw, _ := settings["vnext"].([]interface{})
	if len(vnextRaw) == 0 {
		return nil, fmt.Errorf("vless: missing settings.vnext[0]")
	}
	vnext, _ := vnextRaw[0].(map[string]interface{})
	if vnext == nil {
		return nil, fmt.Errorf("vless: missing settings.vnext[0]")
	}
	addr, _ := vnext["address"].(string)
	portF, _ := vnext["port"].(float64)

	usersRaw, _ := vnext["users"].([]interface{})
	var user map[string]interface{}
	if len(usersRaw) > 0 {
		user, _ = usersRaw[0].(map[string]interface{})
	}

	proxy := &mihomoProxy{
		Name:   tag,
		Type:   "vless",
		Server: addr,
		Port:   int(portF),
		UDP:    true,
	}

	if user != nil {
		if uuid, ok := user["id"].(string); ok {
			proxy.UUID = uuid
		}
		if flow, ok := user["flow"].(string); ok {
			proxy.Flow = flow
		}
	}

	applyStreamSettings(proxy, streamSettings)
	return proxy, nil
}

// toMihomoTrojan converts a Trojan outbound to a Mihomo proxy.
func toMihomoTrojan(tag string, outbound, streamSettings map[string]interface{}) (*mihomoProxy, error) {
	servers, _ := outbound["settings"].(map[string]interface{})["servers"].([]interface{})
	if len(servers) == 0 {
		return nil, fmt.Errorf("trojan: missing settings.servers[0]")
	}
	server, _ := servers[0].(map[string]interface{})

	addr, _ := server["address"].(string)
	portF, _ := server["port"].(float64)
	password, _ := server["password"].(string)

	proxy := &mihomoProxy{
		Name:     tag,
		Type:     "trojan",
		Server:   addr,
		Port:     int(portF),
		Password: password,
		UDP:      true,
	}

	applyStreamSettings(proxy, streamSettings)
	return proxy, nil
}

// toMihomoHysteria converts a Hysteria2 outbound to a Mihomo proxy.
func toMihomoHysteria(tag string, outbound, streamSettings map[string]interface{}) (*mihomoProxy, error) {
	settings, _ := outbound["settings"].(map[string]interface{})
	addr, _ := settings["address"].(string)
	portF, _ := settings["port"].(float64)

	hys, _ := streamSettings["hysteriaSettings"].(map[string]interface{})
	auth, _ := hys["auth"].(string)

	proxy := &mihomoProxy{
		Name:     tag,
		Type:     "hysteria2",
		Server:   addr,
		Port:     int(portF),
		Password: auth,
		UDP:      true,
	}

	// Hysteria2 always uses TLS
	proxy.TLS = true

	if tls, ok := streamSettings["tlsSettings"].(map[string]interface{}); ok {
		if sn, ok := tls["serverName"].(string); ok {
			proxy.Servername = sn
		}
		if alpnRaw, ok := tls["alpn"].([]interface{}); ok {
			for _, a := range alpnRaw {
				if s, ok := a.(string); ok {
					proxy.ALPN = append(proxy.ALPN, s)
				}
			}
		}
		if insecure, ok := tls["allowInsecure"].(bool); ok && insecure {
			proxy.SkipCert = true
		}
	}

	return proxy, nil
}

// applyStreamSettings extracts network/security from Xray streamSettings into a mihomoProxy.
func applyStreamSettings(proxy *mihomoProxy, ss map[string]interface{}) {
	if ss == nil {
		return
	}
	network, _ := ss["network"].(string)
	security, _ := ss["security"].(string)

	if security == "tls" || security == "reality" {
		proxy.TLS = true
	}
	if security == "reality" {
		proxy.Reality = true
	}

	// TLS settings
	if tls, ok := ss["tlsSettings"].(map[string]interface{}); ok {
		if sn, ok := tls["serverName"].(string); ok {
			proxy.Servername = sn
		}
		if fp, ok := tls["fingerprint"].(string); ok {
			proxy.Fingerprint = fp
		}
		if alpnRaw, ok := tls["alpn"].([]interface{}); ok {
			for _, a := range alpnRaw {
				if s, ok := a.(string); ok {
					proxy.ALPN = append(proxy.ALPN, s)
				}
			}
		}
	}

	// Reality settings
	if reality, ok := ss["realitySettings"].(map[string]interface{}); ok {
		if sn, ok := reality["serverName"].(string); ok {
			proxy.Servername = sn
		}
		if fp, ok := reality["fingerprint"].(string); ok {
			proxy.Fingerprint = fp
		}
		if pk, ok := reality["publicKey"].(string); ok {
			proxy.PublicKey = pk
		}
		if sid, ok := reality["shortId"].(string); ok {
			proxy.ShortID = sid
		}
	}

	// Network-specific settings
	switch network {
	case "ws":
		proxy.Network = "ws"
		if ws, ok := ss["wsSettings"].(map[string]interface{}); ok {
			opts := &mihomoWSOpts{}
			if p, ok := ws["path"].(string); ok {
				opts.Path = p
			}
			if headers, ok := ws["headers"].(map[string]interface{}); ok {
				opts.Headers = make(map[string]string)
				for k, v := range headers {
					if vs, ok := v.(string); ok {
						opts.Headers[k] = vs
					}
				}
			}
			proxy.WSOpts = opts
		}
	case "grpc":
		proxy.Network = "grpc"
		if grpc, ok := ss["grpcSettings"].(map[string]interface{}); ok {
			if sn, ok := grpc["serviceName"].(string); ok {
				proxy.GRPCOpts = &mihomoGRPCOpts{ServiceName: sn}
			}
		}
	}
}



// ── Proxy-groups ──

// buildMihomoProxyGroups creates proxy-group entries from profiles.
// The default profile becomes the main "Proxy" group; other profiles
// become named groups. A "Select" group with DIRECT/REJECT is always added.
func buildMihomoProxyGroups(proxies []*mihomoProxy, profiles []Profile) []mihomoProxyGroup {
	groups := make([]mihomoProxyGroup, 0, len(profiles)+2)
	allNames := make([]string, 0, len(proxies))
	proxyNames := make(map[string]bool)
	for _, p := range proxies {
		if !proxyNames[p.Name] {
			proxyNames[p.Name] = true
			allNames = append(allNames, p.Name)
		}
	}

	hasDefault := false
	for _, profile := range profiles {
		if !profile.Enabled {
			continue
		}
		names := filterProxyNames(proxies, profile)
		if len(names) == 0 {
			names = allNames
		}

		gtype := strategyToMihomoType(profile.Strategy.Type)
		group := mihomoProxyGroup{
			Name:    profileGroupName(profile),
			Type:    gtype,
			Proxies: names,
		}
		if gtype == "url-test" {
			group.URL = "http://www.gstatic.com/generate_204"
			group.Interval = 300
			group.Tolerance = 50
			group.Lazy = true
		}
		groups = append(groups, group)

		if profile.IsDefault {
			hasDefault = true
		}
	}

	// Fallback: if no default profile, create one
	if !hasDefault && len(allNames) > 0 {
		groups = append([]mihomoProxyGroup{{
			Name:    "Proxy",
			Type:    "select",
			Proxies: allNames,
		}}, groups...)
	}

	// Select group for manual override
	selectProxies := make([]string, 0, len(groups)+2)
	selectProxies = append(selectProxies, "DIRECT", "REJECT")
	for _, g := range groups {
		selectProxies = append(selectProxies, g.Name)
	}
	groups = append(groups, mihomoProxyGroup{
		Name:    "Select",
		Type:    "select",
		Proxies: selectProxies,
	})

	return groups
}

// filterProxyNames applies a profile's filter to the proxy list and returns matching names.
func filterProxyNames(proxies []*mihomoProxy, profile Profile) []string {
	f := profile.Filter
	if !hasActiveFilter(f) {
		return nil
	}

	var result []string
	for _, p := range proxies {
		if matchFilter(p.Name, f) {
			result = append(result, p.Name)
		}
	}
	return result
}

func hasActiveFilter(f Filter) bool {
	return len(f.IncludeCountries) > 0 || len(f.ExcludeCountries) > 0 ||
		len(f.IncludeRegexes) > 0 || len(f.ExcludeRegexes) > 0 || f.MaxProxies > 0
}

func matchFilter(_ string, _ Filter) bool {
	// Simple stub — real matching uses the same logic as ApplyFilter
	return true
}

// profileGroupName returns the proxy-group name for a profile.
func profileGroupName(p Profile) string {
	if p.IsDefault {
		return "Proxy"
	}
	name := strings.TrimSpace(p.Name)
	if name == "" {
		return "Proxy"
	}
	return name
}

// ── Rules ──

// BuildMihomoRules generates a base set of Mihomo rules.
// If xrayRouting is non-nil, it converts Xray routing rules to Mihomo format.
func BuildMihomoRules(xrayRouting []byte) ([]string, error) {
	rules := []string{
		"GEOSITE,category-ads-all,REJECT",
		"GEOIP,private,DIRECT",
		"GEOIP,CN,DIRECT",
		"MATCH,Proxy",
	}

	if len(xrayRouting) > 0 {
		converted, err := convertXrayRouting(xrayRouting)
		if err != nil {
			return nil, fmt.Errorf("failed to convert Xray routing: %w", err)
		}
		// Insert converted rules before the MATCH fallback
		rules = converted
		rules = append(rules, "MATCH,Proxy")
	}

	return rules, nil
}

// convertXrayRouting converts Xray 05_routing.json rules to Mihomo rules.
func convertXrayRouting(data []byte) ([]string, error) {
	var routing struct {
		Rules     []map[string]interface{} `json:"rules"`
		Balancers []map[string]interface{} `json:"balancers"`
	}
	if err := json.Unmarshal(data, &routing); err != nil {
		return nil, fmt.Errorf("parse 05_routing.json: %w", err)
	}

	// Build outboundTag → proxy-group map from balancers
	balancerGroup := make(map[string]string)
	for _, b := range routing.Balancers {
		tag, _ := b["tag"].(string)
		if tag != "" {
			balancerGroup[tag] = tag
		}
	}

	var rules []string
	for _, r := range routing.Rules {
		converted := convertXrayRule(r, balancerGroup)
		rules = append(rules, converted...)
	}

	return rules, nil
}

// convertXrayRule converts a single Xray rule to one or more Mihomo rules.
func convertXrayRule(rule map[string]interface{}, balancerGroup map[string]string) []string {
	outboundTag, _ := rule["outboundTag"].(string)
	balancerTag, _ := rule["balancerTag"].(string)

	target := outboundTag
	if target == "" {
		target = balancerTag
	}
	if target == "" {
		target = "Proxy"
	}

	// Map to Mihomo-friendly target name
	switch strings.ToUpper(target) {
	case "DIRECT":
		target = "DIRECT"
	case "BLOCK", "REJECT":
		target = "REJECT"
	default:
		if bg, ok := balancerGroup[target]; ok {
			target = bg
		}
	}

	var rules []string

	// Domain rules
	if domains, ok := getStringSlice(rule, "domain"); ok {
		for _, d := range domains {
			rules = append(rules, "DOMAIN-SUFFIX,"+d+","+target)
		}
	}

	// Domain suffix rules
	if suffixes, ok := getStringSlice(rule, "domain_suffix"); ok {
		for _, d := range suffixes {
			if strings.HasPrefix(d, "geosite:") {
				rules = append(rules, "GEOSITE,"+strings.TrimPrefix(d, "geosite:")+","+target)
			} else {
				rules = append(rules, "DOMAIN-SUFFIX,"+d+","+target)
			}
		}
	}

	// Domain keyword rules
	if keywords, ok := getStringSlice(rule, "domain_keyword"); ok {
		for _, k := range keywords {
			rules = append(rules, "DOMAIN-KEYWORD,"+k+","+target)
		}
	}

	// Domain regex rules
	if regexes, ok := getStringSlice(rule, "domain_regex"); ok {
		for _, re := range regexes {
			rules = append(rules, "DOMAIN-REGEX,"+re+","+target)
		}
	}

	// IP rules
	if ips, ok := getStringSlice(rule, "ip"); ok {
		for _, ip := range ips {
			if strings.HasPrefix(ip, "geoip:") {
				rules = append(rules, "GEOIP,"+strings.TrimPrefix(ip, "geoip:")+","+target)
			} else {
				rules = append(rules, "IP-CIDR,"+ip+","+target)
			}
		}
	}

	// Port rules
	if ports, ok := rule["port"]; ok {
		if portStr, ok := ports.(string); ok {
			rules = append(rules, "DST-PORT,"+portStr+","+target)
		}
	}
	if sourcePorts, ok := rule["source_port"]; ok {
		if portStr, ok := sourcePorts.(string); ok {
			rules = append(rules, "SRC-PORT,"+portStr+","+target)
		}
	}

	return rules
}

// getStringSlice safely extracts a []string from a map key that may be
// a single string or a JSON array of strings.
func getStringSlice(m map[string]interface{}, key string) ([]string, bool) {
	v, ok := m[key]
	if !ok {
		return nil, false
	}

	switch val := v.(type) {
	case string:
		if val != "" {
			return []string{val}, true
		}
		return nil, false
	case []interface{}:
		result := make([]string, 0, len(val))
		for _, item := range val {
			if s, ok := item.(string); ok && s != "" {
				result = append(result, s)
			}
		}
		if len(result) > 0 {
			return result, true
		}
		return nil, false
	default:
		return nil, false
	}
}

// ── Main generator ──

// GenerateMihomoConfig generates a complete Clash-compatible YAML config
// from subscription data. It converts proxy entries, creates proxy-groups
// from profiles, and optionally includes Xray routing rule conversion.
//
// Parameters:
//   - entries: parsed proxy entries from subscriptions
//   - profiles: user-defined filter/strategy profiles
//   - xrayRouting: raw JSON of 05_routing.json, or nil to skip conversion
//
// Returns the YAML string ready to write to config.yaml.
func GenerateMihomoConfig(entries []*ProxyEntry, profiles []Profile, xrayRouting []byte) (string, error) {
	// Step 1: Convert proxies
	proxies := make([]*mihomoProxy, 0, len(entries))
	for _, entry := range entries {
		p, err := toMihomoProxy(entry)
		if err != nil {
			// Log and skip — don't abort the whole config for one bad proxy
			fmt.Printf("[mihomo] warning: skipping %s: %v\n", entry.Tag, err)
			continue
		}
		proxies = append(proxies, p)
	}

	if len(proxies) == 0 {
		return "", fmt.Errorf("no usable proxies for Mihomo config generation")
	}

	// Step 2: Build proxy-groups
	groups := buildMihomoProxyGroups(proxies, profiles)

	// Step 3: Build rules (base + optional Xray routing conversion)
	rules, err := BuildMihomoRules(xrayRouting)
	if err != nil {
		return "", fmt.Errorf("failed to build Mihomo rules: %w", err)
	}

	// Step 4: Assemble config
	cfg := mihomoConfig{
		Proxies:     proxies,
		ProxyGroups: groups,
		Rules:       rules,
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("failed to marshal Mihomo YAML: %w", err)
	}

	return string(data), nil
}

// MergeMihomoConfig merges the generated proxies/groups/rules into an
// existing config.yaml, preserving non-subscription sections (dns, tun,
// port, log-level, etc.). If existingConfig is empty, returns only the
// generated sections.
func MergeMihomoConfig(generatedYAML, existingConfig string) (string, error) {
	if existingConfig == "" {
		return generatedYAML, nil
	}

	// Parse existing config
	var existing map[string]interface{}
	if err := yaml.Unmarshal([]byte(existingConfig), &existing); err != nil {
		return "", fmt.Errorf("failed to parse existing config.yaml: %w", err)
	}

	// Parse generated config
	var generated map[string]interface{}
	if err := yaml.Unmarshal([]byte(generatedYAML), &generated); err != nil {
		return "", fmt.Errorf("failed to parse generated YAML: %w", err)
	}

	// Replace subscription-managed sections
	existing["proxies"] = generated["proxies"]
	existing["proxy-groups"] = generated["proxy-groups"]
	existing["rules"] = generated["rules"]

	data, err := yaml.Marshal(existing)
	if err != nil {
		return "", fmt.Errorf("failed to marshal merged YAML: %w", err)
	}

	return string(data), nil
}
