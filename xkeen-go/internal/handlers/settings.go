// Package handlers provides HTTP handlers for XKEEN-UI API endpoints.
package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/gorilla/mux"

	"github.com/fan92rus/xkeen-ui/internal/config"
	"github.com/fan92rus/xkeen-ui/internal/utils"
	"github.com/fan92rus/xkeen-ui/internal/version"
)

// UpdateLogLevelResponse is the response for the UpdateLogLevel endpoint.
type UpdateLogLevelResponse struct {
	Success  bool   `json:"success"`
	LogLevel string `json:"log_level"`
	Message  string `json:"message"`
}

// LogBackupsResponse is the response for ListBackupsForLogConfig.
type LogBackupsResponse struct {
	FilePath string     `json:"file"`
	Backups  []FileInfo `json:"backups"`
}

// MetricsPortResponse is the response for metrics port endpoints.
type MetricsPortResponse struct {
	Ok          bool   `json:"ok"`
	MetricsPort int    `json:"metrics_port"`
	Enabled     bool   `json:"enabled"`
	Error       string `json:"error,omitempty"`
}

// SettingsHandler handles Xray settings operations.
type SettingsHandler struct {
	validator       *utils.PathValidator
	logConfigPath   string
	backupDir       string
	cfg             *config.Config
	configPath      string
	OnMetricsChange func(int)      // called when the metrics port changes (data update, not lifecycle)
	updateMetrics   func(port int) // called to update scheduler + write config file

	// onProxyEntwareChange is called when the proxy_entware setting is toggled.
	// The callback (wired in server.go) regenerates outbounds, restarts xray,
	// and runs `xkeen -pr on/off`.
	onProxyEntwareChange func(enabled bool) error

	// onObservatoryChange is called when observatory concurrency is toggled
	// (wired in server.go to update subscriptionHandler + subScheduler).
	onObservatoryChange func(enabled bool)

	// onAutoUpdateChange is called when the auto_update setting is toggled
	// (wired in server.go to start/stop the UpdateChecker goroutine).
	onAutoUpdateChange func(enabled bool)

	// cfgMu protects concurrent access to cfg fields between HTTP handlers.
	// All cfg reads use RLock, all writes use Lock.
	cfgMu sync.RWMutex
}

// NewSettingsHandler creates a new SettingsHandler.
func NewSettingsHandler(allowedRoots []string, xrayConfigDir, backupDir string, cfg *config.Config, configPath string, onMetricsChange func(int)) *SettingsHandler {
	validator, err := utils.NewPathValidator(allowedRoots)
	if err != nil {
		log.Printf("Warning: failed to create path validator: %v", err)
	}
	return &SettingsHandler{
		validator:       validator,
		logConfigPath:   filepath.Join(xrayConfigDir, "01_log.json"),
		backupDir:       backupDir,
		cfg:             cfg,
		configPath:      configPath,
		OnMetricsChange: onMetricsChange,
	}
}

// XrayLogConfig represents the log configuration structure in Xray.
type XrayLogConfig struct {
	Access   string `json:"access,omitempty"`
	Error    string `json:"error,omitempty"`
	LogLevel string `json:"loglevel,omitempty"`
}

// XrayLogConfigFile represents the full log config file structure.
type XrayLogConfigFile struct {
	Log XrayLogConfig `json:"log"`
}

// XraySettingsResponse is the response for the GetXraySettings endpoint.
type XraySettingsResponse struct {
	LogLevel  string   `json:"log_level"`
	LogLevels []string `json:"log_levels"`
	AccessLog string   `json:"access_log"`
	ErrorLog  string   `json:"error_log"`
}

// UpdateLogLevelRequest is the request body for UpdateLogLevel.
type UpdateLogLevelRequest struct {
	LogLevel string `json:"log_level"`
}

// GetXraySettings returns current Xray logging settings.
// GET /api/xray/settings
func (h *SettingsHandler) GetXraySettings(w http.ResponseWriter, _ *http.Request) {
	// Default response
	response := XraySettingsResponse{
		LogLevel:  "none",
		LogLevels: []string{"debug", "info", "warning", "error", "none"},
		AccessLog: filepath.Join(h.cfg.XrayLogDir, "access.log"),
		ErrorLog:  filepath.Join(h.cfg.XrayLogDir, "error.log"),
	}

	// Read log config file
	data, err := os.ReadFile(h.logConfigPath)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist, return defaults
			respondJSON(w, http.StatusOK, response)
			return
		}
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to read log config: %v", err))
		return
	}

	// Parse JSONC (Xray configs may have comments)
	jsonData, err := utils.JSONCtoJSON(data)
	if err != nil {
		respondError(w, http.StatusBadRequest, fmt.Sprintf("failed to parse log config: %v", err))
		return
	}

	var logCfg XrayLogConfigFile
	if err := json.Unmarshal(jsonData, &logCfg); err != nil {
		respondError(w, http.StatusBadRequest, fmt.Sprintf("failed to parse log config JSON: %v", err))
		return
	}

	// Update response with actual values
	if logCfg.Log.LogLevel != "" {
		response.LogLevel = logCfg.Log.LogLevel
	}
	if logCfg.Log.Access != "" {
		response.AccessLog = logCfg.Log.Access
	}
	if logCfg.Log.Error != "" {
		response.ErrorLog = logCfg.Log.Error
	}

	respondJSON(w, http.StatusOK, response)
}

// UpdateLogLevel updates the Xray log level.
// POST /api/xray/settings/log-level
func (h *SettingsHandler) UpdateLogLevel(w http.ResponseWriter, r *http.Request) {
	var req UpdateLogLevelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
		return
	}

	// Validate log level
	validLevels := map[string]bool{
		"debug":   true,
		"info":    true,
		"warning": true,
		"error":   true,
		"none":    true,
	}
	if !validLevels[req.LogLevel] {
		respondError(w, http.StatusBadRequest, fmt.Sprintf("invalid log level: %s (valid: debug, info, warning, error, none)", req.LogLevel))
		return
	}

	// Read existing config or create new one
	logCfg := XrayLogConfigFile{
		Log: XrayLogConfig{
			Access:   filepath.Join(h.cfg.XrayLogDir, "access.log"),
			Error:    filepath.Join(h.cfg.XrayLogDir, "error.log"),
			LogLevel: req.LogLevel,
		},
	}

	// Try to read existing config to preserve other settings
	data, err := os.ReadFile(h.logConfigPath)
	if err == nil {
		jsonData, parseErr := utils.JSONCtoJSON(data)
		if parseErr == nil {
			var existingConfig XrayLogConfigFile
			if json.Unmarshal(jsonData, &existingConfig) == nil {
				// Preserve existing paths
				if existingConfig.Log.Access != "" {
					logCfg.Log.Access = existingConfig.Log.Access
				}
				if existingConfig.Log.Error != "" {
					logCfg.Log.Error = existingConfig.Log.Error
				}
			}
		}
	}

	// Create backup before writing
	if _, err := os.Stat(h.logConfigPath); err == nil {
		if backupPath, err := h.createBackup(h.logConfigPath); err != nil {
			log.Printf("Warning: failed to create backup: %v", err)
		} else {
			log.Printf("Created backup: %s", backupPath)
		}
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(h.logConfigPath), 0o750); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create config directory: %v", err))
		return
	}

	// Write new config
	newData, err := json.MarshalIndent(logCfg, "", "  ")
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to marshal config: %v", err))
		return
	}

	// Add trailing newline
	newData = append(newData, '\n')

	if err := os.WriteFile(h.logConfigPath, newData, 0o600); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to write config: %v", err))
		return
	}

	respondJSON(w, http.StatusOK, UpdateLogLevelResponse{
		Success:  true,
		LogLevel: req.LogLevel,
		Message:  fmt.Sprintf("Log level updated to '%s'. Restart Xray to apply changes.", req.LogLevel),
	})
}

// createBackup creates a timestamped backup of the specified file.
func (h *SettingsHandler) createBackup(filePath string) (string, error) {
	timestamp := getFileModTime(filePath)
	return createBackupCore(filePath, h.backupDir, timestamp)
}

// getFileModTime returns formatted modification time for backup naming.
func getFileModTime(filePath string) string {
	info, err := os.Stat(filePath)
	if err != nil {
		return "unknown"
	}
	return info.ModTime().Format("20060102-150405")
}

// ListBackupsForLogConfig returns available backups for log config.
// GET /api/xray/settings/backups
func (h *SettingsHandler) ListBackupsForLogConfig(w http.ResponseWriter, _ *http.Request) {
	entries, err := os.ReadDir(h.backupDir)
	if err != nil {
		if os.IsNotExist(err) {
			respondJSON(w, http.StatusOK, []FileInfo{})
			return
		}
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to read backup directory: %v", err))
		return
	}

	baseName := filepath.Base(h.logConfigPath)
	backups := []FileInfo{}
	for _, entry := range entries {
		name := entry.Name()
		// Match pattern: 01_log.json.YYYYMMDD-HHMMSS.bak
		if strings.HasPrefix(name, baseName+".") && strings.HasSuffix(name, ".bak") {
			info, err := entry.Info()
			if err != nil {
				continue
			}
			backups = append(backups, FileInfo{
				Name:     name,
				Path:     filepath.Join(h.backupDir, name),
				Size:     info.Size(),
				Modified: info.ModTime().Unix(),
				IsDir:    false,
			})
		}
	}

	// Sort by modification time, newest first
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].Modified > backups[j].Modified
	})

	respondJSON(w, http.StatusOK, LogBackupsResponse{
		FilePath: h.logConfigPath,
		Backups:  backups,
	})
}

// SetUpdateMetrics sets the callback function called when metrics port changes.
func (h *SettingsHandler) SetUpdateMetrics(fn func(int)) {
	h.updateMetrics = fn
}

// SetProxyEntwareChange sets the callback invoked when proxy_entware is toggled.
// The callback regenerates outbounds, restarts xray, and runs xkeen -pr on/off.
func (h *SettingsHandler) SetProxyEntwareChange(fn func(enabled bool) error) {
	h.onProxyEntwareChange = fn
}

// SetObservatoryChange sets the callback invoked when observatory concurrency is toggled.
func (h *SettingsHandler) SetObservatoryChange(fn func(enabled bool)) {
	h.onObservatoryChange = fn
}

// SetAutoUpdateChange sets the callback invoked when auto_update is toggled.
// The callback starts/stops the UpdateChecker goroutine.
func (h *SettingsHandler) SetAutoUpdateChange(fn func(enabled bool)) {
	h.onAutoUpdateChange = fn
}

// GetMetricsPort returns the current metrics port configuration.
// GET /api/settings/metrics
func (h *SettingsHandler) GetMetricsPort(w http.ResponseWriter, _ *http.Request) {
	h.cfgMu.RLock()
	port := h.cfg.MetricsPort
	h.cfgMu.RUnlock()
	respondJSON(w, http.StatusOK, MetricsPortResponse{
		Ok:          true,
		MetricsPort: port,
		Enabled:     port > 0,
	})
}

// UpdateMetricsPort updates the metrics port in config and triggers handler update.
// PUT /api/settings/metrics
func (h *SettingsHandler) UpdateMetricsPort(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MetricsPort int `json:"metrics_port"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, MetricsPortResponse{Ok: false, Error: "invalid request"})
		return
	}
	if req.MetricsPort < 0 || req.MetricsPort > 65535 {
		respondJSON(w, http.StatusBadRequest, MetricsPortResponse{Ok: false, Error: "port must be 0-65535"})
		return
	}

	h.cfgMu.Lock()
	h.cfg.MetricsPort = req.MetricsPort
	h.cfgMu.Unlock()
	if err := h.cfg.SaveConfig(h.configPath); err != nil {
		respondJSON(w, http.StatusInternalServerError, MetricsPortResponse{Ok: false, Error: "save failed: " + err.Error()})
		return
	}

	if h.OnMetricsChange != nil {
		h.OnMetricsChange(req.MetricsPort)
	}

	// Update scheduler and write 08_metrics.json
	if h.updateMetrics != nil {
		h.updateMetrics(req.MetricsPort)
	}

	respondJSON(w, http.StatusOK, MetricsPortResponse{
		Ok:          true,
		MetricsPort: req.MetricsPort,
		Enabled:     req.MetricsPort > 0,
	})
}

// AWGInterfaceResponse holds AWG LAN/WAN interface and endpoint settings.
type AWGInterfaceResponse struct {
	Ok       string `json:"ok"`
	LanIface string `json:"lan_iface"`
	WanIface string `json:"wan_iface"`
	Endpoint string `json:"endpoint"`
	Error    string `json:"error,omitempty"`
}

// GetAWGInterfaces returns the configured AWG LAN/WAN interfaces and endpoint.
// GET /api/settings/awg-interfaces
func (h *SettingsHandler) GetAWGInterfaces(w http.ResponseWriter, _ *http.Request) {
	h.cfgMu.RLock()
	lanIface := h.cfg.AWGLanIface
	wanIface := h.cfg.AWGWanIface
	endpoint := h.cfg.AWGEndpoint
	h.cfgMu.RUnlock()
	respondJSON(w, http.StatusOK, AWGInterfaceResponse{
		Ok:       "ok",
		LanIface: lanIface,
		WanIface: wanIface,
		Endpoint: endpoint,
	})
}

// UpdateAWGInterfaces updates the AWG LAN/WAN interfaces and endpoint in config.json.
// PUT /api/settings/awg-interfaces
func (h *SettingsHandler) UpdateAWGInterfaces(w http.ResponseWriter, r *http.Request) {
	var req struct {
		LanIface string `json:"lan_iface"`
		WanIface string `json:"wan_iface"`
		Endpoint string `json:"endpoint"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, AWGInterfaceResponse{Error: "invalid request"})
		return
	}

	// Basic validation: interface names are alphanumeric + optional digits/hyphens
	validateIface := func(s string) bool {
		if s == "" {
			return true // empty means auto-detect
		}
		for _, c := range s {
			if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' && c != '_' {
				return false
			}
		}
		return len(s) <= 15
	}
	if !validateIface(req.LanIface) || !validateIface(req.WanIface) {
		respondJSON(w, http.StatusBadRequest, AWGInterfaceResponse{Error: "invalid interface name"})
		return
	}

	// Validate endpoint: empty (auto), IP, or domain name
	if req.Endpoint != "" {
		if len(req.Endpoint) > 100 {
			respondJSON(w, http.StatusBadRequest, AWGInterfaceResponse{Error: "endpoint too long"})
			return
		}
		for _, c := range req.Endpoint {
			if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') &&
				c != '.' && c != '-' && c != ':' && c != '[' && c != ']' {
				respondJSON(w, http.StatusBadRequest, AWGInterfaceResponse{Error: "invalid endpoint (use IP or domain)"})
				return
			}
		}
	}

	h.cfgMu.Lock()
	h.cfg.AWGLanIface = req.LanIface
	h.cfg.AWGWanIface = req.WanIface
	h.cfg.AWGEndpoint = req.Endpoint
	h.cfgMu.Unlock()
	if err := h.cfg.SaveConfig(h.configPath); err != nil {
		respondJSON(w, http.StatusInternalServerError, AWGInterfaceResponse{Error: "save failed: " + err.Error()})
		return
	}

	respondJSON(w, http.StatusOK, AWGInterfaceResponse{
		Ok:       "ok",
		LanIface: req.LanIface,
		WanIface: req.WanIface,
		Endpoint: req.Endpoint,
	})
}

// GetProxyEntware returns the current proxy_entware setting.
// GET /api/settings/proxy-entware
func (h *SettingsHandler) GetProxyEntware(w http.ResponseWriter, _ *http.Request) {
	h.cfgMu.RLock()
	enabled := h.cfg.ProxyEntware
	h.cfgMu.RUnlock()
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"enabled": enabled,
	})
}

// GetObservatoryConcurrency returns the current observatory concurrency setting.
// GET /api/settings/observatory
func (h *SettingsHandler) GetObservatoryConcurrency(w http.ResponseWriter, _ *http.Request) {
	h.cfgMu.RLock()
	enabled := h.cfg.ObservatoryConcurrency
	h.cfgMu.RUnlock()
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"ok":      true,
		"enabled": enabled,
	})
}

// UpdateObservatoryConcurrency updates the observatory concurrency setting.
// PUT /api/settings/observatory
func (h *SettingsHandler) UpdateObservatoryConcurrency(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	h.cfgMu.Lock()
	h.cfg.ObservatoryConcurrency = req.Enabled
	h.cfgMu.Unlock()
	if err := h.cfg.SaveConfig(h.configPath); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to save config")
		return
	}

	if h.onObservatoryChange != nil {
		h.onObservatoryChange(req.Enabled)
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"ok":      true,
		"enabled": req.Enabled,
	})
}

// UpdateProxyEntware toggles routing of router-originated (Entware) traffic through Xray.
// POST /api/settings/proxy-entware {"enabled": true/false}
//
// When enabling: saves config, then the wired callback regenerates outbounds
// with sockopt.mark:255, restarts xray, and runs `xkeen -pr on`.
// When disabling: saves config and runs `xkeen -pr off` immediately (the mark
// on existing outbounds becomes harmless once iptables OUTPUT rules are gone).
func (h *SettingsHandler) UpdateProxyEntware(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	h.cfgMu.Lock()
	h.cfg.ProxyEntware = req.Enabled
	h.cfgMu.Unlock()
	if err := h.cfg.SaveConfig(h.configPath); err != nil {
		http.Error(w, "Failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if h.onProxyEntwareChange != nil {
		if err := h.onProxyEntwareChange(req.Enabled); err != nil {
			// Config was saved; report the apply error but don't revert the setting.
			respondJSON(w, http.StatusInternalServerError, map[string]interface{}{
				"enabled": req.Enabled,
				"error":   err.Error(),
			})
			return
		}
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"enabled": req.Enabled,
	})
}

// GetAutoUpdate returns the current auto_update setting.
// GET /api/settings/auto-update
func (h *SettingsHandler) GetAutoUpdate(w http.ResponseWriter, _ *http.Request) {
	h.cfgMu.RLock()
	enabled := h.cfg.AutoUpdate
	h.cfgMu.RUnlock()
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"ok":      true,
		"enabled": enabled,
	})
}

// UpdateAutoUpdate toggles automatic self-update to the latest stable release.
// PUT /api/settings/auto-update {"enabled": true/false}
func (h *SettingsHandler) UpdateAutoUpdate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	h.cfgMu.Lock()
	h.cfg.AutoUpdate = req.Enabled
	h.cfgMu.Unlock()
	if err := h.cfg.SaveConfig(h.configPath); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to save config")
		return
	}

	if h.onAutoUpdateChange != nil {
		h.onAutoUpdateChange(req.Enabled)
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"ok":      true,
		"enabled": req.Enabled,
	})
}

// ---------------------------------------------------------------------------
// Changelog endpoints
// ---------------------------------------------------------------------------

// GetChangelog returns the full curated changelog.
// GET /api/changelog
func (h *SettingsHandler) GetChangelog(w http.ResponseWriter, _ *http.Request) {
	data, err := version.GetFullChangelog()
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to read changelog")
		return
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"ok":        true,
		"changelog": data,
	})
}

// RegisterSettingsRoutes registers settings-related routes.
func RegisterSettingsRoutes(r *mux.Router, handler *SettingsHandler) {
	r.HandleFunc("/xray/settings", handler.GetXraySettings).Methods("GET")
	r.HandleFunc("/xray/settings/log-level", handler.UpdateLogLevel).Methods("POST")
	r.HandleFunc("/xray/settings/backups", handler.ListBackupsForLogConfig).Methods("GET")
	r.HandleFunc("/settings/metrics", handler.GetMetricsPort).Methods("GET")
	r.HandleFunc("/settings/metrics", handler.UpdateMetricsPort).Methods("PUT")
	r.HandleFunc("/settings/awg-interfaces", handler.GetAWGInterfaces).Methods("GET")
	r.HandleFunc("/settings/awg-interfaces", handler.UpdateAWGInterfaces).Methods("PUT")
	r.HandleFunc("/settings/proxy-entware", handler.GetProxyEntware).Methods("GET")
	r.HandleFunc("/settings/proxy-entware", handler.UpdateProxyEntware).Methods("POST")
	r.HandleFunc("/settings/observatory", handler.GetObservatoryConcurrency).Methods("GET")
	r.HandleFunc("/settings/observatory", handler.UpdateObservatoryConcurrency).Methods("PUT")
	r.HandleFunc("/settings/auto-update", handler.GetAutoUpdate).Methods("GET")
	r.HandleFunc("/settings/auto-update", handler.UpdateAutoUpdate).Methods("PUT")
	r.HandleFunc("/changelog", handler.GetChangelog).Methods("GET")
}
