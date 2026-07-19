package subscription

import (
	"encoding/json"
	"strings"
	"testing"
)

// helper: parse URIs into ProxyEntry list with tags
func makeProxies() []*ProxyEntry {
	uris := []string{
		"vless://uuid1@1.2.3.4:8443?encryption=none&flow=xtls-rprx-vision&type=tcp&security=reality&sni=de.example.com&fp=chrome&pbk=key1&sid=abc#%F0%9F%87%A9%F0%9F%87%AA%20%E2%9A%A1%EF%B8%8F%20Germany",
		"vless://uuid2@5.6.7.8:8443?encryption=none&flow=xtls-rprx-vision&type=tcp&security=reality&sni=nl.example.com&fp=edge&pbk=key2&sid=def#%F0%9F%87%B3%F0%9F%87%B1%20%E2%9A%A1%EF%B8%8F%20Netherlands",
		"vless://uuid3@9.10.11.12:8443?encryption=none&flow=xtls-rprx-vision&type=tcp&security=reality&sni=ee.example.com&fp=qq&pbk=key3&sid=ghi#%F0%9F%87%AA%F0%9F%87%AA%20%E2%9A%A1%EF%B8%8F%20Estonia",
	}
	entries, _ := ParseProxiesFromURIs(uris)
	return entries
}

// defaultProfiles creates a []Profile with a single default profile using the given strategy.
func defaultProfiles(strategy RoutingStrategy) []Profile {
	return []Profile{
		{ID: "default", Name: "Default", Enabled: true, IsDefault: true, Strategy: strategy},
	}
}

// --- GenerateOutboundsJSON ---

func TestGenerateOutboundsJSON_Basic(t *testing.T) {
	proxies := makeProxies()
	data, err := GenerateOutboundsJSON(proxies, 0)
	if err != nil {
		t.Fatalf("GenerateOutboundsJSON failed: %v", err)
	}

	var result map[string]json.RawMessage
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("failed to parse output JSON: %v", err)
	}

	var outbounds []map[string]interface{}
	if err := json.Unmarshal(result["outbounds"], &outbounds); err != nil {
		t.Fatalf("failed to parse outbounds: %v", err)
	}

	// Should be: 3 proxies + direct + block = 5
	if len(outbounds) != 5 {
		t.Fatalf("expected 5 outbounds (3+direct+block), got %d", len(outbounds))
	}

	// First proxy should have tag "proxy"
	firstTag, _ := outbounds[0]["tag"].(string)
	if firstTag != "proxy" {
		t.Errorf("first outbound should have tag 'proxy', got %q", firstTag)
	}

	// Second proxy should have its generated tag (proxy-de-2 or similar)
	secondTag, _ := outbounds[1]["tag"].(string)
	if !strings.HasPrefix(secondTag, "proxy-") {
		t.Errorf("second outbound should have proxy-* tag, got %q", secondTag)
	}
	if secondTag == "proxy" {
		t.Error("second outbound should NOT have tag 'proxy'")
	}

	// Check direct
	directTag, _ := outbounds[3]["tag"].(string)
	if directTag != "direct" {
		t.Errorf("expected 'direct' at index 3, got %q", directTag)
	}

	// Check block
	blockTag, _ := outbounds[4]["tag"].(string)
	if blockTag != "block" {
		t.Errorf("expected 'block' at index 4, got %q", blockTag)
	}
}

func TestGenerateOutboundsJSON_BlockHasHTTPResponse(t *testing.T) {
	proxies := makeProxies()
	data, err := GenerateOutboundsJSON(proxies, 0)
	if err != nil {
		t.Fatalf("GenerateOutboundsJSON failed: %v", err)
	}

	var result struct {
		Outbounds []struct {
			Tag      string `json:"tag"`
			Protocol string `json:"protocol"`
			Settings struct {
				Response struct {
					Type string `json:"type"`
				} `json:"response"`
			} `json:"settings"`
		} `json:"outbounds"`
	}
	json.Unmarshal(data, &result)

	var block struct {
		Tag      string `json:"tag"`
		Protocol string `json:"protocol"`
		Settings struct {
			Response struct {
				Type string `json:"type"`
			} `json:"response"`
		} `json:"settings"`
	}
	for _, o := range result.Outbounds {
		if o.Tag == "block" {
			block = o
			break
		}
	}

	if block.Protocol != "blackhole" {
		t.Errorf("block protocol should be 'blackhole', got %q", block.Protocol)
	}
	if block.Settings.Response.Type != "http" {
		t.Errorf("block response type should be 'http', got %q", block.Settings.Response.Type)
	}
}

func TestGenerateOutboundsJSON_EmptyProxies(t *testing.T) {
	_, err := GenerateOutboundsJSON([]*ProxyEntry{}, 0)
	if err == nil {
		t.Fatal("expected error for empty proxy list")
	}
}

func TestGenerateOutboundsJSON_MuxPresent(t *testing.T) {
	proxies := makeProxies()
	data, err := GenerateOutboundsJSON(proxies, 0)
	if err != nil {
		t.Fatalf("GenerateOutboundsJSON failed: %v", err)
	}

	var result struct {
		Outbounds []struct {
			Tag string      `json:"tag"`
			Mux interface{} `json:"mux"`
		} `json:"outbounds"`
	}
	json.Unmarshal(data, &result)

	// First proxy should have mux settings
	if result.Outbounds[0].Mux == nil {
		t.Error("first outbound should have mux settings")
	}
}

// --- GenerateRoutingJSON ---

func TestGenerateRoutingJSON_StrategyAll(t *testing.T) {
	proxies := makeProxies()
	profiles := defaultProfiles(RoutingStrategy{Type: "all"})

	data, err := GenerateRoutingJSON(proxies, profiles, nil)
	if err != nil {
		t.Fatalf("GenerateRoutingJSON failed: %v", err)
	}

	var result map[string]json.RawMessage
	json.Unmarshal(data, &result)

	var routing struct {
		DomainStrategy string                   `json:"domainStrategy"`
		Balancers      []map[string]interface{} `json:"balancers"`
		Rules          []map[string]interface{} `json:"rules"`
	}
	json.Unmarshal(result["routing"], &routing)

	// No balancers for "all" strategy
	if len(routing.Balancers) != 0 {
		t.Error("strategy 'all' should not have balancers")
	}

	// Last rule should use outboundTag "proxy"
	lastRule := routing.Rules[len(routing.Rules)-1]
	if tag, ok := lastRule["outboundTag"].(string); !ok || tag != "proxy" {
		t.Errorf("last rule should have outboundTag 'proxy', got %v", lastRule["outboundTag"])
	}

	// Should have ads blocking rule
	var hasBlockRule bool
	for _, rule := range routing.Rules {
		if tag, ok := rule["outboundTag"].(string); ok && tag == "block" {
			hasBlockRule = true
		}
	}
	if !hasBlockRule {
		t.Error("expected a block rule for ads")
	}
}

func TestGenerateRoutingJSON_StrategyRandom(t *testing.T) {
	proxies := makeProxies()
	profiles := defaultProfiles(RoutingStrategy{Type: "random"})

	data, err := GenerateRoutingJSON(proxies, profiles, nil)
	if err != nil {
		t.Fatalf("GenerateRoutingJSON failed: %v", err)
	}

	var result map[string]json.RawMessage
	json.Unmarshal(data, &result)

	var routing struct {
		Balancers []struct {
			Tag      string                 `json:"tag"`
			Selector []string               `json:"selector"`
			Strategy map[string]interface{} `json:"strategy"`
		} `json:"balancers"`
		Rules []map[string]interface{} `json:"rules"`
	}
	json.Unmarshal(result["routing"], &routing)

	if len(routing.Balancers) != 1 {
		t.Fatalf("expected 1 balancer, got %d", len(routing.Balancers))
	}

	balancer := routing.Balancers[0]
	if balancer.Tag != "default-balancer" {
		t.Errorf("expected balancer tag 'default-balancer', got %q", balancer.Tag)
	}
	if len(balancer.Selector) != 3 {
		t.Errorf("expected selector with 3 concrete tags (no filter = all pass), got %v", balancer.Selector)
	}
	// First proxy gets tag "proxy" (not its original tag), rest get "proxy-*"
	hasProxy := false
	hasProxyPrefix := 0
	for _, s := range balancer.Selector {
		switch {
		case s == "proxy":
			hasProxy = true
		case strings.HasPrefix(s, "proxy-"):
			hasProxyPrefix++
		default:
			t.Errorf("expected 'proxy' or 'proxy-*' tag in selector, got %q", s)
		}
	}
	if !hasProxy {
		t.Error("expected 'proxy' in balancer selector (for the renamed first outbound)")
	}
	if hasProxyPrefix != 2 {
		t.Errorf("expected 2 proxy-* tags in selector (for the non-first proxies), got %d", hasProxyPrefix)
	}
	if balancer.Strategy["type"] != "random" {
		t.Errorf("expected strategy type 'random', got %v", balancer.Strategy["type"])
	}

	// Rules: ad-block + fallback balancer rule
	if len(routing.Rules) != 2 {
		t.Errorf("expected 2 rules (ad-block + fallback), got %d", len(routing.Rules))
	}
	// Last rule should be the fallback balancer rule
	lastRule := routing.Rules[len(routing.Rules)-1]
	if btag, ok := lastRule["balancerTag"].(string); !ok || btag != "default-balancer" {
		t.Errorf("last rule should have balancerTag 'default-balancer', got %v", lastRule["balancerTag"])
	}
}

func TestGenerateRoutingJSON_StrategyRoundRobin(t *testing.T) {
	proxies := makeProxies()
	profiles := defaultProfiles(RoutingStrategy{Type: "roundrobin"})

	data, err := GenerateRoutingJSON(proxies, profiles, nil)
	if err != nil {
		t.Fatalf("GenerateRoutingJSON failed: %v", err)
	}

	var result map[string]json.RawMessage
	json.Unmarshal(data, &result)

	var routing struct {
		Balancers []struct {
			Strategy map[string]interface{} `json:"strategy"`
		} `json:"balancers"`
	}
	json.Unmarshal(result["routing"], &routing)

	if routing.Balancers[0].Strategy["type"] != "roundrobin" {
		t.Errorf("expected strategy 'roundrobin', got %v", routing.Balancers[0].Strategy["type"])
	}
}

func TestGenerateRoutingJSON_StrategyLeastPing(t *testing.T) {
	proxies := makeProxies()
	profiles := defaultProfiles(RoutingStrategy{Type: "leastping"})

	data, err := GenerateRoutingJSON(proxies, profiles, nil)
	if err != nil {
		t.Fatalf("GenerateRoutingJSON failed: %v", err)
	}

	var result map[string]json.RawMessage
	json.Unmarshal(data, &result)

	var routing struct {
		Balancers []struct {
			Strategy map[string]interface{} `json:"strategy"`
		} `json:"balancers"`
	}
	json.Unmarshal(result["routing"], &routing)

	if routing.Balancers[0].Strategy["type"] != "leastping" {
		t.Errorf("expected strategy 'leastping', got %v", routing.Balancers[0].Strategy["type"])
	}
}

func TestGenerateRoutingJSON_StrategyLeastLoad(t *testing.T) {
	proxies := makeProxies()
	profiles := defaultProfiles(RoutingStrategy{Type: "leastload"})

	data, err := GenerateRoutingJSON(proxies, profiles, nil)
	if err != nil {
		t.Fatalf("GenerateRoutingJSON failed: %v", err)
	}

	var result map[string]json.RawMessage
	json.Unmarshal(data, &result)

	var routing struct {
		Balancers []struct {
			Strategy map[string]interface{} `json:"strategy"`
		} `json:"balancers"`
	}
	json.Unmarshal(result["routing"], &routing)

	if routing.Balancers[0].Strategy["type"] != "leastload" {
		t.Errorf("expected strategy 'leastload', got %v", routing.Balancers[0].Strategy["type"])
	}
}

func TestGenerateRoutingJSON_WithExistingRouting(t *testing.T) {
	proxies := makeProxies()
	profiles := defaultProfiles(RoutingStrategy{Type: "all"})

	existing := `{
		"domainStrategy": "AsIs",
		"rules": [
			{"type":"field","domain":["geosite:private"],"outboundTag":"direct"},
			{"type":"field","outboundTag":"proxy","network":"tcp,udp"},
			{"type":"field","domain":["geosite:category-ads-all"],"outboundTag":"block"}
		]
	}`

	data, err := GenerateRoutingJSON(proxies, profiles, json.RawMessage(existing))
	if err != nil {
		t.Fatalf("GenerateRoutingJSON failed: %v", err)
	}

	var result map[string]json.RawMessage
	json.Unmarshal(data, &result)

	var routing struct {
		DomainStrategy string                   `json:"domainStrategy"`
		Rules          []map[string]interface{} `json:"rules"`
	}
	json.Unmarshal(result["routing"], &routing)

	// Should preserve domainStrategy from existing
	if routing.DomainStrategy != "AsIs" {
		t.Errorf("expected 'AsIs' domain strategy, got %q", routing.DomainStrategy)
	}

	// Should preserve block and direct rules, replace old proxy rule with new
	var proxyRules, blockRules, directRules int
	for _, rule := range routing.Rules {
		if tag, ok := rule["outboundTag"].(string); ok {
			switch tag {
			case "proxy":
				proxyRules++
			case "block":
				blockRules++
			case "direct":
				directRules++
			}
		}
	}

	if proxyRules != 1 {
		t.Errorf("expected 1 proxy rule, got %d", proxyRules)
	}
	if blockRules != 1 {
		t.Errorf("expected 1 block rule, got %d", blockRules)
	}
	if directRules != 1 {
		t.Errorf("expected 1 direct rule, got %d", directRules)
	}
}

func TestGenerateRoutingJSON_ExistingWithBalancerSwitch(t *testing.T) {
	proxies := makeProxies()
	profiles := defaultProfiles(RoutingStrategy{Type: "random"})

	// Existing routing has old balancer rules — they should be preserved (no replace flag)
	existing := `{
		"domainStrategy": "IPIfNonMatch",
		"rules": [
			{"type":"field","domain":["geosite:category-ads-all"],"outboundTag":"block"},
			{"type":"field","balancerTag":"proxy-balancer","network":"tcp,udp"}
		]
	}`

	data, err := GenerateRoutingJSON(proxies, profiles, json.RawMessage(existing))
	if err != nil {
		t.Fatalf("GenerateRoutingJSON failed: %v", err)
	}

	var result map[string]json.RawMessage
	json.Unmarshal(data, &result)

	var routing struct {
		Balancers []struct {
			Tag string `json:"tag"`
		} `json:"balancers"`
		Rules []map[string]interface{} `json:"rules"`
	}
	json.Unmarshal(result["routing"], &routing)

	// Should have exactly one balancer (from default profile)
	if len(routing.Balancers) != 1 {
		t.Errorf("expected 1 balancer, got %d", len(routing.Balancers))
	}

	// Should have block rule + existing balancer rule (preserved)
	var hasBlock, hasBalancerRule bool
	for _, rule := range routing.Rules {
		if tag, ok := rule["outboundTag"].(string); ok && tag == "block" {
			hasBlock = true
		}
		if _, ok := rule["balancerTag"]; ok {
			hasBalancerRule = true
		}
	}
	if !hasBlock {
		t.Error("expected a block rule")
	}
	if !hasBalancerRule {
		t.Error("expected a balancer rule")
	}
}

func TestGenerateRoutingJSON_EmptyProxies(t *testing.T) {
	profiles := defaultProfiles(RoutingStrategy{Type: "all"})
	_, err := GenerateRoutingJSON([]*ProxyEntry{}, profiles, nil)
	if err == nil {
		t.Fatal("expected error for empty proxy list")
	}
}

// --- Cross-check: outbound tags vs balancer selectors (#G1 regression guard) ---

func TestOutboundTagBalancersMatchOutbounds(t *testing.T) {
	proxies := makeProxies()
	profiles := defaultProfiles(RoutingStrategy{Type: "leastping"})

	// Generate both outbounds and routing
	obData, err := GenerateOutboundsJSON(proxies, 0)
	if err != nil {
		t.Fatalf("GenerateOutboundsJSON failed: %v", err)
	}

	rtData, err := GenerateRoutingJSON(proxies, profiles, nil)
	if err != nil {
		t.Fatalf("GenerateRoutingJSON failed: %v", err)
	}

	// Parse outbound tags
	var obWrap map[string]json.RawMessage
	if err := json.Unmarshal(obData, &obWrap); err != nil {
		t.Fatal(err)
	}
	var outbounds []map[string]interface{}
	if err := json.Unmarshal(obWrap["outbounds"], &outbounds); err != nil {
		t.Fatal(err)
	}
	outboundTags := make(map[string]bool)
	for _, o := range outbounds {
		if tag, ok := o["tag"].(string); ok {
			outboundTags[tag] = true
		}
	}

	// Parse balancer selectors
	var rtWrap map[string]json.RawMessage
	if err := json.Unmarshal(rtData, &rtWrap); err != nil {
		t.Fatal(err)
	}
	var routing struct {
		Balancers []struct {
			Tag      string   `json:"tag"`
			Selector []string `json:"selector"`
		} `json:"balancers"`
	}
	if err := json.Unmarshal(rtWrap["routing"], &routing); err != nil {
		t.Fatal(err)
	}

	// Every balancer selector tag MUST exist in outbounds
	for _, b := range routing.Balancers {
		for _, sel := range b.Selector {
			if !outboundTags[sel] {
				t.Errorf("DANGLING balancer selector %q in balancer %q does not exist in outbounds! (first proxy was renamed to 'proxy' but selector referenced its original tag)", sel, b.Tag)
			}
		}
	}
}

func TestGenerateRoutingJSON_FirstProxyInBalancerUsesProxyTag(t *testing.T) {
	proxies := makeProxies()
	profiles := defaultProfiles(RoutingStrategy{Type: "random"})

	data, err := GenerateRoutingJSON(proxies, profiles, nil)
	if err != nil {
		t.Fatalf("GenerateRoutingJSON failed: %v", err)
	}

	var result map[string]json.RawMessage
	json.Unmarshal(data, &result)

	var routing struct {
		Balancers []struct {
			Tag      string   `json:"tag"`
			Selector []string `json:"selector"`
		} `json:"balancers"`
	}
	json.Unmarshal(result["routing"], &routing)

	if len(routing.Balancers) == 0 {
		t.Fatal("expected at least one balancer")
	}

	defaultBalancer := routing.Balancers[0]
	foundProxy := false
	for _, s := range defaultBalancer.Selector {
		if s == "proxy" {
			foundProxy = true
			break
		}
	}
	if !foundProxy {
		t.Errorf("default balancer selector should contain 'proxy' (for the first/renamed outbound), got %v", defaultBalancer.Selector)
	}

	// Verify first proxy's original tag is NOT in the selector (it was renamed to "proxy")
	originalTag := proxies[0].Tag
	for _, s := range defaultBalancer.Selector {
		if s == originalTag {
			t.Errorf("first proxy's original tag %q should NOT be in balancer selector (it was renamed to 'proxy' in outbounds)", originalTag)
		}
	}
}

// --- GenerateObservatoryJSON ---

func TestGenerateObservatoryJSON(t *testing.T) {
	data, err := GenerateObservatoryJSON()
	if err != nil {
		t.Fatalf("GenerateObservatoryJSON failed: %v", err)
	}

	var result struct {
		Observatory struct {
			SubjectSelector []string `json:"subjectSelector"`
			ProbeURL        string   `json:"probeURL"`
			ProbeInterval   string   `json:"probeInterval"`
		} `json:"observatory"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("failed to parse observatory JSON: %v", err)
	}

	if len(result.Observatory.SubjectSelector) != 1 || result.Observatory.SubjectSelector[0] != "proxy" {
		t.Errorf("expected subjectSelector ['proxy'], got %v", result.Observatory.SubjectSelector)
	}
	if result.Observatory.ProbeURL != "https://www.google.com/generate_204" {
		t.Errorf("unexpected probeURL: %q", result.Observatory.ProbeURL)
	}
	if result.Observatory.ProbeInterval != "30s" {
		t.Errorf("unexpected probeInterval: %q", result.Observatory.ProbeInterval)
	}
}

// --- NeedsObservatory ---

func TestNeedsObservatory(t *testing.T) {
	tests := []struct {
		strategy string
		expected bool
	}{
		{"all", false},
		{"random", false},
		{"roundrobin", false},
		{"leastping", true},
		{"leastload", true},
		{"", false},
		{"unknown", false},
	}

	for _, tt := range tests {
		profiles := []Profile{{Enabled: true, Strategy: RoutingStrategy{Type: tt.strategy}}}
		result := NeedsObservatory(profiles)
		if result != tt.expected {
			t.Errorf("NeedsObservatory(profile with strategy=%q) = %v, expected %v", tt.strategy, result, tt.expected)
		}
	}
}

// --- Output formatting ---

func TestGenerateOutboundsJSON_ValidJSON(t *testing.T) {
	proxies := makeProxies()
	data, err := GenerateOutboundsJSON(proxies, 0)
	if err != nil {
		t.Fatalf("GenerateOutboundsJSON failed: %v", err)
	}

	if !json.Valid(data) {
		t.Error("generated outbounds JSON is not valid")
	}

	// Should be pretty-printed (indented)
	if !strings.Contains(string(data), "\n") {
		t.Error("expected pretty-printed JSON with newlines")
	}
}

func TestGenerateRoutingJSON_ValidJSON(t *testing.T) {
	proxies := makeProxies()
	profiles := defaultProfiles(RoutingStrategy{Type: "all"})
	data, err := GenerateRoutingJSON(proxies, profiles, nil)
	if err != nil {
		t.Fatalf("GenerateRoutingJSON failed: %v", err)
	}

	if !json.Valid(data) {
		t.Error("generated routing JSON is not valid")
	}
}

func TestGenerateObservatoryJSON_ValidJSON(t *testing.T) {
	data, err := GenerateObservatoryJSON()
	if err != nil {
		t.Fatalf("GenerateObservatoryJSON failed: %v", err)
	}

	if !json.Valid(data) {
		t.Error("generated observatory JSON is not valid")
	}
}

func TestGenerateRoutingJSON_PreservesRulesRaw(t *testing.T) {
	proxies := []*ProxyEntry{
		{Tag: "proxy-de", Outbound: json.RawMessage(`{"tag":"proxy-de","protocol":"vless"}`)},
	}

	// Simulate existing routing with custom rules that user edited in editor
	existing := json.RawMessage(`{
		"routing": {
			"domainStrategy": "IPIfNonMatch",
			"rules": [
				{"type": "field", "domain": ["geosite:category-ads-all"], "outboundTag": "block"},
				{"type": "field", "ip": ["geoip:private"], "outboundTag": "direct"},
				{"type": "field", "balancerTag": "proxy-balancer", "network": "tcp,udp"},
				{"type": "field", "outboundTag": "proxy", "network": "tcp,udp"},
				{"type": "field", "port": [80, 443], "outboundTag": "proxy"}
			]
		}
	}`)

	start := RoutingStrategy{Type: "random"}
	profiles := defaultProfiles(start)
	data, err := GenerateRoutingJSON(proxies, profiles, existing)
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}

	routing, ok := result["routing"].(map[string]interface{})
	if !ok {
		t.Fatal("missing routing key")
	}

	// ALL 5 rules must be preserved
	rules, ok := routing["rules"].([]interface{})
	if !ok {
		t.Fatal("missing rules")
	}
	if len(rules) != 5 {
		t.Errorf("expected 5 rules preserved, got %d", len(rules))
	}

	// Balancer should be regenerated
	balancers, ok := routing["balancers"].([]interface{})
	if !ok {
		t.Fatal("missing balancers")
	}
	if len(balancers) != 1 {
		t.Errorf("expected 1 balancer, got %d", len(balancers))
	}
	balancerMap := balancers[0].(map[string]interface{})
	strategyMap := balancerMap["strategy"].(map[string]interface{})
	if strategyMap["type"] != "random" {
		t.Errorf("expected strategy type 'random', got %v", strategyMap["type"])
	}
}

func TestGenerateRoutingJSON_StrategyAll_NoBalancer(t *testing.T) {
	proxies := []*ProxyEntry{
		{Tag: "proxy-de", Outbound: json.RawMessage(`{"tag":"proxy-de","protocol":"vless"}`)},
	}

	existing := json.RawMessage(`{
		"routing": {
			"domainStrategy": "IPIfNonMatch",
			"rules": [{"type": "field", "outboundTag": "proxy", "network": "tcp,udp"}],
			"balancers": [{"tag": "proxy-balancer", "selector": ["proxy-"]}]
		}
	}`)

	profiles := defaultProfiles(RoutingStrategy{Type: "all"})
	data, err := GenerateRoutingJSON(proxies, profiles, existing)
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}

	routing := result["routing"].(map[string]interface{})

	// Rules preserved as-is
	rules := routing["rules"].([]interface{})
	if len(rules) != 1 {
		t.Errorf("expected 1 rule, got %d", len(rules))
	}

	// No balancer for "all" strategy (omitempty — key should not exist)
	if _, exists := routing["balancers"]; exists {
		t.Errorf("expected no balancers key for strategy 'all', got: %v", routing["balancers"])
	}
}

func TestGenerateRoutingJSON_PreservesDomainStrategy(t *testing.T) {
	proxies := []*ProxyEntry{
		{Tag: "proxy-de", Outbound: json.RawMessage(`{"tag":"proxy-de","protocol":"vless"}`)},
	}

	existing := json.RawMessage(`{
		"routing": {
			"domainStrategy": "IPOnDemand",
			"rules": [{"type": "field", "outboundTag": "direct"}]
		}
	}`)

	profiles := defaultProfiles(RoutingStrategy{Type: "random"})
	data, err := GenerateRoutingJSON(proxies, profiles, existing)
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}

	routing := result["routing"].(map[string]interface{})
	if routing["domainStrategy"] != "IPOnDemand" {
		t.Errorf("expected domainStrategy 'IPOnDemand', got %v", routing["domainStrategy"])
	}
}

func TestGenerateRoutingJSON_NoExistingRules(t *testing.T) {
	proxies := []*ProxyEntry{
		{Tag: "proxy-de", Outbound: json.RawMessage(`{"tag":"proxy-de","protocol":"vless"}`)},
		{Tag: "proxy-us", Outbound: json.RawMessage(`{"tag":"proxy-us","protocol":"vless"}`)},
	}

	profiles := defaultProfiles(RoutingStrategy{Type: "random"})
	data, err := GenerateRoutingJSON(proxies, profiles, nil)
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}

	routing, ok := result["routing"].(map[string]interface{})
	if !ok {
		t.Fatal("missing routing key")
	}

	// Should have default ad-blocking rule + fallback balancer rule
	rules, ok := routing["rules"].([]interface{})
	if !ok {
		t.Fatal("missing rules")
	}
	if len(rules) != 2 {
		t.Errorf("expected 2 rules (ad-block + fallback), got %d", len(rules))
	}

	// Should have balancer
	balancers, ok := routing["balancers"].([]interface{})
	if !ok {
		t.Fatal("missing balancers")
	}
	if len(balancers) != 1 {
		t.Errorf("expected 1 balancer, got %d", len(balancers))
	}
}

func TestReplaceBalancerRules(t *testing.T) {
	existing := json.RawMessage(`[
		{"type": "field", "domain": ["geosite:category-ads-all"], "outboundTag": "block"},
		{"type": "field", "ip": ["geoip:private"], "outboundTag": "direct"},
		{"type": "field", "balancerTag": "old-balancer", "network": "tcp,udp"},
		{"type": "field", "outboundTag": "proxy", "network": "tcp,udp"},
		{"type": "field", "port": [80, 443], "outboundTag": "proxy"}
	]`)

	result := replaceBalancerRules(existing)

	var rules []map[string]interface{}
	if err := json.Unmarshal(result, &rules); err != nil {
		t.Fatal(err)
	}

	// Should have 5 rules
	if len(rules) != 5 {
		t.Errorf("expected 5 rules, got %d", len(rules))
	}

	// Rule 0: ad-block — preserved as-is
	if rules[0]["outboundTag"] != "block" {
		t.Errorf("rule 0 should be block, got %v", rules[0]["outboundTag"])
	}

	// Rule 2: balancerTag replaced with "default-balancer"
	if rules[2]["balancerTag"] != "default-balancer" {
		t.Errorf("rule 2 should have balancerTag 'default-balancer', got %v", rules[2]["balancerTag"])
	}

	// Rule 3: outboundTag proxy — preserved
	if rules[3]["outboundTag"] != "proxy" {
		t.Errorf("rule 3 should be proxy, got %v", rules[3]["outboundTag"])
	}

	// Rule 4: port rule — preserved
	if rules[4]["outboundTag"] != "proxy" {
		t.Errorf("rule 4 should be proxy, got %v", rules[4]["outboundTag"])
	}
}

func TestGenerateRoutingJSON_WithReplaceBalancerTag(t *testing.T) {
	proxies := []*ProxyEntry{
		{Tag: "proxy-de", Outbound: json.RawMessage(`{"tag":"proxy-de","protocol":"vless"}`)},
	}

	// Existing routing with custom balancer rule
	existing := json.RawMessage(`{
		"routing": {
			"domainStrategy": "IPIfNonMatch",
			"rules": [
				{"type": "field", "domain": ["geosite:category-ads-all"], "outboundTag": "block"},
				{"type": "field", "balancerTag": "old-proxy-balancer", "network": "tcp,udp"},
				{"type": "field", "outboundTag": "proxy", "network": "tcp,udp"}
			]
		}
	}`)

	start := RoutingStrategy{Type: "random", ReplaceBalancerTag: true}
	profiles := defaultProfiles(start)
	data, err := GenerateRoutingJSON(proxies, profiles, existing)
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}

	routing := result["routing"].(map[string]interface{})

	// Rules should have balancerTag replaced with "default-balancer"
	rules := routing["rules"].([]interface{})
	if len(rules) != 3 {
		t.Errorf("expected 3 rules, got %d", len(rules))
	}

	// Rule 1 should have new balancerTag "default-balancer"
	rule1 := rules[1].(map[string]interface{})
	if rule1["balancerTag"] != "default-balancer" {
		t.Errorf("rule 1 balancerTag should be 'default-balancer', got %v", rule1["balancerTag"])
	}

	// Balancer should exist
	balancers := routing["balancers"].([]interface{})
	if len(balancers) != 1 {
		t.Errorf("expected 1 balancer, got %d", len(balancers))
	}
}

func TestCollectFilteredProxies_NoFilters(t *testing.T) {
	proxies := makeProxies()
	profiles := []Profile{
		{ID: "default", Name: "Default", Enabled: true, IsDefault: true, Strategy: RoutingStrategy{Type: "all"}},
	}

	result := CollectFilteredProxies(proxies, profiles)
	if len(result) != len(proxies) {
		t.Errorf("expected %d proxies (no filter), got %d", len(proxies), len(result))
	}
}

func TestCollectFilteredProxies_DefaultFilterExcludesAll(t *testing.T) {
	proxies := makeProxies()
	profiles := []Profile{
		{ID: "default", Name: "Default", Enabled: true, IsDefault: true,
			Filter:   Filter{ExcludeRegexes: []string{"⚡"}},
			Strategy: RoutingStrategy{Type: "all"}},
	}

	result := CollectFilteredProxies(proxies, profiles)
	if len(result) != 0 {
		t.Errorf("expected 0 proxies (all excluded by regex), got %d", len(result))
	}
}

func TestCollectFilteredProxies_UnionOfMultipleProfiles(t *testing.T) {
	proxies := makeProxies() // DE, NL, EE countries
	profiles := []Profile{
		{ID: "default", Name: "Default", Enabled: true, IsDefault: true,
			Filter:   Filter{IncludeCountries: []string{"DE"}},
			Strategy: RoutingStrategy{Type: "all"}},
		{ID: "eu", Name: "EU", Enabled: true,
			Filter:   Filter{IncludeCountries: []string{"NL"}},
			Strategy: RoutingStrategy{Type: "random"}},
	}

	result := CollectFilteredProxies(proxies, profiles)
	// DE + NL = 2 proxies
	if len(result) != 2 {
		t.Errorf("expected 2 proxies (DE+NL), got %d", len(result))
	}
}

func TestCollectFilteredProxies_DisabledProfileSkipped(t *testing.T) {
	proxies := makeProxies()
	profiles := []Profile{
		{ID: "default", Name: "Default", Enabled: true, IsDefault: true,
			Filter:   Filter{IncludeCountries: []string{"DE"}},
			Strategy: RoutingStrategy{Type: "all"}},
		{ID: "disabled", Name: "Disabled", Enabled: false,
			Filter:   Filter{IncludeCountries: []string{"NL", "EE"}},
			Strategy: RoutingStrategy{Type: "random"}},
	}

	result := CollectFilteredProxies(proxies, profiles)
	// Only default profile counts → DE only = 1 proxy
	if len(result) != 1 {
		t.Errorf("expected 1 proxy (DE only, disabled skipped), got %d", len(result))
	}
}

func TestCollectFilteredProxies_EmptyProxies(t *testing.T) {
	profiles := []Profile{
		{ID: "default", Name: "Default", Enabled: true, IsDefault: true, Strategy: RoutingStrategy{Type: "all"}},
	}

	result := CollectFilteredProxies(nil, profiles)
	if result != nil {
		t.Errorf("expected nil for nil proxies, got %v", result)
	}
}

// --- Multi-profile tests ---

func TestGenerateRoutingJSON_MultipleProfiles(t *testing.T) {
	proxies := makeProxies()
	profiles := []Profile{
		{ID: "default", Name: "Default", Enabled: true, IsDefault: true, Strategy: RoutingStrategy{Type: "random"}},
		{ID: "eu", Name: "EU", Enabled: true, Strategy: RoutingStrategy{Type: "roundrobin"},
			Filter: Filter{IncludeCountries: []string{"DE", "NL"}}},
	}

	data, err := GenerateRoutingJSON(proxies, profiles, nil)
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]interface{}
	json.Unmarshal(data, &result)
	routing := result["routing"].(map[string]interface{})

	// Should have 2 balancers
	balancers := routing["balancers"].([]interface{})
	if len(balancers) != 2 {
		t.Fatalf("expected 2 balancers, got %d", len(balancers))
	}

	// First balancer = default
	b0 := balancers[0].(map[string]interface{})
	if b0["tag"] != "default-balancer" {
		t.Errorf("first balancer tag should be 'default-balancer', got %v", b0["tag"])
	}

	// Second balancer = eu with concrete selector
	b1 := balancers[1].(map[string]interface{})
	if b1["tag"] != "eu-balancer" {
		t.Errorf("second balancer tag should be 'eu-balancer', got %v", b1["tag"])
	}
	selector := b1["selector"].([]interface{})
	if len(selector) == 0 {
		t.Error("eu balancer should have non-empty selector")
	}
	// EU profile includes DE (first proxy → "proxy") and NL (→ "proxy-nl-*")
	// After the outbound tag fix, the first proxy's selector entry is "proxy", not its original tag.
	for _, s := range selector {
		tag := s.(string)
		if !strings.HasPrefix(tag, "proxy") {
			t.Errorf("expected 'proxy' or 'proxy-*' tag in selector, got %q", tag)
		}
	}
}

func TestGenerateRoutingJSON_DisabledProfile(t *testing.T) {
	proxies := makeProxies()
	profiles := []Profile{
		{ID: "default", Name: "Default", Enabled: true, IsDefault: true, Strategy: RoutingStrategy{Type: "random"}},
		{ID: "disabled", Name: "Disabled", Enabled: false, Strategy: RoutingStrategy{Type: "random"}},
	}

	data, err := GenerateRoutingJSON(proxies, profiles, nil)
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]interface{}
	json.Unmarshal(data, &result)
	routing := result["routing"].(map[string]interface{})

	balancers := routing["balancers"].([]interface{})
	if len(balancers) != 1 {
		t.Errorf("disabled profile should be skipped, expected 1 balancer, got %d", len(balancers))
	}
}

// TestApplyPath_NoDanglingSelector_WhenFirstProxyFiltered is a regression guard for
// the reviewer's MEDIUM finding: when outbounds use filteredProxies but routing is
// (mistakenly) given allProxies, the first proxy of filteredProxies gets tag "proxy"
// in outbounds while routing resolves a different tag. This test exercises the
// CORRECT contract — both generators receive the SAME filtered list — and asserts
// every balancer selector tag exists in the outbounds, even when allProxies[0] is
// excluded by the default profile's filter.
func TestApplyPath_NoDanglingSelector_WhenFirstProxyFiltered(t *testing.T) {
	allProxies := makeProxies() // tags proxy-de-1 (index0), proxy-nl-1, proxy-ee-1 (sorted by stable key)
	// Default profile excludes the FIRST proxy's country so it is filtered out.
	// makeProxies order after stable sort is not guaranteed; exclude by regex on remarks
	// to drop exactly one proxy regardless of order, then check the survivors.
	profiles := []Profile{{
		ID: "default", Name: "Default", Enabled: true, IsDefault: true,
		Filter:   Filter{ExcludeRegexes: []string{"Germany"}},
		Strategy: RoutingStrategy{Type: "random"},
	}}

	// Simulate the Apply path: filter, then pass the SAME filtered list to both generators.
	filtered := CollectFilteredProxies(allProxies, profiles)
	if len(filtered) == 0 {
		t.Fatal("expected at least one proxy to survive the filter")
	}
	obData, err := GenerateOutboundsJSON(filtered, 0)
	if err != nil {
		t.Fatal(err)
	}
	rtData, err := GenerateRoutingJSON(filtered, profiles, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Collect outbound tags.
	var obWrap map[string]json.RawMessage
	json.Unmarshal(obData, &obWrap)
	var outbounds []map[string]interface{}
	json.Unmarshal(obWrap["outbounds"], &outbounds)
	actual := map[string]bool{}
	for _, o := range outbounds {
		if tag, ok := o["tag"].(string); ok {
			actual[tag] = true
		}
	}

	// Every balancer selector MUST reference an existing outbound tag.
	var rtWrap map[string]json.RawMessage
	json.Unmarshal(rtData, &rtWrap)
	var routing struct {
		Balancers []struct {
			Tag      string   `json:"tag"`
			Selector []string `json:"selector"`
		} `json:"balancers"`
	}
	json.Unmarshal(rtWrap["routing"], &routing)

	for _, b := range routing.Balancers {
		for _, sel := range b.Selector {
			if !actual[sel] {
				t.Errorf("DANGLING balancer selector %q (balancer %q) not found in outbounds %v", sel, b.Tag, actual)
			}
		}
	}
}

// --- applyMark / GenerateOutboundsJSON mark tests ---

func TestGenerateOutboundsJSON_MarkZero_NoStreamSettings(t *testing.T) {
	// mark=0 should NOT add sockopt.mark to any outbound
	proxies := makeProxies()
	data, err := GenerateOutboundsJSON(proxies, 0)
	if err != nil {
		t.Fatalf("GenerateOutboundsJSON failed: %v", err)
	}
	var result struct {
		Outbounds []map[string]interface{} `json:"outbounds"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("failed to parse: %v", err)
	}
	for i, ob := range result.Outbounds {
		ss, ok := ob["streamSettings"].(map[string]interface{})
		if !ok {
			continue // no streamSettings is fine
		}
		if sockopt, ok := ss["sockopt"].(map[string]interface{}); ok {
			if _, hasMark := sockopt["mark"]; hasMark {
				t.Errorf("outbound[%d] (tag=%v) should NOT have sockopt.mark when mark=0", i, ob["tag"])
			}
		}
	}
}

func TestGenerateOutboundsJSON_Mark255_AllOutbounds(t *testing.T) {
	// mark=255 should add streamSettings.sockopt.mark=255 to ALL outbounds
	proxies := makeProxies()
	data, err := GenerateOutboundsJSON(proxies, 255)
	if err != nil {
		t.Fatalf("GenerateOutboundsJSON failed: %v", err)
	}
	var result struct {
		Outbounds []map[string]interface{} `json:"outbounds"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("failed to parse: %v", err)
	}
	// 3 proxies + direct + block = 5
	if len(result.Outbounds) != 5 {
		t.Fatalf("expected 5 outbounds, got %d", len(result.Outbounds))
	}
	for i, ob := range result.Outbounds {
		ss, ok := ob["streamSettings"].(map[string]interface{})
		if !ok {
			t.Errorf("outbound[%d] (tag=%v) missing streamSettings", i, ob["tag"])
			continue
		}
		sockopt, ok := ss["sockopt"].(map[string]interface{})
		if !ok {
			t.Errorf("outbound[%d] (tag=%v) missing sockopt", i, ob["tag"])
			continue
		}
		mark, ok := sockopt["mark"].(float64)
		if !ok {
			t.Errorf("outbound[%d] (tag=%v) sockopt.mark missing or wrong type", i, ob["tag"])
			continue
		}
		if int(mark) != 255 {
			t.Errorf("outbound[%d] (tag=%v) mark=%d, want 255", i, ob["tag"], int(mark))
		}
	}
}

func TestApplyMark_PreservesExistingStreamSettings(t *testing.T) {
	// outbound already has streamSettings with tlsSettings — mark merge must preserve it
	outbound := map[string]interface{}{
		"protocol": "vless",
		"streamSettings": map[string]interface{}{
			"network": "tcp",
			"tlsSettings": map[string]interface{}{
				"serverName": "example.com",
			},
			"sockopt": map[string]interface{}{
				"tcpKeepAliveInterval": 30,
			},
		},
	}
	applyMark(outbound, 255)

	ss := outbound["streamSettings"].(map[string]interface{})
	// tlsSettings must be preserved
	if _, ok := ss["tlsSettings"]; !ok {
		t.Error("tlsSettings was removed by applyMark")
	}
	// network must be preserved
	if ss["network"] != "tcp" {
		t.Error("network was removed by applyMark")
	}
	// existing sockopt field must be preserved (int value from literal, not float)
	sockopt := ss["sockopt"].(map[string]interface{})
	if v, ok := sockopt["tcpKeepAliveInterval"]; !ok || v != 30 {
		t.Errorf("existing sockopt.tcpKeepAliveInterval was lost: %v", sockopt["tcpKeepAliveInterval"])
	}
	// mark must be added (added by applyMark as int)
	if v, ok := sockopt["mark"]; !ok || v != 255 {
		t.Errorf("sockopt.mark = %v, want 255", sockopt["mark"])
	}
}

func TestApplyMark_ZeroIsNoop(t *testing.T) {
	outbound := map[string]interface{}{
		"protocol": "vless",
	}
	applyMark(outbound, 0)
	if _, ok := outbound["streamSettings"]; ok {
		t.Error("mark=0 should not add streamSettings")
	}
}
