package subscription

import (
	"regexp"
	"strings"
	"testing"
)

func makeProxy(country, remarks string) *ProxyEntry {
	return &ProxyEntry{
		Tag:      "proxy-test",
		Protocol: "vless",
		Country:  country,
		Remarks:  remarks,
	}
}

func TestApplyFilter_NilFilter(t *testing.T) {
	proxies := []*ProxyEntry{
		makeProxy("DE", "Germany"),
		makeProxy("NL", "Netherlands"),
	}
	result := ApplyFilter(proxies, nil)
	if len(result) != 2 {
		t.Fatalf("nil filter should return all proxies, got %d", len(result))
	}
}

func TestApplyFilter_EmptyFilter(t *testing.T) {
	proxies := []*ProxyEntry{
		makeProxy("DE", "Germany"),
	}
	result := ApplyFilter(proxies, &Filter{})
	if len(result) != 1 {
		t.Fatalf("empty filter should return all proxies, got %d", len(result))
	}
}

func TestApplyFilter_EmptyInput(t *testing.T) {
	result := ApplyFilter([]*ProxyEntry{}, &Filter{})
	if len(result) != 0 {
		t.Fatalf("expected 0, got %d", len(result))
	}
}

// --- IncludeCountries ---

func TestApplyFilter_IncludeCountries(t *testing.T) {
	proxies := []*ProxyEntry{
		makeProxy("DE", "Germany"),
		makeProxy("NL", "Netherlands"),
		makeProxy("US", "USA"),
	}
	filter := &Filter{
		IncludeCountries: []string{"DE", "NL"},
	}
	result := ApplyFilter(proxies, filter)
	if len(result) != 2 {
		t.Fatalf("expected 2 (DE+NL), got %d", len(result))
	}
}

func TestApplyFilter_IncludeCountries_EmptyCountryPasses(t *testing.T) {
	// Proxies without a country should pass through include_countries filter
	proxies := []*ProxyEntry{
		makeProxy("DE", "Germany"),
		makeProxy("", "Unknown"),
		makeProxy("US", "USA"),
	}
	filter := &Filter{
		IncludeCountries: []string{"DE"},
	}
	result := ApplyFilter(proxies, filter)
	if len(result) != 2 {
		t.Fatalf("expected 2 (DE + empty country passes), got %d", len(result))
	}
}

func TestApplyFilter_IncludeCountries_CaseInsensitive(t *testing.T) {
	proxies := []*ProxyEntry{
		makeProxy("DE", "Germany"),
	}
	filter := &Filter{
		IncludeCountries: []string{"de"}, // lowercase
	}
	result := ApplyFilter(proxies, filter)
	if len(result) != 1 {
		t.Fatalf("expected country matching to be case-insensitive, got %d", len(result))
	}
}

// --- ExcludeCountries ---

func TestApplyFilter_ExcludeCountries(t *testing.T) {
	proxies := []*ProxyEntry{
		makeProxy("DE", "Germany"),
		makeProxy("RU", "Russia"),
		makeProxy("NL", "Netherlands"),
	}
	filter := &Filter{
		ExcludeCountries: []string{"RU"},
	}
	result := ApplyFilter(proxies, filter)
	if len(result) != 2 {
		t.Fatalf("expected 2 (RU excluded), got %d", len(result))
	}
}

// --- IncludeRegexes (multiple, AND logic) ---

func TestApplyFilter_IncludeRegexes_Single(t *testing.T) {
	proxies := []*ProxyEntry{
		makeProxy("DE", "Germany Fast"),
		makeProxy("NL", "Netherlands Standard"),
		makeProxy("US", "USA Premium"),
	}
	filter := &Filter{
		IncludeRegexes: []string{"Fast|Premium"},
	}
	result := ApplyFilter(proxies, filter)
	if len(result) != 2 {
		t.Fatalf("expected 2 (Fast+Premium), got %d", len(result))
	}
}

func TestApplyFilter_IncludeRegexes_Multiple(t *testing.T) {
	// OR logic: proxy passes if it matches ANY include regex
	proxies := []*ProxyEntry{
		makeProxy("DE", "Germany Fast Server"),
		makeProxy("NL", "Netherlands Fast"),
		makeProxy("US", "USA Premium Server"),
		makeProxy("DE", "Germany Premium"),
	}
	filter := &Filter{
		IncludeRegexes: []string{"Fast|Premium", "Server"},
	}
	result := ApplyFilter(proxies, filter)
	if len(result) != 4 {
		t.Fatalf("expected 4 (OR: any regex match), got %d", len(result))
	}
	// Verify each result matches at least one of the include regexes
	for _, p := range result {
		matched := false
		for _, re := range filter.IncludeRegexes {
			if ok, _ := regexp.MatchString(re, p.Remarks); ok {
				matched = true
				break
			}
		}
		if !matched {
			t.Errorf("remarks should match at least one include regex, got %q", p.Remarks)
		}
	}
}

func TestApplyFilter_IncludeRegexes_InvalidRegex(t *testing.T) {
	proxies := []*ProxyEntry{
		makeProxy("DE", "Germany"),
	}
	filter := &Filter{
		IncludeRegexes: []string{"[invalid"}, // invalid regex — skipped
	}
	result := ApplyFilter(proxies, filter)
	if len(result) != 1 {
		t.Fatalf("invalid include regex should be skipped, got %d", len(result))
	}
}

func TestApplyFilter_IncludeRegexes_EmptyArray(t *testing.T) {
	proxies := []*ProxyEntry{
		makeProxy("DE", "Germany"),
	}
	filter := &Filter{
		IncludeRegexes: []string{},
	}
	result := ApplyFilter(proxies, filter)
	if len(result) != 1 {
		t.Fatalf("empty include regexes should pass all, got %d", len(result))
	}
}

// --- ExcludeRegexes (multiple, OR logic) ---

func TestApplyFilter_ExcludeRegexes_Single(t *testing.T) {
	proxies := []*ProxyEntry{
		makeProxy("DE", "Germany Fast"),
		makeProxy("NL", "Netherlands Standard"),
		makeProxy("US", "USA Gaming"),
	}
	filter := &Filter{
		ExcludeRegexes: []string{"(?i)gaming|mobile"},
	}
	result := ApplyFilter(proxies, filter)
	if len(result) != 2 {
		t.Fatalf("expected 2 (Gaming excluded), got %d", len(result))
	}
}

func TestApplyFilter_ExcludeRegexes_Multiple(t *testing.T) {
	// OR logic: proxy excluded if it matches ANY exclude regex
	proxies := []*ProxyEntry{
		makeProxy("DE", "Germany Fast"),
		makeProxy("NL", "Netherlands LTE"),
		makeProxy("US", "USA Gaming"),
		makeProxy("DE", "Germany Premium"),
	}
	filter := &Filter{
		ExcludeRegexes: []string{"LTE", "Gaming"},
	}
	result := ApplyFilter(proxies, filter)
	if len(result) != 2 {
		t.Fatalf("expected 2 (LTE and Gaming excluded), got %d", len(result))
	}
	for _, p := range result {
		if contains(p.Remarks, "LTE") || contains(p.Remarks, "Gaming") {
			t.Errorf("should not have LTE or Gaming: %q", p.Remarks)
		}
	}
}

func TestApplyFilter_ExcludeRegexes_InvalidRegex(t *testing.T) {
	proxies := []*ProxyEntry{
		makeProxy("DE", "Germany"),
	}
	filter := &Filter{
		ExcludeRegexes: []string{"[invalid"},
	}
	result := ApplyFilter(proxies, filter)
	if len(result) != 1 {
		t.Fatalf("invalid exclude regex should be skipped, got %d", len(result))
	}
}

// --- MaxProxies ---

func TestApplyFilter_MaxProxies(t *testing.T) {
	proxies := []*ProxyEntry{
		makeProxy("DE", "Germany"),
		makeProxy("NL", "Netherlands"),
		makeProxy("US", "USA"),
		makeProxy("GB", "UK"),
	}
	filter := &Filter{
		MaxProxies: 2,
	}
	result := ApplyFilter(proxies, filter)
	if len(result) != 2 {
		t.Fatalf("expected 2 (max), got %d", len(result))
	}
}

func TestApplyFilter_MaxProxies_Zero(t *testing.T) {
	proxies := []*ProxyEntry{
		makeProxy("DE", "Germany"),
	}
	filter := &Filter{
		MaxProxies: 0, // no limit
	}
	result := ApplyFilter(proxies, filter)
	if len(result) != 1 {
		t.Fatalf("max=0 should mean no limit, got %d", len(result))
	}
}

// --- Combined filters ---

func TestApplyFilter_CombinedCountriesAndMax(t *testing.T) {
	proxies := []*ProxyEntry{
		makeProxy("DE", "Germany Fast"),
		makeProxy("NL", "Netherlands Fast"),
		makeProxy("DE", "Germany Standard"),
		makeProxy("RU", "Russia Fast"),
	}
	filter := &Filter{
		IncludeCountries: []string{"DE", "NL"},
		ExcludeCountries: []string{"RU"},
		MaxProxies:       2,
	}
	result := ApplyFilter(proxies, filter)
	if len(result) != 2 {
		t.Fatalf("expected 2 (DE+NL, no RU), got %d", len(result))
	}
	for _, p := range result {
		if p.Country == "RU" {
			t.Error("RU should be excluded")
		}
	}
}

func TestApplyFilter_AllRulesCombined(t *testing.T) {
	proxies := []*ProxyEntry{
		makeProxy("DE", "Germany Fast Server 1"),
		makeProxy("DE", "Germany Fast Server 2"),
		makeProxy("NL", "Netherlands Fast"),
		makeProxy("DE", "Germany Standard"),
		makeProxy("DE", "Germany Gaming"),
	}
	filter := &Filter{
		IncludeCountries: []string{"DE"},
		ExcludeCountries: []string{"RU"},
		IncludeRegexes:   []string{"Server"},
		ExcludeRegexes:   []string{},
		MaxProxies:       10,
	}
	result := ApplyFilter(proxies, filter)
	if len(result) != 2 {
		t.Fatalf("expected 2 (DE fast servers matching 'Server'), got %d", len(result))
	}
	for _, p := range result {
		if !contains(p.Remarks, "Server") {
			t.Errorf("expected remarks to contain 'Server', got %q", p.Remarks)
		}
	}
}

// --- ValidateRegexes ---

func TestValidateRegexes_Valid(t *testing.T) {
	f := &Filter{
		IncludeRegexes: []string{"Fast|Premium", "(?i)server"},
		ExcludeRegexes: []string{"Gaming"},
	}
	if err := ValidateRegexes(f); err != nil {
		t.Errorf("expected nil error for valid regexes, got: %v", err)
	}
}

func TestValidateRegexes_InvalidInclude(t *testing.T) {
	f := &Filter{
		IncludeRegexes: []string{"valid", "[invalid", "also-valid"},
	}
	err := ValidateRegexes(f)
	if err == nil {
		t.Fatal("expected error for invalid include regex")
	}
	if !containsSubstr(err.Error(), "include_regexes[1]") {
		t.Errorf("error should mention include_regexes[1], got: %v", err)
	}
	if !containsSubstr(err.Error(), "[invalid") {
		t.Errorf("error should include the invalid pattern, got: %v", err)
	}
}

func TestValidateRegexes_InvalidExclude(t *testing.T) {
	f := &Filter{
		ExcludeRegexes: []string{"bad(regex"},
	}
	err := ValidateRegexes(f)
	if err == nil {
		t.Fatal("expected error for invalid exclude regex")
	}
	if !containsSubstr(err.Error(), "exclude_regexes[0]") {
		t.Errorf("error should mention exclude_regexes[0], got: %v", err)
	}
}

func TestValidateRegexes_EmptyOk(t *testing.T) {
	f := &Filter{}
	if err := ValidateRegexes(f); err != nil {
		t.Errorf("expected nil error for empty filter, got: %v", err)
	}

	f2 := &Filter{
		IncludeRegexes: []string{""}, // empty string should be skipped
		ExcludeRegexes: nil,
	}
	if err := ValidateRegexes(f2); err != nil {
		t.Errorf("expected nil error for empty pattern, got: %v", err)
	}
}

func TestValidateRegexes_EmptyPatternInSlice(t *testing.T) {
	f := &Filter{
		IncludeRegexes: []string{"valid", "", "also-valid"},
	}
	if err := ValidateRegexes(f); err != nil {
		t.Errorf("empty string in slice should be skipped, got: %v", err)
	}
}

func containsSubstr(s, sub string) bool {
	return strings.Contains(s, sub)
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || sub == "" ||
		(s != "" && sub != "" && findSubstr(s, sub)))
}

func findSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestApplyFilter_IncludeExcludeRegex_Interaction(t *testing.T) {
	// User scenario: include "Hysteria2", exclude "LTE"
	// Proxies that have "Hysteria2" in remarks AND "LTE" should be excluded
	proxies := []*ProxyEntry{
		{Remarks: "Hysteria2 Fast Server", Tag: "proxy-1"},
		{Remarks: "Hysteria2 LTE Germany", Tag: "proxy-2"},
		{Remarks: "VLESS Standard", Tag: "proxy-3"},
		{Remarks: "Hysteria2 LTE Japan", Tag: "proxy-4"},
	}

	filter := &Filter{
		IncludeRegexes: []string{"Hysteria2"},
		ExcludeRegexes: []string{"LTE"},
	}

	result := ApplyFilter(proxies, filter)

	// Should include only proxies matching "Hysteria2" that do NOT match "LTE"
	if len(result) != 1 {
		t.Errorf("expected 1 result, got %d: %v", len(result), tags(result))
	}
	if len(result) > 0 && result[0].Tag != "proxy-1" {
		t.Errorf("expected proxy-1 (Hysteria2 Fast Server), got %s", result[0].Tag)
	}
}

func TestApplyFilter_IncludeMatchButExcludeAlsoMatches(t *testing.T) {
	// Proxy matches include AND exclude — must be excluded
	proxies := []*ProxyEntry{
		{Remarks: "Premium Gaming Server", Tag: "p1"},
		{Remarks: "Premium Server", Tag: "p2"},
		{Remarks: "Standard Server", Tag: "p3"},
	}

	filter := &Filter{
		IncludeRegexes: []string{"Premium"},
		ExcludeRegexes: []string{"Gaming"},
	}

	result := ApplyFilter(proxies, filter)

	if len(result) != 1 {
		t.Errorf("expected 1 result (p2 only), got %d: %v", len(result), tags(result))
	}
	if len(result) > 0 && result[0].Tag != "p2" {
		t.Errorf("expected p2 (Premium Server), got %s", result[0].Tag)
	}
}

func tags(proxies []*ProxyEntry) []string {
	t := make([]string, len(proxies))
	for i, p := range proxies {
		t[i] = p.Tag
	}
	return t
}
