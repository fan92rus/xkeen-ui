// Package handlers provides HTTP handlers for XKEEN-GO API endpoints.
package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// TestListFiles_ExcludesBackupsFolder verifies that the backups directory
// is excluded from the file listing response.
func TestListFiles_ExcludesBackupsFolder(t *testing.T) {
	// Create a temporary directory structure for testing
	tempDir, err := os.MkdirTemp("", "xkeen-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create test files and directories
	testFiles := []string{
		"config.json",
		"rules.jsonc",
		"subdir/nested.json",
	}
	for _, file := range testFiles {
		fullPath := filepath.Join(tempDir, file)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("Failed to create parent dir: %v", err)
		}
		if err := os.WriteFile(fullPath, []byte("{}"), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", file, err)
		}
	}

	// Create backups directory (should be hidden)
	backupDir := filepath.Join(tempDir, "backups")
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		t.Fatalf("Failed to create backups dir: %v", err)
	}
	// Add a file inside backups to ensure it's not just empty
	if err := os.WriteFile(filepath.Join(backupDir, "config.json.20240101-120000.bak"), []byte("{}"), 0644); err != nil {
		t.Fatalf("Failed to create backup file: %v", err)
	}

	// Create a regular directory that should be visible
	normalDir := filepath.Join(tempDir, "normaldir")
	if err := os.MkdirAll(normalDir, 0755); err != nil {
		t.Fatalf("Failed to create normal dir: %v", err)
	}

	// Create handler with temp dir as allowed root
	handler := NewConfigHandler([]string{tempDir}, filepath.Join(tempDir, "backups"), tempDir)

	// Create request
	req := httptest.NewRequest("GET", "/api/config/files?path="+tempDir, nil)
	rr := httptest.NewRecorder()

	handler.ListFiles(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp ListFilesResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Check that backups folder is NOT in the list
	for _, file := range resp.Files {
		if file.Name == "backups" {
			t.Error("backups directory should be excluded from file listing")
		}
	}

	// Check that normal directory IS in the list
	foundNormalDir := false
	for _, file := range resp.Files {
		if file.Name == "normaldir" {
			foundNormalDir = true
			break
		}
	}
	if !foundNormalDir {
		t.Error("normaldir should be included in file listing")
	}

	// Check that JSON files are included
	foundConfig := false
	for _, file := range resp.Files {
		if file.Name == "config.json" {
			foundConfig = true
			break
		}
	}
	if !foundConfig {
		t.Error("config.json should be included in file listing")
	}

	t.Logf("Found %d files (backups should be excluded)", len(resp.Files))
	for _, f := range resp.Files {
		t.Logf("  - %s (isDir=%v)", f.Name, f.IsDir)
	}
}

// TestListFiles_DefaultPath tests that the default path is used when no path is provided.
func TestListFiles_DefaultPath(t *testing.T) {
	// Create a temporary directory structure for testing
	tempDir, err := os.MkdirTemp("", "xkeen-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create test file
	if err := os.WriteFile(filepath.Join(tempDir, "config.json"), []byte("{}"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create handler with temp dir as default path
	handler := NewConfigHandler([]string{tempDir}, filepath.Join(tempDir, "backups"), tempDir)

	// Create request without path parameter
	req := httptest.NewRequest("GET", "/api/config/files", nil)
	rr := httptest.NewRecorder()

	handler.ListFiles(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	var resp ListFilesResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp.Path != tempDir {
		t.Errorf("Expected path %s, got %s", tempDir, resp.Path)
	}

	if len(resp.Files) != 1 || resp.Files[0].Name != "config.json" {
		t.Errorf("Expected 1 file (config.json), got %v", resp.Files)
	}
}

// TestListFiles_InvalidPath tests that invalid paths are rejected.
func TestListFiles_InvalidPath(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "xkeen-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	handler := NewConfigHandler([]string{tempDir}, filepath.Join(tempDir, "backups"), tempDir)

	// Try to access a path outside allowed roots
	req := httptest.NewRequest("GET", "/api/config/files?path=/etc/passwd", nil)
	rr := httptest.NewRecorder()

	handler.ListFiles(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("Expected status 403, got %d", rr.Code)
	}
}

// TestListFiles_NonexistentDirectory tests handling of non-existent directories.
func TestListFiles_NonexistentDirectory(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "xkeen-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	handler := NewConfigHandler([]string{tempDir}, filepath.Join(tempDir, "backups"), tempDir)

	// Try to access a non-existent subdirectory
	req := httptest.NewRequest("GET", "/api/config/files?path="+filepath.Join(tempDir, "nonexistent"), nil)
	rr := httptest.NewRecorder()

	handler.ListFiles(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", rr.Code)
	}
}
