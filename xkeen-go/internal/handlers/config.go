// Package handlers provides HTTP handlers for XKEEN-GO API endpoints.
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

	"github.com/user/xkeen-go/internal/utils"
)

// ConfigHandler handles config file operations.
type ConfigHandler struct {
	validator    *utils.PathValidator
	backupDir    string
	defaultPath   string
}

// NewConfigHandler creates a new ConfigHandler.
func NewConfigHandler(allowedRoots []string, backupDir string, defaultPath string) *ConfigHandler {
	validator, err := utils.NewPathValidator(allowedRoots)
	if err != nil {
		log.Printf("Warning: failed to create path validator: %v", err)
	}
	return &ConfigHandler{
		validator:  validator,
		backupDir:  backupDir,
		defaultPath: defaultPath,
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

// ListFilesResponse is the response for the ListFiles endpoint.
type ListFilesResponse struct {
	Path  string     `json:"path"`
	Files []FileInfo `json:"files"`
}

// ReadFileResponse is the response for the ReadFile endpoint.
type ReadFileResponse struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Valid   bool   `json:"valid"`
}

// WriteFileRequest is the request body for the WriteFile endpoint.
type WriteFileRequest struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// ErrorResponse represents an API error response.
type ErrorResponse struct {
	Error string `json:"error"`
}

// ListFiles returns a list of config files in the specified directory.
// GET /api/config/files?path=/opt/etc/xray/configs
func (h *ConfigHandler) ListFiles(w http.ResponseWriter, r *http.Request) {
	queryPath := r.URL.Query().Get("path")
	if queryPath == "" {
		queryPath = h.defaultPath
	}

	// Validate path
	cleanPath, err := h.validator.Validate(queryPath)
	if err != nil {
		h.respondError(w, http.StatusForbidden, err.Error())
		return
	}

	// Read directory
	entries, err := os.ReadDir(cleanPath)
	if err != nil {
		if os.IsNotExist(err) {
			h.respondError(w, http.StatusNotFound, "directory not found")
			return
		}
		h.respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to read directory: %v", err))
		return
	}

	// Filter JSON/JSONC files, exclude backups directory
	files := []FileInfo{}
	for _, entry := range entries {
		name := entry.Name()

		// Skip backups directory - it's an internal implementation detail
		if entry.IsDir() && name == "backups" {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		isJSONFile := strings.HasSuffix(strings.ToLower(name), ".json") ||
			strings.HasSuffix(strings.ToLower(name), ".jsonc")

		if entry.IsDir() || isJSONFile {
			files = append(files, FileInfo{
				Name:     name,
				Path:     filepath.Join(cleanPath, name),
				Size:     info.Size(),
				Modified: info.ModTime().Unix(),
				IsDir:    entry.IsDir(),
			})
		}
	}

	h.respondJSON(w, http.StatusOK, ListFilesResponse{
		Path:  cleanPath,
		Files: files,
	})
}

// ReadFile returns the content of a config file.
// GET /api/config/file?path=/opt/etc/xkeen/config.json
func (h *ConfigHandler) ReadFile(w http.ResponseWriter, r *http.Request) {
	filePath := r.URL.Query().Get("path")
	if filePath == "" {
		h.respondError(w, http.StatusBadRequest, "path parameter is required")
		return
	}

	// Validate path
	cleanPath, err := h.validator.Validate(filePath)
	if err != nil {
		h.respondError(w, http.StatusForbidden, err.Error())
		return
	}

	// Read file
	data, err := os.ReadFile(cleanPath)
	if err != nil {
		if os.IsNotExist(err) {
			h.respondError(w, http.StatusNotFound, "file not found")
			return
		}
		h.respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to read file: %v", err))
		return
	}

	// Validate JSON
	jsonData, err := utils.JSONCtoJSON(data)
	if err != nil {
		h.respondError(w, http.StatusBadRequest, fmt.Sprintf("failed to parse JSONC: %v", err))
		return
	}

	isValid := json.Valid(jsonData)

	h.respondJSON(w, http.StatusOK, ReadFileResponse{
		Path:    cleanPath,
		Content: string(data),
		Valid:   isValid,
	})
}

// WriteFile saves content to a config file with automatic backup.
// POST /api/config/file
func (h *ConfigHandler) WriteFile(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 10*1024*1024)

	var req WriteFileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
		return
	}

	if req.Path == "" {
		h.respondError(w, http.StatusBadRequest, "path is required")
		return
	}

	if req.Content == "" {
		h.respondError(w, http.StatusBadRequest, "content is required")
		return
	}

	// Validate path
	cleanPath, err := h.validator.Validate(req.Path)
	if err != nil {
		h.respondError(w, http.StatusForbidden, err.Error())
		return
	}

	// Validate JSON
	jsonData, err := utils.JSONCtoJSON([]byte(req.Content))
	if err != nil {
		h.respondError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSONC: %v", err))
		return
	}

	if !json.Valid(jsonData) {
		h.respondError(w, http.StatusBadRequest, "invalid JSON content")
		return
	}

	// Create backup
	backupPath, err := h.createBackup(cleanPath)
	if err != nil {
		log.Printf("Warning: failed to create backup for %s: %v", cleanPath, err)
	}

	// Ensure parent directory exists
	parentDir := filepath.Dir(cleanPath)
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		h.respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create parent directory: %v", err))
		return
	}

	// Write file
	if err := os.WriteFile(cleanPath, []byte(req.Content), 0644); err != nil {
		h.respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to write file: %v", err))
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"path":    cleanPath,
		"backup":  backupPath,
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

// respondJSON writes a JSON response.
func (h *ConfigHandler) respondJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("Error encoding JSON response: %v", err)
	}
}

// respondError writes an error response.
func (h *ConfigHandler) respondError(w http.ResponseWriter, statusCode int, message string) {
	h.respondJSON(w, statusCode, ErrorResponse{Error: message})
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
		h.respondError(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
		return
	}

	if req.Path == "" {
		h.respondError(w, http.StatusBadRequest, "path is required")
		return
	}

	// Validate path
	cleanPath, err := h.validator.Validate(req.Path)
	if err != nil {
		h.respondError(w, http.StatusForbidden, err.Error())
		return
	}

	// Check if file exists
	if _, err := os.Stat(cleanPath); os.IsNotExist(err) {
		h.respondError(w, http.StatusNotFound, "file not found")
		return
	}

	// Create backup before deletion
	backupPath, err := h.createBackup(cleanPath)
	if err != nil {
		log.Printf("Warning: failed to create backup before deletion: %v", err)
	}

	// Delete file
	if err := os.Remove(cleanPath); err != nil {
		h.respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to delete file: %v", err))
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"path":    cleanPath,
		"backup":  backupPath,
		"message": "file deleted",
	})
}

// RenameFile renames/moves a config file with automatic backup.
// POST /api/config/rename
func (h *ConfigHandler) RenameFile(w http.ResponseWriter, r *http.Request) {
	var req RenameFileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
		return
	}

	if req.OldPath == "" || req.NewPath == "" {
		h.respondError(w, http.StatusBadRequest, "old_path and new_path are required")
		return
	}

	// Validate both paths
	oldCleanPath, err := h.validator.Validate(req.OldPath)
	if err != nil {
		h.respondError(w, http.StatusForbidden, fmt.Sprintf("old_path: %v", err))
		return
	}

	newCleanPath, err := h.validator.Validate(req.NewPath)
	if err != nil {
		h.respondError(w, http.StatusForbidden, fmt.Sprintf("new_path: %v", err))
		return
	}

	// Check if source file exists
	if _, err := os.Stat(oldCleanPath); os.IsNotExist(err) {
		h.respondError(w, http.StatusNotFound, "source file not found")
		return
	}

	// Check if destination already exists
	if _, err := os.Stat(newCleanPath); err == nil {
		h.respondError(w, http.StatusConflict, "destination file already exists")
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
		h.respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create parent directory: %v", err))
		return
	}

	// Rename file
	if err := os.Rename(oldCleanPath, newCleanPath); err != nil {
		h.respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to rename file: %v", err))
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"old_path": oldCleanPath,
		"new_path": newCleanPath,
		"backup":   backupPath,
	})
}

// CreateFile creates a new empty config file.
// POST /api/config/create
func (h *ConfigHandler) CreateFile(w http.ResponseWriter, r *http.Request) {
	var req WriteFileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
		return
	}

	if req.Path == "" {
		h.respondError(w, http.StatusBadRequest, "path is required")
		return
	}

	// Validate path
	cleanPath, err := h.validator.Validate(req.Path)
	if err != nil {
		h.respondError(w, http.StatusForbidden, err.Error())
		return
	}

	// Check if file already exists
	if _, err := os.Stat(cleanPath); err == nil {
		h.respondError(w, http.StatusConflict, "file already exists")
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
		h.respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create parent directory: %v", err))
		return
	}

	// Write file
	if err := os.WriteFile(cleanPath, []byte(content), 0644); err != nil {
		h.respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create file: %v", err))
		return
	}

	h.respondJSON(w, http.StatusCreated, map[string]interface{}{
		"success": true,
		"path":    cleanPath,
		"message": "file created",
	})
}

// ListBackups returns available backups for a file.
// GET /api/config/backups?path=/opt/etc/xkeen/config.json
func (h *ConfigHandler) ListBackups(w http.ResponseWriter, r *http.Request) {
	filePath := r.URL.Query().Get("path")
	if filePath == "" {
		h.respondError(w, http.StatusBadRequest, "path parameter is required")
		return
	}

	// Read backup directory
	entries, err := os.ReadDir(h.backupDir)
	if err != nil {
		if os.IsNotExist(err) {
			h.respondJSON(w, http.StatusOK, []FileInfo{})
			return
		}
		h.respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to read backup directory: %v", err))
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

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"file":    filePath,
		"backups": backups,
	})
}

// RestoreBackup restores a file from backup.
// POST /api/config/restore
func (h *ConfigHandler) RestoreBackup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BackupPath string `json:"backup_path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
		return
	}

	if req.BackupPath == "" {
		h.respondError(w, http.StatusBadRequest, "backup_path is required")
		return
	}

	// Validate backup path is in backup directory
	cleanBackupPath, err := h.validator.Validate(req.BackupPath)
	if err != nil {
		// Backup might be outside allowed roots, check if it's in backup dir
		cleanPath := filepath.Clean(req.BackupPath)
		absPath, absErr := filepath.Abs(cleanPath)
		if absErr != nil {
			h.respondError(w, http.StatusBadRequest, "invalid backup path")
			return
		}
		// Verify the cleaned absolute path is within backup dir
		if !strings.HasPrefix(absPath, h.backupDir+string(filepath.Separator)) && absPath != h.backupDir {
			h.respondError(w, http.StatusForbidden, "backup path must be in backup directory")
			return
		}
		cleanBackupPath = absPath
	}

	// Extract original filename from backup name
	// Pattern: filename.YYYYMMDD-HHMMSS.bak
	backupName := filepath.Base(cleanBackupPath)
	idx := strings.LastIndex(backupName, ".20") // Find timestamp
	if idx == -1 {
		h.respondError(w, http.StatusBadRequest, "invalid backup filename format")
		return
	}
	originalName := backupName[:idx]

	// Read backup content
	data, err := os.ReadFile(cleanBackupPath)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to read backup: %v", err))
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"success":         true,
		"backup_path":     cleanBackupPath,
		"original_name":   originalName,
		"content":         string(data),
		"restore_to_path": filepath.Join(filepath.Dir(cleanBackupPath), "..", originalName),
	})
}

// GetBackupContent returns the content of a specific backup file.
// GET /api/config/backups/content?backup_path=<path>
func (h *ConfigHandler) GetBackupContent(w http.ResponseWriter, r *http.Request) {
	backupPath := r.URL.Query().Get("backup_path")
	if backupPath == "" {
		h.respondError(w, http.StatusBadRequest, "backup_path parameter is required")
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
			h.respondError(w, http.StatusBadRequest, "invalid backup path")
			return
		}
		// Verify the cleaned absolute path is within backup dir
		if !strings.HasPrefix(absPath, h.backupDir+string(filepath.Separator)) && absPath != h.backupDir {
			h.respondError(w, http.StatusForbidden, "backup path must be in backup directory")
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
			h.respondError(w, http.StatusNotFound, "backup not found")
			return
		}
		h.respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to read backup: %v", err))
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"success":     true,
		"backup_path": cleanBackupPath,
		"content":     string(data),
	})
}

// RegisterConfigRoutes registers config-related routes.
func RegisterConfigRoutes(r *mux.Router, handler *ConfigHandler) {
	r.HandleFunc("/config/files", handler.ListFiles).Methods("GET")
	r.HandleFunc("/config/file", handler.ReadFile).Methods("GET")
	r.HandleFunc("/config/file", handler.WriteFile).Methods("POST")
	r.HandleFunc("/config/file", handler.DeleteFile).Methods("DELETE")
	r.HandleFunc("/config/create", handler.CreateFile).Methods("POST")
	r.HandleFunc("/config/rename", handler.RenameFile).Methods("POST")
	r.HandleFunc("/config/backups", handler.ListBackups).Methods("GET")
	r.HandleFunc("/config/backups/content", handler.GetBackupContent).Methods("GET")
	r.HandleFunc("/config/restore", handler.RestoreBackup).Methods("POST")
}
