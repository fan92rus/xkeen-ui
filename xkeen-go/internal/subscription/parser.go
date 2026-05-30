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

	if strings.HasPrefix(rawURI, "vless://") {
		return parseVless(rawURI)
	}
	if strings.HasPrefix(rawURI, "vmess://") {
		return parseVmess(rawURI)
	}
	if strings.HasPrefix(rawURI, "trojan://") {
		return parseTrojan(rawURI)
	}
	if strings.HasPrefix(rawURI, "ss://") {
		return parseShadowsocks(rawURI)
	}
	return nil, fmt.Errorf("unsupported protocol scheme in URI: %s", rawURI[:min(20, len(rawURI))])
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
	marker := extractMarker(remarks)

	return &ProxyEntry{
		Protocol: "vless",
		Outbound: outbound,
		RawURI:   rawURI,
		Remarks:  remarks,
		Country:  country,
		Marker:   marker,
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

// --- VMESS ---

func parseVmess(rawURI string) (*ProxyEntry, error) {
	// vmess://base64encodedJSON
	encoded := strings.TrimPrefix(rawURI, "vmess://")
	decoded, err := base64Decode(encoded)
	if err != nil {
		return nil, fmt.Errorf("invalid vmess base64: %w", err)
	}

	var vmess struct {
		V    string `json:"v"`
		Ps   string `json:"ps"`
		Add  string `json:"add"`
		Port any    `json:"port"`
		ID   string `json:"id"`
		Aid  any    `json:"aid"`
		Net  string `json:"net"`
		Type string `json:"type"`
		Host string `json:"host"`
		Path string `json:"path"`
		TLS  string `json:"tls"`
		Sni  string `json:"sni"`
		ALPN string `json:"alpn"`
		FP   string `json:"fp"`
	}

	if err := json.Unmarshal(decoded, &vmess); err != nil {
		return nil, fmt.Errorf("invalid vmess JSON: %w", err)
	}

	port := jsonInt(vmess.Port)
	aid := jsonInt(vmess.Aid)

	// Build outbound
	security := "none"
	if vmess.TLS == "tls" {
		security = "tls"
	}

	streamSettings := map[string]interface{}{
		"network":  vmess.Net,
		"security": security,
	}

	if security == "tls" {
		tlsSettings := map[string]interface{}{}
		if vmess.Sni != "" {
			tlsSettings["serverName"] = vmess.Sni
		}
		if vmess.FP != "" {
			tlsSettings["fingerprint"] = vmess.FP
		}
		if vmess.ALPN != "" {
			tlsSettings["alpn"] = strings.Split(vmess.ALPN, ",")
		}
		streamSettings["tlsSettings"] = tlsSettings
	}

	switch vmess.Net {
	case "ws":
		ws := map[string]interface{}{}
		if vmess.Path != "" {
			ws["path"] = vmess.Path
		}
		if vmess.Host != "" {
			ws["headers"] = map[string]interface{}{"Host": vmess.Host}
		}
		streamSettings["wsSettings"] = ws
	case "grpc":
		grpc := map[string]interface{}{}
		if vmess.Path != "" {
			grpc["serviceName"] = vmess.Path
		}
		streamSettings["grpcSettings"] = grpc
	}

	user := map[string]interface{}{
		"id":       vmess.ID,
		"alterId":  aid,
		"security": "auto",
	}

	vnextEntry := map[string]interface{}{
		"address": vmess.Add,
		"port":    port,
		"users":   []interface{}{user},
	}
	outbound := map[string]interface{}{
		"protocol": "vmess",
		"settings": map[string]interface{}{
			"vnext": []interface{}{vnextEntry},
		},
		"streamSettings": streamSettings,
	}
	outbound["mux"] = DefaultMux

	data, err := json.Marshal(outbound)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal vmess outbound: %w", err)
	}

	remarks := vmess.Ps
	country := extractCountry(remarks)
	marker := extractMarker(remarks)

	return &ProxyEntry{
		Protocol: "vmess",
		Outbound: data,
		RawURI:   rawURI,
		Remarks:  remarks,
		Country:  country,
		Marker:   marker,
	}, nil
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

	remarks := ""
	if fragment != "" {
		decoded, err := url.QueryUnescape(fragment)
		if err != nil {
			decoded = fragment
		}
		remarks = decoded
	}

	network := params.Get("type")
	if network == "" {
		network = "tcp"
	}
	security := params.Get("security")
	if security == "" {
		security = "tls"
	}

	streamSettings := map[string]interface{}{
		"network":  network,
		"security": security,
	}

	if security == "tls" {
		streamSettings["tlsSettings"] = buildTLSSettings(params)
	}

	switch network {
	case "ws":
		ws := map[string]interface{}{}
		if p := params.Get("path"); p != "" {
			ws["path"] = p
		}
		if h := params.Get("host"); h != "" {
			ws["headers"] = map[string]interface{}{"Host": h}
		}
		streamSettings["wsSettings"] = ws
	case "grpc":
		grpc := map[string]interface{}{}
		if sn := params.Get("serviceName"); sn != "" {
			grpc["serviceName"] = sn
		}
		streamSettings["grpcSettings"] = grpc
	}

	serverEntry := map[string]interface{}{
		"address":  host,
		"port":     port,
		"password": password,
	}
	outbound := map[string]interface{}{
		"protocol": "trojan",
		"settings": map[string]interface{}{
			"servers": []interface{}{serverEntry},
		},
		"streamSettings": streamSettings,
	}
	outbound["mux"] = DefaultMux

	data, err := json.Marshal(outbound)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal trojan outbound: %w", err)
	}

	country := extractCountry(remarks)
	marker := extractMarker(remarks)

	return &ProxyEntry{
		Protocol: "trojan",
		Outbound: data,
		RawURI:   rawURI,
		Remarks:  remarks,
		Country:  country,
		Marker:   marker,
	}, nil
}

// --- SHADOWSOCKS ---

func parseShadowsocks(rawURI string) (*ProxyEntry, error) {
	// ss://base64(method:password)@host:port#fragment
	// or ss://base64(method:password@host:port)#fragment (legacy)
	withoutFragment := rawURI
	fragment := ""
	if idx := strings.LastIndex(rawURI, "#"); idx != -1 {
		withoutFragment = rawURI[:idx]
		fragment = rawURI[idx+1:]
	}

	rest := strings.TrimPrefix(withoutFragment, "ss://")

	remarks := ""
	if fragment != "" {
		decoded, err := url.QueryUnescape(fragment)
		if err != nil {
			decoded = fragment
		}
		remarks = decoded
	}

	// Try modern format: base64(method:password)@host:port
	atIdx := strings.LastIndex(rest, "@")
	if atIdx != -1 {
		encoded := rest[:atIdx]
		hostPort := rest[atIdx+1:]

		decoded, err := base64Decode(encoded)
		if err != nil {
			return nil, fmt.Errorf("invalid ss base64 credential: %w", err)
		}

		parts := strings.SplitN(string(decoded), ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid ss credentials format")
		}
		method := parts[0]
		password := parts[1]

		host, portStr, err := parseHostPort(hostPort)
		if err != nil {
			return nil, fmt.Errorf("invalid ss host:port: %w", err)
		}
		port, err := strconv.Atoi(portStr)
		if err != nil {
			return nil, fmt.Errorf("invalid ss port: %w", err)
		}

		serverEntry := map[string]interface{}{
			"address":  host,
			"port":     port,
			"method":   method,
			"password": password,
		}
		outbound := map[string]interface{}{
			"protocol": "shadowsocks",
			"settings": map[string]interface{}{
				"servers": []interface{}{serverEntry},
			},
		}
		outbound["mux"] = DefaultMux

		data, err := json.Marshal(outbound)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal ss outbound: %w", err)
		}

		country := extractCountry(remarks)
		marker := extractMarker(remarks)

		return &ProxyEntry{
			Protocol: "shadowsocks",
			Outbound: data,
			RawURI:   rawURI,
			Remarks:  remarks,
			Country:  country,
			Marker:   marker,
		}, nil
	}

	// Legacy format: entire thing is base64
	decoded, err := base64Decode(rest)
	if err != nil {
		return nil, fmt.Errorf("invalid ss legacy base64: %w", err)
	}

	// method:password@host:port
	parts := strings.SplitN(string(decoded), "@", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid ss legacy format")
	}

	credParts := strings.SplitN(parts[0], ":", 2)
	if len(credParts) != 2 {
		return nil, fmt.Errorf("invalid ss legacy credentials")
	}
	method := credParts[0]
	password := credParts[1]

	host, portStr, err := parseHostPort(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid ss legacy host:port: %w", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("invalid ss legacy port: %w", err)
	}

	serverEntry := map[string]interface{}{
		"address":  host,
		"port":     port,
		"method":   method,
		"password": password,
	}
	outbound := map[string]interface{}{
		"protocol": "shadowsocks",
		"settings": map[string]interface{}{
			"servers": []interface{}{serverEntry},
		},
	}
	outbound["mux"] = DefaultMux

	data, err := json.Marshal(outbound)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal ss outbound: %w", err)
	}

	country := extractCountry(remarks)
	marker := extractMarker(remarks)

	return &ProxyEntry{
		Protocol: "shadowsocks",
		Outbound: data,
		RawURI:   rawURI,
		Remarks:  remarks,
		Country:  country,
		Marker:   marker,
	}, nil
}

// --- Tag Generation ---

// GenerateTags assigns unique tags to all proxy entries based on country code.
// Format: proxy-{country_code}-{index} (e.g., proxy-de-1, proxy-de-2, proxy-us-1).
// Entries without a country get "proxy-xu-{index}" (unknown).
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

// knownMarkerVariants maps marker strings to their canonical form.
var knownMarkerVariants = map[string]string{
	"⚡":  "⚡",   // fast (with and without variation selector)
	"⭐":  "⭐",   // standard
	"🎮":  "🎮",   // gaming
	"0.5X": "0.5X", // mobile
	"1X":   "1X",
	"⬇":  "⬇",   // download
	"💎":  "💎",   // auto
}

// extractMarker finds the first known marker in the remarks string.
func extractMarker(remarks string) string {
	// Check text markers first (0.5X, 1X)
	if idx := strings.Index(remarks, "0.5X"); idx != -1 {
		return "0.5X"
	}
	if idx := strings.Index(remarks, "1X"); idx != -1 && (idx == 0 || remarks[idx-1] != ' ') {
		// Make sure it's not part of a bigger word
		return "1X"
	}

	// Check emoji markers
	for i := 0; i < len(remarks); {
		r, size := utf8.DecodeRuneInString(remarks[i:])
		// Skip variation selector
		nextIdx := i + size
		if nextIdx < len(remarks) {
			if nextR, nextSz := utf8.DecodeRuneInString(remarks[nextIdx:]); nextR == 0xFE0F {
				nextIdx += nextSz
			}
		}
		if canonical, ok := knownMarkerVariants[string(r)]; ok {
			return canonical
		}
		i = nextIdx
	}

	return ""
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

// min returns the smaller of a and b.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

