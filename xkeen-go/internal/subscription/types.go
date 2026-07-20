// Package subscription provides subscription management for xray proxy configs.
// It handles fetching, parsing, filtering, and generating xray configuration
// from subscription URLs (Hiddify-compatible format).
package subscription

import (
	"encoding/json"
	"fmt"
	"time"
)

// FlexibleInt is an int that can be unmarshalled from either a JSON number or a JSON string.
// This allows the API to accept both `"interval": 10` and `"interval": "10"`.
type FlexibleInt int

// UnmarshalJSON implements json.Unmarshaler for FlexibleInt.
// It accepts both JSON numbers and JSON strings.
func (fi *FlexibleInt) UnmarshalJSON(data []byte) error {
	// Try number first
	var n int
	if err := json.Unmarshal(data, &n); err == nil {
		*fi = FlexibleInt(n)
		return nil
	}
	// Try string
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		var parsed int
		if _, err := fmt.Sscanf(s, "%d", &parsed); err != nil {
			return fmt.Errorf("cannot parse %q as int", s)
		}
		*fi = FlexibleInt(parsed)
		return nil
	}
	return fmt.Errorf("expected number or string, got %s", string(data))
}

// Subscription represents a single subscription source (e.g., Hiddify panel URL).
type Subscription struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	URL        string      `json:"url"`
	Enabled    bool        `json:"enabled"`
	Interval   FlexibleInt `json:"interval"` // minutes, 0 = manual only
	LastFetch  time.Time   `json:"last_fetch"`
	LastError  string      `json:"last_error"`
	ProxyCount int         `json:"proxy_count"`
	IsBuiltin  bool        `json:"is_builtin"`  // system subscription, cannot be deleted
	LastSource string      `json:"last_source"` // how last successful fetch reached us: "xray-proxy" | "direct" (empty for legacy)
}

// ProxyEntry represents a single parsed proxy from a subscription.
type ProxyEntry struct {
	Tag            string          `json:"tag"`             // generated tag like "proxy-de-1"
	Protocol       string          `json:"protocol"`        // vless, vmess, trojan, ss
	Fingerprint    string          `json:"fingerprint,omitempty"`  // TLS fingerprint (chrome, random, ios, …)
	TLSSecurity    string          `json:"tls_security,omitempty"` // connection security: tls, reality, none
	Network        string          `json:"network,omitempty"`      // transport: tcp, ws, grpc, hysteria
	Outbound       json.RawMessage `json:"outbound"`        // complete xray outbound JSON
	RawURI         string          `json:"raw_uri"`         // original share URI
	Remarks        string          `json:"remarks"`         // decoded name from #fragment
	Country        string          `json:"country"`         // country code like "DE", "US"
	SubscriptionID string          `json:"subscription_id"` // owning subscription ID
}

// Filter rules for proxy filtering.
type Filter struct {
	IncludeCountries    []string `json:"include_countries"`
	ExcludeCountries    []string `json:"exclude_countries"`
	IncludeProtocols    []string `json:"include_protocols"`
	ExcludeProtocols    []string `json:"exclude_protocols"`
	IncludeFingerprints []string `json:"include_fingerprints"`
	ExcludeFingerprints []string `json:"exclude_fingerprints"`
	IncludeNetwork      []string `json:"include_network"`
	ExcludeNetwork      []string `json:"exclude_network"`
	IncludeTLS          []string `json:"include_tls"`
	ExcludeTLS          []string `json:"exclude_tls"`
	IncludeRegexes      []string `json:"include_regexes"`
	ExcludeRegexes      []string `json:"exclude_regexes"`
	MaxProxies          int      `json:"max_proxies"`

	// Legacy fields — migrated to slices on load.
	LegacyIncludeRegex string `json:"include_regex,omitempty"`
	LegacyExcludeRegex string `json:"exclude_regex,omitempty"`
}

// RoutingStrategy defines how traffic is distributed across proxies.
type RoutingStrategy struct {
	Type               string            `json:"type"`                 // "all", "random", "roundrobin", "leastping", "leastload"
	ReplaceBalancerTag bool              `json:"replace_balancer_tag"` // if true, replace existing balancerTag rules with new ones
	Fallback           string            `json:"fallback"`             // "": off, "direct": freedom, "block": blackhole
	Settings           *StrategySettings `json:"settings,omitempty"`   // advanced settings for leastping/leastload
}

// StrategySettings holds tunable parameters for leastping/leastload strategies.
// Stored in snake_case; the generator converts to Xray's camelCase JSON keys.
// All fields are omitempty — unset fields are omitted from the generated config,
// letting Xray use its built-in defaults.
type StrategySettings struct {
	Expected  int      `json:"expected,omitempty"`  // number of best nodes to select (1 = all traffic through best node)
	MaxRTT    string   `json:"max_rtt,omitempty"`    // exclude nodes with RTT above this threshold (e.g. "2s", "500ms")
	Tolerance float64  `json:"tolerance,omitempty"`  // exclude nodes with failure rate above this (0.0–1.0, e.g. 0.1 = 10%)
	Baselines []string `json:"baselines,omitempty"` // RTT baselines to exclude jittery nodes (e.g. ["500ms"])
}

// Profile is a named group of proxies with its own balancer.
// Each active profile generates a separate balancer entry in routing config.
type Profile struct {
	ID        string          `json:"id"`         // unique identifier, "default" for the built-in profile
	Name      string          `json:"name"`       // display name
	Enabled   bool            `json:"enabled"`    // inactive profiles are skipped during generation
	IsDefault bool            `json:"is_default"` // exactly one profile is default; cannot be deleted
	Filter    Filter          `json:"filter"`     // determines which proxies belong to this profile
	Strategy  RoutingStrategy `json:"strategy"`   // balancer strategy for this profile
}

// MaxProfiles limits the number of profiles to prevent config bloat on MIPSLE routers.
const MaxProfiles = 10

// ReservedAWGSubscriptionID is the ID for the built-in AWG subscription.
const ReservedAWGSubscriptionID = "__awg__"

// AWGRole classifies an AWG config by its function.
type AWGRole string

const (
	// AWGRoleAuto means detect from config content.
	AWGRoleAuto AWGRole = "auto"
	// AWGRoleClient is an outbound tunnel (WARP, VPN provider).
	// Uses fwmark routing for Xray integration.
	AWGRoleClient AWGRole = "client"
	// AWGRoleServer is an inbound VPN server (home access).
	// Uses iptables INPUT/FORWARD/NAT + route for client access.
	AWGRoleServer AWGRole = "server"
)

// AWGConfig tracks an AWG interface configuration with mark persistence
// and role classification.
type AWGConfig struct {
	Name string  `json:"name"` // config name (filename without .conf)
	Mark int     `json:"mark"` // fwmark for routing (100+)
	Role AWGRole `json:"role"` // auto-detected or overridden role
}

// Config is the persisted subscription configuration.
type Config struct {
	Subscriptions []Subscription `json:"subscriptions"`
	Profiles      []Profile      `json:"profiles"`
	GeneratedAt   time.Time      `json:"generated_at"`
	OutboundsFile string         `json:"outbounds_file"` // path to 04_outbounds.json
	RoutingFile   string         `json:"routing_file"`   // path to 05_routing.json

	// AWGConfigs tracks AWG interface configurations with persistent marks.
	AWGConfigs []AWGConfig `json:"awg_configs"`

	// AutoApply configures automatic proxy refresh + apply on a cron schedule.
	AutoApplyEnabled bool   `json:"auto_apply_enabled"` // enable/disable
	AutoApplyCron    string `json:"auto_apply_cron"`    // cron expression, e.g. "0 */6 * * *"

	// Legacy fields — migrated to default profile on first load.
	Filters  *Filter          `json:"filters,omitempty"`
	Strategy *RoutingStrategy `json:"strategy,omitempty"`
}

// DefaultMux is the default mux configuration copied to all generated outbounds.
var DefaultMux = map[string]interface{}{
	"enabled":         true,
	"concurrency":     -1,
	"xudpConcurrency": 16,
	"xudpProxyUDP443": "reject",
}

// StrategyTypes lists all valid routing strategy types.
var StrategyTypes = []string{"all", "random", "roundrobin", "leastping", "leastload"}
