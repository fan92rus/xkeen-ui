package subscription

import (
	"encoding/json"
	"strings"
	"testing"
)

// ── helpers ──

func makeOutbound(protocol string, data map[string]interface{}) json.RawMessage {
	d := map[string]interface{}{"protocol": protocol}
	for k, v := range data {
		d[k] = v
	}
	b, _ := json.Marshal(d)
	return b
}

func makeStreamSettings(network, security string, extra map[string]interface{}) map[string]interface{} {
	ss := map[string]interface{}{"network": network}
	if security != "" {
		ss["security"] = security
	}
	for k, v := range extra {
		ss[k] = v
	}
	return ss
}

func TestToMihomoProxy_VLESS(t *testing.T) {
	entry := &ProxyEntry{
		Tag:      "proxy-de-1",
		Protocol: "vless",
		Outbound: makeOutbound("vless", map[string]interface{}{
			"settings": map[string]interface{}{
				"vnext": []interface{}{
					map[string]interface{}{
						"address": "de1.example.com",
						"port":    443.0,
						"users": []interface{}{
							map[string]interface{}{
								"id":     "uuid-123",
								"flow":   "xtls-rprx-vision",
								"encryption": "none",
							},
						},
					},
				},
			},
			"streamSettings": makeStreamSettings("tcp", "tls", map[string]interface{}{
				"tlsSettings": map[string]interface{}{
					"serverName":  "de1.example.com",
					"fingerprint": "chrome",
				},
			}),
		}),
	}

	p, err := toMihomoProxy(entry)
	if err != nil {
		t.Fatalf("toMihomoProxy failed: %v", err)
	}

	if p.Name != "proxy-de-1" { t.Errorf("Name = %q, want proxy-de-1", p.Name) }
	if p.Type != "vless" { t.Errorf("Type = %q, want vless", p.Type) }
	if p.Server != "de1.example.com" { t.Errorf("Server = %q, want de1.example.com", p.Server) }
	if p.Port != 443 { t.Errorf("Port = %d, want 443", p.Port) }
	if p.UUID != "uuid-123" { t.Errorf("UUID = %q, want uuid-123", p.UUID) }
	if p.Flow != "xtls-rprx-vision" { t.Errorf("Flow = %q, want xtls-rprx-vision", p.Flow) }
	if !p.TLS { t.Error("TLS = false, want true") }
	if p.Servername != "de1.example.com" { t.Errorf("Servername = %q, want de1.example.com", p.Servername) }
	if p.Fingerprint != "chrome" { t.Errorf("Fingerprint = %q, want chrome", p.Fingerprint) }
	if p.UDP != true { t.Error("UDP = false, want true") }
}

func TestToMihomoProxy_VLESS_WS(t *testing.T) {
	entry := &ProxyEntry{
		Tag:      "proxy-us-1",
		Protocol: "vless",
		Outbound: makeOutbound("vless", map[string]interface{}{
			"settings": map[string]interface{}{
				"vnext": []interface{}{
					map[string]interface{}{
						"address": "us1.example.com",
						"port":    8443.0,
						"users": []interface{}{
							map[string]interface{}{"id": "uuid-456"},
						},
					},
				},
			},
			"streamSettings": makeStreamSettings("ws", "tls", map[string]interface{}{
				"tlsSettings": map[string]interface{}{"serverName": "us1.example.com"},
				"wsSettings": map[string]interface{}{
					"path": "/ws",
					"headers": map[string]interface{}{"Host": "us1.example.com"},
				},
			}),
		}),
	}

	p, err := toMihomoProxy(entry)
	if err != nil {
		t.Fatalf("toMihomoProxy failed: %v", err)
	}

	if p.Server != "us1.example.com" { t.Errorf("Server = %q, want us1.example.com", p.Server) }
	if p.Port != 8443 { t.Errorf("Port = %d, want 8443", p.Port) }
	if p.Network != "ws" { t.Errorf("Network = %q, want ws", p.Network) }
	if p.WSOpts == nil { t.Fatal("WSOpts is nil") }
	if p.WSOpts.Path != "/ws" { t.Errorf("WSOpts.Path = %q, want /ws", p.WSOpts.Path) }
	if p.WSOpts.Headers["Host"] != "us1.example.com" {
		t.Errorf("WSOpts.Headers[Host] = %q, want us1.example.com", p.WSOpts.Headers["Host"])
	}
}

func TestToMihomoProxy_VLESS_GRPC(t *testing.T) {
	entry := &ProxyEntry{
		Tag:      "proxy-jp-1",
		Protocol: "vless",
		Outbound: makeOutbound("vless", map[string]interface{}{
			"settings": map[string]interface{}{
				"vnext": []interface{}{
					map[string]interface{}{
						"address": "jp1.example.com",
						"port":    443.0,
						"users": []interface{}{
							map[string]interface{}{"id": "uuid-789"},
						},
					},
				},
			},
			"streamSettings": makeStreamSettings("grpc", "tls", map[string]interface{}{
				"grpcSettings": map[string]interface{}{"serviceName": "grpc-test"},
			}),
		}),
	}

	p, err := toMihomoProxy(entry)
	if err != nil {
		t.Fatalf("toMihomoProxy failed: %v", err)
	}

	if p.Network != "grpc" { t.Errorf("Network = %q, want grpc", p.Network) }
	if p.GRPCOpts == nil { t.Fatal("GRPCOpts is nil") }
	if p.GRPCOpts.ServiceName != "grpc-test" {
		t.Errorf("GRPCOpts.ServiceName = %q, want grpc-test", p.GRPCOpts.ServiceName)
	}
}

func TestToMihomoProxy_VLESS_Reality(t *testing.T) {
	entry := &ProxyEntry{
		Tag:      "proxy-ru-1",
		Protocol: "vless",
		Outbound: makeOutbound("vless", map[string]interface{}{
			"settings": map[string]interface{}{
				"vnext": []interface{}{
					map[string]interface{}{
						"address": "ru1.example.com",
						"port":    443.0,
						"users": []interface{}{
							map[string]interface{}{"id": "uuid-reality"},
						},
					},
				},
			},
			"streamSettings": makeStreamSettings("tcp", "reality", map[string]interface{}{
				"realitySettings": map[string]interface{}{
					"serverName":  "ru1.example.com",
					"fingerprint": "chrome",
					"publicKey":   "pk-abc",
					"shortId":     "sid-123",
				},
			}),
		}),
	}

	p, err := toMihomoProxy(entry)
	if err != nil {
		t.Fatalf("toMihomoProxy failed: %v", err)
	}

	if !p.Reality { t.Error("Reality = false, want true") }
	if !p.TLS { t.Error("TLS = false, want true") }
	if p.PublicKey != "pk-abc" { t.Errorf("PublicKey = %q, want pk-abc", p.PublicKey) }
	if p.ShortID != "sid-123" { t.Errorf("ShortID = %q, want sid-123", p.ShortID) }
}

func TestToMihomoProxy_Trojan(t *testing.T) {
	entry := &ProxyEntry{
		Tag:      "proxy-fr-1",
		Protocol: "trojan",
		Outbound: makeOutbound("trojan", map[string]interface{}{
			"settings": map[string]interface{}{
				"servers": []interface{}{
					map[string]interface{}{
						"address":  "fr1.example.com",
						"port":     443.0,
						"password": "trojan-pass",
					},
				},
			},
			"streamSettings": makeStreamSettings("tcp", "tls", map[string]interface{}{
				"tlsSettings": map[string]interface{}{"serverName": "fr1.example.com"},
			}),
		}),
	}

	p, err := toMihomoProxy(entry)
	if err != nil {
		t.Fatalf("toMihomoProxy failed: %v", err)
	}

	if p.Type != "trojan" { t.Errorf("Type = %q, want trojan", p.Type) }
	if p.Server != "fr1.example.com" { t.Errorf("Server = %q, want fr1.example.com", p.Server) }
	if p.Password != "trojan-pass" { t.Errorf("Password = %q, want trojan-pass", p.Password) }
	if !p.TLS { t.Error("TLS = false, want true") }
}

func TestToMihomoProxy_Hysteria2(t *testing.T) {
	entry := &ProxyEntry{
		Tag:      "proxy-sg-1",
		Protocol: "hysteria",
		Outbound: makeOutbound("hysteria", map[string]interface{}{
			"settings": map[string]interface{}{
				"version": 2.0,
				"address": "sg1.example.com",
				"port":    443.0,
			},
			"streamSettings": makeStreamSettings("hysteria", "tls", map[string]interface{}{
				"hysteriaSettings": map[string]interface{}{
					"version": 2.0,
					"auth":    "hy2-pass",
				},
				"tlsSettings": map[string]interface{}{
					"serverName":    "sg1.example.com",
					"allowInsecure": true,
				},
			}),
		}),
	}

	p, err := toMihomoProxy(entry)
	if err != nil {
		t.Fatalf("toMihomoProxy failed: %v", err)
	}

	if p.Type != "hysteria2" { t.Errorf("Type = %q, want hysteria2", p.Type) }
	if p.Server != "sg1.example.com" { t.Errorf("Server = %q, want sg1.example.com", p.Server) }
	if p.Password != "hy2-pass" { t.Errorf("Password = %q, want hy2-pass", p.Password) }
	if !p.TLS { t.Error("TLS = false, want true") }
	if p.Servername != "sg1.example.com" { t.Errorf("Servername = %q, want sg1.example.com", p.Servername) }
	if !p.SkipCert { t.Error("SkipCert = false, want true") }
}

func TestToMihomoProxy_UnsupportedProtocol(t *testing.T) {
	entry := &ProxyEntry{
		Tag:      "proxy-bad",
		Protocol: "ss",
		Outbound: makeOutbound("ss", map[string]interface{}{
			"settings": map[string]interface{}{},
		}),
	}

	_, err := toMihomoProxy(entry)
	if err == nil {
		t.Fatal("expected error for unsupported protocol")
	}
	if !strings.Contains(err.Error(), "unsupported protocol") {
		t.Errorf("error = %q, want unsupported protocol message", err)
	}
}

// ── GenerateMihomoConfig tests ──

func TestGenerateMihomoConfig_Simple(t *testing.T) {
	entries := []*ProxyEntry{
		{
			Tag: "proxy-de-1", Protocol: "vless",
			Outbound: makeOutbound("vless", map[string]interface{}{
				"settings": map[string]interface{}{
					"vnext": []interface{}{map[string]interface{}{
						"address": "de1.com", "port": 443.0,
						"users": []interface{}{map[string]interface{}{"id": "u1"}},
					}},
				},
				"streamSettings": makeStreamSettings("tcp", "", nil),
			}),
		},
		{
			Tag: "proxy-us-1", Protocol: "vless",
			Outbound: makeOutbound("vless", map[string]interface{}{
				"settings": map[string]interface{}{
					"vnext": []interface{}{map[string]interface{}{
						"address": "us1.com", "port": 443.0,
						"users": []interface{}{map[string]interface{}{"id": "u2"}},
					}},
				},
				"streamSettings": makeStreamSettings("ws", "tls", map[string]interface{}{
					"tlsSettings": map[string]interface{}{"serverName": "us1.com"},
					"wsSettings":  map[string]interface{}{"path": "/ws"},
				}),
			}),
		},
	}

	profiles := []Profile{
		{ID: "default", Name: "Default", Enabled: true, IsDefault: true,
			Strategy: RoutingStrategy{Type: "all"},
		},
	}

	yaml, err := GenerateMihomoConfig(entries, profiles, nil)
	if err != nil {
		t.Fatalf("GenerateMihomoConfig failed: %v", err)
	}

	// Check YAML contains expected content
	checks := []string{
		"proxies:",
		"proxy-groups:",
		"rules:",
		"name: proxy-de-1",
		"name: proxy-us-1",
		"name: Proxy",     // proxy-group from default profile
		"name: Select",    // manual override group
		"DIRECT",          // in Select group
		"REJECT",          // in Select group
		"MATCH,Proxy",     // fallback rule
		"GEOIP,CN,DIRECT", // geo rules
		"GEOSITE,category-ads-all,REJECT",
	}
	for _, c := range checks {
		if !strings.Contains(yaml, c) {
			t.Errorf("YAML missing expected string: %q\nYAML:\n%s", c, yaml)
		}
	}
}

func TestGenerateMihomoConfig_StrategyMapping(t *testing.T) {
	entry := &ProxyEntry{
		Tag: "proxy-test-1", Protocol: "vless",
		Outbound: makeOutbound("vless", map[string]interface{}{
			"settings": map[string]interface{}{
				"vnext": []interface{}{map[string]interface{}{
					"address": "test.com", "port": 443.0,
					"users": []interface{}{map[string]interface{}{"id": "u1"}},
				}},
			},
			"streamSettings": makeStreamSettings("tcp", "", nil),
		}),
	}

	tests := []struct {
		strategyType string
		wantGroupType string
	}{
		{"all", "select"},
		{"random", "random"},
		{"roundrobin", "load-balance"},
		{"leastping", "url-test"},
		{"leastload", "url-test"},
	}

	for _, tc := range tests {
		profiles := []Profile{
			{ID: "default", Enabled: true, IsDefault: true,
				Strategy: RoutingStrategy{Type: tc.strategyType},
			},
		}

		yaml, err := GenerateMihomoConfig([]*ProxyEntry{entry}, profiles, nil)
		if err != nil {
			t.Fatalf("strategy %s: GenerateMihomoConfig failed: %v", tc.strategyType, err)
		}

		if !strings.Contains(yaml, "type: "+tc.wantGroupType) {
			t.Errorf("strategy %s: expected group type %q, got YAML:\n%s", tc.strategyType, tc.wantGroupType, yaml)
		}
	}
}

func TestGenerateMihomoConfig_CustomProfile(t *testing.T) {
	entries := []*ProxyEntry{
		{Tag: "proxy-de-1", Protocol: "vless",
			Outbound: makeOutbound("vless", map[string]interface{}{
				"settings": map[string]interface{}{
					"vnext": []interface{}{map[string]interface{}{
						"address": "de1.com", "port": 443.0,
						"users": []interface{}{map[string]interface{}{"id": "u1"}},
					}},
				},
				"streamSettings": makeStreamSettings("tcp", "", nil),
			}),
		},
		{Tag: "proxy-jp-1", Protocol: "vless",
			Outbound: makeOutbound("vless", map[string]interface{}{
				"settings": map[string]interface{}{
					"vnext": []interface{}{map[string]interface{}{
						"address": "jp1.com", "port": 443.0,
						"users": []interface{}{map[string]interface{}{"id": "u2"}},
					}},
				},
				"streamSettings": makeStreamSettings("tcp", "", nil),
			}),
		},
	}

	profiles := []Profile{
		{ID: "default", Name: "Default", Enabled: true, IsDefault: true,
			Strategy: RoutingStrategy{Type: "all"},
		},
		{ID: "germany", Name: "Germany", Enabled: true,
			Filter: Filter{IncludeCountries: []string{"DE"}},
			Strategy: RoutingStrategy{Type: "leastping"},
		},
	}

	yaml, err := GenerateMihomoConfig(entries, profiles, nil)
	if err != nil {
		t.Fatalf("GenerateMihomoConfig failed: %v", err)
	}

	if !strings.Contains(yaml, "name: Germany") {
		t.Errorf("YAML missing custom profile group 'Germany':\n%s", yaml)
	}
	if !strings.Contains(yaml, "url-test") {
		t.Errorf("YAML missing url-test for Germany profile:\n%s", yaml)
	}
}

// ── Xray routing conversion tests ──

func TestConvertXrayRouting_Basic(t *testing.T) {
	routingJSON := []byte(`{
		"rules": [
			{
				"domain": ["google.com", "youtube.com"],
				"outboundTag": "direct"
			},
			{
				"domain_suffix": ["geosite:netflix"],
				"outboundTag": "proxy"
			},
			{
				"domain_keyword": ["facebook"],
				"outboundTag": "proxy"
			},
			{
				"ip": ["geoip:cn", "10.0.0.0/8"],
				"outboundTag": "direct"
			},
			{
				"domain_regex": ["^.*\\.cn$"],
				"outboundTag": "block"
			}
		],
		"balancers": []
	}`)

	rules, err := convertXrayRouting(routingJSON)
	if err != nil {
		t.Fatalf("convertXrayRouting failed: %v", err)
	}

	if len(rules) == 0 {
		t.Fatal("expected at least one converted rule")
	}

	ruleStrs := make([]string, len(rules))
	copy(ruleStrs, rules)

	find := func(s string) bool {
		for _, r := range ruleStrs {
			if r == s { return true }
		}
		return false
	}

	if !find("DOMAIN-SUFFIX,google.com,DIRECT") { t.Errorf("missing domain rule") }
	if !find("DOMAIN-SUFFIX,youtube.com,DIRECT") { t.Errorf("missing domain rule") }
	if !find("GEOSITE,netflix,proxy") { t.Errorf("missing geosite rule") }
	if !find("DOMAIN-KEYWORD,facebook,proxy") { t.Errorf("missing keyword rule") }
	if !find("GEOIP,cn,DIRECT") { t.Errorf("missing geoip rule") }
	if !find("IP-CIDR,10.0.0.0/8,DIRECT") { t.Errorf("missing IP-CIDR rule") }
	if !find("DOMAIN-REGEX,^.*\\.cn$,REJECT") { t.Errorf("missing domain regex rule (target should be REJECT for block)") }
}

func TestConvertXrayRouting_PortRules(t *testing.T) {
	routingJSON := []byte(`{
		"rules": [{
			"port": "0-1023",
			"outboundTag": "direct"
		}],
		"balancers": []
	}`)

	rules, err := convertXrayRouting(routingJSON)
	if err != nil { t.Fatalf("convertXrayRouting failed: %v", err) }

	found := false
	for _, r := range rules {
		if r == "DST-PORT,0-1023,DIRECT" {
			found = true
			break
		}
	}
	if !found { t.Errorf("missing DST-PORT rule") }
}

func TestConvertXrayRouting_BalancerToGroup(t *testing.T) {
	routingJSON := []byte(`{
		"rules": [{
			"domain_suffix": ["example.com"],
			"balancerTag": "mybalancer"
		}],
		"balancers": [{"tag": "mybalancer", "selector": ["proxy"]}]
	}`)

	rules, err := convertXrayRouting(routingJSON)
	if err != nil { t.Fatalf("convertXrayRouting failed: %v", err) }

	found := false
	for _, r := range rules {
		if r == "DOMAIN-SUFFIX,example.com,mybalancer" {
			found = true
			break
		}
	}
	if !found { t.Errorf("missing rule with balancer target") }
}

func TestBuildMihomoRules_WithConversion(t *testing.T) {
	routingJSON := []byte(`{
		"rules": [{
			"domain": ["custom.com"],
			"outboundTag": "proxy"
		}],
		"balancers": []
	}`)

	// Insert converted rules before fallback MATCH
	rules, err := BuildMihomoRules(routingJSON)
	if err != nil { t.Fatalf("BuildMihomoRules failed: %v", err) }

	// Last rule should still be MATCH,Proxy
	lastRule := rules[len(rules)-1]
	if lastRule != "MATCH,Proxy" {
		t.Errorf("last rule = %q, want MATCH,Proxy", lastRule)
	}

	// Should have converted rule
	found := false
	for _, r := range rules {
		if r == "DOMAIN-SUFFIX,custom.com,proxy" {
			found = true
			break
		}
	}
	if !found { t.Errorf("missing converted custom.com rule") }
}

// ── MergeMihomoConfig tests ──

func TestMergeMihomoConfig(t *testing.T) {
	generated := `proxies:
- name: proxy-de-1
  type: vless
  server: de1.com
  port: 443
proxy-groups:
- name: Proxy
  type: select
  proxies: [proxy-de-1]
rules:
- MATCH,Proxy
`

	existing := `mixed-port: 7890
log-level: info
dns:
  enable: true
  ipv6: false
proxies: []
proxy-groups: []
rules: []
`

	merged, err := MergeMihomoConfig(generated, existing)
	if err != nil {
		t.Fatalf("MergeMihomoConfig failed: %v", err)
	}

	checks := []string{
		"mixed-port: 7890",
		"log-level: info",
		"dns:",
		"enable: true",
		"name: proxy-de-1",
		"name: Proxy",
		"type: select",
		"MATCH,Proxy",
	}
	for _, c := range checks {
		if !strings.Contains(merged, c) {
			t.Errorf("merged YAML missing: %q\nMerged:\n%s", c, merged)
		}
	}

	// Verify subscription sections were replaced (no empty arrays left)
	for _, unwanted := range []string{"proxies: []", "proxy-groups: []", "rules: []"} {
		if strings.Contains(merged, unwanted) {
			t.Errorf("merged YAML still contains %q", unwanted)
		}
	}
}

func TestMergeMihomoConfig_EmptyExisting(t *testing.T) {
	gen := "proxies:\n- name: test\n  type: vless\n  server: x.com\n  port: 443\nproxy-groups: []\nrules: []\n"
	merged, err := MergeMihomoConfig(gen, "")
	if err != nil { t.Fatalf("MergeMihomoConfig failed: %v", err) }
	if !strings.Contains(merged, "name: test") {
		t.Errorf("merged YAML missing proxy entry")
	}
}
