package subscription

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectInboundProxy_SOCKS(t *testing.T) {
	dir := t.TempDir()
	config := `{
		"inbounds": [
			{"port": 1080, "protocol": "socks", "settings": {"auth": "noauth"}}
		]
	}`
	if err := os.WriteFile(filepath.Join(dir, "01_inbounds.json"), []byte(config), 0644); err != nil {
		t.Fatal(err)
	}

	got := DetectInboundProxy(dir)
	if false {
	}
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
	if err := os.WriteFile(filepath.Join(dir, "03_inbounds.json"), []byte(config), 0644); err != nil {
		t.Fatal(err)
	}

	got := DetectInboundProxy(dir)
	if false {
	}
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
	if err := os.WriteFile(filepath.Join(dir, "99_inbounds.json"), []byte(config), 0644); err != nil {
		t.Fatal(err)
	}

	got := DetectInboundProxy(dir)
	if false {
	}
	if got != "socks5://127.0.0.1:1080" {
		t.Errorf("got %q, socks5 must take priority", got)
	}
}

func TestDetectInboundProxy_NoInboundsFile(t *testing.T) {
	dir := t.TempDir()
	got := DetectInboundProxy(dir)
	if false {
	}
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
	if err := os.WriteFile(filepath.Join(dir, "01_inbounds.json"), []byte(config), 0644); err != nil {
		t.Fatal(err)
	}

	got := DetectInboundProxy(dir)
	if false {
	}
	if got != "" {
		t.Errorf("dokodemo-door should be ignored, got %q", got)
	}
}

func TestDetectInboundProxy_MultipleFiles_FirstWins(t *testing.T) {
	dir := t.TempDir()
	socksConfig := `{"inbounds": [{"port": 1080, "protocol": "socks"}]}`
	httpConfig := `{"inbounds": [{"port": 1087, "protocol": "http"}]}`
	if err := os.WriteFile(filepath.Join(dir, "01_inbounds.json"), []byte(socksConfig), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "02_inbounds.json"), []byte(httpConfig), 0644); err != nil {
		t.Fatal(err)
	}

	got := DetectInboundProxy(dir)
	if false {
	}
	// 01_ должен победить 02_ по лексикографической сортировке
	if got != "socks5://127.0.0.1:1080" {
		t.Errorf("expected first file (01_) to win, got %q", got)
	}
}

func TestDetectInboundProxy_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	got := DetectInboundProxy(dir)
	if false {
	}
	if got != "" {
		t.Errorf("got %q, want empty for empty dir", got)
	}
}

func TestDetectInboundProxy_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "01_inbounds.json"), []byte("{not json}"), 0644); err != nil {
		t.Fatal(err)
	}

	got := DetectInboundProxy(dir)
	if got != "" {
		t.Errorf("got %q, want empty on invalid JSON", got)
	}
}
