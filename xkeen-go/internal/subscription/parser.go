package subscription

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
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

	if !strings.HasPrefix(rawURI, "vless://") {
		return nil, fmt.Errorf("unsupported protocol: only vless is supported, got %s", rawURI[:min(20, len(rawURI))])
	}
	return parseVless(rawURI)
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

// extractMarker dynamically extracts a category marker from the remarks string.
// It works with any subscription format by:
//   1. Tokenizing by spaces, pipes, dashes
//   2. Skipping flag emojis (🇩🇪)
//   3. Returning the first "marker-class" token:
//      - emoji tokens (⚡, ⭐, 🎮, ⬇, 💎, etc.) are always markers
//      - text tokens that aren't country codes, numbers, or domains
//
// Heuristic: if only 1 non-flag text token remains, it's a country name → no marker.
func extractMarker(remarks string) string {
	if remarks == "" {
		return ""
	}

	// Split by common delimiters
	rawTokens := strings.FieldsFunc(remarks, func(r rune) bool {
		return r == ' ' || r == '|' || r == '—' || r == '–' || r == '-' || r == '\t'
	})

	var candidates []string

	for _, token := range rawTokens {
		if token == "" {
			continue
		}

		// Strip variation selectors for comparison
		cleaned := strings.Map(func(r rune) rune {
			if r == 0xFE0F {
				return -1
			}
			return r
		}, token)
		if cleaned == "" {
			continue
		}

		// Skip flag emojis entirely (two regional indicators)
		if isFlagToken(cleaned) {
			continue
		}

		// Emoji tokens (non-flag, non-letter) are always marker candidates
		// BUT skip if domain-like (has TLD-like suffix: .com, .net, .ru, etc.)
		if isEmojiToken(cleaned) && !isDomainLike(cleaned) {
			candidates = append(candidates, cleaned)
			continue
		}

		// Skip numeric-only tokens (01, 1, 23)
		if isNumericOnly(cleaned) {
			continue
		}

		// Skip 2-letter country codes (US, DE, NL)
		if isCountryCode(cleaned) {
			continue
		}

		// Skip domain-like tokens (contain dots with letter suffix, like com.twitter)
		if isDomainLike(cleaned) {
			continue
		}

		// Text token that survived filtering
		candidates = append(candidates, cleaned)
	}

	// If only emoji candidates survived: return the first one.
	// If only text candidates: likely country/city names, not markers.
	// Mixed (emoji + text): return the first emoji.
	var emojiCandidates []string
	var textCandidates []string
	for _, c := range candidates {
		if isEmojiToken(c) {
			emojiCandidates = append(emojiCandidates, c)
		} else {
			textCandidates = append(textCandidates, c)
		}
	}

	// Prefer emoji markers over text
	if len(emojiCandidates) > 0 {
		return emojiCandidates[0]
	}

	// Text-only candidates: valid if:
	//   - pipe-delimited format (Marzban/V2Board), OR
	//   - remarks contains a flag emoji AND first text candidate is short (≤4 chars)
	hasFlag := false
	for _, r := range remarks {
		if r >= 0x1F1E6 && r <= 0x1F1FF {
			hasFlag = true
			break
		}
	}
	if len(textCandidates) > 0 {
		// Trim trailing punctuation for the short-marker check
		trimmedCandidate := strings.TrimRight(textCandidates[0], ",.;:!")
		if strings.ContainsRune(remarks, '|') || (hasFlag && utf8.RuneCountInString(trimmedCandidate) <= 4) {
			return textCandidates[0]
		}
	}

	return ""
}

// isDomainLike checks if a token looks like a domain (e.g. "com.twitter", "google.com").
// Returns true if the token contains a dot followed by 2+ letters.
func isDomainLike(s string) bool {
	dotIdx := strings.LastIndex(s, ".")
	if dotIdx < 0 || dotIdx >= len(s)-2 {
		return false
	}
	suffix := s[dotIdx+1:]
	for _, r := range suffix {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')) {
			return false
		}
	}
	return true
}

// isFlagToken checks if a string consists of exactly two regional indicator runes.
func isFlagToken(s string) bool {
	count := 0
	for _, r := range s {
		if r >= 0x1F1E6 && r <= 0x1F1FF {
			count++
		} else {
			return false
		}
	}
	return count == 2
}

// isEmojiToken checks if a token is primarily an emoji (not a word with punctuation).
// Returns true only if the token starts with a non-letter/digit rune (typical for emojis).
func isEmojiToken(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r == 0xFE0F || r == 0x200D { // variation selector, ZWJ
			continue
		}
		// If the first real character is a letter or digit, it's text
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || (r >= 0x0400 && r <= 0x04FF) {
			return false
		}
		// First real character is non-letter → likely emoji
		return true
	}
	return false
}

// isLetterOrDigit checks if a rune is a letter or digit (Unicode-aware).
func isLetterOrDigit(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || (r >= 0x0400 && r <= 0x04FF) // Cyrillic
}

// isCountryCode checks if a 2-letter string looks like a country code.
func isCountryCode(s string) bool {
	if len(s) != 2 {
		return false
	}
	for _, r := range s {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return true
}

// isNumericOnly checks if a string consists entirely of digits.
func isNumericOnly(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// ExtractAllMarkers returns a sorted list of unique markers that appear
// in at least 2 entries. Single-occurrence markers are filtered as noise.
func ExtractAllMarkers(entries []*ProxyEntry) []string {
	counts := make(map[string]int)
	for _, e := range entries {
		if e.Marker != "" {
			counts[e.Marker]++
		}
	}
	var markers []string
	for m, c := range counts {
		if c >= 2 {
			markers = append(markers, m)
		}
	}
	sort.Strings(markers)
	return markers
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



