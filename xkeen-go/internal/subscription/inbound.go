package subscription

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
)

// inboundPattern matches Xray inbound config files like "01_inbounds.json".
var inboundPattern = regexp.MustCompile(`^.*_inbounds\.json$`)

// ManagedSocksInboundFile is the filename xkeen-ui writes to the Xray config
// directory to add a local SOCKS5 inbound for routing subscription fetches
// through the VPN tunnel. Loaded by Xray alongside the user's own files.
const ManagedSocksInboundFile = "99_xkeen_ui_socks.json"

// ManagedSocksPort is the port the managed SOCKS5 inbound listens on.
const ManagedSocksPort = 10808

// managedSocksInboundPattern matches the xkeen-ui-managed SOCKS5 inbound file.
var managedSocksInboundPattern = regexp.MustCompile(`^` + regexp.QuoteMeta(ManagedSocksInboundFile) + `$`)

// isInboundFile reports whether name is a file that may contain Xray inbounds.
func isInboundFile(name string) bool {
	return inboundPattern.MatchString(name) || managedSocksInboundPattern.MatchString(name)
}

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
		if !e.IsDir() && isInboundFile(e.Name()) {
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

// ManagedSocksInboundConfig returns the JSON content for the xkeen-ui-managed
// SOCKS5 inbound file. This file is placed in the Xray config directory so
// that Xray loads it as an additional inbound, allowing subscription
// fetches to route through the VPN tunnel.
//
// The inbound listens on 127.0.0.1 only (not exposed externally) and accepts
// unauthenticated connections (safe because it's loopback-only).
func ManagedSocksInboundConfig() string {
	return fmt.Sprintf(`{
  "inbounds": [
    {
      "port": %d,
      "protocol": "socks",
      "listen": "127.0.0.1",
      "settings": {
        "auth": "noauth",
        "udp": false
      },
      "tag": "xkeen-ui"
    }
  ]
}
`, ManagedSocksPort)
}
