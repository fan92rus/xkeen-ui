package subscription

import (
	"regexp"
	"strings"
)

// ApplyFilter filters a list of proxies according to the given Filter rules.
// Rules are applied in order: countries → regex → max.
// Nil filter returns the input unchanged.
func ApplyFilter(proxies []*ProxyEntry, filter *Filter) []*ProxyEntry {
	if filter == nil || len(proxies) == 0 {
		return proxies
	}

	// Compile regexes once before the loop
	includeRes := compileRegexes(filter.IncludeRegexes)
	excludeRes := compileRegexes(filter.ExcludeRegexes)

	result := make([]*ProxyEntry, 0, len(proxies))

	for _, p := range proxies {

		if !passesIncludeCountries(p, filter) {
			continue
		}
		if isExcludedCountry(p, filter) {
			continue
		}
		if !passesAnyIncludeRegex(p, includeRes) {
			continue
		}
		if isExcludedByAnyRegex(p, excludeRes) {
			continue
		}
		result = append(result, p)
	}

	// Truncate to max
	if filter.MaxProxies > 0 && len(result) > filter.MaxProxies {
		result = result[:filter.MaxProxies]
	}

	return result
}

// compileRegexes compiles a list of regex pattern strings.
// Invalid patterns are silently skipped.
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



// passesIncludeCountries returns true if the proxy's country is in IncludeCountries,
// if IncludeCountries is empty (no filter), or if the proxy has no country (pass through).
func passesIncludeCountries(p *ProxyEntry, f *Filter) bool {
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

// isExcludedCountry returns true if the proxy's country is in ExcludeCountries.
func isExcludedCountry(p *ProxyEntry, f *Filter) bool {
	for _, c := range f.ExcludeCountries {
		if strings.EqualFold(p.Country, c) {
			return true
		}
	}
	return false
}

// passesAnyIncludeRegex returns true if the proxy's remarks match ANY include regex (OR).
// Empty list means no filter (pass through).
func passesAnyIncludeRegex(p *ProxyEntry, includeRes []*regexp.Regexp) bool {
	if len(includeRes) == 0 {
		return true
	}
	for _, re := range includeRes {
		if re.MatchString(p.Remarks) {
			return true
		}
	}
	return false
}

// isExcludedByAnyRegex returns true if the proxy's remarks match ANY exclude regex (OR).
func isExcludedByAnyRegex(p *ProxyEntry, excludeRes []*regexp.Regexp) bool {
	for _, re := range excludeRes {
		if re.MatchString(p.Remarks) {
			return true
		}
	}
	return false
}
