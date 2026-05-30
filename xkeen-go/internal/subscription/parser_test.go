package subscription

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

// --- VLESS Reality (primary format from real subscription) ---

func TestParseVlessReality(t *testing.T) {
	uri := "vless://d9819f3a-8ff2-4ba8-ad57-dfd05beeaa72@50.7.157.252:8443?encryption=none&flow=xtls-rprx-vision&type=tcp&headerType=none&security=reality&sni=auto.quattro-tech.ru&fp=qq&pbk=10rVZPoOUP1TlQviIAsQ_jAROX0fRQxH0C92nq_zGQc&sid=43dcff53849b81e6#%F0%9F%92%8E%20%D0%90%D0%B2%D1%82%D0%BE"

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
	if vnext["address"] != "50.7.157.252" {
		t.Errorf("address = %v, want 50.7.157.252", vnext["address"])
	}
	if port, ok := vnext["port"].(float64); !ok || int(port) != 8443 {
		t.Errorf("port = %v, want 8443", vnext["port"])
	}
	users := vnext["users"].([]interface{})
	user := users[0].(map[string]interface{})
	if user["id"] != "d9819f3a-8ff2-4ba8-ad57-dfd05beeaa72" {
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
	if realitySettings["serverName"] != "auto.quattro-tech.ru" {
		t.Errorf("sni = %v, want auto.quattro-tech.ru", realitySettings["serverName"])
	}
	if realitySettings["publicKey"] != "10rVZPoOUP1TlQviIAsQ_jAROX0fRQxH0C92nq_zGQc" {
		t.Errorf("pbk mismatch")
	}
	if realitySettings["shortId"] != "43dcff53849b81e6" {
		t.Errorf("sid = %v, want 43dcff53849b81e6", realitySettings["shortId"])
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
	uri := "vless://d9819f3a-8ff2-0005-ad57-dfd05beeaa72@46.243.142.12:8443?encryption=none&flow=xtls-rprx-vision&type=tcp&headerType=none&security=reality&sni=de.quattro-tech.ru&fp=qq&pbk=10rVZPoOUP1TlQviIAsQ_jAROX0fRQxH0C92nq_zGQc&sid=43dcff53849b81e6#%F0%9F%87%A9%F0%9F%87%AA%20%E2%9A%A1%EF%B8%8F%20%D0%93%D0%B5%D1%80%D0%BC%D0%B0%D0%BD%D0%B8%D1%8F"

	entry, err := ParseURI(uri)
	if err != nil {
		t.Fatalf("ParseURI failed: %v", err)
	}

	if entry.Country != "DE" {
		t.Errorf("Country = %q, want %q", entry.Country, "DE")
	}
	if entry.Marker != "⚡" {
		t.Errorf("Marker = %q, want ⚡", entry.Marker)
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

// --- Marker Extraction ---

func TestExtractMarker(t *testing.T) {
	tests := []struct {
		remarks string
		want    string
	}{
		{"🇩🇪 ⚡ Германия", "⚡"},
		{"🇳🇱 ⭐ Нидерланды", "⭐"},
		{"🇪🇪 🎮 Гейминг", "🎮"},
		{"🇷🇺 0.5X Мобильные", "0.5X"},
		{"⬇ Быстрые", "⬇"},
		{"💎 Авто", "💎"},
		{"🇩🇪 Германия", ""},
		{"", ""},
	}

	for _, tt := range tests {
		got := extractMarker(tt.remarks)
		if got != tt.want {
			t.Errorf("extractMarker(%q) = %q, want %q", tt.remarks, got, tt.want)
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

// --- VMESS ---

func TestParseVmess(t *testing.T) {
	vmessConfig := map[string]interface{}{
		"v":    "2",
		"ps":   "🇺🇸 US Node",
		"add":  "1.2.3.4",
		"port": 443,
		"id":   "uuid-here",
		"aid":  0,
		"net":  "ws",
		"type": "none",
		"host": "ws.example.com",
		"path": "/ws",
		"tls":  "tls",
		"sni":  "example.com",
	}
	configJSON, _ := json.Marshal(vmessConfig)
	uri := "vmess://" + base64.StdEncoding.EncodeToString(configJSON)

	entry, err := ParseURI(uri)
	if err != nil {
		t.Fatalf("ParseURI(vmess) failed: %v", err)
	}

	if entry.Protocol != "vmess" {
		t.Errorf("Protocol = %q, want vmess", entry.Protocol)
	}
	if entry.Country != "US" {
		t.Errorf("Country = %q, want US", entry.Country)
	}

	var outbound map[string]interface{}
	json.Unmarshal(entry.Outbound, &outbound)
	if outbound["protocol"] != "vmess" {
		t.Errorf("outbound.protocol = %v, want vmess", outbound["protocol"])
	}
	stream := outbound["streamSettings"].(map[string]interface{})
	if stream["network"] != "ws" {
		t.Errorf("network = %v, want ws", stream["network"])
	}
}

// --- Shadowsocks ---

func TestParseShadowsocks(t *testing.T) {
	creds := base64.StdEncoding.EncodeToString([]byte("aes-256-gcm:mypassword"))
	uri := "ss://" + creds + "@1.2.3.4:8388#MySS"

	entry, err := ParseURI(uri)
	if err != nil {
		t.Fatalf("ParseURI(ss) failed: %v", err)
	}

	if entry.Protocol != "shadowsocks" {
		t.Errorf("Protocol = %q, want shadowsocks", entry.Protocol)
	}

	var outbound map[string]interface{}
	json.Unmarshal(entry.Outbound, &outbound)
	settings := outbound["settings"].(map[string]interface{})
	servers := settings["servers"].([]interface{})
	server := servers[0].(map[string]interface{})

	if server["method"] != "aes-256-gcm" {
		t.Errorf("method = %v, want aes-256-gcm", server["method"])
	}
	if server["password"] != "mypassword" {
		t.Errorf("password = %v, want mypassword", server["password"])
	}
	if server["address"] != "1.2.3.4" {
		t.Errorf("address = %v, want 1.2.3.4", server["address"])
	}
}

// --- Trojan ---

func TestParseTrojan(t *testing.T) {
	uri := "trojan://mypassword@example.com:443?security=tls&sni=example.com&type=tcp#%F0%9F%87%A9%F0%9F%87%AA%20Trojan"

	entry, err := ParseURI(uri)
	if err != nil {
		t.Fatalf("ParseURI(trojan) failed: %v", err)
	}

	if entry.Protocol != "trojan" {
		t.Errorf("Protocol = %q, want trojan", entry.Protocol)
	}
	if entry.Country != "DE" {
		t.Errorf("Country = %q, want DE", entry.Country)
	}

	var outbound map[string]interface{}
	json.Unmarshal(entry.Outbound, &outbound)
	settings := outbound["settings"].(map[string]interface{})
	servers := settings["servers"].([]interface{})
	server := servers[0].(map[string]interface{})
	if server["password"] != "mypassword" {
		t.Errorf("password = %v, want mypassword", server["password"])
	}
}

// --- Subscription Content Parsing ---

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
	_, err := ParseURI("http://example.com")
	if err == nil {
		t.Error("expected error for unsupported scheme")
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
	if entry.Marker != "⚡" {
		t.Errorf("Marker = %q, want ⚡", entry.Marker)
	}
}

func TestParseVless_MobileMarker(t *testing.T) {
	uri := "vless://uuid@1.2.3.4:443?security=reality&type=tcp#%F0%9F%87%A9%F0%9F%87%AA%200.5X%20Mobile"

	entry, err := ParseURI(uri)
	if err != nil {
		t.Fatalf("ParseURI failed: %v", err)
	}
	if entry.Marker != "0.5X" {
		t.Errorf("Marker = %q, want 0.5X", entry.Marker)
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
	uri := "vless://d9819f3a-8ff2-4ba8-ad57-dfd05beeaa72@46.243.142.12:8443?encryption=none&flow=xtls-rprx-vision&type=tcp&headerType=none&security=reality&sni=auto.quattro-tech.ru&fp=qq&pbk=10rVZPoOUP1TlQviIAsQ_jAROX0fRQxH0C92nq_zGQc&sid=43dcff53849b81e6#%F0%9F%87%A6%F0%9F%87%A9%20%D0%90%D0%BD%D0%B4%D0%BE%D1%80%D1%80%D0%B0"

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
	if vnext["address"] != "46.243.142.12" {
		t.Errorf("address = %v, want 46.243.142.12", vnext["address"])
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
