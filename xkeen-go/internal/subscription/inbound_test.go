package subscription

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestDetectInboundProxy_SOCKS(t *testing.T) {
	dir := t.TempDir()
	config := `{
		"inbounds": [
			{"port": 1080, "protocol": "socks", "settings": {"auth": "noauth"}}
		]
	}`
	if err := os.WriteFile(filepath.Join(dir, "01_inbounds.json"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}

	got := DetectInboundProxy(dir)
	if got != "socks5://127.0.0.1:1080" {
		t.Errorf("got %q, want socks5://127.0.0.1:1080", got)
	}
}

func TestDetectInboundProxy_HTTP(t *testing.T) {
	dir := t.TempDir()
	config := `{
		"inbounds": [
			{"port": 1087, "protocol": "http", "settings": {}}
		]
	}`
	if err := os.WriteFile(filepath.Join(dir, "03_inbounds.json"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}

	got := DetectInboundProxy(dir)
	if got != "http://127.0.0.1:1087" {
		t.Errorf("got %q, want http://127.0.0.1:1087", got)
	}
}

func TestDetectInboundProxy_PrioritySocksOverHTTP(t *testing.T) {
	dir := t.TempDir()
	config := `{
		"inbounds": [
			{"port": 1087, "protocol": "http"},
			{"port": 1080, "protocol": "socks"}
		]
	}`
	if err := os.WriteFile(filepath.Join(dir, "99_inbounds.json"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}

	got := DetectInboundProxy(dir)
	if got != "socks5://127.0.0.1:1080" {
		t.Errorf("got %q, socks5 must take priority", got)
	}
}

func TestDetectInboundProxy_NoInboundsFile(t *testing.T) {
	dir := t.TempDir()
	got := DetectInboundProxy(dir)
	if got != "" {
		t.Errorf("got %q, want empty string when no inbounds file", got)
	}
}

func TestDetectInboundProxy_OnlyDokodemoDoor(t *testing.T) {
	dir := t.TempDir()
	config := `{
		"inbounds": [
			{"port": 12345, "protocol": "dokodemo-door"}
		]
	}`
	if err := os.WriteFile(filepath.Join(dir, "01_inbounds.json"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}

	got := DetectInboundProxy(dir)
	if got != "" {
		t.Errorf("dokodemo-door should be ignored, got %q", got)
	}
}

func TestDetectInboundProxy_MultipleFiles_FirstWins(t *testing.T) {
	dir := t.TempDir()
	socksConfig := `{"inbounds": [{"port": 1080, "protocol": "socks"}]}`
	httpConfig := `{"inbounds": [{"port": 1087, "protocol": "http"}]}`
	if err := os.WriteFile(filepath.Join(dir, "01_inbounds.json"), []byte(socksConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "02_inbounds.json"), []byte(httpConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	got := DetectInboundProxy(dir)
	// 01_ должен победить 02_ по лексикографической сортировке
	if got != "socks5://127.0.0.1:1080" {
		t.Errorf("expected first file (01_) to win, got %q", got)
	}
}

func TestDetectInboundProxy_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	got := DetectInboundProxy(dir)
	if got != "" {
		t.Errorf("got %q, want empty for empty dir", got)
	}
}

func TestDetectInboundProxy_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "01_inbounds.json"), []byte("{not json}"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := DetectInboundProxy(dir)
	if got != "" {
		t.Errorf("got %q, want empty on invalid JSON", got)
	}
}

func TestDetectInboundProxy_ManagedSocksFile(t *testing.T) {
	dir := t.TempDir()
	// Only the xkeen-ui-managed SOCKS5 file exists.
	if err := os.WriteFile(filepath.Join(dir, ManagedSocksInboundFile), []byte(ManagedSocksInboundConfig()), 0o644); err != nil {
		t.Fatal(err)
	}

	got := DetectInboundProxy(dir)
	want := "socks5://127.0.0.1:" + strconv.Itoa(ManagedSocksPort)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDetectInboundProxy_UserInboundWinsOverManaged(t *testing.T) {
	dir := t.TempDir()
	// User's own SOCKS5 inbound (03_inbounds.json) should take priority
	// over the xkeen-ui-managed file (99_xkeen_ui_socks.json) because it
	// sorts first lexicographically.
	userConfig := `{"inbounds": [{"port": 7777, "protocol": "socks"}]}`
	if err := os.WriteFile(filepath.Join(dir, "03_inbounds.json"), []byte(userConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ManagedSocksInboundFile), []byte(ManagedSocksInboundConfig()), 0o644); err != nil {
		t.Fatal(err)
	}

	got := DetectInboundProxy(dir)
	if got != "socks5://127.0.0.1:7777" {
		t.Errorf("user inbound should win, got %q", got)
	}
}

func TestManagedSocksInboundConfig_ValidJSON(t *testing.T) {
	config := ManagedSocksInboundConfig()

	var wrapper struct {
		Inbounds []xrayInbound `json:"inbounds"`
	}
	if err := json.Unmarshal([]byte(config), &wrapper); err != nil {
		t.Fatalf("managed config is not valid JSON: %v", err)
	}
	if len(wrapper.Inbounds) != 1 {
		t.Fatalf("expected 1 inbound, got %d", len(wrapper.Inbounds))
	}
	ib := wrapper.Inbounds[0]
	if ib.Protocol != "socks" {
		t.Errorf("expected protocol socks, got %s", ib.Protocol)
	}
	if ib.Port != ManagedSocksPort {
		t.Errorf("expected port %d, got %d", ManagedSocksPort, ib.Port)
	}
}

func TestManagedSocksInboundConfig_Detectable(t *testing.T) {
	// The config produced by ManagedSocksInboundConfig should be detectable
	// by DetectInboundProxy when written to a directory.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ManagedSocksInboundFile), []byte(ManagedSocksInboundConfig()), 0o644); err != nil {
		t.Fatal(err)
	}

	// Also add a non-inbound file to ensure it's ignored.
	if err := os.WriteFile(filepath.Join(dir, "05_routing.json"), []byte(`{"routing": {}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	got := DetectInboundProxy(dir)
	if got == "" {
		t.Error("DetectInboundProxy failed to find managed SOCKS5 inbound")
	}
}

func TestIsInboundFile(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"", false},
		{"01_inbounds.json", true},
		{"03_inbounds.json", true},
		{"99_xkeen_ui_socks.json", true}, // managed file
		{"05_routing.json", false},
		{"04_outbounds.json", false},
		{"01_log.json", false},
		{"random.json", false},
		{"inbounds.json", false}, // missing prefix underscore
		{"99_xkeen_ui_socks.json.bak", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isInboundFile(tc.name); got != tc.want {
				t.Errorf("isInboundFile(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}
