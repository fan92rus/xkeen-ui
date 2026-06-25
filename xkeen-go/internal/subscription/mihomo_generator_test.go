package subscription

import (
	"encoding/json"
	"strings"
	"testing"
)

// ── Helpers for creating test data ─────────────────────────────────────────

func testProxy(tag, protocol, host string, port int, outbound map[string]interface{}) *ProxyEntry {
	outbound["protocol"] = protocol
	raw, _ := json.Marshal(outbound)
	return &ProxyEntry{
		Tag:      tag,
		Protocol: protocol,
		Outbound: raw,
	}
}

func vlessOutbound(id, host string, port int, flow, network, security, path, sni string) map[string]interface{} {
	ss := map[string]interface{}{
		"network":  network,
		"security": security,
	}
	switch network {
	case "ws":
		ws := map[string]interface{}{}
		if path != "" {
			ws["path"] = path
		}
		ss["wsSettings"] = ws
	}

	tlsSettings := map[string]interface{}{}
	if sni != "" {
		tlsSettings["serverName"] = sni
	}
	if security == "tls" {
		ss["tlsSettings"] = tlsSettings
	} else if security == "reality" {
		ss["realitySettings"] = tlsSettings
		ss["realitySettings"].(map[string]interface{})["fingerprint"] = "chrome"
	}

	user := map[string]interface{}{"id": id, "encryption": "none"}
	if flow != "" {
		user["flow"] = flow
	}

	return map[string]interface{}{
		"settings": map[string]interface{}{
			"vnext": []interface{}{
				map[string]interface{}{
					"address": host,
					"port":    port,
					"users":   []interface{}{user},
				},
			},
		},
		"streamSettings": ss,
		"mux":            DefaultMux,
	}
}

func trojanOutbound(password, host string, port int, network, path, sni string) map[string]interface{} {
	ss := map[string]interface{}{
		"network":  network,
		"security": "tls",
	}
	if network == "ws" && path != "" {
		ss["wsSettings"] = map[string]interface{}{"path": path}
	}
	if sni != "" {
		ss["tlsSettings"] = map[string]interface{}{"serverName": sni}
	}

	return map[string]interface{}{
		"settings": map[string]interface{}{
			"servers": []interface{}{
				map[string]interface{}{
					"address":  host,
					"port":     port,
					"password": password,
				},
			},
		},
		"streamSettings": ss,
		"mux":            DefaultMux,
	}
}

func hysteria2Outbound(password, host string, port int, sni string) map[string]interface{} {
	ss := map[string]interface{}{
		"network":  "hysteria",
		"security": "tls",
		"hysteriaSettings": map[string]interface{}{
			"version": 2,
			"auth":    password,
		},
	}
	if sni != "" {
		ss["tlsSettings"] = map[string]interface{}{"serverName": sni}
	}

	return map[string]interface{}{
		"settings": map[string]interface{}{
			"version": 2,
			"address": host,
			"port":    port,
		},
		"streamSettings": ss,
		"mux":            DefaultMux,
	}
}

// ── proxyToMihomo tests ────────────────────────────────────────────────────

func TestProxyToMihomo_VLESS(t *testing.T) {
	entry := testProxy("proxy-de-1", "vless", "1.2.3.4", 443,
		vlessOutbound("uuid-123", "1.2.3.4", 443, "xtls-rprx-vision", "tcp", "tls", "", "example.com"))

	m, err := proxyToMihomo(entry)
	if err != nil {
		t.Fatalf("proxyToMihomo failed: %v", err)
	}

	checkString(t, m, "name", "proxy-de-1")
	checkString(t, m, "type", "vless")
	checkString(t, m, "server", "1.2.3.4")
	checkInt(t, m, "port", 443)
	checkString(t, m, "uuid", "uuid-123")
	checkString(t, m, "flow", "xtls-rprx-vision")
	checkBool(t, m, "tls", true)
	checkString(t, m, "servername", "example.com")

	// TLS with WS should set network + ws-opts
	entryWS := testProxy("proxy-us-1", "vless", "5.6.7.8", 8443,
		vlessOutbound("uuid-456", "5.6.7.8", 8443, "", "ws", "tls", "/ws-path", "ws.example.com"))

	m2, err := proxyToMihomo(entryWS)
	if err != nil {
		t.Fatalf("proxyToMihomo failed: %v", err)
	}

	checkString(t, m2, "name", "proxy-us-1")
	checkString(t, m2, "type", "vless")
	checkString(t, m2, "server", "5.6.7.8")
	checkInt(t, m2, "port", 8443)
	checkString(t, m2, "network", "ws")
	checkBool(t, m2, "tls", true)

	wsOpts, ok := m2["ws-opts"].(map[string]interface{})
	if !ok {
		t.Fatal("expected ws-opts")
	}
	checkString(t, wsOpts, "path", "/ws-path")
}

func TestProxyToMihomo_VLESS_NoTLS(t *testing.T) {
	entry := testProxy("proxy-de-2", "vless", "9.9.9.9", 80,
		vlessOutbound("uuid-no-tls", "9.9.9.9", 80, "", "tcp", "none", "", ""))

	m, err := proxyToMihomo(entry)
	if err != nil {
		t.Fatalf("proxyToMihomo failed: %v", err)
	}

	checkString(t, m, "name", "proxy-de-2")
	// tls=false is removed by cleanMap; verify it's not true
	if tls, ok := m["tls"]; ok && tls.(bool) {
		t.Error("tls should be false or absent for no-TLS proxy")
	}
	if _, ok := m["network"]; ok {
		t.Error("network should not be set for TCP transport")
	}
}

func TestProxyToMihomo_Trojan(t *testing.T) {
	entry := testProxy("proxy-jp-1", "trojan", "10.0.0.1", 443,
		trojanOutbound("pass123", "10.0.0.1", 443, "tcp", "", "trojan.example.com"))

	m, err := proxyToMihomo(entry)
	if err != nil {
		t.Fatalf("proxyToMihomo failed: %v", err)
	}

	checkString(t, m, "name", "proxy-jp-1")
	checkString(t, m, "type", "trojan")
	checkString(t, m, "server", "10.0.0.1")
	checkInt(t, m, "port", 443)
	checkString(t, m, "password", "pass123")
	checkBool(t, m, "tls", true)
	checkString(t, m, "servername", "trojan.example.com")
}

func TestProxyToMihomo_Hysteria2(t *testing.T) {
	entry := testProxy("proxy-sg-1", "hysteria", "20.0.0.1", 8443,
		hysteria2Outbound("hy-pass", "20.0.0.1", 8443, "hy.example.com"))

	m, err := proxyToMihomo(entry)
	if err != nil {
		t.Fatalf("proxyToMihomo failed: %v", err)
	}

	checkString(t, m, "name", "proxy-sg-1")
	checkString(t, m, "type", "hysteria2")
	checkString(t, m, "server", "20.0.0.1")
	checkInt(t, m, "port", 8443)
	checkString(t, m, "password", "hy-pass")
	checkBool(t, m, "tls", true)
	checkString(t, m, "sni", "hy.example.com")
}

func TestProxyToMihomo_EmptyOutbound(t *testing.T) {
	entry := &ProxyEntry{Tag: "bad", Outbound: nil}
	_, err := proxyToMihomo(entry)
	if err == nil {
		t.Fatal("expected error for empty outbound")
	}
}

// ── GenerateMihomoConfig tests ──────────────────────────────────────────────

func TestGenerateMihomoConfig_Basic(t *testing.T) {
	proxies := []*ProxyEntry{
		testProxy("proxy-de-1", "vless", "1.2.3.4", 443,
			vlessOutbound("uuid-1", "1.2.3.4", 443, "", "tcp", "tls", "", "de.example.com")),
		testProxy("proxy-us-1", "vless", "5.6.7.8", 443,
			vlessOutbound("uuid-2", "5.6.7.8", 443, "", "tcp", "tls", "", "us.example.com")),
	}

	profiles := []Profile{
		{
			ID:        "default",
			Name:      "Default",
			Enabled:   true,
			IsDefault: true,
			Filter:    Filter{},
			Strategy:  RoutingStrategy{Type: "all"},
		},
	}

	opts := MihomoGenerateOptions{}
	data, err := GenerateMihomoConfig(proxies, profiles, opts)
	if err != nil {
		t.Fatalf("GenerateMihomoConfig failed: %v", err)
	}

	output := string(data)
	if !strings.Contains(output, "proxies:") {
		t.Error("expected proxies section")
	}
	if !strings.Contains(output, "proxy-groups:") {
		t.Error("expected proxy-groups section")
	}
	if !strings.Contains(output, "rules:") {
		t.Error("expected rules section")
	}
	if !strings.Contains(output, "proxy-de-1") {
		t.Error("expected proxy-de-1 in output")
	}
	if !strings.Contains(output, "proxy-us-1") {
		t.Error("expected proxy-us-1 in output")
	}
	if !strings.Contains(output, "MATCH,Proxy") {
		t.Error("expected MATCH rule")
	}
}

func TestGenerateMihomoConfig_WithRouterConversion(t *testing.T) {
	proxies := []*ProxyEntry{
		testProxy("proxy-de-1", "vless", "1.2.3.4", 443,
			vlessOutbound("uuid-1", "1.2.3.4", 443, "", "tcp", "tls", "", "de.example.com")),
	}

	profiles := []Profile{
		{
			ID:        "default",
			Name:      "Default",
			Enabled:   true,
			IsDefault: true,
			Filter:    Filter{},
			Strategy:  RoutingStrategy{Type: "all"},
		},
	}

	xrayRouting := `{
		"routing": {
			"rules": [
				{"type": "field", "domain_suffix": ["geosite:youtube", "google.com"], "outboundTag": "direct"},
				{"type": "field", "ip": ["geoip:cn", "10.0.0.0/8"], "outboundTag": "direct"},
				{"type": "field", "network": "tcp", "outboundTag": "proxy"},
				{"type": "field", "port": "80", "outboundTag": "block"},
				{"type": "field", "outboundTag": "proxy", "network": "tcp,udp"}
			]
		}
	}`

	opts := MihomoGenerateOptions{
		ConvertXrayRouting: true,
		XrayRoutingJSON:    []byte(xrayRouting),
	}
	data, err := GenerateMihomoConfig(proxies, profiles, opts)
	if err != nil {
		t.Fatalf("GenerateMihomoConfig failed: %v", err)
	}

	output := string(data)
	if !strings.Contains(output, "GEOSITE,youtube,DIRECT") {
		t.Error("expected GEOSITE rule from geosite:youtube")
	}
	if !strings.Contains(output, "DOMAIN-SUFFIX,google.com,DIRECT") {
		t.Error("expected DOMAIN-SUFFIX rule from google.com")
	}
	if !strings.Contains(output, "GEOIP,cn,DIRECT") {
		t.Error("expected GEOIP rule from geoip:cn")
	}
	if !strings.Contains(output, "IP-CIDR,10.0.0.0/8,DIRECT") {
		t.Error("expected IP-CIDR rule from 10.0.0.0/8")
	}
	if !strings.Contains(output, "DST-PORT,80,REJECT") {
		t.Error("expected DST-PORT rule from port 80 -> block -> REJECT")
	}
}

func TestGenerateMihomoConfig_WithExistingConfig(t *testing.T) {
	proxies := []*ProxyEntry{
		testProxy("proxy-de-1", "vless", "1.2.3.4", 443,
			vlessOutbound("uuid-1", "1.2.3.4", 443, "", "tcp", "tls", "", "de.example.com")),
	}

	profiles := []Profile{
		{
			ID:        "default",
			Name:      "Default",
			Enabled:   true,
			IsDefault: true,
			Filter:    Filter{},
			Strategy:  RoutingStrategy{Type: "all"},
		},
	}

	existingConfig := `mixed-port: 7891
log-level: info
allow-lan: true
dns:
  enable: true
  listen: 0.0.0.0:53
`

	opts := MihomoGenerateOptions{
		ExistingMihomoConfig: []byte(existingConfig),
	}
	data, err := GenerateMihomoConfig(proxies, profiles, opts)
	if err != nil {
		t.Fatalf("GenerateMihomoConfig failed: %v", err)
	}

	output := string(data)
	if !strings.Contains(output, "dns:") {
		t.Error("expected dns section to be preserved")
	}
	if !strings.Contains(output, "proxies:") {
		t.Error("expected proxies section")
	}
}

func TestGenerateMihomoConfig_UrlTestStrategy(t *testing.T) {
	proxies := []*ProxyEntry{
		testProxy("proxy-de-1", "vless", "1.2.3.4", 443,
			vlessOutbound("uuid-1", "1.2.3.4", 443, "", "tcp", "tls", "", "de.example.com")),
		testProxy("proxy-us-1", "vless", "5.6.7.8", 443,
			vlessOutbound("uuid-2", "5.6.7.8", 443, "", "tcp", "tls", "", "us.example.com")),
	}

	profiles := []Profile{
		{
			ID:        "default",
			Name:      "Default",
			Enabled:   true,
			IsDefault: true,
			Filter:    Filter{},
			Strategy:  RoutingStrategy{Type: "leastping"},
		},
	}

	opts := MihomoGenerateOptions{}
	data, err := GenerateMihomoConfig(proxies, profiles, opts)
	if err != nil {
		t.Fatalf("GenerateMihomoConfig failed: %v", err)
	}

	output := string(data)
	if !strings.Contains(output, "url-test") {
		t.Error("expected url-test type for leastping strategy")
	}
	if !strings.Contains(output, "http://www.gstatic.com/generate_204") {
		t.Error("expected url-test URL")
	}
}

func TestGenerateMihomoConfig_NoProxies(t *testing.T) {
	_, err := GenerateMihomoConfig(nil, nil, MihomoGenerateOptions{})
	if err == nil {
		t.Fatal("expected error for nil proxies")
	}
}

// ── Routing conversion tests ───────────────────────────────────────────────

func TestConvertXrayRoutingRules(t *testing.T) {
	rules := []XrayRule{
		{"type": "field", "domain": []interface{}{"ads.com"}, "outboundTag": "block"},
		{"type": "field", "domain_suffix": []interface{}{"geosite:google", "example.com"}, "outboundTag": "proxy"},
		{"type": "field", "domain_keyword": []interface{}{"tracker"}, "outboundTag": "block"},
		{"type": "field", "ip": []interface{}{"geoip:cn", "192.168.0.0/16"}, "outboundTag": "direct"},
		{"type": "field", "port": "443", "outboundTag": "proxy"},
	}

	// "proxy" must be known — the first outbound always gets tag "proxy"
	proxyTags := []string{"proxy", "proxy-de-1", "proxy-us-1", "direct", "block"}

	result, warnings := convertXrayRoutingRules(rules, proxyTags)
	_ = warnings

	ruleStrs := make([]string, len(result))
	for i, r := range result {
		ruleStrs[i] = string(r)
	}

	expectRule := func(rules []string, expected string) {
		for _, r := range rules {
			if r == expected {
				return
			}
		}
		t.Errorf("expected rule %q not found in %v", expected, rules)
	}

	expectRule(ruleStrs, "DOMAIN,ads.com,REJECT")
	expectRule(ruleStrs, "GEOSITE,google,proxy")
	expectRule(ruleStrs, "DOMAIN-SUFFIX,example.com,proxy")
	expectRule(ruleStrs, "DOMAIN-KEYWORD,tracker,REJECT")
	expectRule(ruleStrs, "GEOIP,cn,DIRECT")
	expectRule(ruleStrs, "IP-CIDR,192.168.0.0/16,DIRECT")
	expectRule(ruleStrs, "DST-PORT,443,proxy")
}

func TestMapTargetToMihomo(t *testing.T) {
	tags := map[string]bool{"proxy-de-1": true, "direct": true, "block": true}

	tests := []struct {
		target string
		want   string
	}{
		{"", "DIRECT"},
		{"direct", "DIRECT"},
		{"freedom", "DIRECT"},
		{"block", "REJECT"},
		{"blackhole", "REJECT"},
		{"proxy-de-1", "proxy-de-1"},
		{"default-balancer", "default"},
		{"unknown-tag", "DIRECT"},
	}

	for _, tt := range tests {
		got := mapTargetToMihomo(tt.target, tags)
		if got != tt.want {
			t.Errorf("mapTargetToMihomo(%q) = %q, want %q", tt.target, got, tt.want)
		}
	}
}

// ── Strategy mapping tests ─────────────────────────────────────────────────

func TestMihomoStrategyType(t *testing.T) {
	tests := []struct {
		xray string
		want string
	}{
		{"all", "select"},
		{"random", "random"},
		{"roundrobin", "load-balance"},
		{"leastping", "url-test"},
		{"leastload", "url-test"},
		{"unknown", "select"},
	}

	for _, tt := range tests {
		got := mihomoStrategyType(tt.xray)
		if got != tt.want {
			t.Errorf("mihomoStrategyType(%q) = %q, want %q", tt.xray, got, tt.want)
		}
	}
}

// ── Helpers ────────────────────────────────────────────────────────────────

func checkString(t *testing.T, m map[string]interface{}, key, want string) {
	t.Helper()
	got, ok := m[key].(string)
	if !ok {
		t.Errorf("missing or non-string key %q", key)
		return
	}
	if got != want {
		t.Errorf("%q = %q, want %q", key, got, want)
	}
}

func checkInt(t *testing.T, m map[string]interface{}, key string, want int) {
	t.Helper()
	got, ok := m[key].(int)
	if !ok {
		t.Errorf("missing or non-int key %q (got %T %v)", key, m[key], m[key])
		return
	}
	if got != want {
		t.Errorf("%q = %d, want %d", key, got, want)
	}
}

func checkBool(t *testing.T, m map[string]interface{}, key string, want bool) {
	t.Helper()
	got, ok := m[key].(bool)
	if !ok {
		t.Errorf("missing or non-bool key %q", key)
		return
	}
	if got != want {
		t.Errorf("%q = %v, want %v", key, got, want)
	}
}
