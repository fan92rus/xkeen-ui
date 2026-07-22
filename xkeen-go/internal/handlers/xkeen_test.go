package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- XkeenInfoHandler tests ---

func TestXkeenInfo_DetectVersion(t *testing.T) {
	dir := t.TempDir()
	vfile := filepath.Join(dir, "info.sh")
	os.WriteFile(vfile, []byte(`xkeen_current_version="2.0.1"
xkeen_build="Beta"`), 0o644)

	h := NewXkeenInfoHandlerWithFile(vfile)
	info := h.GetInfo()

	if !info.Installed {
		t.Fatal("expected Installed=true")
	}
	if info.Version != "2.0.1" {
		t.Errorf("version = %q, want 2.0.1", info.Version)
	}
	if !info.SpeedBalancerSupported {
		t.Error("expected SpeedBalancerSupported=true for 2.0.1")
	}
}

func TestXkeenInfo_OlderVersion(t *testing.T) {
	dir := t.TempDir()
	vfile := filepath.Join(dir, "info.sh")
	os.WriteFile(vfile, []byte(`xkeen_current_version="1.5.0"`), 0o644)

	h := NewXkeenInfoHandlerWithFile(vfile)
	info := h.GetInfo()

	if !info.Installed {
		t.Fatal("expected Installed=true")
	}
	if info.SpeedBalancerSupported {
		t.Error("expected SpeedBalancerSupported=false for 1.5.0")
	}
}

func TestXkeenInfo_NewerVersion(t *testing.T) {
	dir := t.TempDir()
	vfile := filepath.Join(dir, "info.sh")
	os.WriteFile(vfile, []byte(`xkeen_current_version="2.1.3"`), 0o644)

	h := NewXkeenInfoHandlerWithFile(vfile)
	info := h.GetInfo()

	if !info.SpeedBalancerSupported {
		t.Error("expected SpeedBalancerSupported=true for 2.1.3")
	}
}

func TestXkeenInfo_NoFile(t *testing.T) {
	h := NewXkeenInfoHandlerWithFile("/nonexistent/path/info.sh")
	info := h.GetInfo()

	if info.Installed {
		t.Error("expected Installed=false when file missing")
	}
	if info.SpeedBalancerSupported {
		t.Error("expected SpeedBalancerSupported=false when not installed")
	}
}

func TestXkeenInfo_CacheWorks(t *testing.T) {
	dir := t.TempDir()
	vfile := filepath.Join(dir, "info.sh")
	os.WriteFile(vfile, []byte(`xkeen_current_version="2.0.1"`), 0o644)

	h := NewXkeenInfoHandlerWithFile(vfile)

	// First call reads from disk
	info1 := h.GetInfo()

	// Delete file, second call should return cached result
	os.Remove(vfile)
	info2 := h.GetInfo()

	if info2.Version != info1.Version {
		t.Error("cache should have returned same version after file deletion")
	}
}

func TestCompareVersionsGE(t *testing.T) {
	tests := []struct {
		version string
		min     string
		want    bool
	}{
		{"2.0.1", "2.0.1", true},   // equal
		{"2.1.0", "2.0.1", true},   // newer minor
		{"3.0.0", "2.0.1", true},   // newer major
		{"1.9.9", "2.0.1", false},  // older
		{"2.0.0", "2.0.1", false},  // older patch
		{"2.0.10", "2.0.1", true},  // higher patch
	}
	for _, tt := range tests {
		got := compareVersionsGE(tt.version, tt.min)
		if got != tt.want {
			t.Errorf("compareVersionsGE(%q, %q) = %v, want %v", tt.version, tt.min, got, tt.want)
		}
	}
}

// --- SpeedBalancerHandler tests ---

func TestSpeedBalancer_ReadDefaults(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "xkeen.json")
	// No file = all defaults

	h := NewSpeedBalancerHandler(configPath, "xkeen")
	settings, err := h.readSettings()
	if err != nil {
		t.Fatalf("readSettings error: %v", err)
	}

	if settings.Enabled {
		t.Error("expected Enabled=false by default")
	}
	if settings.Interval != 15 {
		t.Errorf("Interval = %d, want 15", settings.Interval)
	}
	if settings.Balancer != "default-balancer" {
		t.Errorf("Balancer = %q, want default-balancer", settings.Balancer)
	}
	if settings.RoutingFile != defaultRoutingFile {
		t.Errorf("RoutingFile = %q, want %q", settings.RoutingFile, defaultRoutingFile)
	}
	if settings.OutboundsFile != defaultOutboundsFile {
		t.Errorf("OutboundsFile = %q, want %q", settings.OutboundsFile, defaultOutboundsFile)
	}
	if settings.Log {
		t.Error("expected Log=false by default")
	}
}

func TestSpeedBalancer_WriteAndRead(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "xkeen.json")

	h := NewSpeedBalancerHandler(configPath, "xkeen")

	settings := SpeedBalancerSettings{
		Enabled:       true,
		Interval:      10,
		Hysteresis:    30,
		Balancer:      "my-balancer",
		MaxTime:       12,
		TestURL:       "https://example.com/test",
		RoutingFile:   "custom_routing.json",
		OutboundsFile: "custom_outbounds.json",
		Log:           true,
	}

	if err := h.writeSettings(settings); err != nil {
		t.Fatalf("writeSettings error: %v", err)
	}

	// Verify file was created
	data, _ := os.ReadFile(configPath)
	if len(data) == 0 {
		t.Fatal("xkeen.json not written")
	}

	// Read back
	read, err := h.readSettings()
	if err != nil {
		t.Fatalf("readSettings error: %v", err)
	}
	if read.Enabled != true || read.Interval != 10 || read.Balancer != "my-balancer" {
		t.Errorf("readback mismatch: %+v", read)
	}
	if read.RoutingFile != "custom_routing.json" {
		t.Errorf("RoutingFile readback = %q, want custom_routing.json", read.RoutingFile)
	}
	if read.OutboundsFile != "custom_outbounds.json" {
		t.Errorf("OutboundsFile readback = %q, want custom_outbounds.json", read.OutboundsFile)
	}
	if !read.Log {
		t.Error("expected Log=true after readback")
	}
}

func TestSpeedBalancer_PreservesOtherKeys(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "xkeen.json")

	// Start with existing keys that must be preserved
	os.WriteFile(configPath, []byte(`{
  "xkeen": {
    "retries_download": 3,
    "speed_balancer": {
      "enabled": false,
      "interval": 20
    }
  }
}`), 0o644)

	h := NewSpeedBalancerHandler(configPath, "xkeen")

	newSettings := SpeedBalancerSettings{
		Enabled:  true,
		Interval: 10,
	}
	// Fill defaults for fields we don't care about
	newSettings.Hysteresis = 25
	newSettings.Balancer = "default-balancer"
	newSettings.MaxTime = 8
	newSettings.TestURL = "https://speed.cloudflare.com/__down?bytes=50000000"
	newSettings.RoutingFile = defaultRoutingFile
	newSettings.OutboundsFile = defaultOutboundsFile

	if err := h.writeSettings(newSettings); err != nil {
		t.Fatalf("writeSettings error: %v", err)
	}

	// Parse and verify retries_download is preserved
	var raw map[string]interface{}
	data, _ := os.ReadFile(configPath)
	json.Unmarshal(data, &raw)

	xkeen := raw["xkeen"].(map[string]interface{})
	if xkeen["retries_download"] != float64(3) {
		t.Error("retries_download was not preserved")
	}

	sb := xkeen["speed_balancer"].(map[string]interface{})
	if sb["enabled"] != true {
		t.Error("enabled was not updated to true")
	}
	if sb["interval"] != float64(10) {
		t.Error("interval was not updated")
	}
}

func TestSpeedBalancer_GetEndpoint(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "xkeen.json")

	h := NewSpeedBalancerHandler(configPath, "xkeen")
	req := httptest.NewRequest(http.MethodGet, "/api/settings/speed-balancer", http.NoBody)
	rec := httptest.NewRecorder()
	h.GetSpeedBalancer(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["ok"] != true {
		t.Error("expected ok=true")
	}
}

func TestSpeedBalancer_UpdateEndpoint_Enable(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "xkeen.json")

	h := NewSpeedBalancerHandler(configPath, "xkeen")

	// Mock enable/disable with channel sync (enable runs in goroutine)
	enableDone := make(chan struct{})
	h.SetRunEnable(func() error { close(enableDone); return nil })
	h.SetRunDisable(func() error { return nil })

	body := `{"settings":{"enabled":true,"interval":10,"hysteresis":25,"balancer":"default-balancer","max_time":8,"test_url":"https://example.com"}}`
	req := httptest.NewRequest("PUT", "/api/settings/speed-balancer", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.UpdateSpeedBalancer(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Response should return immediately (fire-and-forget)
	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["ok"] != true {
		t.Error("expected ok=true")
	}

	// Wait for goroutine
	select {
	case <-enableDone:
	case <-time.After(5 * time.Second):
		t.Error("expected runEnable to be called")
	}
}

func TestSpeedBalancer_UpdateEndpoint_Disable(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "xkeen.json")

	// Pre-write enabled state
	os.WriteFile(configPath, []byte(`{"xkeen":{"speed_balancer":{"enabled":true}}}`), 0o644)

	h := NewSpeedBalancerHandler(configPath, "xkeen")

	disableDone := make(chan struct{})
	h.SetRunEnable(func() error { return nil })
	h.SetRunDisable(func() error { close(disableDone); return nil })

	body := `{"settings":{"enabled":false,"interval":15,"hysteresis":25,"balancer":"default-balancer","max_time":8,"test_url":"https://speed.cloudflare.com/__down?bytes=50000000"}}`
	req := httptest.NewRequest("PUT", "/api/settings/speed-balancer", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.UpdateSpeedBalancer(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	select {
	case <-disableDone:
	case <-time.After(5 * time.Second):
		t.Error("expected runDisable to be called")
	}
}

func TestSpeedBalancer_UpdateEndpoint_NoToggle(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "xkeen.json")
	os.WriteFile(configPath, []byte(`{"xkeen":{"speed_balancer":{"enabled":false}}}`), 0o644)

	h := NewSpeedBalancerHandler(configPath, "xkeen")

	called := make(chan struct{}, 2)
	h.SetRunEnable(func() error { called <- struct{}{}; return nil })
	h.SetRunDisable(func() error { called <- struct{}{}; return nil })

	// Settings change but enabled stays false → no enable/disable call
	body := `{"settings":{"enabled":false,"interval":20,"hysteresis":25,"balancer":"default-balancer","max_time":8,"test_url":"https://speed.cloudflare.com/__down?bytes=50000000"}}`
	req := httptest.NewRequest("PUT", "/api/settings/speed-balancer", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.UpdateSpeedBalancer(rec, req)

	select {
	case <-called:
		t.Error("expected no enable/disable call when enabled flag unchanged")
	case <-time.After(200 * time.Millisecond):
		// Good — no call expected
	}
}

func TestSpeedBalancer_DefaultFilesNotWritten(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "xkeen.json")

	h := NewSpeedBalancerHandler(configPath, "xkeen")

	settings := defaultSpeedBalancerSettings()
	settings.Enabled = true
	// RoutingFile and OutboundsFile are default, Log is false

	if err := h.writeSettings(settings); err != nil {
		t.Fatalf("writeSettings error: %v", err)
	}

	data, _ := os.ReadFile(configPath)
	var raw map[string]interface{}
	json.Unmarshal(data, &raw)

	xkeen := raw["xkeen"].(map[string]interface{})
	sb := xkeen["speed_balancer"].(map[string]interface{})

	if _, exists := sb["routing_file"]; exists {
		t.Error("routing_file should NOT be written when equal to default")
	}
	if _, exists := sb["outbounds_file"]; exists {
		t.Error("outbounds_file should NOT be written when equal to default")
	}
	if _, exists := sb["log"]; exists {
		t.Error("log should NOT be written when false (default)")
	}
}

func TestSpeedBalancer_CustomFilesWritten(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "xkeen.json")

	h := NewSpeedBalancerHandler(configPath, "xkeen")

	settings := defaultSpeedBalancerSettings()
	settings.RoutingFile = "custom_routing.json"
	settings.OutboundsFile = "custom_outbounds.json"
	settings.Log = true

	if err := h.writeSettings(settings); err != nil {
		t.Fatalf("writeSettings error: %v", err)
	}

	data, _ := os.ReadFile(configPath)
	var raw map[string]interface{}
	json.Unmarshal(data, &raw)

	xkeen := raw["xkeen"].(map[string]interface{})
	sb := xkeen["speed_balancer"].(map[string]interface{})

	if sb["routing_file"] != "custom_routing.json" {
		t.Errorf("routing_file = %v, want custom_routing.json", sb["routing_file"])
	}
	if sb["outbounds_file"] != "custom_outbounds.json" {
		t.Errorf("outbounds_file = %v, want custom_outbounds.json", sb["outbounds_file"])
	}
	if sb["log"] != true {
		t.Errorf("log = %v, want true", sb["log"])
	}
}

func TestSpeedBalancer_StatusEndpoint(t *testing.T) {
	h := NewSpeedBalancerHandler("/dev/null", "nonexistent-binary")

	req := httptest.NewRequest(http.MethodGet, "/api/settings/speed-balancer/status", http.NoBody)
	rec := httptest.NewRecorder()
	h.GetSpeedBalancerStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	// Binary doesn't exist → ok should be false, output may contain error text
	if resp["ok"] == true {
		t.Error("expected ok=false for nonexistent binary")
	}
	if _, ok := resp["output"]; !ok {
		t.Error("expected output field in response")
	}
}

func TestSpeedBalancer_UpdateEndpoint_Validation(t *testing.T) {
	dir := t.TempDir()
	h := NewSpeedBalancerHandler(filepath.Join(dir, "xkeen.json"), "xkeen")

	tests := []struct {
		name   string
		body   string
	}{
		{"negative interval", `{"settings":{"enabled":false,"interval":-1,"hysteresis":0,"balancer":"x","max_time":1,"test_url":"x"}}`},
		{"zero interval", `{"settings":{"enabled":false,"interval":0,"hysteresis":0,"balancer":"x","max_time":1,"test_url":"x"}}`},
		{"negative hysteresis", `{"settings":{"enabled":false,"interval":1,"hysteresis":-1,"balancer":"x","max_time":1,"test_url":"x"}}`},
		{"zero max_time", `{"settings":{"enabled":false,"interval":1,"hysteresis":0,"balancer":"x","max_time":0,"test_url":"x"}}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("PUT", "/api/settings/speed-balancer", strings.NewReader(tt.body))
			rec := httptest.NewRecorder()
			h.UpdateSpeedBalancer(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

