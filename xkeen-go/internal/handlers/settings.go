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

	"github.com/gorilla/mux"

	"github.com/fan92rus/xkeen-ui/internal/config"
	"github.com/fan92rus/xkeen-ui/internal/utils"
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
	Ok          bool `json:"ok"`
	MetricsPort int  `json:"metrics_port"`
	Enabled     bool `json:"enabled"`
	Error       string `json:"error,omitempty"`
}

// SettingsHandler handles Xray settings operations.
type SettingsHandler struct {
	validator       *utils.PathValidator
	logConfigPath   string
	backupDir       string
	cfg             *config.Config
	configPath      string
	OnMetricsChange func(int) *MetricsHandler
	updateMetrics   func(port int) // called to update scheduler + write config file
}

// NewSettingsHandler creates a new SettingsHandler.
func NewSettingsHandler(allowedRoots []string, xrayConfigDir string, backupDir string, cfg *config.Config, configPath string, onMetricsChange func(int) *MetricsHandler) *SettingsHandler {
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
func (h *SettingsHandler) GetXraySettings(w http.ResponseWriter, r *http.Request) {
	// Default response
	response := XraySettingsResponse{
		LogLevel:  "none",
		LogLevels: []string{"debug", "info", "warning", "error", "none"},
		AccessLog: "/opt/var/log/xray/access.log",
		ErrorLog:  "/opt/var/log/xray/error.log",
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

	var config XrayLogConfigFile
	if err := json.Unmarshal(jsonData, &config); err != nil {
		respondError(w, http.StatusBadRequest, fmt.Sprintf("failed to parse log config JSON: %v", err))
		return
	}

	// Update response with actual values
	if config.Log.LogLevel != "" {
		response.LogLevel = config.Log.LogLevel
	}
	if config.Log.Access != "" {
		response.AccessLog = config.Log.Access
	}
	if config.Log.Error != "" {
		response.ErrorLog = config.Log.Error
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
	config := XrayLogConfigFile{
		Log: XrayLogConfig{
			Access:   "/opt/var/log/xray/access.log",
			Error:    "/opt/var/log/xray/error.log",
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
					config.Log.Access = existingConfig.Log.Access
				}
				if existingConfig.Log.Error != "" {
					config.Log.Error = existingConfig.Log.Error
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
	if err := os.MkdirAll(filepath.Dir(h.logConfigPath), 0755); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create config directory: %v", err))
		return
	}

	// Write new config
	newData, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to marshal config: %v", err))
		return
	}

	// Add trailing newline
	newData = append(newData, '\n')

	if err := os.WriteFile(h.logConfigPath, newData, 0644); err != nil {
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
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return "", nil
	}

	if err := os.MkdirAll(h.backupDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create backup directory: %w", err)
	}

	timestamp := getFileModTime(filePath)
	baseName := filepath.Base(filePath)
	backupName := fmt.Sprintf("%s.%s.bak", baseName, timestamp)
	backupPath := filepath.Join(h.backupDir, backupName)

	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read file for backup: %w", err)
	}

	if err := os.WriteFile(backupPath, data, 0644); err != nil {
		return "", fmt.Errorf("failed to write backup file: %w", err)
	}

	return backupPath, nil
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
func (h *SettingsHandler) ListBackupsForLogConfig(w http.ResponseWriter, r *http.Request) {
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

func (h *SettingsHandler) SetUpdateMetrics(fn func(int)) {
	h.updateMetrics = fn
}

// GetMetricsPort returns the current metrics port configuration.
// GET /api/settings/metrics
func (h *SettingsHandler) GetMetricsPort(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, MetricsPortResponse{
		Ok:          true,
		MetricsPort: h.cfg.MetricsPort,
		Enabled:     h.cfg.MetricsPort > 0,
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

	h.cfg.MetricsPort = req.MetricsPort
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

// RegisterSettingsRoutes registers settings-related routes.
func RegisterSettingsRoutes(r *mux.Router, handler *SettingsHandler) {
	r.HandleFunc("/xray/settings", handler.GetXraySettings).Methods("GET")
	r.HandleFunc("/xray/settings/log-level", handler.UpdateLogLevel).Methods("POST")
	r.HandleFunc("/xray/settings/backups", handler.ListBackupsForLogConfig).Methods("GET")
	r.HandleFunc("/settings/metrics", handler.GetMetricsPort).Methods("GET")
	r.HandleFunc("/settings/metrics", handler.UpdateMetricsPort).Methods("PUT")
}
