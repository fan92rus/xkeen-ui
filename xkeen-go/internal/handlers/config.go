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
	"time"

	"github.com/gorilla/mux"

	"github.com/fan92rus/xkeen-ui/internal/utils"
)

// ConfigHandler handles config file operations.
type ConfigHandler struct {
	validator       *utils.PathValidator
	backupDir       string
	defaultPath     string
	xrayConfigDir   string
	mihomoConfigDir string
	awgConfigDir    string
	configPath      string
	currentMode     string // "xray" or "mihomo"
}

// NewConfigHandler creates a new ConfigHandler.
func NewConfigHandler(allowedRoots []string, backupDir, xrayConfigDir, mihomoConfigDir, awgConfigDir, configPath, initialMode string) *ConfigHandler {
	validator, err := utils.NewPathValidator(allowedRoots)
	if err != nil {
		log.Printf("Warning: failed to create path validator: %v", err)
	}
	return &ConfigHandler{
		validator:       validator,
		backupDir:       backupDir,
		defaultPath:     xrayConfigDir,
		xrayConfigDir:   xrayConfigDir,
		mihomoConfigDir: mihomoConfigDir,
		awgConfigDir:    awgConfigDir,
		configPath:      configPath,
		currentMode:     initialMode,
	}
}

// FileInfo represents metadata about a config file.
type FileInfo struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	Modified int64  `json:"modified"`
	IsDir    bool   `json:"is_dir"`
}

// ModeInfo represents mode availability information.
type ModeInfo struct {
	Mode            string `json:"mode"`
	XrayAvailable   bool   `json:"xray_available"`
	MihomoAvailable bool   `json:"mihomo_available"`
}

// ModeRequest is the request body for SetMode.
type ModeRequest struct {
	Mode string `json:"mode"` // "xray" or "mihomo"
}

// isYAMLFile checks if a file is a YAML file.
func isYAMLFile(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".yml") || strings.HasSuffix(lower, ".yaml")
}

// isJSONFile checks if a file is a JSON/JSONC file.
func isJSONFile(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".json") || strings.HasSuffix(lower, ".jsonc")
}

// isConfFile checks if a file is a config file (AWG/WireGuard format).
func isConfFile(name string) bool {
	return strings.HasSuffix(strings.ToLower(name), ".conf")
}

// ListFilesResponse is the response for the ListFiles endpoint.
type ListFilesResponse struct {
	Path  string     `json:"path"`
	Files []FileInfo `json:"files"`
}

// ReadFileResponse is the response for the ReadFile endpoint.
type ReadFileResponse struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	Valid    bool   `json:"valid"`
	Modified int64  `json:"modified"`
}

// WriteFileRequest is the request body for the WriteFile endpoint.
type WriteFileRequest struct {
	Path             string `json:"path"`
	Content          string `json:"content"`
	ExpectedModified int64  `json:"expected_modified"`
}

// ErrorResponse represents an API error response.
type ErrorResponse struct {
	Error string `json:"error"`
}

// SetModeResponse is the response for setting the operation mode.
type SetModeResponse struct {
	Success bool   `json:"success"`
	Mode    string `json:"mode"`
}

// SaveFileResponse is the response for saving a config file.
type SaveFileResponse struct {
	Success  bool   `json:"success"`
	Path     string `json:"path"`
	Backup   string `json:"backup"`
	Modified int64  `json:"modified,omitempty"`
	Message  string `json:"message,omitempty"`
}

// RenameFileResponse is the response for renaming a config file.
type RenameFileResponse struct {
	Success bool   `json:"success"`
	OldPath string `json:"old_path"`
	NewPath string `json:"new_path"`
	Backup  string `json:"backup"`
}

// CreateFileResponse is the response for creating a config file.
type CreateFileResponse struct {
	Success bool   `json:"success"`
	Path    string `json:"path"`
	Message string `json:"message"`
}

// ListBackupsResponse is the response for listing backups.
type ListBackupsResponse struct {
	FilePath string     `json:"file"`
	Backups  []FileInfo `json:"backups"`
}

// RestoreBackupResponse is the response for restoring from a backup.
type RestoreBackupResponse struct {
	Success       bool   `json:"success"`
	BackupPath    string `json:"backup_path"`
	Content       string `json:"content,omitempty"`
	OriginalName  string `json:"original_name,omitempty"`
	RestoreToPath string `json:"restore_to_path,omitempty"`
}

// LogEntriesResponse is the response for reading log entries.
type LogEntriesResponse struct {
	Path    string       `json:"path"`
	Entries []LogMessage `json:"entries"`
}

// ListFiles returns a list of config files in the specified directory.
// GET /api/config/files?path=/opt/etc/xray/configs&mode=xray
func (h *ConfigHandler) ListFiles(w http.ResponseWriter, r *http.Request) {
	queryPath := r.URL.Query().Get("path")
	mode := r.URL.Query().Get("mode")

	// Determine default path based on mode
	if queryPath == "" {
		switch mode {
		case "mihomo":
			queryPath = h.mihomoConfigDir
		case "awg":
			queryPath = h.awgConfigDir
		default:
			queryPath = h.xrayConfigDir
		}
	}

	// Validate path
	cleanPath, err := h.validator.Validate(queryPath)
	if err != nil {
		respondError(w, http.StatusForbidden, err.Error())
		return
	}

	// Read directory
	entries, err := os.ReadDir(cleanPath)
	if err != nil {
		if os.IsNotExist(err) {
			respondError(w, http.StatusNotFound, "directory not found")
			return
		}
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to read directory: %v", err))
		return
	}

	files := h.filterFiles(entries, cleanPath, mode)

	respondJSON(w, http.StatusOK, ListFilesResponse{
		Path:  cleanPath,
		Files: files,
	})
}

// filterFiles filters directory entries based on mode/file type.
func (h *ConfigHandler) filterFiles(entries []os.DirEntry, cleanPath, mode string) []FileInfo {
	files := []FileInfo{}
	for _, entry := range entries {
		name := entry.Name()

		// Skip backups directory
		if entry.IsDir() && name == "backups" {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		// Skip directories - only show config files
		if entry.IsDir() {
			continue
		}

		// Include based on file type and mode
		include := false
		switch mode {
		case "mihomo":
			include = isYAMLFile(name)
		case "awg":
			include = isConfFile(name)
		default:
			include = isJSONFile(name)
		}

		if include {
			files = append(files, FileInfo{
				Name:     name,
				Path:     filepath.Join(cleanPath, name),
				Size:     info.Size(),
				Modified: info.ModTime().Unix(),
				IsDir:    false,
			})
		}
	}
	return files
}

// GroupedFile represents a group of files from one directory.
type GroupedFile struct {
	Section string     `json:"section"`
	Label   string     `json:"label"`
	Path    string     `json:"path"`
	Files   []FileInfo `json:"files"`
}

// ListFilesGroupedResponse is the response for the ListFilesGrouped endpoint.
type ListFilesGroupedResponse struct {
	Groups []GroupedFile `json:"groups"`
}

// ListFilesGrouped returns files grouped by config directory (xray, mihomo, awg).
// GET /api/config/files/grouped
func (h *ConfigHandler) ListFilesGrouped(w http.ResponseWriter, r *http.Request) {
	groups := []GroupedFile{}

	// Xray files
	if h.dirExists(h.xrayConfigDir) {
		entries, _ := os.ReadDir(h.xrayConfigDir)
		groups = append(groups, GroupedFile{
			Section: "xray",
			Label:   "Xray",
			Path:    h.xrayConfigDir,
			Files:   h.filterFiles(entries, h.xrayConfigDir, "xray"),
		})
	}

	// Mihomo files
	if h.dirExists(h.mihomoConfigDir) {
		entries, _ := os.ReadDir(h.mihomoConfigDir)
		groups = append(groups, GroupedFile{
			Section: "mihomo",
			Label:   "Mihomo",
			Path:    h.mihomoConfigDir,
			Files:   h.filterFiles(entries, h.mihomoConfigDir, "mihomo"),
		})
	}

	// AWG files
	if h.dirExists(h.awgConfigDir) {
		entries, _ := os.ReadDir(h.awgConfigDir)
		groups = append(groups, GroupedFile{
			Section: "awg",
			Label:   "AmneziaWG",
			Path:    h.awgConfigDir,
			Files:   h.filterFiles(entries, h.awgConfigDir, "awg"),
		})
	}

	respondJSON(w, http.StatusOK, ListFilesGroupedResponse{Groups: groups})
}

// GetMode returns current mode and availability.
// GET /api/config/mode
func (h *ConfigHandler) GetMode(w http.ResponseWriter, r *http.Request) {
	xrayAvailable := h.dirExists(h.xrayConfigDir)
	mihomoAvailable := h.dirExists(h.mihomoConfigDir)

	respondJSON(w, http.StatusOK, ModeInfo{
		Mode:            h.currentMode,
		XrayAvailable:   xrayAvailable,
		MihomoAvailable: mihomoAvailable,
	})
}

// SetMode sets the current mode.
// POST /api/config/mode
func (h *ConfigHandler) SetMode(w http.ResponseWriter, r *http.Request) {
	var req ModeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
		return
	}

	if req.Mode != "xray" && req.Mode != "mihomo" {
		respondError(w, http.StatusBadRequest, "mode must be 'xray' or 'mihomo'")
		return
	}

	// Check availability
	if req.Mode == "mihomo" && !h.dirExists(h.mihomoConfigDir) {
		respondError(w, http.StatusBadRequest, "mihomo is not available")
		return
	}
	if req.Mode == "xray" && !h.dirExists(h.xrayConfigDir) {
		respondError(w, http.StatusBadRequest, "xray is not available")
		return
	}

	// Update mode in memory
	h.currentMode = req.Mode

	// Save to config file
	if err := h.saveModeToConfig(req.Mode); err != nil {
		log.Printf("Warning: failed to save mode to config: %v", err)
		// Continue anyway, mode is updated in memory
	}

	respondJSON(w, http.StatusOK, SetModeResponse{
		Success: true,
		Mode:    req.Mode,
	})
}

// saveModeToConfig saves the mode to the config file.
func (h *ConfigHandler) saveModeToConfig(mode string) error {
	if h.configPath == "" {
		return nil
	}

	// Read existing config
	data, err := os.ReadFile(h.configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	// Parse as generic map to preserve all fields
	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse config file: %w", err)
	}

	// Update mode
	config["mode"] = mode

	// Write back
	newData, err := json.MarshalIndent(config, "", "    ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(h.configPath, newData, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// dirExists checks if a directory exists.
func (h *ConfigHandler) dirExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// ReadFile returns the content of a config file.
// GET /api/config/file?path=/opt/etc/xkeen/config.json
func (h *ConfigHandler) ReadFile(w http.ResponseWriter, r *http.Request) {
	filePath := r.URL.Query().Get("path")
	if filePath == "" {
		respondError(w, http.StatusBadRequest, "path parameter is required")
		return
	}

	// Validate path
	cleanPath, err := h.validator.Validate(filePath)
	if err != nil {
		respondError(w, http.StatusForbidden, err.Error())
		return
	}

	// Read file
	data, err := os.ReadFile(cleanPath)
	if err != nil {
		if os.IsNotExist(err) {
			respondError(w, http.StatusNotFound, "file not found")
			return
		}
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to read file: %v", err))
		return
	}

	// Get file modification time
	modified := int64(0)
	if info, err := os.Stat(cleanPath); err == nil {
		modified = info.ModTime().Unix()
	}

	// Validate based on file type
	var isValid bool
	switch {
	case isYAMLFile(cleanPath):
		// YAML files - basic validation (non-empty)
		isValid = len(strings.TrimSpace(string(data))) > 0
	case isConfFile(cleanPath):
		// AWG/WireGuard conf files - always valid (plain text)
		isValid = true
	default:
		// JSON/JSONC files - validate JSON
		jsonData, err := utils.JSONCtoJSON(data)
		if err != nil {
			respondError(w, http.StatusBadRequest, fmt.Sprintf("failed to parse JSONC: %v", err))
			return
		}
		isValid = json.Valid(jsonData)
	}

	respondJSON(w, http.StatusOK, ReadFileResponse{
		Path:     cleanPath,
		Content:  string(data),
		Valid:    isValid,
		Modified: modified,
	})
}

// WriteFile saves content to a config file with automatic backup.
// POST /api/config/file
func (h *ConfigHandler) WriteFile(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 10*1024*1024)

	var req WriteFileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
		return
	}

	if req.Path == "" {
		respondError(w, http.StatusBadRequest, "path is required")
		return
	}

	if req.Content == "" {
		respondError(w, http.StatusBadRequest, "content is required")
		return
	}

	// Validate path
	cleanPath, err := h.validator.Validate(req.Path)
	if err != nil {
		respondError(w, http.StatusForbidden, err.Error())
		return
	}

	// Optimistic locking: check expected_modified against current file mtime
	if req.ExpectedModified != 0 {
		if info, statErr := os.Stat(cleanPath); statErr == nil {
			currentModified := info.ModTime().Unix()
			if currentModified != req.ExpectedModified {
				respondError(w, http.StatusConflict, "file modified on disk")
				return
			}
		} else if !os.IsNotExist(statErr) {
			respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to stat file: %v", statErr))
			return
		}
		// If file doesn't exist yet and expected_modified != 0, that's fine — proceed
	}

	// Validate based on file type
	switch {
	case isYAMLFile(cleanPath):
		// YAML files - basic validation (non-empty)
		if strings.TrimSpace(req.Content) == "" {
			respondError(w, http.StatusBadRequest, "YAML content cannot be empty")
			return
		}
	case isConfFile(cleanPath):
		// AWG/WireGuard conf files - any text is valid
		if len(req.Content) == 0 {
			respondError(w, http.StatusBadRequest, "content cannot be empty")
			return
		}
	default:
		// JSON/JSONC files - validate JSON
		jsonData, err := utils.JSONCtoJSON([]byte(req.Content))
		if err != nil {
			respondError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSONC: %v", err))
			return
		}

		if !json.Valid(jsonData) {
			respondError(w, http.StatusBadRequest, "invalid JSON content")
			return
		}
	}

	// Create backup
	backupPath, err := h.createBackup(cleanPath)
	if err != nil {
		log.Printf("Warning: failed to create backup for %s: %v", cleanPath, err)
	}

	// Ensure parent directory exists
	parentDir := filepath.Dir(cleanPath)
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create parent directory: %v", err))
		return
	}

	// Write file
	if err := os.WriteFile(cleanPath, []byte(req.Content), 0644); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to write file: %v", err))
		return
	}

	// Get new mtime after write
	newModified := int64(0)
	if info, statErr := os.Stat(cleanPath); statErr == nil {
		newModified = info.ModTime().Unix()
	}

	respondJSON(w, http.StatusOK, SaveFileResponse{
		Success:  true,
		Path:     cleanPath,
		Backup:   backupPath,
		Modified: newModified,
	})
}

// createBackup creates a timestamped backup of the specified file.
func (h *ConfigHandler) createBackup(filePath string) (string, error) {
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return "", nil
	}

	if err := os.MkdirAll(h.backupDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create backup directory: %w", err)
	}

	timestamp := time.Now().Format("20060102-150405")
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

	// Cleanup old backups (keep only 5)
	h.cleanupOldBackups(filePath, 5)

	return backupPath, nil
}

// cleanupOldBackups removes old backups, keeping only the most recent ones.
func (h *ConfigHandler) cleanupOldBackups(filePath string, keep int) {
	entries, err := os.ReadDir(h.backupDir)
	if err != nil {
		return
	}

	baseName := filepath.Base(filePath)
	var backups []os.DirEntry
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, baseName+".") && strings.HasSuffix(name, ".bak") {
			backups = append(backups, entry)
		}
	}

	// Sort by modification time (newest first)
	sort.Slice(backups, func(i, j int) bool {
		infoI, errI := backups[i].Info()
		infoJ, errJ := backups[j].Info()
		if errI != nil || errJ != nil {
			return false
		}
		return infoI.ModTime().After(infoJ.ModTime())
	})

	// Remove old backups beyond keep limit
	for i := keep; i < len(backups); i++ {
		backupPath := filepath.Join(h.backupDir, backups[i].Name())
		os.Remove(backupPath)
	}
}

// DeleteFileRequest is the request body for the DeleteFile endpoint.
type DeleteFileRequest struct {
	Path string `json:"path"`
}

// RenameFileRequest is the request body for the RenameFile endpoint.
type RenameFileRequest struct {
	OldPath string `json:"old_path"`
	NewPath string `json:"new_path"`
}

// DeleteFile deletes a config file with automatic backup.
// DELETE /api/config/file
func (h *ConfigHandler) DeleteFile(w http.ResponseWriter, r *http.Request) {
	var req DeleteFileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
		return
	}

	if req.Path == "" {
		respondError(w, http.StatusBadRequest, "path is required")
		return
	}

	// Validate path
	cleanPath, err := h.validator.Validate(req.Path)
	if err != nil {
		respondError(w, http.StatusForbidden, err.Error())
		return
	}

	// Check if file exists
	if _, err := os.Stat(cleanPath); os.IsNotExist(err) {
		respondError(w, http.StatusNotFound, "file not found")
		return
	}

	// Create backup before deletion
	backupPath, err := h.createBackup(cleanPath)
	if err != nil {
		log.Printf("Warning: failed to create backup before deletion: %v", err)
	}

	// Delete file
	if err := os.Remove(cleanPath); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to delete file: %v", err))
		return
	}

	respondJSON(w, http.StatusOK, SaveFileResponse{
		Success: true,
		Path:    cleanPath,
		Backup:  backupPath,
		Message: "file deleted",
	})
}

// RenameFile renames/moves a config file with automatic backup.
// POST /api/config/rename
func (h *ConfigHandler) RenameFile(w http.ResponseWriter, r *http.Request) {
	var req RenameFileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
		return
	}

	if req.OldPath == "" || req.NewPath == "" {
		respondError(w, http.StatusBadRequest, "old_path and new_path are required")
		return
	}

	// Validate both paths
	oldCleanPath, err := h.validator.Validate(req.OldPath)
	if err != nil {
		respondError(w, http.StatusForbidden, fmt.Sprintf("old_path: %v", err))
		return
	}

	newCleanPath, err := h.validator.Validate(req.NewPath)
	if err != nil {
		respondError(w, http.StatusForbidden, fmt.Sprintf("new_path: %v", err))
		return
	}

	// Check if source file exists
	if _, err := os.Stat(oldCleanPath); os.IsNotExist(err) {
		respondError(w, http.StatusNotFound, "source file not found")
		return
	}

	// Check if destination already exists
	if _, err := os.Stat(newCleanPath); err == nil {
		respondError(w, http.StatusConflict, "destination file already exists")
		return
	}

	// Create backup before rename
	backupPath, err := h.createBackup(oldCleanPath)
	if err != nil {
		log.Printf("Warning: failed to create backup before rename: %v", err)
	}

	// Ensure parent directory exists for destination
	parentDir := filepath.Dir(newCleanPath)
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create parent directory: %v", err))
		return
	}

	// Rename file
	if err := os.Rename(oldCleanPath, newCleanPath); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to rename file: %v", err))
		return
	}

	respondJSON(w, http.StatusOK, RenameFileResponse{
		Success: true,
		OldPath: oldCleanPath,
		NewPath: newCleanPath,
		Backup:  backupPath,
	})
}

// CreateFile creates a new empty config file.
// POST /api/config/create
func (h *ConfigHandler) CreateFile(w http.ResponseWriter, r *http.Request) {
	var req WriteFileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
		return
	}

	if req.Path == "" {
		respondError(w, http.StatusBadRequest, "path is required")
		return
	}

	// Validate path
	cleanPath, err := h.validator.Validate(req.Path)
	if err != nil {
		respondError(w, http.StatusForbidden, err.Error())
		return
	}

	// Check if file already exists
	if _, err := os.Stat(cleanPath); err == nil {
		respondError(w, http.StatusConflict, "file already exists")
		return
	}

	// Default content if not provided
	content := req.Content
	if content == "" {
		content = "{\n\t\n}\n"
	}

	// Ensure parent directory exists
	parentDir := filepath.Dir(cleanPath)
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create parent directory: %v", err))
		return
	}

	// Write file
	if err := os.WriteFile(cleanPath, []byte(content), 0644); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create file: %v", err))
		return
	}

	respondJSON(w, http.StatusCreated, CreateFileResponse{
		Success: true,
		Path:    cleanPath,
		Message: "file created",
	})
}

// ListBackups returns available backups for a file.
// GET /api/config/backups?path=/opt/etc/xkeen/config.json
func (h *ConfigHandler) ListBackups(w http.ResponseWriter, r *http.Request) {
	filePath := r.URL.Query().Get("path")
	if filePath == "" {
		respondError(w, http.StatusBadRequest, "path parameter is required")
		return
	}

	// Read backup directory
	entries, err := os.ReadDir(h.backupDir)
	if err != nil {
		if os.IsNotExist(err) {
			respondJSON(w, http.StatusOK, []FileInfo{})
			return
		}
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to read backup directory: %v", err))
		return
	}

	// Filter backups for this file
	baseName := filepath.Base(filePath)
	backups := []FileInfo{}
	for _, entry := range entries {
		name := entry.Name()
		// Match pattern: filename.YYYYMMDD-HHMMSS.bak
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

	respondJSON(w, http.StatusOK, ListBackupsResponse{
		FilePath: filePath,
		Backups:  backups,
	})
}

// RestoreBackup restores a file from backup.
// POST /api/config/restore
func (h *ConfigHandler) RestoreBackup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BackupPath string `json:"backup_path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
		return
	}

	if req.BackupPath == "" {
		respondError(w, http.StatusBadRequest, "backup_path is required")
		return
	}

	// Validate backup path is in backup directory
	cleanBackupPath, err := h.validator.Validate(req.BackupPath)
	if err != nil {
		// Backup might be outside allowed roots, check if it's in backup dir
		cleanPath := filepath.Clean(req.BackupPath)
		absPath, absErr := filepath.Abs(cleanPath)
		if absErr != nil {
			respondError(w, http.StatusBadRequest, "invalid backup path")
			return
		}
		rel, relErr := filepath.Rel(h.backupDir, absPath)
		if relErr != nil || rel == ".." || strings.HasPrefix(rel, "..") {
			respondError(w, http.StatusForbidden, "backup path must be in backup directory")
			return
		}
		cleanBackupPath = absPath
	}

	// Extract original filename from backup name
	// Pattern: filename.YYYYMMDD-HHMMSS.bak
	backupName := filepath.Base(cleanBackupPath)
	idx := strings.LastIndex(backupName, ".20") // Find timestamp
	if idx == -1 {
		respondError(w, http.StatusBadRequest, "invalid backup filename format")
		return
	}
	originalName := backupName[:idx]

	// Read backup content
	data, err := os.ReadFile(cleanBackupPath)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to read backup: %v", err))
		return
	}

	respondJSON(w, http.StatusOK, RestoreBackupResponse{
		Success:       true,
		BackupPath:    cleanBackupPath,
		OriginalName:  originalName,
		Content:       string(data),
		RestoreToPath: filepath.Join(filepath.Dir(cleanBackupPath), "..", originalName),
	})
}

// GetBackupContent returns the content of a specific backup file.
// GET /api/config/backups/content?backup_path=<path>
func (h *ConfigHandler) GetBackupContent(w http.ResponseWriter, r *http.Request) {
	backupPath := r.URL.Query().Get("backup_path")
	if backupPath == "" {
		respondError(w, http.StatusBadRequest, "backup_path parameter is required")
		return
	}

	// Validate backup path
	var cleanBackupPath string
	validatedPath, err := h.validator.Validate(backupPath)
	if err != nil {
		// Backup might be outside allowed_roots, check if it's in backup dir
		cleanBackupPath = filepath.Clean(backupPath)
		absPath, absErr := filepath.Abs(cleanBackupPath)
		if absErr != nil {
			respondError(w, http.StatusBadRequest, "invalid backup path")
			return
		}
		rel, relErr := filepath.Rel(h.backupDir, absPath)
		if relErr != nil || rel == ".." || strings.HasPrefix(rel, "..") {
			respondError(w, http.StatusForbidden, "backup path must be in backup directory")
			return
		}
		cleanBackupPath = absPath
	} else {
		cleanBackupPath = validatedPath
	}

	// Read backup content
	data, err := os.ReadFile(cleanBackupPath)
	if err != nil {
		if os.IsNotExist(err) {
			respondError(w, http.StatusNotFound, "backup not found")
			return
		}
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to read backup: %v", err))
		return
	}

	respondJSON(w, http.StatusOK, RestoreBackupResponse{
		Success:     true,
		BackupPath:  cleanBackupPath,
		Content:     string(data),
	})
}

// RegisterConfigRoutes registers config-related routes.
func RegisterConfigRoutes(r *mux.Router, handler *ConfigHandler) {
	r.HandleFunc("/config/mode", handler.GetMode).Methods("GET")
	r.HandleFunc("/config/mode", handler.SetMode).Methods("POST")
	r.HandleFunc("/config/files", handler.ListFiles).Methods("GET")
	r.HandleFunc("/config/files/grouped", handler.ListFilesGrouped).Methods("GET")
	r.HandleFunc("/config/file", handler.ReadFile).Methods("GET")
	r.HandleFunc("/config/file", handler.WriteFile).Methods("POST")
	r.HandleFunc("/config/file", handler.DeleteFile).Methods("DELETE")
	r.HandleFunc("/config/create", handler.CreateFile).Methods("POST")
	r.HandleFunc("/config/rename", handler.RenameFile).Methods("POST")
	r.HandleFunc("/config/backups", handler.ListBackups).Methods("GET")
	r.HandleFunc("/config/backups/content", handler.GetBackupContent).Methods("GET")
	r.HandleFunc("/config/restore", handler.RestoreBackup).Methods("POST")
}
