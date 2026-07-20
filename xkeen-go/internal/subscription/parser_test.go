package subscription

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

// --- VLESS Reality (primary format from real subscription) ---

func TestParseVlessReality(t *testing.T) {
	uri := "vless://a1b2c3d4-e5f6-7890-abcd-ef1234567890@10.0.0.1:8443?encryption=none&flow=xtls-rprx-vision&type=tcp&headerType=none&security=reality&sni=example.com&fp=qq&pbk=fakePublicKeyBase64EncodedHere_a1b2c3&sid=aabb112233445566#%F0%9F%92%8E%20%D0%90%D0%B2%D1%82%D0%BE"

	entry, err := ParseURI(uri)
	if err != nil {
		t.Fatalf("ParseURI failed: %v", err)
	}

	if entry.Protocol != "vless" {
		t.Errorf("Protocol = %q, want %q", entry.Protocol, "vless")
	}

	// Verify the outbound JSON structure
	var outbound map[string]interface{}
	if err := json.Unmarshal(entry.Outbound, &outbound); err != nil {
		t.Fatalf("Outbound is not valid JSON: %v", err)
	}

	if outbound["protocol"] != "vless" {
		t.Errorf("outbound.protocol = %v, want vless", outbound["protocol"])
	}

	settings := outbound["settings"].(map[string]interface{})
	vnext := settings["vnext"].([]interface{})[0].(map[string]interface{})
	if vnext["address"] != "10.0.0.1" {
		t.Errorf("address = %v, want 10.0.0.1", vnext["address"])
	}
	if port, ok := vnext["port"].(float64); !ok || int(port) != 8443 {
		t.Errorf("port = %v, want 8443", vnext["port"])
	}
	users := vnext["users"].([]interface{})
	user := users[0].(map[string]interface{})
	if user["id"] != "a1b2c3d4-e5f6-7890-abcd-ef1234567890" {
		t.Errorf("user.id mismatch")
	}
	if user["flow"] != "xtls-rprx-vision" {
		t.Errorf("user.flow = %v, want xtls-rprx-vision", user["flow"])
	}
	if user["encryption"] != "none" {
		t.Errorf("user.encryption = %v, want none", user["encryption"])
	}

	stream := outbound["streamSettings"].(map[string]interface{})
	if stream["network"] != "tcp" {
		t.Errorf("network = %v, want tcp", stream["network"])
	}
	if stream["security"] != "reality" {
		t.Errorf("security = %v, want reality", stream["security"])
	}

	realitySettings := stream["realitySettings"].(map[string]interface{})
	if realitySettings["fingerprint"] != "qq" {
		t.Errorf("fp = %v, want qq", realitySettings["fingerprint"])
	}
	if realitySettings["serverName"] != "example.com" {
		t.Errorf("sni = %v, want example.com", realitySettings["serverName"])
	}
	if realitySettings["publicKey"] != "fakePublicKeyBase64EncodedHere_a1b2c3" {
		t.Errorf("pbk mismatch")
	}
	if realitySettings["shortId"] != "aabb112233445566" {
		t.Errorf("sid = %v, want aabb112233445566", realitySettings["shortId"])
	}
	if realitySettings["show"] != false {
		t.Errorf("show = %v, want false", realitySettings["show"])
	}

	// Check mux
	mux := outbound["mux"].(map[string]interface{})
	if mux["enabled"] != true {
		t.Errorf("mux.enabled = %v, want true", mux["enabled"])
	}
	if mux["concurrency"] != float64(-1) {
		t.Errorf("mux.concurrency = %v, want -1", mux["concurrency"])
	}
	if mux["xudpConcurrency"] != float64(16) {
		t.Errorf("mux.xudpConcurrency = %v, want 16", mux["xudpConcurrency"])
	}
	if mux["xudpProxyUDP443"] != "reject" {
		t.Errorf("mux.xudpProxyUDP443 = %v, want reject", mux["xudpProxyUDP443"])
	}

	if entry.Remarks == "" {
		t.Error("Remarks is empty")
	}
}

func TestParseVlessRealityGermanNode(t *testing.T) {
	uri := "vless://a1b2c3d4-e5f6-0005-abcd-ef1234567890@10.0.0.2:8443?encryption=none&flow=xtls-rprx-vision&type=tcp&headerType=none&security=reality&sni=de.example.com&fp=qq&pbk=fakePublicKeyBase64EncodedHere_a1b2c3&sid=aabb112233445566#%F0%9F%87%A9%F0%9F%87%AA%20%E2%9A%A1%EF%B8%8F%20%D0%93%D0%B5%D1%80%D0%BC%D0%B0%D0%BD%D0%B8%D1%8F"

	entry, err := ParseURI(uri)
	if err != nil {
		t.Fatalf("ParseURI failed: %v", err)
	}

	if entry.Country != "DE" {
		t.Errorf("Country = %q, want %q", entry.Country, "DE")
	}
	if entry.Remarks == "" {
		t.Error("Remarks is empty")
	}
}

func TestParseVlessWithTLS(t *testing.T) {
	uri := "vless://uuid@example.com:443?encryption=none&type=tcp&security=tls&sni=example.com&fp=chrome&alpn=h2,http/1.1#Test"

	entry, err := ParseURI(uri)
	if err != nil {
		t.Fatalf("ParseURI failed: %v", err)
	}

	var outbound map[string]interface{}
	json.Unmarshal(entry.Outbound, &outbound)

	stream := outbound["streamSettings"].(map[string]interface{})
	if stream["security"] != "tls" {
		t.Errorf("security = %v, want tls", stream["security"])
	}
	tlsSettings := stream["tlsSettings"].(map[string]interface{})
	if tlsSettings["serverName"] != "example.com" {
		t.Errorf("serverName = %v, want example.com", tlsSettings["serverName"])
	}
	if tlsSettings["fingerprint"] != "chrome" {
		t.Errorf("fingerprint = %v, want chrome", tlsSettings["fingerprint"])
	}
}

func TestParseVlessWithWebSocket(t *testing.T) {
	uri := "vless://uuid@example.com:443?encryption=none&type=ws&security=tls&sni=example.com&path=/ws&host=ws.example.com#WS-Test"

	entry, err := ParseURI(uri)
	if err != nil {
		t.Fatalf("ParseURI failed: %v", err)
	}

	var outbound map[string]interface{}
	json.Unmarshal(entry.Outbound, &outbound)

	stream := outbound["streamSettings"].(map[string]interface{})
	if stream["network"] != "ws" {
		t.Errorf("network = %v, want ws", stream["network"])
	}
	wsSettings := stream["wsSettings"].(map[string]interface{})
	if wsSettings["path"] != "/ws" {
		t.Errorf("ws path = %v, want /ws", wsSettings["path"])
	}
	headers := wsSettings["headers"].(map[string]interface{})
	if headers["Host"] != "ws.example.com" {
		t.Errorf("ws host = %v, want ws.example.com", headers["Host"])
	}
}

func TestParseVlessWithGRPC(t *testing.T) {
	uri := "vless://uuid@example.com:443?encryption=none&type=grpc&security=tls&sni=example.com&serviceName=mygrpc#GRPC-Test"

	entry, err := ParseURI(uri)
	if err != nil {
		t.Fatalf("ParseURI failed: %v", err)
	}

	var outbound map[string]interface{}
	json.Unmarshal(entry.Outbound, &outbound)

	stream := outbound["streamSettings"].(map[string]interface{})
	if stream["network"] != "grpc" {
		t.Errorf("network = %v, want grpc", stream["network"])
	}
	grpcSettings := stream["grpcSettings"].(map[string]interface{})
	if grpcSettings["serviceName"] != "mygrpc" {
		t.Errorf("grpc serviceName = %v, want mygrpc", grpcSettings["serviceName"])
	}
}

// --- Country Code Extraction ---

func TestExtractCountry(t *testing.T) {
	tests := []struct {
		remarks string
		want    string
	}{
		{"🇩🇪 ⚡ Германия", "DE"},
		{"🇳🇱 ⚡ Нидерланды", "NL"},
		{"🇪🇪 ⚡ Эстония", "EE"},
		{"🇫🇮 ⭐ Финляндия", "FI"},
		{"🇬🇧 ⚡ Великобритания", "GB"},
		{"🇺🇸 ⭐ США", "US"},
		{"🇯🇵 ⚡ Япония", "JP"},
		{"🇷🇺 ⭐ Россия", "RU"},
		{"🇹🇷 Турция", "TR"},
		{"💎 Авто | Самый быстрый", ""},
		{"No country here", ""},
		{"", ""},
		{"🇩🇪🇫🇷 double", "DE"},
	}

	for _, tt := range tests {
		got := extractCountry(tt.remarks)
		if got != tt.want {
			t.Errorf("extractCountry(%q) = %q, want %q", tt.remarks, got, tt.want)
		}
	}
}

// --- Tag Generation ---

func TestGenerateTags(t *testing.T) {
	entries := []*ProxyEntry{
		{Country: "DE"},
		{Country: "DE"},
		{Country: "NL"},
		{Country: "US"},
		{Country: "DE"},
		{Country: ""},
	}
	GenerateTags(entries)

	expected := []string{
		"proxy-de-1",
		"proxy-de-2",
		"proxy-nl-1",
		"proxy-us-1",
		"proxy-de-3",
		"proxy-xu-1",
	}

	for i, e := range entries {
		if e.Tag != expected[i] {
			t.Errorf("entry[%d].Tag = %q, want %q", i, e.Tag, expected[i])
		}
	}
}

func TestGenerateTags_UpdatesOutboundJSON(t *testing.T) {
	entry := &ProxyEntry{
		Country:  "DE",
		Outbound: json.RawMessage(`{"protocol":"vless","mux":{"enabled":true}}`),
	}
	GenerateTags([]*ProxyEntry{entry})

	var outbound map[string]interface{}
	json.Unmarshal(entry.Outbound, &outbound)

	if outbound["tag"] != "proxy-de-1" {
		t.Errorf("outbound.tag = %v, want proxy-de-1", outbound["tag"])
	}
}

func TestParseSubscriptionContent_Base64(t *testing.T) {
	lines := []string{
		"vless://uuid1@1.1.1.1:443?security=reality&sni=test.com&type=tcp#%F0%9F%87%A9%F0%9F%87%AA%20Node1",
		"vless://uuid2@2.2.2.2:443?security=reality&sni=test.com&type=tcp#%F0%9F%87%B3%F0%9F%87%B1%20Node2",
	}
	content := base64.StdEncoding.EncodeToString([]byte(strings.Join(lines, "\n")))

	entries, err := ParseSubscriptionContent([]byte(content))
	if err != nil {
		t.Fatalf("ParseSubscriptionContent failed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	if entries[0].Tag == "" || entries[1].Tag == "" {
		t.Error("tags not generated")
	}
	if entries[0].Country != "DE" {
		t.Errorf("entry[0].Country = %q, want DE", entries[0].Country)
	}
	if entries[1].Country != "NL" {
		t.Errorf("entry[1].Country = %q, want NL", entries[1].Country)
	}
}

func TestParseSubscriptionContent_PlainText(t *testing.T) {
	lines := []string{
		"vless://uuid1@1.1.1.1:443?security=reality&type=tcp#Test1",
		"vless://uuid2@2.2.2.2:443?security=reality&type=tcp#Test2",
	}
	content := strings.Join(lines, "\n")

	entries, err := ParseSubscriptionContent([]byte(content))
	if err != nil {
		t.Fatalf("ParseSubscriptionContent failed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
}

// --- Edge Cases ---

func TestParseURI_Empty(t *testing.T) {
	_, err := ParseURI("")
	if err == nil {
		t.Error("expected error for empty URI")
	}
}

func TestParseURI_Unsupported(t *testing.T) {
	_, err := ParseURI("vmess://dGVzdA==")
	if err == nil {
		t.Error("expected error for vmess scheme")
	}
	if !strings.Contains(err.Error(), "unsupported protocol") {
		t.Errorf("error should mention unsupported protocol, got: %v", err)
	}

	_, err2 := ParseURI("ss://creds@host:8388")
	if err2 == nil {
		t.Error("expected error for ss scheme")
	}

	_, err3 := ParseURI("naive://uuid:host@host:443")
	if err3 == nil {
		t.Error("expected error for naive scheme")
	}
}

func TestParseVless_NoFragment(t *testing.T) {
	uri := "vless://uuid@1.2.3.4:443?security=reality&type=tcp"

	entry, err := ParseURI(uri)
	if err != nil {
		t.Fatalf("ParseURI failed: %v", err)
	}
	if entry.Remarks != "" {
		t.Errorf("Remarks = %q, want empty", entry.Remarks)
	}
	if entry.Country != "" {
		t.Errorf("Country = %q, want empty", entry.Country)
	}
}

func TestParseVless_Defaults(t *testing.T) {
	uri := "vless://uuid@1.2.3.4:443#Test"

	entry, err := ParseURI(uri)
	if err != nil {
		t.Fatalf("ParseURI failed: %v", err)
	}

	var outbound map[string]interface{}
	json.Unmarshal(entry.Outbound, &outbound)

	stream := outbound["streamSettings"].(map[string]interface{})
	if stream["network"] != "tcp" {
		t.Errorf("default network = %v, want tcp", stream["network"])
	}
	if stream["security"] != "none" {
		t.Errorf("default security = %v, want none", stream["security"])
	}

	settings := outbound["settings"].(map[string]interface{})
	vnext := settings["vnext"].([]interface{})[0].(map[string]interface{})
	user := vnext["users"].([]interface{})[0].(map[string]interface{})
	if user["encryption"] != "none" {
		t.Errorf("default encryption = %v, want none", user["encryption"])
	}
}

func TestParseVless_SpecialCharsInName(t *testing.T) {
	uri := "vless://uuid@1.2.3.4:443?security=reality&type=tcp#%F0%9F%87%A9%F0%9F%87%AA%20%E2%9A%A1%EF%B8%8F%20%D0%93%D0%B5%D1%80%D0%BC%D0%B0%D0%BD%D0%B8%D1%8F%20%231"

	entry, err := ParseURI(uri)
	if err != nil {
		t.Fatalf("ParseURI failed: %v", err)
	}
	if entry.Country != "DE" {
		t.Errorf("Country = %q, want DE", entry.Country)
	}
}

func TestParseVless_MultipleSameCountry(t *testing.T) {
	uris := []string{
		"vless://uuid1@1.1.1.1:443?security=reality&type=tcp#%F0%9F%87%A9%F0%9F%87%AA%20DE1",
		"vless://uuid2@2.2.2.2:443?security=reality&type=tcp#%F0%9F%87%A9%F0%9F%87%AA%20DE2",
		"vless://uuid3@3.3.3.3:443?security=reality&type=tcp#%F0%9F%87%A9%F0%9F%87%AA%20DE3",
	}

	entries, err := ParseProxiesFromURIs(uris)
	if err != nil {
		t.Fatalf("ParseProxiesFromURIs failed: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}

	expectedTags := []string{"proxy-de-1", "proxy-de-2", "proxy-de-3"}
	for i, e := range entries {
		if e.Tag != expectedTags[i] {
			t.Errorf("entry[%d].Tag = %q, want %q", i, e.Tag, expectedTags[i])
		}
	}
}

func TestParseVless_RealSubscriptionURI(t *testing.T) {
	uri := "vless://a1b2c3d4-e5f6-7890-abcd-ef1234567890@10.0.0.2:8443?encryption=none&flow=xtls-rprx-vision&type=tcp&headerType=none&security=reality&sni=example.com&fp=qq&pbk=fakePublicKeyBase64EncodedHere_a1b2c3&sid=aabb112233445566#%F0%9F%87%A6%F0%9F%87%A9%20%D0%90%D0%BD%D0%B4%D0%BE%D1%80%D1%80%D0%B0"

	entry, err := ParseURI(uri)
	if err != nil {
		t.Fatalf("ParseURI failed: %v", err)
	}
	if entry.Country != "AD" {
		t.Errorf("Country = %q, want AD", entry.Country)
	}

	var outbound map[string]interface{}
	json.Unmarshal(entry.Outbound, &outbound)
	settings := outbound["settings"].(map[string]interface{})
	vnext := settings["vnext"].([]interface{})[0].(map[string]interface{})
	if vnext["address"] != "10.0.0.2" {
		t.Errorf("address = %v, want 10.0.0.2", vnext["address"])
	}
	stream := outbound["streamSettings"].(map[string]interface{})
	reality := stream["realitySettings"].(map[string]interface{})
	if reality["fingerprint"] != "qq" {
		t.Errorf("fp = %v, want qq", reality["fingerprint"])
	}
}

// --- HostPort parsing ---

func TestParseHostPort_IPv4(t *testing.T) {
	host, port, err := parseHostPort("1.2.3.4:8443")
	if err != nil {
		t.Fatal(err)
	}
	if host != "1.2.3.4" {
		t.Errorf("host = %q, want 1.2.3.4", host)
	}
	if port != "8443" {
		t.Errorf("port = %q, want 8443", port)
	}
}

func TestParseHostPort_IPv6(t *testing.T) {
	host, port, err := parseHostPort("[::1]:8443")
	if err != nil {
		t.Fatal(err)
	}
	if host != "::1" {
		t.Errorf("host = %q, want ::1", host)
	}
	if port != "8443" {
		t.Errorf("port = %q, want 8443", port)
	}
}

func TestParseHostPort_NoPort(t *testing.T) {
	_, _, err := parseHostPort("example.com")
	if err == nil {
		t.Error("expected error for missing port")
	}
}

func TestBase64Decode_Variants(t *testing.T) {
	original := "hello world"

	// Standard
	encoded := base64.StdEncoding.EncodeToString([]byte(original))
	decoded, err := base64Decode(encoded)
	if err != nil || string(decoded) != original {
		t.Errorf("std base64: decoded=%q, err=%v", decoded, err)
	}

	// No padding
	encodedNoPad := base64.RawStdEncoding.EncodeToString([]byte(original))
	decoded, err = base64Decode(encodedNoPad)
	if err != nil || string(decoded) != original {
		t.Errorf("raw base64: decoded=%q, err=%v", decoded, err)
	}

	// URL-safe
	encodedURL := base64.URLEncoding.EncodeToString([]byte(original))
	decoded, err = base64Decode(encodedURL)
	if err != nil || string(decoded) != original {
		t.Errorf("url base64: decoded=%q, err=%v", decoded, err)
	}
}

// --- TROJAN ---

func TestParseTrojan_TLS_TCP(t *testing.T) {
	uri := "trojan://my-password@example.com:443?security=tls&type=tcp&sni=example.com&fp=chrome#Example%20TLS%20Trojan"
	entry, err := ParseURI(uri)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.Protocol != "trojan" {
		t.Errorf("expected protocol trojan, got %s", entry.Protocol)
	}
	if entry.Remarks != "Example TLS Trojan" {
		t.Errorf("expected remarks 'Example TLS Trojan', got %q", entry.Remarks)
	}

	// Verify outbound JSON structure
	var outbound map[string]interface{}
	if err := json.Unmarshal(entry.Outbound, &outbound); err != nil {
		t.Fatalf("failed to unmarshal outbound: %v", err)
	}
	if outbound["protocol"] != "trojan" {
		t.Errorf("expected protocol trojan in outbound, got %v", outbound["protocol"])
	}
	settings := outbound["settings"].(map[string]interface{})
	servers := settings["servers"].([]interface{})
	server := servers[0].(map[string]interface{})
	if server["password"] != "my-password" {
		t.Errorf("expected password my-password, got %v", server["password"])
	}
	if server["address"] != "example.com" {
		t.Errorf("expected address example.com, got %v", server["address"])
	}
	if int(server["port"].(float64)) != 443 {
		t.Errorf("expected port 443, got %v", server["port"])
	}

	// Check stream settings
	ss := outbound["streamSettings"].(map[string]interface{})
	if ss["network"] != "tcp" {
		t.Errorf("expected network tcp, got %v", ss["network"])
	}
	if ss["security"] != "tls" {
		t.Errorf("expected security tls, got %v", ss["security"])
	}
}

func TestParseTrojan_GRPC(t *testing.T) {
	uri := "trojan://pass@manil.space:443?security=tls&type=grpc&serviceName=my-service&sni=manil.space&alpn=h2#manil.space%20grpc%20trojan"
	entry, err := ParseURI(uri)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.Protocol != "trojan" {
		t.Errorf("expected protocol trojan, got %s", entry.Protocol)
	}

	var outbound map[string]interface{}
	json.Unmarshal(entry.Outbound, &outbound)
	ss := outbound["streamSettings"].(map[string]interface{})
	if ss["network"] != "grpc" {
		t.Errorf("expected network grpc, got %v", ss["network"])
	}
	grpc := ss["grpcSettings"].(map[string]interface{})
	if grpc["serviceName"] != "my-service" {
		t.Errorf("expected serviceName my-service, got %v", grpc["serviceName"])
	}
}

func TestParseTrojan_DefaultTLS(t *testing.T) {
	// trojan without security param should default to tls
	uri := "trojan://pass@host:443?sni=host#test"
	entry, err := ParseURI(uri)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var outbound map[string]interface{}
	json.Unmarshal(entry.Outbound, &outbound)
	ss := outbound["streamSettings"].(map[string]interface{})
	if ss["security"] != "tls" {
		t.Errorf("trojan should default to tls security, got %v", ss["security"])
	}
}

// --- HYSTERIA2 ---

// getStr is a test helper to extract a string from nested map
func getStr(m map[string]interface{}, path ...string) string {
	for i, k := range path {
		v, ok := m[k]
		if !ok {
			return ""
		}
		if i == len(path)-1 {
			s, _ := v.(string)
			return s
		}
		m, ok = v.(map[string]interface{})
		if !ok {
			return ""
		}
	}
	return ""
}

// getNum is a test helper to extract a float64 from nested map
func getNum(m map[string]interface{}, path ...string) float64 {
	for i, k := range path {
		v, ok := m[k]
		if !ok {
			return 0
		}
		if i == len(path)-1 {
			n, _ := v.(float64)
			return n
		}
		m, ok = v.(map[string]interface{})
		if !ok {
			return 0
		}
	}
	return 0
}

func unmarshalOutbound(t *testing.T, raw json.RawMessage) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("failed to unmarshal outbound: %v", err)
	}
	return m
}

func TestHysteria2_BasicStructure(t *testing.T) {
	uri := "hysteria2://my-password@example.com:443?sni=example.com#Test"
	entry, err := ParseURI(uri)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.Protocol != "hysteria2" {
		t.Fatalf("expected protocol hysteria2, got %s", entry.Protocol)
	}

	o := unmarshalOutbound(t, entry.Outbound)

	// protocol must be "hysteria" in Xray outbound (not hysteria2)
	if o["protocol"] != "hysteria" {
		t.Errorf("protocol: got %v, want hysteria", o["protocol"])
	}

	// settings: flat structure {version, address, port}
	settings := o["settings"].(map[string]interface{})
	if v := getNum(settings, "version"); v != 2 {
		t.Errorf("settings.version: got %v, want 2", v)
	}
	if addr := getStr(settings, "address"); addr != "example.com" {
		t.Errorf("settings.address: got %q, want example.com", addr)
	}
	if port := getNum(settings, "port"); port != 443 {
		t.Errorf("settings.port: got %v, want 443", port)
	}
	// NO servers array
	if _, has := settings["servers"]; has {
		t.Error("settings should NOT contain 'servers' key")
	}
	// NO password in settings
	if _, has := settings["password"]; has {
		t.Error("settings should NOT contain 'password' key (belongs in hysteriaSettings.auth)")
	}

	// streamSettings.network must be "hysteria"
	ss := o["streamSettings"].(map[string]interface{})
	if ss["network"] != "hysteria" {
		t.Errorf("streamSettings.network: got %v, want hysteria", ss["network"])
	}
	if ss["security"] != "tls" {
		t.Errorf("streamSettings.security: got %v, want tls", ss["security"])
	}

	// hysteriaSettings should contain the password
	hsRaw, hasHS := ss["hysteriaSettings"]
	if !hasHS {
		t.Fatal("streamSettings missing hysteriaSettings")
	}
	hs := hsRaw.(map[string]interface{})
	if getNum(hs, "version") != 2 {
		t.Errorf("hysteriaSettings.version: got %v, want 2", getNum(hs, "version"))
	}
	if hs["auth"] != "my-password" {
		t.Errorf("hysteriaSettings.auth: got %v, want my-password", hs["auth"])
	}

	// tlsSettings should contain the SNI
	tlsRaw, hasTLS := ss["tlsSettings"]
	if !hasTLS {
		t.Fatal("streamSettings missing tlsSettings")
	}
	tls := tlsRaw.(map[string]interface{})
	if tls["serverName"] != "example.com" {
		t.Errorf("tlsSettings.serverName: got %v, want example.com", tls["serverName"])
	}
}

func TestHysteria2_ObfsIgnored(t *testing.T) {
	// Xray does NOT support obfs/salamander — params should be silently ignored
	uri := "hysteria2://pass@host:443?sni=host&obfs=salamander&obfs-password=secret#Obfs"
	entry, err := ParseURI(uri)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	o := unmarshalOutbound(t, entry.Outbound)

	// settings must NOT contain obfs
	settings := o["settings"].(map[string]interface{})
	if _, has := settings["obfs"]; has {
		t.Error("settings should NOT contain 'obfs' (Xray doesn't support it)")
	}

	// hysteriaSettings should NOT contain obfs
	ss := o["streamSettings"].(map[string]interface{})
	hs := ss["hysteriaSettings"].(map[string]interface{})
	if _, has := hs["obfs"]; has {
		t.Error("hysteriaSettings should NOT contain 'obfs'")
	}
}

func TestHysteria2_Insecure(t *testing.T) {
	uri := "hysteria2://pass@host:443?sni=host&insecure=true#Insecure"
	entry, err := ParseURI(uri)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	o := unmarshalOutbound(t, entry.Outbound)
	ss := o["streamSettings"].(map[string]interface{})
	tls := ss["tlsSettings"].(map[string]interface{})
	insecure, _ := tls["allowInsecure"].(bool)
	if !insecure {
		t.Error("expected tlsSettings.allowInsecure = true")
	}
}

func TestParseSubscriptionContent_MixedProtocols(t *testing.T) {
	content := `vless://uuid@host1:443?security=tls&type=tcp&sni=host1#Node%201
trojan://pass@host2:443?security=tls&type=tcp&sni=host2#Node%202
hysteria2://pass@host3:443?sni=host3#Node%203
naive://ignored
`
	entries, err := ParseSubscriptionContent([]byte(content))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries (vless+trojan+hysteria2), got %d", len(entries))
	}
	if entries[0].Protocol != "vless" {
		t.Errorf("entry[0] expected vless, got %s", entries[0].Protocol)
	}
	if entries[1].Protocol != "trojan" {
		t.Errorf("entry[1] expected trojan, got %s", entries[1].Protocol)
	}
	if entries[2].Protocol != "hysteria2" {
		t.Errorf("entry[2] expected hysteria2, got %s", entries[2].Protocol)
	}
	// Verify hysteria2 outbound structure
	o := unmarshalOutbound(t, entries[2].Outbound)
	if o["protocol"] != "hysteria" {
		t.Errorf("hysteria2 outbound protocol: got %v, want hysteria", o["protocol"])
	}
	settings := o["settings"].(map[string]interface{})
	if getStr(settings, "address") != "host3" {
		t.Errorf("address: got %q, want host3", getStr(settings, "address"))
	}
}

// --- Port Validation (#P4) ---

func TestParseVless_InvalidPort_OutOfRange(t *testing.T) {
	tests := []struct {
		name string
		uri  string
	}{
		{"port 0", "vless://uuid@1.2.3.4:0?security=tls&type=tcp#Test"},
		{"port 99999", "vless://uuid@1.2.3.4:99999?security=tls&type=tcp#Test"},
		{"port 65536", "vless://uuid@1.2.3.4:65536?security=tls&type=tcp#Test"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseURI(tt.uri)
			if err == nil {
				t.Error("expected error for invalid port " + tt.name)
			}
			if !strings.Contains(err.Error(), "out of range") {
				t.Errorf("error should mention out of range, got: %v", err)
			}
		})
	}
}

func TestParseTrojan_InvalidPort_OutOfRange(t *testing.T) {
	_, err := ParseURI("trojan://pass@host:0?security=tls&type=tcp#Test")
	if err == nil {
		t.Error("expected error for port 0")
	}
	if !strings.Contains(err.Error(), "out of range") {
		t.Errorf("error should mention out of range, got: %v", err)
	}
}

func TestParseHysteria2_InvalidPort_OutOfRange(t *testing.T) {
	_, err := ParseURI("hysteria2://pass@host:0?security=tls#Test")
	if err == nil {
		t.Error("expected error for port 0")
	}
	if !strings.Contains(err.Error(), "out of range") {
		t.Errorf("error should mention out of range, got: %v", err)
	}
}

// --- Stable Tags (#S7) ---

func TestGenerateTags_StableAcrossReorder(t *testing.T) {
	// Create 4 proxies with different RawURIs — like a real subscription
	entries1 := []*ProxyEntry{
		{Country: "DE", RawURI: "vless://a@1.1.1.1:443#DE1"},
		{Country: "DE", RawURI: "vless://b@2.2.2.2:443#DE2"},
		{Country: "NL", RawURI: "vless://c@3.3.3.3:443#NL1"},
	}
	GenerateTags(entries1)
	tags1 := map[string]string{}
	for _, e := range entries1 {
		tags1[e.RawURI] = e.Tag
	}

	// Same proxies in REVERSE order
	entries2 := []*ProxyEntry{
		{Country: "NL", RawURI: "vless://c@3.3.3.3:443#NL1"},
		{Country: "DE", RawURI: "vless://b@2.2.2.2:443#DE2"},
		{Country: "DE", RawURI: "vless://a@1.1.1.1:443#DE1"},
	}
	GenerateTags(entries2)

	// Each proxy should have the SAME tag in both calls
	for uri, want := range tags1 {
		got := ""
		for _, e := range entries2 {
			if e.RawURI == uri {
				got = e.Tag
				break
			}
		}
		if got != want {
			t.Errorf("proxy %q: expected tag %q, got %q (tags should be stable across reorder)", uri, want, got)
		}
	}
}

func TestGenerateTags_StableAcrossRefreshSameSet(t *testing.T) {
	// Same set of proxies should produce identical tags on two separate calls
	uris := []string{
		"vless://a@1.1.1.1:443?security=tls&type=tcp#%F0%9F%87%A9%F0%9F%87%AA%20DE1",
		"vless://b@2.2.2.2:443?security=tls&type=tcp#%F0%9F%87%A9%F0%9F%87%AA%20DE2",
		"vless://c@3.3.3.3:443?security=tls&type=tcp#%F0%9F%87%A9%F0%9F%87%AA%20DE3",
	}

	entries1, _ := ParseProxiesFromURIs(uris)
	entries2, _ := ParseProxiesFromURIs(uris)

	// reorder to simulate list changed
	entries3, _ := ParseProxiesFromURIs([]string{uris[2], uris[0], uris[1]})

	tags1 := map[string]string{}
	for _, e := range entries1 {
		tags1[e.RawURI] = e.Tag
	}

	for _, e := range entries2 {
		expected, ok := tags1[e.RawURI]
		if !ok {
			t.Errorf("unexpected proxy in entries2: %s", e.RawURI)
			continue
		}
		if e.Tag != expected {
			t.Errorf("proxy %s: second call tag %q != first call %q", e.RawURI, e.Tag, expected)
		}
	}

	for _, e := range entries3 {
		expected, ok := tags1[e.RawURI]
		if !ok {
			t.Errorf("unexpected proxy in entries3: %s", e.RawURI)
			continue
		}
		if e.Tag != expected {
			t.Errorf("proxy %s (reordered): tag %q != first call %q", e.RawURI, e.Tag, expected)
		}
	}
}

// --- #P1 Guard: merged batch no collision ---

func TestGenerateTags_MergedBatchNoCollision(t *testing.T) {
	// Simulate two subscriptions' proxies merged into ONE list before GenerateTags
	merged := []*ProxyEntry{
		{Country: "DE", RawURI: "vless://sub1-de@1.1.1.1:443#Sub1-DE"},
		{Country: "DE", RawURI: "vless://sub1-de@2.2.2.2:443#Sub1-DE2"},
		{Country: "DE", RawURI: "vless://sub2-de@3.3.3.3:443#Sub2-DE"},
		{Country: "NL", RawURI: "vless://sub1-nl@4.4.4.4:443#Sub1-NL"},
	}
	GenerateTags(merged)

	tagSet := make(map[string]bool)
	for _, e := range merged {
		if tagSet[e.Tag] {
			t.Errorf("DUPLICATE tag %q found for proxy %s", e.Tag, e.RawURI)
		}
		tagSet[e.Tag] = true
	}
	if len(tagSet) != len(merged) {
		t.Errorf("expected %d unique tags, got %d", len(merged), len(tagSet))
	}
	// Verify tag format
	for _, e := range merged {
		if !strings.HasPrefix(e.Tag, "proxy-") {
			t.Errorf("tag %q should start with 'proxy-'", e.Tag)
		}
	}
}

// --- #P8 Size limit ---

func TestParseSubscriptionContent_TooLarge(t *testing.T) {
	// Create a payload just over the limit
	big := make([]byte, maxSubscriptionSize+1)
	for i := range big {
		big[i] = 'a'
	}
	_, err := ParseSubscriptionContent(big)
	if err == nil {
		t.Error("expected error for oversized subscription body")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Errorf("error should mention too large, got: %v", err)
	}
}

// --- #P6/#P7 ParseProxiesFromURIs returns error on total failure ---

func TestParseProxiesFromURIs_AllFailReturnsError(t *testing.T) {
	uris := []string{
		"vmess://dGVzdA==",
		"ss://creds@host:8388",
		"invalid-uri-without-scheme",
	}
	entries, err := ParseProxiesFromURIs(uris)
	if err == nil {
		t.Error("expected error when all URIs fail")
	}
	if !strings.Contains(err.Error(), "failed") {
		t.Errorf("error should mention failure, got: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestParseProxiesFromURIs_MixedBadGood(t *testing.T) {
	uris := []string{
		"vmess://dGVzdA==",
		"vless://uuid@1.2.3.4:443?security=tls&type=tcp#Test",
		"invalid-uri",
	}
	entries, err := ParseProxiesFromURIs(uris)
	if err != nil {
		t.Errorf("expected no error for mixed input, got: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 good entry, got %d", len(entries))
	}
	if len(entries) > 0 && entries[0].Protocol != "vless" {
		t.Errorf("expected vless protocol, got %s", entries[0].Protocol)
	}
}

func TestParseTrojan_Reality(t *testing.T) {
	uri := "trojan://pass@10.0.0.1:443?security=reality&type=tcp&pbk=fakePublicKeyBase64EncodedHere_a1b2c3&fp=chrome&sni=example.com&sid=aabb112233445566&flow=xtls-rprx-vision#Trojan%20Reality"
	entry, err := ParseURI(uri)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.Protocol != "trojan" {
		t.Fatalf("expected trojan, got %s", entry.Protocol)
	}

	var outbound map[string]interface{}
	json.Unmarshal(entry.Outbound, &outbound)
	ss := outbound["streamSettings"].(map[string]interface{})
	if ss["security"] != "reality" {
		t.Errorf("expected security reality, got %v", ss["security"])
	}
	rs := ss["realitySettings"].(map[string]interface{})
	if rs["publicKey"] != "fakePublicKeyBase64EncodedHere_a1b2c3" {
		t.Errorf("expected publicKey, got %v", rs["publicKey"])
	}
	if rs["fingerprint"] != "chrome" {
		t.Errorf("expected fingerprint chrome, got %v", rs["fingerprint"])
	}
	if rs["serverName"] != "example.com" {
		t.Errorf("expected serverName example.com, got %v", rs["serverName"])
	}
	if rs["shortId"] != "aabb112233445566" {
		t.Errorf("expected shortId, got %v", rs["shortId"])
	}
}

func TestParseTrojan_WS(t *testing.T) {
	uri := "trojan://pass@host:443?security=tls&type=ws&path=/ws&host=ws-host&sni=host#WS%20Trojan"
	entry, err := ParseURI(uri)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var outbound map[string]interface{}
	json.Unmarshal(entry.Outbound, &outbound)
	ss := outbound["streamSettings"].(map[string]interface{})
	if ss["network"] != "ws" {
		t.Errorf("expected network ws, got %v", ss["network"])
	}
	ws := ss["wsSettings"].(map[string]interface{})
	if ws["path"] != "/ws" {
		t.Errorf("expected path /ws, got %v", ws["path"])
	}
	headers := ws["headers"].(map[string]interface{})
	if headers["Host"] != "ws-host" {
		t.Errorf("expected host ws-host, got %v", headers["Host"])
	}
}

func TestParseTrojan_CountryExtraction(t *testing.T) {
	uri := "trojan://pass@host:443?security=tls&sni=host#%F0%9F%87%A9%F0%9F%87%AA%20%E2%9A%A1%20Fast%20Trojan"
	entry, err := ParseURI(uri)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.Country != "DE" {
		t.Errorf("expected country DE, got %q", entry.Country)
	}
}

func TestParseTrojan_MissingAt(t *testing.T) {
	_, err := ParseURI("trojan://no-at-separator:443")
	if err == nil {
		t.Error("expected error for missing @ separator")
	}
	if !strings.Contains(err.Error(), "missing @") {
		t.Errorf("error should mention missing @, got: %v", err)
	}
}

func TestParseTrojan_InvalidPort(t *testing.T) {
	_, err := ParseURI("trojan://pass@host:notaport")
	if err == nil {
		t.Error("expected error for invalid port")
	}
	if !strings.Contains(err.Error(), "port") {
		t.Errorf("error should mention port, got: %v", err)
	}
}

func TestParseHysteria2_Insecure(t *testing.T) {
	uri := "hysteria2://pass@host:443?sni=host&insecure=true#Insecure%20HY2"
	entry, err := ParseURI(uri)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var outbound map[string]interface{}
	json.Unmarshal(entry.Outbound, &outbound)
	ss := outbound["streamSettings"].(map[string]interface{})
	_ = ss["tlsSettings"]
}

func TestHysteria2_WithALPN(t *testing.T) {
	uri := "hysteria2://pass@host:443?sni=host&alpn=h3,h2#ALPN"
	entry, err := ParseURI(uri)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	o := unmarshalOutbound(t, entry.Outbound)
	ss := o["streamSettings"].(map[string]interface{})
	tls := ss["tlsSettings"].(map[string]interface{})
	alpn := tls["alpn"].([]interface{})
	if len(alpn) != 2 || alpn[0] != "h3" || alpn[1] != "h2" {
		t.Errorf("expected alpn [h3,h2], got %v", alpn)
	}
}

func TestHysteria2_CountryExtraction(t *testing.T) {
	uri := "hysteria2://pass@host:443?sni=host#%F0%9F%87%BA%F0%9F%87%B8%20%F0%9F%8E%AE%20Gaming"
	entry, err := ParseURI(uri)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.Country != "US" {
		t.Errorf("expected country US, got %q", entry.Country)
	}
}

func TestHysteria2_NoSNI(t *testing.T) {
	uri := "hysteria2://pass@host:443#NoSNI"
	entry, err := ParseURI(uri)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	o := unmarshalOutbound(t, entry.Outbound)
	// settings must still have address and port
	settings := o["settings"].(map[string]interface{})
	if getStr(settings, "address") != "host" {
		t.Errorf("address: got %q, want host", getStr(settings, "address"))
	}
	if getNum(settings, "port") != 443 {
		t.Errorf("port: got %v, want 443", getNum(settings, "port"))
	}

	// hysteriaSettings must still exist with auth
	ss := o["streamSettings"].(map[string]interface{})
	hs := ss["hysteriaSettings"].(map[string]interface{})
	if hs["auth"] != "pass" {
		t.Errorf("auth: got %v, want pass", hs["auth"])
	}
}

func TestParseHysteria2_MissingAt(t *testing.T) {
	_, err := ParseURI("hysteria2://no-at-separator:443")
	if err == nil {
		t.Error("expected error for missing @ separator")
	}
}

func TestParseHysteria2_InvalidPort(t *testing.T) {
	_, err := ParseURI("hysteria2://pass@host:notaport")
	if err == nil {
		t.Error("expected error for invalid port")
	}
}

func TestParseSubscriptionContent_Base64MixedProtocols(t *testing.T) {
	lines := "vless://uuid@host1:443?type=tcp&security=reality&pbk=key&fp=chrome&sni=host1&sid=abcd#VLESS%20Node\n" +
		"trojan://pass@host2:443?security=tls&type=tcp&sni=host2#Trojan%20Node\n" +
		"hysteria2://pass@host3:443?sni=host3#HY2%20Node\n"
	encoded := base64.StdEncoding.EncodeToString([]byte(lines))

	entries, err := ParseSubscriptionContent([]byte(encoded))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	protocols := make(map[string]bool)
	for _, e := range entries {
		protocols[e.Protocol] = true
	}
	if !protocols["vless"] || !protocols["trojan"] || !protocols["hysteria2"] {
		t.Errorf("expected all 3 protocols, got %v", protocols)
	}
}
