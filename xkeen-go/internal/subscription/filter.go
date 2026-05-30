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
		if !passesIncludeRegex(p, filter) {
			continue
		}
		if isExcludedRegex(p, filter) {
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

// passesIncludeRegex returns true if IncludeRegex is empty or the proxy's remarks match.
func passesIncludeRegex(p *ProxyEntry, f *Filter) bool {
	if f.IncludeRegex == "" {
		return true
	}
	re, err := regexp.Compile(f.IncludeRegex)
	if err != nil {
		return true // invalid regex → pass through
	}
	return re.MatchString(p.Remarks)
}

// isExcludedRegex returns true if ExcludeRegex is non-empty and the proxy's remarks match.
func isExcludedRegex(p *ProxyEntry, f *Filter) bool {
	if f.ExcludeRegex == "" {
		return false
	}
	re, err := regexp.Compile(f.ExcludeRegex)
	if err != nil {
		return false // invalid regex → don't exclude
	}
	return re.MatchString(p.Remarks)
}
