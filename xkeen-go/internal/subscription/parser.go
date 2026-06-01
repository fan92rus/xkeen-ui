package subscription

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ParseURI dispatches parsing based on protocol scheme.
func ParseURI(rawURI string) (*ProxyEntry, error) {
	rawURI = strings.TrimSpace(rawURI)
	if rawURI == "" {
		return nil, fmt.Errorf("empty URI")
	}

	switch {
	case strings.HasPrefix(rawURI, "vless://"):
		return parseVless(rawURI)
	case strings.HasPrefix(rawURI, "trojan://"):
		return parseTrojan(rawURI)
	case strings.HasPrefix(rawURI, "hysteria2://"):
		return parseHysteria2(rawURI)
	default:
		return nil, fmt.Errorf("unsupported protocol: got %s", rawURI[:min(20, len(rawURI))])
	}
}

// --- VLESS ---

func parseVless(rawURI string) (*ProxyEntry, error) {
	// vless://uuid@host:port?params#fragment
	withoutFragment := rawURI
	fragment := ""
	if idx := strings.LastIndex(rawURI, "#"); idx != -1 {
		withoutFragment = rawURI[:idx]
		fragment = rawURI[idx+1:]
	}

	// Strip scheme
	rest := strings.TrimPrefix(withoutFragment, "vless://")

	// Split uuid@host:port?params
	atIdx := strings.Index(rest, "@")
	if atIdx == -1 {
		return nil, fmt.Errorf("invalid vless URI: missing @ separator")
	}
	uuid := rest[:atIdx]
	hostPortParams := rest[atIdx+1:]

	// Split host:port and query
	var hostPort, queryStr string
	qIdx := strings.Index(hostPortParams, "?")
	if qIdx == -1 {
		hostPort = hostPortParams
	} else {
		hostPort = hostPortParams[:qIdx]
		queryStr = hostPortParams[qIdx+1:]
	}

	// Parse host:port — handle IPv6
	host, portStr, err := parseHostPort(hostPort)
	if err != nil {
		return nil, fmt.Errorf("invalid vless host:port: %w", err)
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("invalid vless port %q: %w", portStr, err)
	}

	// Parse query params
	params, _ := url.ParseQuery(queryStr)

	// Decode fragment for remarks
	remarks := ""
	if fragment != "" {
		decoded, err := url.QueryUnescape(fragment)
		if err != nil {
			decoded = fragment
		}
		remarks = decoded
	}

	// Build outbound JSON
	outbound, err := buildVlessOutbound(uuid, host, port, params, "")
	if err != nil {
		return nil, err
	}

	country := extractCountry(remarks)

	return &ProxyEntry{
		Protocol: "vless",
		Outbound: outbound,
		RawURI:   rawURI,
		Remarks:  remarks,
		Country:  country,
	}, nil
}

func buildVlessOutbound(uuid, host string, port int, params url.Values, tag string) (json.RawMessage, error) {
	// Encryption
	encryption := params.Get("encryption")
	if encryption == "" {
		encryption = "none"
	}

	// Flow
	flow := params.Get("flow")

	// User object
	user := map[string]interface{}{
		"id":         uuid,
		"encryption": encryption,
	}
	if flow != "" {
		user["flow"] = flow
	}

	// Vnext
	vnext := map[string]interface{}{
		"address": host,
		"port":    port,
		"users":   []interface{}{user},
	}

	// Stream settings
	network := params.Get("type")
	if network == "" {
		network = "tcp"
	}
	security := params.Get("security")
	if security == "" {
		security = "none"
	}

	streamSettings := map[string]interface{}{
		"network":  network,
		"security": security,
	}

	// Security-specific settings
	switch security {
	case "reality":
		streamSettings["realitySettings"] = buildRealitySettings(params)
	case "tls":
		streamSettings["tlsSettings"] = buildTLSSettings(params)
	}

	// Network-specific settings
	switch network {
	case "ws":
		wsSettings := map[string]interface{}{}
		if p := params.Get("path"); p != "" {
			wsSettings["path"] = p
		}
		if h := params.Get("host"); h != "" {
			wsSettings["headers"] = map[string]interface{}{"Host": h}
		}
		streamSettings["wsSettings"] = wsSettings
	case "grpc":
		grpcSettings := map[string]interface{}{}
		if sn := params.Get("serviceName"); sn != "" {
			grpcSettings["serviceName"] = sn
		}
		streamSettings["grpcSettings"] = grpcSettings
	case "tcp":
		// Default, no extra settings needed
		if ht := params.Get("headerType"); ht != "" && ht != "none" {
			tcpSettings := map[string]interface{}{
				"header": map[string]interface{}{"type": ht},
			}
			streamSettings["tcpSettings"] = tcpSettings
		}
	}

	// Build complete outbound
	outbound := map[string]interface{}{
		"protocol": "vless",
		"settings": map[string]interface{}{
			"vnext": []interface{}{vnext},
		},
		"streamSettings": streamSettings,
	}
	outbound["mux"] = DefaultMux
	if tag != "" {
		outbound["tag"] = tag
	}

	data, err := json.Marshal(outbound)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal vless outbound: %w", err)
	}
	return data, nil
}

func buildRealitySettings(params url.Values) map[string]interface{} {
	return map[string]interface{}{
		"show":        false,
		"fingerprint": params.Get("fp"),
		"serverName":  params.Get("sni"),
		"publicKey":   params.Get("pbk"),
		"shortId":     params.Get("sid"),
		"spiderX":     params.Get("spiderX"),
	}
}

func buildTLSSettings(params url.Values) map[string]interface{} {
	tls := map[string]interface{}{}
	if sni := params.Get("sni"); sni != "" {
		tls["serverName"] = sni
	}
	if fp := params.Get("fp"); fp != "" {
		tls["fingerprint"] = fp
	}
	if alpn := params.Get("alpn"); alpn != "" {
		tls["alpn"] = strings.Split(alpn, ",")
	}
	return tls
}

// --- TROJAN ---

func parseTrojan(rawURI string) (*ProxyEntry, error) {
	// trojan://password@host:port?params#fragment
	withoutFragment := rawURI
	fragment := ""
	if idx := strings.LastIndex(rawURI, "#"); idx != -1 {
		withoutFragment = rawURI[:idx]
		fragment = rawURI[idx+1:]
	}

	rest := strings.TrimPrefix(withoutFragment, "trojan://")

	atIdx := strings.Index(rest, "@")
	if atIdx == -1 {
		return nil, fmt.Errorf("invalid trojan URI: missing @ separator")
	}
	password := rest[:atIdx]
	hostPortParams := rest[atIdx+1:]

	var hostPort, queryStr string
	qIdx := strings.Index(hostPortParams, "?")
	if qIdx == -1 {
		hostPort = hostPortParams
	} else {
		hostPort = hostPortParams[:qIdx]
		queryStr = hostPortParams[qIdx+1:]
	}

	host, portStr, err := parseHostPort(hostPort)
	if err != nil {
		return nil, fmt.Errorf("invalid trojan host:port: %w", err)
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("invalid trojan port %q: %w", portStr, err)
	}

	params, _ := url.ParseQuery(queryStr)
	remarks := extractRemarks(fragment)

	outbound, err := buildTrojanOutbound(password, host, port, params, "")
	if err != nil {
		return nil, err
	}

	country := extractCountry(remarks)

	return &ProxyEntry{
		Protocol: "trojan",
		Outbound: outbound,
		RawURI:   rawURI,
		Remarks:  remarks,
		Country:  country,
	}, nil
}

func buildTrojanOutbound(password, host string, port int, params url.Values, tag string) (json.RawMessage, error) {
	// Trojan server entry
	server := map[string]interface{}{
		"address":  host,
		"port":     port,
		"password": password,
	}

	// Stream settings — same structure as vless
	network := params.Get("type")
	if network == "" {
		network = "tcp"
	}
	security := params.Get("security")
	if security == "" {
		// Trojan typically uses TLS
		security = "tls"
	}

	streamSettings := map[string]interface{}{
		"network":  network,
		"security": security,
	}

	switch security {
	case "reality":
		streamSettings["realitySettings"] = buildRealitySettings(params)
	case "tls":
		streamSettings["tlsSettings"] = buildTLSSettings(params)
	}

	switch network {
	case "ws":
		wsSettings := map[string]interface{}{}
		if p := params.Get("path"); p != "" {
			wsSettings["path"] = p
		}
		if h := params.Get("host"); h != "" {
			wsSettings["headers"] = map[string]interface{}{"Host": h}
		}
		streamSettings["wsSettings"] = wsSettings
	case "grpc":
		grpcSettings := map[string]interface{}{}
		if sn := params.Get("serviceName"); sn != "" {
			grpcSettings["serviceName"] = sn
		}
		streamSettings["grpcSettings"] = grpcSettings
	case "tcp":
		if ht := params.Get("headerType"); ht != "" && ht != "none" {
			tcpSettings := map[string]interface{}{
				"header": map[string]interface{}{"type": ht},
			}
			streamSettings["tcpSettings"] = tcpSettings
		}
	}

	outbound := map[string]interface{}{
		"protocol": "trojan",
		"settings": map[string]interface{}{
			"servers": []interface{}{server},
		},
		"streamSettings": streamSettings,
	}
	outbound["mux"] = DefaultMux
	if tag != "" {
		outbound["tag"] = tag
	}

	data, err := json.Marshal(outbound)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal trojan outbound: %w", err)
	}
	return data, nil
}

// --- HYSTERIA2 ---

func parseHysteria2(rawURI string) (*ProxyEntry, error) {
	// hysteria2://password@host:port?params#fragment
	withoutFragment := rawURI
	fragment := ""
	if idx := strings.LastIndex(rawURI, "#"); idx != -1 {
		withoutFragment = rawURI[:idx]
		fragment = rawURI[idx+1:]
	}

	rest := strings.TrimPrefix(withoutFragment, "hysteria2://")

	atIdx := strings.Index(rest, "@")
	if atIdx == -1 {
		return nil, fmt.Errorf("invalid hysteria2 URI: missing @ separator")
	}
	password := rest[:atIdx]
	hostPortParams := rest[atIdx+1:]

	var hostPort, queryStr string
	qIdx := strings.Index(hostPortParams, "?")
	if qIdx == -1 {
		hostPort = hostPortParams
	} else {
		hostPort = hostPortParams[:qIdx]
		queryStr = hostPortParams[qIdx+1:]
	}

	host, portStr, err := parseHostPort(hostPort)
	if err != nil {
		return nil, fmt.Errorf("invalid hysteria2 host:port: %w", err)
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("invalid hysteria2 port %q: %w", portStr, err)
	}

	params, _ := url.ParseQuery(queryStr)
	remarks := extractRemarks(fragment)

	outbound, err := buildHysteria2Outbound(password, host, port, params, "")
	if err != nil {
		return nil, err
	}

	country := extractCountry(remarks)

	return &ProxyEntry{
		Protocol: "hysteria2",
		Outbound: outbound,
		RawURI:   rawURI,
		Remarks:  remarks,
		Country:  country,
	}, nil
}

func buildHysteria2Outbound(password, host string, port int, params url.Values, tag string) (json.RawMessage, error) {
	server := map[string]interface{}{
		"address":  host,
		"port":     port,
		"password": password,
	}

	// Obfs
	if obfs := params.Get("obfs"); obfs != "" {
		server["obfs"] = map[string]interface{}{
			"type": obfs,
			"password": params.Get("obfs-password"),
		}
	}

	outbound := map[string]interface{}{
		"protocol": "hysteria2",
		"settings": map[string]interface{}{
			"servers": []interface{}{server},
		},
	}

	// TLS is built-in for hysteria2, but we pass sni/port-hopping via sockopt
	sockopt := map[string]interface{}{}
	if sni := params.Get("sni"); sni != "" {
		sockopt["dialer"] = map[string]interface{}{
			"domainStrategy": "AsIs",
		}
	}

	streamSettings := map[string]interface{}{
		"network":  "tcp",
		"security": "tls",
		"tlsSettings": map[string]interface{}{
			"serverName": params.Get("sni"),
		},
	}
	if alpn := params.Get("alpn"); alpn != "" {
		streamSettings["tlsSettings"].(map[string]interface{})["alpn"] = strings.Split(alpn, ",")
	}
	outbound["streamSettings"] = streamSettings

	if len(sockopt) > 0 {
		outbound["sockopt"] = sockopt
	}

	outbound["mux"] = DefaultMux
	if tag != "" {
		outbound["tag"] = tag
	}

	data, err := json.Marshal(outbound)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal hysteria2 outbound: %w", err)
	}
	return data, nil
}

// --- VMESS ---

func GenerateTags(entries []*ProxyEntry) {
	counters := make(map[string]int)

	for _, entry := range entries {
		cc := strings.ToLower(entry.Country)
		if cc == "" {
			cc = "xu" // unknown
		}

		counters[cc]++
		entry.Tag = fmt.Sprintf("proxy-%s-%d", cc, counters[cc])
	}

	// Update the tag in the outbound JSON for each entry
	for _, entry := range entries {
		if len(entry.Outbound) > 0 {
			var outbound map[string]interface{}
			if err := json.Unmarshal(entry.Outbound, &outbound); err == nil {
				outbound["tag"] = entry.Tag
				if data, err := json.Marshal(outbound); err == nil {
					entry.Outbound = data
				}
			}
		}
	}
}

// --- Helpers ---

// extractCountry extracts a 2-letter country code from emoji flags in remarks.
// Emoji flags use regional indicator symbols: 🇩🇪 = U+1F1E9 U+1F1EA → "DE".
func extractCountry(remarks string) string {
	for i := 0; i < len(remarks); {
		r, size := utf8.DecodeRuneInString(remarks[i:])
		if isRegionalIndicator(r) && i+size <= len(remarks) {
			r2, _ := utf8.DecodeRuneInString(remarks[i+size:])
			if isRegionalIndicator(r2) {
				c1 := rune(r - 0x1F1E6 + 'A')
				c2 := rune(r2 - 0x1F1E6 + 'A')
				return string([]rune{c1, c2})
			}
		}
		i += size
	}
	return ""
}

// isRegionalIndicator checks if a rune is a regional indicator symbol (U+1F1E6..U+1F1FF).
func isRegionalIndicator(r rune) bool {
	return r >= 0x1F1E6 && r <= 0x1F1FF
}

// parseHostPort splits host:port handling IPv6 brackets.
func parseHostPort(s string) (host, port string, err error) {
	if strings.HasPrefix(s, "[") {
		// IPv6: [::1]:port
		closeBracket := strings.Index(s, "]")
		if closeBracket == -1 {
			return "", "", fmt.Errorf("missing closing bracket in IPv6 address")
		}
		host = s[1:closeBracket]
		rest := s[closeBracket+1:]
		if len(rest) == 0 || rest[0] != ':' {
			return "", "", fmt.Errorf("missing port after IPv6 address")
		}
		port = rest[1:]
	} else {
		// IPv4: host:port
		lastColon := strings.LastIndex(s, ":")
		if lastColon == -1 {
			return "", "", fmt.Errorf("missing port")
		}
		host = s[:lastColon]
		port = s[lastColon+1:]
	}
	if host == "" {
		return "", "", fmt.Errorf("empty host")
	}
	if port == "" {
		return "", "", fmt.Errorf("empty port")
	}
	return host, port, nil
}

// base64Decode tries standard and URL-safe base64 with flexible padding.
func base64Decode(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	// Try standard base64
	if data, err := base64.StdEncoding.DecodeString(s); err == nil {
		return data, nil
	}
	// Try with padding
	padded := s + strings.Repeat("=", (4-len(s)%4)%4)
	if data, err := base64.StdEncoding.DecodeString(padded); err == nil {
		return data, nil
	}
	// Try URL-safe
	if data, err := base64.URLEncoding.DecodeString(s); err == nil {
		return data, nil
	}
	paddedURL := s + strings.Repeat("=", (4-len(s)%4)%4)
	return base64.URLEncoding.DecodeString(paddedURL)
}

// jsonInt extracts an integer from a JSON number that might be string or float.
func jsonInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case string:
		if i, err := strconv.Atoi(n); err == nil {
			return i
		}
	case int:
		return n
	}
	return 0
}

// extractRemarks decodes the fragment portion of a URI.
func extractRemarks(fragment string) string {
	if fragment == "" {
		return ""
	}
	decoded, err := url.QueryUnescape(fragment)
	if err != nil {
		return fragment
	}
	return decoded
}

// ParseSubscriptionContent parses a raw subscription response (base64-encoded or plain text).
// Returns a list of ProxyEntry from all recognized URIs.
func ParseSubscriptionContent(data []byte) ([]*ProxyEntry, error) {
	content := strings.TrimSpace(string(data))

	// Try base64 decode first
	decoded, err := base64Decode(content)
	if err == nil {
		content = strings.TrimSpace(string(decoded))
	}

	lines := strings.Split(content, "\n")
	var entries []*ProxyEntry
	var errors []string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		entry, err := ParseURI(line)
		if err != nil {
			errors = append(errors, err.Error())
			continue
		}
		entries = append(entries, entry)
	}

	// Assign tags
	GenerateTags(entries)

	if len(entries) == 0 && len(errors) > 0 {
		return nil, fmt.Errorf("failed to parse any proxies: %s", strings.Join(errors, "; "))
	}

	return entries, nil
}

// ParseProxiesFromURIs parses a list of share URIs (not base64-encoded).
func ParseProxiesFromURIs(uris []string) ([]*ProxyEntry, error) {
	var entries []*ProxyEntry
	for _, uri := range uris {
		uri = strings.TrimSpace(uri)
		if uri == "" {
			continue
		}
		entry, err := ParseURI(uri)
		if err != nil {
			continue // skip unrecognized
		}
		entries = append(entries, entry)
	}
	GenerateTags(entries)
	return entries, nil
}



