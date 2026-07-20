package subscription

import (
	"fmt"
	"regexp"
	"strings"
)

// filterFunc is a single filter step in the filter chain.
// Returns true if the proxy passes this step.
type filterFunc func(p *ProxyEntry, f *Filter, ctx *chainCtx) bool

// filterStep names a filter step for testing/debugging.
type filterStep struct {
	name string
	fn   filterFunc
}

// filterChain defines the ordered sequence of filter steps.
// Each step is evaluated in order; a proxy must pass ALL steps to be included.
var filterChain = []filterStep{
	{"include_countries", matchIncludeCountries},
	{"exclude_countries", matchExcludeCountries},
	{"include_protocols", matchIncludeProtocols},
	{"exclude_protocols", matchExcludeProtocols},
	{"include_fingerprints", matchIncludeFingerprints},
	{"exclude_fingerprints", matchExcludeFingerprints},
	{"include_network", matchIncludeNetwork},
	{"exclude_network", matchExcludeNetwork},
	{"include_tls", matchIncludeTLS},
	{"exclude_tls", matchExcludeTLS},
	{"include_regexes", matchIncludeRegexes},
	{"exclude_regexes", matchExcludeRegexes},
}

// ApplyFilter filters a list of proxies according to the given Filter rules.
// Rules are applied in order via the filter chain, then truncated to MaxProxies.
// Nil filter returns the input unchanged.
func ApplyFilter(proxies []*ProxyEntry, filter *Filter) []*ProxyEntry {
	if filter == nil || len(proxies) == 0 {
		return proxies
	}

	// Compile regexes once (shared across all proxies)
	includeRes := compileRegexes(filter.IncludeRegexes)
	excludeRes := compileRegexes(filter.ExcludeRegexes)

	result := make([]*ProxyEntry, 0, len(proxies))

	for _, p := range proxies {
		if passesFilterChain(p, filter, includeRes, excludeRes) {
			result = append(result, p)
		}
	}

	// Truncate to max
	if filter.MaxProxies > 0 && len(result) > filter.MaxProxies {
		result = result[:filter.MaxProxies]
	}

	return result
}

// chainCtx carries pre-compiled regexes through the filter chain evaluation.
type chainCtx struct {
	includeRes []*regexp.Regexp
	excludeRes []*regexp.Regexp
}

// passesFilterChain evaluates all steps in the filter chain for one proxy.
func passesFilterChain(p *ProxyEntry, f *Filter, includeRes, excludeRes []*regexp.Regexp) bool {
	ctx := &chainCtx{includeRes: includeRes, excludeRes: excludeRes}
	for _, step := range filterChain {
		if !step.fn(p, f, ctx) {
			return false
		}
	}
	return true
}

// --- Country filters ---

func matchIncludeCountries(p *ProxyEntry, f *Filter, _ *chainCtx) bool {
	if len(f.IncludeCountries) == 0 {
		return true
	}
	if p.Country == "" {
		return true // proxies without country pass through include filter
	}
	for _, c := range f.IncludeCountries {
		if strings.EqualFold(p.Country, c) {
			return true
		}
	}
	return false
}

func matchExcludeCountries(p *ProxyEntry, f *Filter, _ *chainCtx) bool {
	for _, c := range f.ExcludeCountries {
		if strings.EqualFold(p.Country, c) {
			return false
		}
	}
	return true
}

// --- Protocol filters ---

func matchIncludeProtocols(p *ProxyEntry, f *Filter, _ *chainCtx) bool {
	if len(f.IncludeProtocols) == 0 {
		return true
	}
	for _, pt := range f.IncludeProtocols {
		if strings.EqualFold(p.Protocol, pt) {
			return true
		}
	}
	return false
}

func matchExcludeProtocols(p *ProxyEntry, f *Filter, _ *chainCtx) bool {
	for _, pt := range f.ExcludeProtocols {
		if strings.EqualFold(p.Protocol, pt) {
			return false
		}
	}
	return true
}

// --- Fingerprint filters ---

func matchIncludeFingerprints(p *ProxyEntry, f *Filter, _ *chainCtx) bool {
	if len(f.IncludeFingerprints) == 0 {
		return true
	}
	if p.Fingerprint == "" {
		return false // no fingerprint info → block if include filter is active
	}
	for _, fp := range f.IncludeFingerprints {
		if strings.EqualFold(p.Fingerprint, fp) {
			return true
		}
	}
	return false
}

func matchExcludeFingerprints(p *ProxyEntry, f *Filter, _ *chainCtx) bool {
	for _, fp := range f.ExcludeFingerprints {
		if strings.EqualFold(p.Fingerprint, fp) {
			return false
		}
	}
	return true
}

// --- Network filters ---

func matchIncludeNetwork(p *ProxyEntry, f *Filter, _ *chainCtx) bool {
	if len(f.IncludeNetwork) == 0 {
		return true
	}
	if p.Network == "" {
		return true
	}
	for _, n := range f.IncludeNetwork {
		if strings.EqualFold(p.Network, n) {
			return true
		}
	}
	return false
}

func matchExcludeNetwork(p *ProxyEntry, f *Filter, _ *chainCtx) bool {
	for _, n := range f.ExcludeNetwork {
		if strings.EqualFold(p.Network, n) {
			return false
		}
	}
	return true
}

// --- TLS filters ---

func matchIncludeTLS(p *ProxyEntry, f *Filter, _ *chainCtx) bool {
	if len(f.IncludeTLS) == 0 {
		return true
	}
	if p.TLSSecurity == "" {
		return true
	}
	for _, t := range f.IncludeTLS {
		if strings.EqualFold(p.TLSSecurity, t) {
			return true
		}
	}
	return false
}

func matchExcludeTLS(p *ProxyEntry, f *Filter, _ *chainCtx) bool {
	for _, t := range f.ExcludeTLS {
		if strings.EqualFold(p.TLSSecurity, t) {
			return false
		}
	}
	return true
}

// --- Regex (by Remarks) filters ---

func matchIncludeRegexes(p *ProxyEntry, _ *Filter, ctx *chainCtx) bool {
	if len(ctx.includeRes) == 0 {
		return true
	}
	for _, re := range ctx.includeRes {
		if re.MatchString(p.Remarks) {
			return true
		}
	}
	return false
}

func matchExcludeRegexes(p *ProxyEntry, _ *Filter, ctx *chainCtx) bool {
	if len(ctx.excludeRes) == 0 {
		return true
	}
	for _, re := range ctx.excludeRes {
		if re.MatchString(p.Remarks) {
			return false
		}
	}
	return true
}

// --- Validation ---

// ValidateFilters validates all regex patterns in a filter, returning an error
// describing the first invalid pattern. Returns nil if all are valid.
func ValidateFilters(f *Filter) error {
	check := func(field, pattern string) error {
		if pattern == "" {
			return nil
		}
		if _, err := regexp.Compile(pattern); err != nil {
			return fmt.Errorf("invalid regex in %s: %q: %w", field, pattern, err)
		}
		return nil
	}
	for i, p := range f.IncludeRegexes {
		if err := check(fmt.Sprintf("include_regexes[%d]", i), p); err != nil {
			return err
		}
	}
	for i, p := range f.ExcludeRegexes {
		if err := check(fmt.Sprintf("exclude_regexes[%d]", i), p); err != nil {
			return err
		}
	}
	return nil
}

// ValidateRegexes is a compatibility alias for ValidateFilters.
var ValidateRegexes = ValidateFilters

// --- Helpers ---

func compileRegexes(patterns []string) []*regexp.Regexp {
	var res []*regexp.Regexp
	for _, p := range patterns {
		if p == "" {
			continue
		}
		re, err := regexp.Compile(p)
		if err == nil {
			res = append(res, re)
		}
	}
	return res
}
