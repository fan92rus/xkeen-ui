package subscription

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
)

// inboundPattern matches Xray inbound config files like "01_inbounds.json".
var inboundPattern = regexp.MustCompile(`^.*_inbounds\.json$`)

// xrayInbound represents a single inbound entry in an Xray config.
type xrayInbound struct {
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
}

// DetectInboundProxy scans dir for the first file matching .*_inbounds.json,
// parses it, and returns a proxy URL for the first usable SOCKS5/HTTP inbound.
//
// Priority: socks5 > http. dokodemo-door and unknown protocols are ignored.
//
// Returns an empty string if no usable inbound is found (missing/unreadable
// dir, invalid JSON, no matching protocol). This is intentional: a missing
// inbound config is not fatal — the caller simply falls back to direct
// fetching.
func DetectInboundProxy(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "" // unreadable dir → silent fallback to direct
	}

	// Collect matching files, sorted by name for deterministic precedence.
	var matches []string
	for _, e := range entries {
		if !e.IsDir() && inboundPattern.MatchString(e.Name()) {
			matches = append(matches, e.Name())
		}
	}
	if len(matches) == 0 {
		return ""
	}
	sort.Strings(matches)

	for _, name := range matches {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue // unreadable file → try next
		}

		var wrapper struct {
			Inbounds []xrayInbound `json:"inbounds"`
		}
		if err := json.Unmarshal(data, &wrapper); err != nil {
			continue // invalid JSON → try next file
		}

		// First pass: look for SOCKS (highest priority).
		for _, ib := range wrapper.Inbounds {
			if ib.Protocol == "socks" && ib.Port > 0 {
				return "socks5://127.0.0.1:" + strconv.Itoa(ib.Port)
			}
		}
		// Second pass: look for HTTP.
		for _, ib := range wrapper.Inbounds {
			if ib.Protocol == "http" && ib.Port > 0 {
				return "http://127.0.0.1:" + strconv.Itoa(ib.Port)
			}
		}
	}

	return "" // no usable inbound found
}
