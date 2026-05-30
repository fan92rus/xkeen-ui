// Package subscription provides subscription management for xray proxy configs.
// It handles fetching, parsing, filtering, and generating xray configuration
// from subscription URLs (Hiddify-compatible format).
package subscription

import (
	"encoding/json"
	"time"
)

// Subscription represents a single subscription source (e.g., Hiddify panel URL).
type Subscription struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	URL        string    `json:"url"`
	Enabled    bool      `json:"enabled"`
	Interval   int       `json:"interval"`     // minutes, 0 = manual only
	LastFetch  time.Time `json:"last_fetch"`
	LastError  string    `json:"last_error"`
	ProxyCount int       `json:"proxy_count"`
}

// ProxyEntry represents a single parsed proxy from a subscription.
type ProxyEntry struct {
	Tag      string          `json:"tag"`      // generated tag like "proxy-de-1"
	Protocol string          `json:"protocol"` // vless, vmess, trojan, ss
	Outbound json.RawMessage `json:"outbound"` // complete xray outbound JSON
	RawURI   string          `json:"raw_uri"`  // original share URI
	Remarks  string          `json:"remarks"`  // decoded name from #fragment
	Country  string          `json:"country"`  // country code like "DE", "US"
	Marker   string          `json:"marker"`   // known marker: "⚡️", "⭐️", "🎮", "0.5X", "⬇️", "💎"
}

// Filter rules for proxy filtering.
type Filter struct {
	IncludeMarkers   []string `json:"include_markers"`
	ExcludeMarkers   []string `json:"exclude_markers"`
	IncludeCountries []string `json:"include_countries"`
	ExcludeCountries []string `json:"exclude_countries"`
	IncludeRegex     string   `json:"include_regex"`
	ExcludeRegex     string   `json:"exclude_regex"`
	MaxProxies       int      `json:"max_proxies"`
}

// RoutingStrategy defines how traffic is distributed across proxies.
type RoutingStrategy struct {
	Type        string `json:"type"`         // "all", "random", "roundrobin", "leastping", "leastload"
	FallbackTag string `json:"fallback_tag"` // fallback outbound tag
}

// SubscriptionConfig is the persisted subscription configuration.
type SubscriptionConfig struct {
	Subscriptions []Subscription  `json:"subscriptions"`
	Filters       Filter          `json:"filters"`
	Strategy      RoutingStrategy `json:"strategy"`
	GeneratedAt   time.Time       `json:"generated_at"`
	OutboundsFile string          `json:"outbounds_file"` // path to 04_outbounds.json
	RoutingFile   string          `json:"routing_file"`   // path to 05_routing.json
}

// KnownMarkers is the set of recognized markers in proxy remarks.
var KnownMarkers = []string{
	"⚡",   // fast
	"⭐",   // standard
	"🎮",   // gaming
	"0.5X", // mobile
	"⬇",   // download
	"💎",   // auto
}

// DefaultMux is the default mux configuration copied to all generated outbounds.
var DefaultMux = map[string]interface{}{
	"enabled":          true,
	"concurrency":      -1,
	"xudpConcurrency":  16,
	"xudpProxyUDP443": "reject",
}

// StrategyTypes lists all valid routing strategy types.
var StrategyTypes = []string{"all", "random", "roundrobin", "leastping", "leastload"}
