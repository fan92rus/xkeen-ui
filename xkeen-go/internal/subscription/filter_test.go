package subscription

import "testing"

func makeProxy(marker, country, remarks string) *ProxyEntry {
	return &ProxyEntry{
		Tag:      "proxy-test",
		Protocol: "vless",
		Marker:   marker,
		Country:  country,
		Remarks:  remarks,
	}
}

func TestApplyFilter_NilFilter(t *testing.T) {
	proxies := []*ProxyEntry{
		makeProxy("⚡", "DE", "Germany"),
		makeProxy("⭐", "NL", "Netherlands"),
	}
	result := ApplyFilter(proxies, nil)
	if len(result) != 2 {
		t.Fatalf("nil filter should return all proxies, got %d", len(result))
	}
}

func TestApplyFilter_EmptyFilter(t *testing.T) {
	proxies := []*ProxyEntry{
		makeProxy("⚡", "DE", "Germany"),
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

// --- IncludeMarkers ---

func TestApplyFilter_IncludeMarkers(t *testing.T) {
	proxies := []*ProxyEntry{
		makeProxy("⚡", "DE", "Fast"),
		makeProxy("⭐", "NL", "Standard"),
		makeProxy("🎮", "US", "Gaming"),
	}
	filter := &Filter{
		IncludeMarkers: []string{"⚡", "⭐"},
	}
	result := ApplyFilter(proxies, filter)
	if len(result) != 2 {
		t.Fatalf("expected 2 (fast+standard), got %d", len(result))
	}
	for _, p := range result {
		if p.Marker == "🎮" {
			t.Error("gaming should be excluded")
		}
	}
}

func TestApplyFilter_IncludeMarkers_EmptyPassesAll(t *testing.T) {
	proxies := []*ProxyEntry{
		makeProxy("⚡", "DE", "Fast"),
		makeProxy("⭐", "NL", "Standard"),
	}
	filter := &Filter{
		IncludeMarkers: []string{}, // empty
	}
	result := ApplyFilter(proxies, filter)
	if len(result) != 2 {
		t.Fatalf("empty include markers should pass all, got %d", len(result))
	}
}

// --- ExcludeMarkers ---

func TestApplyFilter_ExcludeMarkers(t *testing.T) {
	proxies := []*ProxyEntry{
		makeProxy("⚡", "DE", "Fast"),
		makeProxy("0.5X", "NL", "Mobile"),
		makeProxy("⭐", "US", "Standard"),
	}
	filter := &Filter{
		ExcludeMarkers: []string{"0.5X", "🎮"},
	}
	result := ApplyFilter(proxies, filter)
	if len(result) != 2 {
		t.Fatalf("expected 2 (mobile excluded), got %d", len(result))
	}
	for _, p := range result {
		if p.Marker == "0.5X" {
			t.Error("mobile should be excluded")
		}
	}
}

func TestApplyFilter_IncludeAndExcludeMarkers(t *testing.T) {
	proxies := []*ProxyEntry{
		makeProxy("⚡", "DE", "Fast"),
		makeProxy("⭐", "NL", "Standard"),
		makeProxy("0.5X", "DE", "Mobile"),
	}
	filter := &Filter{
		IncludeMarkers:   []string{"⚡", "⭐", "0.5X"},
		ExcludeMarkers:   []string{"0.5X"},
	}
	// Include passes all three, then exclude removes mobile
	result := ApplyFilter(proxies, filter)
	if len(result) != 2 {
		t.Fatalf("expected 2 (mobile excluded by exclude), got %d", len(result))
	}
}

// --- IncludeCountries ---

func TestApplyFilter_IncludeCountries(t *testing.T) {
	proxies := []*ProxyEntry{
		makeProxy("⚡", "DE", "Germany"),
		makeProxy("⚡", "NL", "Netherlands"),
		makeProxy("⚡", "US", "USA"),
	}
	filter := &Filter{
		IncludeCountries: []string{"DE", "NL"},
	}
	result := ApplyFilter(proxies, filter)
	if len(result) != 2 {
		t.Fatalf("expected 2 (DE+NL), got %d", len(result))
	}
}

func TestApplyFilter_IncludeCountries_CaseInsensitive(t *testing.T) {
	proxies := []*ProxyEntry{
		makeProxy("⚡", "DE", "Germany"),
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
		makeProxy("⚡", "DE", "Germany"),
		makeProxy("⚡", "RU", "Russia"),
		makeProxy("⚡", "NL", "Netherlands"),
	}
	filter := &Filter{
		ExcludeCountries: []string{"RU"},
	}
	result := ApplyFilter(proxies, filter)
	if len(result) != 2 {
		t.Fatalf("expected 2 (RU excluded), got %d", len(result))
	}
}

// --- IncludeRegex ---

func TestApplyFilter_IncludeRegex(t *testing.T) {
	proxies := []*ProxyEntry{
		makeProxy("⚡", "DE", "Germany Fast"),
		makeProxy("⭐", "NL", "Netherlands Standard"),
		makeProxy("⚡", "US", "USA Premium"),
	}
	filter := &Filter{
		IncludeRegex: "Fast|Premium",
	}
	result := ApplyFilter(proxies, filter)
	if len(result) != 2 {
		t.Fatalf("expected 2 (Fast+Premium), got %d", len(result))
	}
}

func TestApplyFilter_IncludeRegex_InvalidRegex(t *testing.T) {
	proxies := []*ProxyEntry{
		makeProxy("⚡", "DE", "Germany"),
	}
	filter := &Filter{
		IncludeRegex: "[invalid", // invalid regex
	}
	result := ApplyFilter(proxies, filter)
	if len(result) != 1 {
		t.Fatalf("invalid include regex should pass all, got %d", len(result))
	}
}

// --- ExcludeRegex ---

func TestApplyFilter_ExcludeRegex(t *testing.T) {
	proxies := []*ProxyEntry{
		makeProxy("⚡", "DE", "Germany Fast"),
		makeProxy("⭐", "NL", "Netherlands Standard"),
		makeProxy("⚡", "US", "USA Gaming"),
	}
	filter := &Filter{
		ExcludeRegex: "(?i)gaming|mobile",
	}
	result := ApplyFilter(proxies, filter)
	if len(result) != 2 {
		t.Fatalf("expected 2 (Gaming excluded), got %d", len(result))
	}
}

func TestApplyFilter_ExcludeRegex_InvalidRegex(t *testing.T) {
	proxies := []*ProxyEntry{
		makeProxy("⚡", "DE", "Germany"),
	}
	filter := &Filter{
		ExcludeRegex: "[invalid",
	}
	result := ApplyFilter(proxies, filter)
	if len(result) != 1 {
		t.Fatalf("invalid exclude regex should exclude none, got %d", len(result))
	}
}

// --- MaxProxies ---

func TestApplyFilter_MaxProxies(t *testing.T) {
	proxies := []*ProxyEntry{
		makeProxy("⚡", "DE", "Germany"),
		makeProxy("⚡", "NL", "Netherlands"),
		makeProxy("⚡", "US", "USA"),
		makeProxy("⚡", "GB", "UK"),
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
		makeProxy("⚡", "DE", "Germany"),
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

func TestApplyFilter_CombinedMarkersAndCountries(t *testing.T) {
	proxies := []*ProxyEntry{
		makeProxy("⚡", "DE", "Germany Fast"),
		makeProxy("⚡", "NL", "Netherlands Fast"),
		makeProxy("⭐", "DE", "Germany Standard"),
		makeProxy("0.5X", "DE", "Germany Mobile"),
		makeProxy("⚡", "RU", "Russia Fast"),
	}
	filter := &Filter{
		IncludeMarkers:   []string{"⚡", "⭐"},
		ExcludeMarkers:   []string{"0.5X"},
		IncludeCountries: []string{"DE", "NL"},
		ExcludeCountries: []string{"RU"},
		MaxProxies:       2,
	}
	result := ApplyFilter(proxies, filter)
	if len(result) != 2 {
		t.Fatalf("expected 2 (DE+NL fast/standard, no mobile, no RU), got %d", len(result))
	}
	// Should be DE-Fast and NL-Fast (first two matching)
	for _, p := range result {
		if p.Country == "RU" {
			t.Error("RU should be excluded")
		}
		if p.Marker == "0.5X" {
			t.Error("mobile should be excluded")
		}
	}
}

func TestApplyFilter_AllRulesCombined(t *testing.T) {
	proxies := []*ProxyEntry{
		makeProxy("⚡", "DE", "Germany Fast Server 1"),
		makeProxy("⚡", "DE", "Germany Fast Server 2"),
		makeProxy("⚡", "NL", "Netherlands Fast"),
		makeProxy("⭐", "DE", "Germany Standard"),
		makeProxy("0.5X", "DE", "Germany Mobile"),
		makeProxy("🎮", "DE", "Germany Gaming"),
	}
	filter := &Filter{
		IncludeMarkers:    []string{"⚡"},
		ExcludeMarkers:    []string{"0.5X", "🎮"},
		IncludeCountries:  []string{"DE"},
		ExcludeCountries:  []string{"RU"},
		IncludeRegex:      "Server",
		ExcludeRegex:      "",
		MaxProxies:        10,
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

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		(len(s) > 0 && len(sub) > 0 && findSubstr(s, sub)))
}

func findSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
