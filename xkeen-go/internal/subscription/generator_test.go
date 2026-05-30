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

// --- GenerateOutboundsJSON ---

func TestGenerateOutboundsJSON_Basic(t *testing.T) {
	proxies := makeProxies()
	data, err := GenerateOutboundsJSON(proxies)
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
	data, err := GenerateOutboundsJSON(proxies)
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

	// Find block outbound
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
	_, err := GenerateOutboundsJSON([]*ProxyEntry{})
	if err == nil {
		t.Fatal("expected error for empty proxy list")
	}
}

func TestGenerateOutboundsJSON_MuxPresent(t *testing.T) {
	proxies := makeProxies()
	data, err := GenerateOutboundsJSON(proxies)
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
	strategy := RoutingStrategy{Type: "all"}

	data, err := GenerateRoutingJSON(proxies, strategy, nil)
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
	strategy := RoutingStrategy{Type: "random"}

	data, err := GenerateRoutingJSON(proxies, strategy, nil)
	if err != nil {
		t.Fatalf("GenerateRoutingJSON failed: %v", err)
	}

	var result map[string]json.RawMessage
	json.Unmarshal(data, &result)

	var routing struct {
		Balancers []struct {
			Tag         string                 `json:"tag"`
			Selector    []string               `json:"selector"`
			Strategy    map[string]interface{} `json:"strategy"`
			FallbackTag string                 `json:"fallbackTag"`
		} `json:"balancers"`
		Rules []map[string]interface{} `json:"rules"`
	}
	json.Unmarshal(result["routing"], &routing)

	if len(routing.Balancers) != 1 {
		t.Fatalf("expected 1 balancer, got %d", len(routing.Balancers))
	}

	balancer := routing.Balancers[0]
	if balancer.Tag != "proxy-balancer" {
		t.Errorf("expected balancer tag 'proxy-balancer', got %q", balancer.Tag)
	}
	if len(balancer.Selector) != 1 || balancer.Selector[0] != "proxy-" {
		t.Errorf("expected selector ['proxy-'], got %v", balancer.Selector)
	}
	if balancer.Strategy["type"] != "random" {
		t.Errorf("expected strategy type 'random', got %v", balancer.Strategy["type"])
	}
	if balancer.FallbackTag != "direct" {
		t.Errorf("expected fallback 'direct', got %q", balancer.FallbackTag)
	}

	// Last rule should use balancerTag
	lastRule := routing.Rules[len(routing.Rules)-1]
	if btag, ok := lastRule["balancerTag"].(string); !ok || btag != "proxy-balancer" {
		t.Errorf("last rule should have balancerTag 'proxy-balancer', got %v", lastRule["balancerTag"])
	}
}

func TestGenerateRoutingJSON_StrategyRoundRobin(t *testing.T) {
	proxies := makeProxies()
	strategy := RoutingStrategy{Type: "roundrobin"}

	data, err := GenerateRoutingJSON(proxies, strategy, nil)
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
	strategy := RoutingStrategy{Type: "leastping"}

	data, err := GenerateRoutingJSON(proxies, strategy, nil)
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
	strategy := RoutingStrategy{Type: "leastload"}

	data, err := GenerateRoutingJSON(proxies, strategy, nil)
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

func TestGenerateRoutingJSON_CustomFallbackTag(t *testing.T) {
	proxies := makeProxies()
	strategy := RoutingStrategy{Type: "random", FallbackTag: "block"}

	data, err := GenerateRoutingJSON(proxies, strategy, nil)
	if err != nil {
		t.Fatalf("GenerateRoutingJSON failed: %v", err)
	}

	var result map[string]json.RawMessage
	json.Unmarshal(data, &result)

	var routing struct {
		Balancers []struct {
			FallbackTag string `json:"fallbackTag"`
		} `json:"balancers"`
	}
	json.Unmarshal(result["routing"], &routing)

	if routing.Balancers[0].FallbackTag != "block" {
		t.Errorf("expected fallback 'block', got %q", routing.Balancers[0].FallbackTag)
	}
}

func TestGenerateRoutingJSON_WithExistingRouting(t *testing.T) {
	proxies := makeProxies()
	strategy := RoutingStrategy{Type: "all"}

	existing := `{
		"domainStrategy": "AsIs",
		"rules": [
			{"type":"field","domain":["geosite:private"],"outboundTag":"direct"},
			{"type":"field","outboundTag":"proxy","network":"tcp,udp"},
			{"type":"field","domain":["geosite:category-ads-all"],"outboundTag":"block"}
		]
	}`

	data, err := GenerateRoutingJSON(proxies, strategy, json.RawMessage(existing))
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
	strategy := RoutingStrategy{Type: "random"}

	// Existing routing has old balancer rules — they should be removed
	existing := `{
		"domainStrategy": "IPIfNonMatch",
		"rules": [
			{"type":"field","domain":["geosite:category-ads-all"],"outboundTag":"block"},
			{"type":"field","balancerTag":"proxy-balancer","network":"tcp,udp"}
		]
	}`

	data, err := GenerateRoutingJSON(proxies, strategy, json.RawMessage(existing))
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

	// Should have exactly one balancer
	if len(routing.Balancers) != 1 {
		t.Errorf("expected 1 balancer, got %d", len(routing.Balancers))
	}

	// Should have block rule + proxy-balancer rule
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
	_, err := GenerateRoutingJSON([]*ProxyEntry{}, RoutingStrategy{Type: "all"}, nil)
	if err == nil {
		t.Fatal("expected error for empty proxy list")
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

	if len(result.Observatory.SubjectSelector) != 1 || result.Observatory.SubjectSelector[0] != "proxy-" {
		t.Errorf("expected subjectSelector ['proxy-'], got %v", result.Observatory.SubjectSelector)
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
		result := NeedsObservatory(tt.strategy)
		if result != tt.expected {
			t.Errorf("NeedsObservatory(%q) = %v, expected %v", tt.strategy, result, tt.expected)
		}
	}
}

// --- Output formatting ---

func TestGenerateOutboundsJSON_ValidJSON(t *testing.T) {
	proxies := makeProxies()
	data, err := GenerateOutboundsJSON(proxies)
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
	data, err := GenerateRoutingJSON(proxies, RoutingStrategy{Type: "all"}, nil)
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
