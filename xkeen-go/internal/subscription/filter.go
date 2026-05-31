package subscription

import (
	"regexp"
	"strings"
)

// ApplyFilter filters a list of proxies according to the given Filter rules.
// Rules are applied in order: markers → countries → regex → max.
// Nil filter returns the input unchanged.
func ApplyFilter(proxies []*ProxyEntry, filter *Filter) []*ProxyEntry {
	if filter == nil || len(proxies) == 0 {
		return proxies
	}

	// Compile regexes once before the loop
	var includeRe, excludeRe *regexp.Regexp
	if filter.IncludeRegex != "" {
		var err error
		includeRe, err = regexp.Compile(filter.IncludeRegex)
		if err != nil {
			includeRe = nil // invalid regex → pass through
		}
	}
	if filter.ExcludeRegex != "" {
		var err error
		excludeRe, err = regexp.Compile(filter.ExcludeRegex)
		if err != nil {
			excludeRe = nil // invalid regex → don't exclude
		}
	}

	result := make([]*ProxyEntry, 0, len(proxies))

	for _, p := range proxies {
		if !passesIncludeMarkers(p, filter) {
			continue
		}
		if isExcludedMarker(p, filter) {
			continue
		}
		if !passesIncludeCountries(p, filter) {
			continue
		}
		if isExcludedCountry(p, filter) {
			continue
		}
		if !passesCompiledIncludeRegex(p, includeRe) {
			continue
		}
		if isExcludedByCompiledRegex(p, excludeRe) {
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

// passesIncludeMarkers returns true if the proxy's marker is in IncludeMarkers
// or if IncludeMarkers is empty (no filter).
func passesIncludeMarkers(p *ProxyEntry, f *Filter) bool {
	if len(f.IncludeMarkers) == 0 {
		return true
	}
	for _, m := range f.IncludeMarkers {
		if p.Marker == m {
			return true
		}
	}
	return false
}

// isExcludedMarker returns true if the proxy's marker is in ExcludeMarkers.
func isExcludedMarker(p *ProxyEntry, f *Filter) bool {
	for _, m := range f.ExcludeMarkers {
		if p.Marker == m {
			return true
		}
	}
	return false
}

// passesIncludeCountries returns true if the proxy's country is in IncludeCountries
// or if IncludeCountries is empty (no filter).
func passesIncludeCountries(p *ProxyEntry, f *Filter) bool {
	if len(f.IncludeCountries) == 0 {
		return true
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

// passesCompiledIncludeRegex returns true if includeRe is nil (no filter or invalid regex)
// or the proxy's remarks match.
func passesCompiledIncludeRegex(p *ProxyEntry, includeRe *regexp.Regexp) bool {
	if includeRe == nil {
		return true
	}
	return includeRe.MatchString(p.Remarks)
}

// isExcludedByCompiledRegex returns true if excludeRe is non-nil and the proxy's remarks match.
func isExcludedByCompiledRegex(p *ProxyEntry, excludeRe *regexp.Regexp) bool {
	if excludeRe == nil {
		return false
	}
	return excludeRe.MatchString(p.Remarks)
}
