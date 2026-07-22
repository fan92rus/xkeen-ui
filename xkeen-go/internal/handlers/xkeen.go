package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
)

// minSpeedBalancerVersion is the first XKeen release that includes the
// speed_balancer tool (scripts/_xkeen/04_tools/08_tools_balancer).
const minSpeedBalancerVersion = "2.0.1"

// Default file names for Xray config — used by XKeen and overridable via
// .xkeen.speed_balancer.routing_file / outbounds_file in xkeen.json.
const defaultRoutingFile = "05_routing.json"
const defaultOutboundsFile = "04_outbounds.json"

// xkeenDefaultVersionFile is the shell variable file shipped with every
// XKeen install: xkeen_current_version="2.0.1".
const xkeenDefaultVersionFile = "/opt/sbin/.xkeen/01_info/01_info_variable.sh"

const versionCacheTTL = 1 * time.Hour

// XkeenInfoHandler detects the installed XKeen version and which optional
// features it supports.
type XkeenInfoHandler struct {
	versionFile string
	cached      *XkeenInfo
	cacheTime   time.Time
	mu          sync.Mutex
}

// XkeenInfo is the parsed result of an XKeen version probe.
type XkeenInfo struct {
	Installed              bool   `json:"installed"`
	Version                string `json:"version"`
	SpeedBalancerSupported bool   `json:"speed_balancer_supported"`
}

// NewXkeenInfoHandler creates a handler using the default version file path.
func NewXkeenInfoHandler() *XkeenInfoHandler {
	return &XkeenInfoHandler{versionFile: xkeenDefaultVersionFile}
}

// NewXkeenInfoHandlerWithFile creates a handler with a custom version file
// path (for tests).
func NewXkeenInfoHandlerWithFile(path string) *XkeenInfoHandler {
	return &XkeenInfoHandler{versionFile: path}
}

// GetInfo returns cached XkeenInfo, refreshing from disk if stale.
func (h *XkeenInfoHandler) GetInfo() XkeenInfo {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.cached != nil && time.Since(h.cacheTime) < versionCacheTTL {
		return *h.cached
	}

	info := h.detect()
	h.cached = &info
	h.cacheTime = time.Now()
	return info
}

func (h *XkeenInfoHandler) detect() XkeenInfo {
	info := XkeenInfo{}

	data, err := os.ReadFile(h.versionFile)
	if err != nil {
		return info
	}

	re := regexp.MustCompile(`xkeen_current_version\s*=\s*"([^"]+)"`)
	m := re.FindSubmatch(data)
	if m == nil {
		return info
	}

	info.Installed = true
	info.Version = string(m[1])
	info.SpeedBalancerSupported = compareVersionsGE(info.Version, minSpeedBalancerVersion)
	return info
}

// compareVersionsGE returns true if version >= minVersion.
func compareVersionsGE(version, minVersion string) bool {
	v1 := parseSemver(version)
	v2 := parseSemver(minVersion)
	for i := 0; i < 3; i++ {
		if v1[i] > v2[i] {
			return true
		}
		if v1[i] < v2[i] {
			return false
		}
	}
	return true // equal
}

func parseSemver(v string) [3]int {
	var parts [3]int
	var idx int
	current := ""
	for _, ch := range v {
		if ch == '.' {
			if idx < 3 {
				_, _ = fmt.Sscanf(current, "%d", &parts[idx])
				idx++
			}
			current = ""
		} else {
			current += string(ch)
		}
	}
	if idx < 3 {
		_, _ = fmt.Sscanf(current, "%d", &parts[idx])
	}
	return parts
}

// GetXkeenVersion handles GET /api/xkeen/version.
func (h *XkeenInfoHandler) GetXkeenVersion(w http.ResponseWriter, _ *http.Request) {
	info := h.GetInfo()
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"ok":   true,
		"info": info,
	})
}

// --- Speed balancer settings ---

// SpeedBalancerSettings mirrors .xkeen.speed_balancer.* in xkeen.json.
type SpeedBalancerSettings struct {
	Enabled       bool   `json:"enabled"`
	Interval      int    `json:"interval,omitempty"`
	Hysteresis    int    `json:"hysteresis,omitempty"`
	Balancer      string `json:"balancer,omitempty"`
	MaxTime       int    `json:"max_time,omitempty"`
	TestURL       string `json:"test_url,omitempty"`
	RoutingFile   string `json:"routing_file"`
	OutboundsFile string `json:"outbounds_file"`
	Log           bool   `json:"log"`
}

func defaultSpeedBalancerSettings() SpeedBalancerSettings {
	return SpeedBalancerSettings{
		Enabled:       false,
		Interval:      15,
		Hysteresis:    25,
		Balancer:      "default-balancer",
		MaxTime:       8,
		TestURL:       "https://speed.cloudflare.com/__down?bytes=50000000",
		RoutingFile:   defaultRoutingFile,
		OutboundsFile: defaultOutboundsFile,
		Log:           false,
	}
}

// SpeedBalancerHandler reads/writes .xkeen.speed_balancer in xkeen.json
// and runs `xkeen -sb on/off` to manage the cron job.
type SpeedBalancerHandler struct {
	configPath  string
	xkeenBinary string
	runEnable   func() error
	runDisable  func() error
	mu          sync.Mutex // protects readSettings + writeSettings (file R/M/W)
	cmdMu       sync.Mutex // serializes enable/disable/status commands
}

// NewSpeedBalancerHandler creates a handler for the given xkeen.json path.
func NewSpeedBalancerHandler(xkeenConfigPath, xkeenBinary string) *SpeedBalancerHandler {
	h := &SpeedBalancerHandler{
		configPath:  xkeenConfigPath,
		xkeenBinary: xkeenBinary,
	}
	h.runEnable = h.doEnable
	h.runDisable = h.doDisable
	return h
}

// SetRunEnable overrides the enable function (for tests).
func (h *SpeedBalancerHandler) SetRunEnable(fn func() error) { h.runEnable = fn }

// SetRunDisable overrides the disable function (for tests).
func (h *SpeedBalancerHandler) SetRunDisable(fn func() error) { h.runDisable = fn }

func (h *SpeedBalancerHandler) readSettings() (SpeedBalancerSettings, error) {
	defaults := defaultSpeedBalancerSettings()

	data, err := os.ReadFile(h.configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return defaults, nil
		}
		return defaults, err
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return defaults, fmt.Errorf("invalid xkeen.json: %w", err)
	}

	xkeen, _ := raw["xkeen"].(map[string]interface{})
	sb, _ := xkeen["speed_balancer"].(map[string]interface{})

	settings := defaults
	if v, ok := sb["enabled"].(bool); ok {
		settings.Enabled = v
	}
	settings.Interval = jsonIntOr(sb["interval"], defaults.Interval)
	settings.Hysteresis = jsonIntOr(sb["hysteresis"], defaults.Hysteresis)
	if v, ok := sb["balancer"].(string); ok && v != "" {
		settings.Balancer = v
	}
	settings.MaxTime = jsonIntOr(sb["max_time"], defaults.MaxTime)
	if v, ok := sb["test_url"].(string); ok && v != "" {
		settings.TestURL = v
	}
	if v, ok := sb["routing_file"].(string); ok && v != "" {
		settings.RoutingFile = v
	}
	if v, ok := sb["outbounds_file"].(string); ok && v != "" {
		settings.OutboundsFile = v
	}
	if v, ok := sb["log"].(bool); ok {
		settings.Log = v
	}
	return settings, nil
}

func (h *SpeedBalancerHandler) writeSettings(settings SpeedBalancerSettings) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.writeSettingsLocked(settings)
}

func (h *SpeedBalancerHandler) writeSettingsLocked(settings SpeedBalancerSettings) error {

	raw := map[string]interface{}{}
	if data, err := os.ReadFile(h.configPath); err == nil {
		_ = json.Unmarshal(data, &raw)
	}

	xkeen, _ := raw["xkeen"].(map[string]interface{})
	if xkeen == nil {
		xkeen = map[string]interface{}{}
	}

	sb := map[string]interface{}{
		"enabled": settings.Enabled,
	}
	if settings.Interval > 0 {
		sb["interval"] = settings.Interval
	}
	if settings.Hysteresis > 0 {
		sb["hysteresis"] = settings.Hysteresis
	}
	if settings.Balancer != "" {
		sb["balancer"] = settings.Balancer
	}
	if settings.MaxTime > 0 {
		sb["max_time"] = settings.MaxTime
	}
	if settings.TestURL != "" {
		sb["test_url"] = settings.TestURL
	}
	// Only write routing_file/outbounds_file if they differ from defaults
	if settings.RoutingFile != "" && settings.RoutingFile != defaultRoutingFile {
		sb["routing_file"] = settings.RoutingFile
	}
	if settings.OutboundsFile != "" && settings.OutboundsFile != defaultOutboundsFile {
		sb["outbounds_file"] = settings.OutboundsFile
	}
	// Only write log if true (default is false)
	if settings.Log {
		sb["log"] = settings.Log
	}

	xkeen["speed_balancer"] = sb
	raw["xkeen"] = xkeen

	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal xkeen.json: %w", err)
	}
	out = append(out, '\n')

	return os.WriteFile(h.configPath, out, 0o600)
}

func (h *SpeedBalancerHandler) doEnable() error {
	h.cmdMu.Lock()
	defer h.cmdMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, h.xkeenBinary, "-sb", "on") //nolint:gosec // binary path from config, not user input
	cmd.Stdin = strings.NewReader("y\n") // auto-accept interactive prompt
	_, err := cmd.CombinedOutput()
	return err
}

func (h *SpeedBalancerHandler) doDisable() error {
	h.cmdMu.Lock()
	defer h.cmdMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, h.xkeenBinary, "-sb", "off") //nolint:gosec // binary path from config, not user input
	_, err := cmd.CombinedOutput()
	return err
}

// GetSpeedBalancer handles GET /api/settings/speed-balancer.
func (h *SpeedBalancerHandler) GetSpeedBalancer(w http.ResponseWriter, _ *http.Request) {
	settings, err := h.readSettings()
	if err != nil {
		respondError(w, http.StatusInternalServerError,
			fmt.Sprintf("failed to read speed balancer settings: %v", err))
		return
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"ok":       true,
		"settings": settings,
	})
}

// GetSpeedBalancerStatus handles GET /api/settings/speed-balancer/status.
// Runs `xkeen -sb status` and returns the raw output (with ANSI codes — the
// frontend renders them via renderAnsi).
func (h *SpeedBalancerHandler) GetSpeedBalancerStatus(w http.ResponseWriter, _ *http.Request) {
	h.cmdMu.Lock()
	defer h.cmdMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, h.xkeenBinary, "-sb", "status") //nolint:gosec // binary path from config, not user input
	output, err := cmd.CombinedOutput()
	resp := map[string]interface{}{"ok": err == nil, "output": string(output)}
	if err != nil {
		resp["error"] = err.Error()
	}
	respondJSON(w, http.StatusOK, resp)
}

// UpdateSpeedBalancer handles PUT /api/settings/speed-balancer.
func (h *SpeedBalancerHandler) UpdateSpeedBalancer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Settings SpeedBalancerSettings `json:"settings"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Validate input ranges
	if req.Settings.Interval < 1 {
		respondError(w, http.StatusBadRequest, "interval must be >= 1")
		return
	}
	if req.Settings.Hysteresis < 0 {
		respondError(w, http.StatusBadRequest, "hysteresis must be >= 0")
		return
	}
	if req.Settings.MaxTime < 1 {
		respondError(w, http.StatusBadRequest, "max_time must be >= 1")
		return
	}

	// Atomic read-modify-write under mu to prevent race conditions
	h.mu.Lock()
	old, err := h.readSettings()
	if err != nil {
		h.mu.Unlock()
		respondError(w, http.StatusInternalServerError,
			fmt.Sprintf("failed to read settings: %v", err))
		return
	}
	if err := h.writeSettingsLocked(req.Settings); err != nil {
		h.mu.Unlock()
		respondError(w, http.StatusInternalServerError,
			fmt.Sprintf("failed to save settings: %v", err))
		return
	}
	h.mu.Unlock()

	result := map[string]interface{}{"ok": true}
	if !old.Enabled && req.Settings.Enabled {
		// Fire-and-forget: xkeen -sb on may take 30-60s (adds API inbound,
		// validates config, restarts Xray). Don't block the HTTP request.
		go func() {
			if err := h.runEnable(); err != nil {
				log.Printf("[speed-balancer] enable failed: %v", err)
			}
		}()
	} else if old.Enabled && !req.Settings.Enabled {
		go func() {
			if err := h.runDisable(); err != nil {
				log.Printf("[speed-balancer] disable failed: %v", err)
			}
		}()
	}

	respondJSON(w, http.StatusOK, result)
}

func jsonIntOr(v interface{}, def int) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	}
	return def
}

// RegisterXkeenRoutes registers XKeen-related routes.
func RegisterXkeenRoutes(r *mux.Router, infoHandler *XkeenInfoHandler, sbHandler *SpeedBalancerHandler) {
	r.HandleFunc("/xkeen/version", infoHandler.GetXkeenVersion).Methods("GET")
	r.HandleFunc("/settings/speed-balancer", sbHandler.GetSpeedBalancer).Methods("GET")
	r.HandleFunc("/settings/speed-balancer", sbHandler.UpdateSpeedBalancer).Methods("PUT")
	r.HandleFunc("/settings/speed-balancer/status", sbHandler.GetSpeedBalancerStatus).Methods("GET")
}
