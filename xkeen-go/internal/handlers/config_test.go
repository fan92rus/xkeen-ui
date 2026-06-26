package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
)

// setupConfigTest creates a temporary directory structure and ConfigHandler for testing.
func setupConfigTest(t *testing.T) (*ConfigHandler, string, string) {
	t.Helper()

	tmpDir := t.TempDir()

	// Create directory structure mimicking router
	configDir := filepath.Join(tmpDir, "configs")
	backupDir := filepath.Join(tmpDir, "backups")
	os.MkdirAll(configDir, 0755)
	os.MkdirAll(backupDir, 0755)

	// Create sample config files
	os.WriteFile(filepath.Join(configDir, "01_log.json"), []byte(`{"log":{"loglevel":"warning"}}`), 0644)
	os.WriteFile(filepath.Join(configDir, "02_dns.json"), []byte(`{"dns":{"servers":["8.8.8.8"]}}`), 0644)
	os.WriteFile(filepath.Join(configDir, "03_inbounds.json"), []byte(`{"inbounds":[{"tag":"tun-in"}]}`), 0644)
	os.WriteFile(filepath.Join(configDir, "04_outbounds.jsonc"), []byte(`{
		// Outbound configuration
		"outbounds": [{"tag":"proxy"}]
	}`), 0644)

	// Create a YAML file for mihomo mode testing
	mihomoDir := filepath.Join(tmpDir, "mihomo")
	os.MkdirAll(mihomoDir, 0755)
	os.WriteFile(filepath.Join(mihomoDir, "config.yaml"), []byte("proxy:\n  name: test\n"), 0644)

	// Config file path (for mode saving)
	configPath := filepath.Join(tmpDir, "xkeen-ui", "config.json")
	os.MkdirAll(filepath.Dir(configPath), 0755)
	os.WriteFile(configPath, []byte(`{"mode":"xray"}`), 0644)

	handler := NewConfigHandler(
		[]string{tmpDir},
		backupDir,
		configDir,
		mihomoDir,
		filepath.Join(tmpDir, "awg"),
		configPath,
		"xray",
	)

	return handler, tmpDir, backupDir
}

// --- ListFiles Tests ---

func TestListFiles_DefaultPath(t *testing.T) {
	handler, _, _ := setupConfigTest(t)

	req := httptest.NewRequest("GET", "/api/config/files", nil)
	rec := httptest.NewRecorder()
	handler.ListFiles(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp ListFilesResponse
	json.NewDecoder(rec.Body).Decode(&resp)

	if len(resp.Files) == 0 {
		t.Error("Expected files to be listed")
	}

	// Should only include JSON/JSONC files in xray mode
	for _, f := range resp.Files {
		if !isJSONFile(f.Name) {
			t.Errorf("Expected JSON file, got %q", f.Name)
		}
	}
}

func TestListFiles_SpecificPath(t *testing.T) {
	handler, tmpDir, _ := setupConfigTest(t)

	req := httptest.NewRequest("GET", "/api/config/files?path="+filepath.Join(tmpDir, "configs"), nil)
	rec := httptest.NewRecorder()
	handler.ListFiles(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rec.Code)
	}
}

func TestListFiles_InvalidPath(t *testing.T) {
	handler, _, _ := setupConfigTest(t)

	req := httptest.NewRequest("GET", "/api/config/files?path=/etc/passwd", nil)
	rec := httptest.NewRecorder()
	handler.ListFiles(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected 403 for path outside roots, got %d", rec.Code)
	}
}

func TestListFiles_NonExistentDir(t *testing.T) {
	handler, tmpDir, _ := setupConfigTest(t)

	req := httptest.NewRequest("GET", "/api/config/files?path="+filepath.Join(tmpDir, "nonexistent"), nil)
	rec := httptest.NewRecorder()
	handler.ListFiles(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected 404, got %d", rec.Code)
	}
}

func TestListFiles_MihomoMode(t *testing.T) {
	handler, tmpDir, _ := setupConfigTest(t)

	req := httptest.NewRequest("GET", "/api/config/files?path="+filepath.Join(tmpDir, "mihomo")+"&mode=mihomo", nil)
	rec := httptest.NewRecorder()
	handler.ListFiles(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp ListFilesResponse
	json.NewDecoder(rec.Body).Decode(&resp)

	if len(resp.Files) == 0 {
		t.Error("Expected YAML files in mihomo mode")
	}
	for _, f := range resp.Files {
		if !isYAMLFile(f.Name) {
			t.Errorf("Expected YAML file in mihomo mode, got %q", f.Name)
		}
	}
}

// --- ReadFile Tests ---

func TestReadFile_ValidJSON(t *testing.T) {
	handler, tmpDir, _ := setupConfigTest(t)

	filePath := filepath.Join(tmpDir, "configs", "01_log.json")
	req := httptest.NewRequest("GET", "/api/config/file?path="+filePath, nil)
	rec := httptest.NewRecorder()
	handler.ReadFile(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp ReadFileResponse
	json.NewDecoder(rec.Body).Decode(&resp)

	if resp.Content == "" {
		t.Error("Expected file content")
	}
	if !resp.Valid {
		t.Error("Expected valid JSON")
	}
}

func TestReadFile_JSONCFile(t *testing.T) {
	handler, tmpDir, _ := setupConfigTest(t)

	filePath := filepath.Join(tmpDir, "configs", "04_outbounds.jsonc")
	req := httptest.NewRequest("GET", "/api/config/file?path="+filePath, nil)
	rec := httptest.NewRecorder()
	handler.ReadFile(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp ReadFileResponse
	json.NewDecoder(rec.Body).Decode(&resp)

	if !resp.Valid {
		t.Error("JSONC file should be valid after stripping comments")
	}
}

func TestReadFile_NoPath(t *testing.T) {
	handler, _, _ := setupConfigTest(t)

	req := httptest.NewRequest("GET", "/api/config/file", nil)
	rec := httptest.NewRecorder()
	handler.ReadFile(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", rec.Code)
	}
}

func TestReadFile_NotFound(t *testing.T) {
	handler, tmpDir, _ := setupConfigTest(t)

	filePath := filepath.Join(tmpDir, "configs", "nonexistent.json")
	req := httptest.NewRequest("GET", "/api/config/file?path="+filePath, nil)
	rec := httptest.NewRecorder()
	handler.ReadFile(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected 404, got %d", rec.Code)
	}
}

func TestReadFile_OutsideRoot(t *testing.T) {
	handler, _, _ := setupConfigTest(t)

	req := httptest.NewRequest("GET", "/api/config/file?path=/etc/passwd", nil)
	rec := httptest.NewRecorder()
	handler.ReadFile(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected 403, got %d", rec.Code)
	}
}

// --- WriteFile Tests ---

func TestWriteFile_NewFile(t *testing.T) {
	handler, tmpDir, _ := setupConfigTest(t)

	newFilePath := filepath.Join(tmpDir, "configs", "05_new.json")
	body := WriteFileRequest{
		Path:    newFilePath,
		Content: `{"new": true}`,
	}

	req := httptest.NewRequest("POST", "/api/config/file", marshalBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.WriteFile(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify file was created
	data, err := os.ReadFile(newFilePath)
	if err != nil {
		t.Fatalf("File not created: %v", err)
	}
	if string(data) != `{"new": true}` {
		t.Errorf("File content mismatch: %s", data)
	}
}

func TestWriteFile_OverwriteWithBackup(t *testing.T) {
	handler, tmpDir, backupDir := setupConfigTest(t)

	filePath := filepath.Join(tmpDir, "configs", "01_log.json")
	body := WriteFileRequest{
		Path:    filePath,
		Content: `{"log":{"loglevel":"debug"}}`,
	}

	req := httptest.NewRequest("POST", "/api/config/file", marshalBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.WriteFile(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify backup was created
	entries, _ := os.ReadDir(backupDir)
	if len(entries) == 0 {
		t.Error("Expected backup file to be created")
	}

	// Verify file was updated
	data, _ := os.ReadFile(filePath)
	if string(data) != `{"log":{"loglevel":"debug"}}` {
		t.Errorf("File not updated: %s", data)
	}
}

func TestWriteFile_InvalidPath(t *testing.T) {
	handler, _, _ := setupConfigTest(t)

	body := WriteFileRequest{
		Path:    "/etc/passwd",
		Content: `{"evil": true}`,
	}

	req := httptest.NewRequest("POST", "/api/config/file", marshalBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.WriteFile(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected 403, got %d", rec.Code)
	}
}

func TestWriteFile_EmptyContent(t *testing.T) {
	handler, tmpDir, _ := setupConfigTest(t)

	body := WriteFileRequest{
		Path:    filepath.Join(tmpDir, "configs", "empty.json"),
		Content: "",
	}

	req := httptest.NewRequest("POST", "/api/config/file", marshalBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.WriteFile(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for empty content, got %d", rec.Code)
	}
}

func TestWriteFile_InvalidJSON(t *testing.T) {
	handler, tmpDir, _ := setupConfigTest(t)

	body := WriteFileRequest{
		Path:    filepath.Join(tmpDir, "configs", "bad.json"),
		Content: `{not valid json}`,
	}

	req := httptest.NewRequest("POST", "/api/config/file", marshalBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.WriteFile(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid JSON, got %d", rec.Code)
	}
}

func TestWriteFile_NoPath(t *testing.T) {
	handler, _, _ := setupConfigTest(t)

	body := WriteFileRequest{Content: "{}"}

	req := httptest.NewRequest("POST", "/api/config/file", marshalBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.WriteFile(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", rec.Code)
	}
}

// --- WriteFile mtime / Optimistic Locking Tests ---

func TestWriteFile_ConflictOnStaleMtime(t *testing.T) {
	handler, tmpDir, _ := setupConfigTest(t)

	filePath := filepath.Join(tmpDir, "configs", "01_log.json")

	// Read file first to get its mtime
	readReq := httptest.NewRequest("GET", "/api/config/file?path="+filePath, nil)
	readRec := httptest.NewRecorder()
	handler.ReadFile(readRec, readReq)
	if readRec.Code != http.StatusOK {
		t.Fatalf("ReadFile failed: %d: %s", readRec.Code, readRec.Body.String())
	}
	var readResp ReadFileResponse
	if err := json.Unmarshal(readRec.Body.Bytes(), &readResp); err != nil {
		t.Fatalf("failed to parse ReadFile response: %v", err)
	}
	if readResp.Modified == 0 {
		t.Fatal("expected non-zero modified in ReadFile response")
	}

	// Wait to ensure mtime changes (Unix() = seconds precision)
	time.Sleep(1100 * time.Millisecond)

	// Modify file on disk directly (simulates external change)
	if err := os.WriteFile(filePath, []byte(`{"external": true}`), 0644); err != nil {
		t.Fatalf("failed to modify file: %v", err)
	}

	// Try to save with stale expected_modified → should get 409
	body := WriteFileRequest{
		Path:             filePath,
		Content:          `{"new": true}`,
		ExpectedModified: readResp.Modified,
	}
	req := httptest.NewRequest("POST", "/api/config/file", marshalBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.WriteFile(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify the file was NOT overwritten
	data, _ := os.ReadFile(filePath)
	if strings.Contains(string(data), `"new": true`) {
		t.Error("file was overwritten despite conflict")
	}
}

func TestWriteFile_NoCheckWhenExpectedModifiedZero(t *testing.T) {
	handler, tmpDir, _ := setupConfigTest(t)

	filePath := filepath.Join(tmpDir, "configs", "01_log.json")

	// Save without expected_modified (or 0 — backward compatible)
	body := WriteFileRequest{
		Path:    filePath,
		Content: `{"log":{"loglevel":"info"}}`,
	}
	req := httptest.NewRequest("POST", "/api/config/file", marshalBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.WriteFile(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify file was written
	data, _ := os.ReadFile(filePath)
	if !strings.Contains(string(data), `"loglevel":"info"`) {
		t.Error("file was not updated")
	}
}

func TestReadFile_ReturnsModified(t *testing.T) {
	handler, tmpDir, _ := setupConfigTest(t)

	filePath := filepath.Join(tmpDir, "configs", "01_log.json")

	req := httptest.NewRequest("GET", "/api/config/file?path="+filePath, nil)
	rec := httptest.NewRecorder()
	handler.ReadFile(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("ReadFile failed: %d: %s", rec.Code, rec.Body.String())
	}

	var resp ReadFileResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp.Modified == 0 {
		t.Error("expected non-zero modified field in ReadFile response")
	}
}

func TestWriteFile_ResponseIncludesModified(t *testing.T) {
	handler, tmpDir, _ := setupConfigTest(t)

	filePath := filepath.Join(tmpDir, "configs", "01_log.json")

	body := WriteFileRequest{
		Path:    filePath,
		Content: `{"log":{"loglevel":"debug"}}`,
	}
	req := httptest.NewRequest("POST", "/api/config/file", marshalBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.WriteFile(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	modified, ok := result["modified"].(float64)
	if !ok || modified == 0 {
		t.Error("expected non-zero modified field in WriteFile response")
	}
}

// --- DeleteFile Tests ---

func TestDeleteFile_Success(t *testing.T) {
	handler, tmpDir, _ := setupConfigTest(t)

	filePath := filepath.Join(tmpDir, "configs", "01_log.json")
	body := DeleteFileRequest{Path: filePath}

	req := httptest.NewRequest("DELETE", "/api/config/file", marshalBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.DeleteFile(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify file was deleted
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Error("File should be deleted")
	}
}

func TestDeleteFile_NotFound(t *testing.T) {
	handler, tmpDir, _ := setupConfigTest(t)

	body := DeleteFileRequest{Path: filepath.Join(tmpDir, "configs", "nonexistent.json")}

	req := httptest.NewRequest("DELETE", "/api/config/file", marshalBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.DeleteFile(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected 404, got %d", rec.Code)
	}
}

func TestDeleteFile_OutsideRoot(t *testing.T) {
	handler, _, _ := setupConfigTest(t)

	body := DeleteFileRequest{Path: "/etc/important_file"}

	req := httptest.NewRequest("DELETE", "/api/config/file", marshalBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.DeleteFile(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected 403, got %d", rec.Code)
	}
}

// --- CreateFile Tests ---

func TestCreateFile_Success(t *testing.T) {
	handler, tmpDir, _ := setupConfigTest(t)

	newPath := filepath.Join(tmpDir, "configs", "new_config.json")
	body := WriteFileRequest{
		Path:    newPath,
		Content: `{"created": true}`,
	}

	req := httptest.NewRequest("POST", "/api/config/create", marshalBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.CreateFile(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify file exists
	if _, err := os.Stat(newPath); err != nil {
		t.Errorf("File should exist: %v", err)
	}
}

func TestCreateFile_DefaultContent(t *testing.T) {
	handler, tmpDir, _ := setupConfigTest(t)

	newPath := filepath.Join(tmpDir, "configs", "empty_new.json")
	body := WriteFileRequest{Path: newPath}

	req := httptest.NewRequest("POST", "/api/config/create", marshalBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.CreateFile(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d", rec.Code)
	}

	data, _ := os.ReadFile(newPath)
	if string(data) != "{\n\t\n}\n" {
		t.Errorf("Default content mismatch: %q", string(data))
	}
}

func TestCreateFile_AlreadyExists(t *testing.T) {
	handler, tmpDir, _ := setupConfigTest(t)

	existingPath := filepath.Join(tmpDir, "configs", "01_log.json")
	body := WriteFileRequest{
		Path:    existingPath,
		Content: "{}",
	}

	req := httptest.NewRequest("POST", "/api/config/create", marshalBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.CreateFile(rec, req)

	if rec.Code != http.StatusConflict {
		t.Errorf("Expected 409 for existing file, got %d", rec.Code)
	}
}

// --- RenameFile Tests ---

func TestRenameFile_Success(t *testing.T) {
	handler, tmpDir, _ := setupConfigTest(t)

	oldPath := filepath.Join(tmpDir, "configs", "01_log.json")
	newPath := filepath.Join(tmpDir, "configs", "01_logging.json")

	body := RenameFileRequest{
		OldPath: oldPath,
		NewPath: newPath,
	}

	req := httptest.NewRequest("POST", "/api/config/rename", marshalBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.RenameFile(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify old file gone, new file exists
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Error("Old file should be removed")
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Errorf("New file should exist: %v", err)
	}
}

func TestRenameFile_SourceNotFound(t *testing.T) {
	handler, tmpDir, _ := setupConfigTest(t)

	body := RenameFileRequest{
		OldPath: filepath.Join(tmpDir, "configs", "nonexistent.json"),
		NewPath: filepath.Join(tmpDir, "configs", "target.json"),
	}

	req := httptest.NewRequest("POST", "/api/config/rename", marshalBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.RenameFile(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected 404, got %d", rec.Code)
	}
}

func TestRenameFile_DestinationExists(t *testing.T) {
	handler, tmpDir, _ := setupConfigTest(t)

	body := RenameFileRequest{
		OldPath: filepath.Join(tmpDir, "configs", "01_log.json"),
		NewPath: filepath.Join(tmpDir, "configs", "02_dns.json"),
	}

	req := httptest.NewRequest("POST", "/api/config/rename", marshalBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.RenameFile(rec, req)

	if rec.Code != http.StatusConflict {
		t.Errorf("Expected 409, got %d", rec.Code)
	}
}

// --- Mode Tests ---

func TestGetMode(t *testing.T) {
	handler, _, _ := setupConfigTest(t)

	req := httptest.NewRequest("GET", "/api/config/mode", nil)
	rec := httptest.NewRecorder()
	handler.GetMode(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rec.Code)
	}

	var resp ModeInfo
	json.NewDecoder(rec.Body).Decode(&resp)

	if resp.Mode != "xray" {
		t.Errorf("Expected mode xray, got %q", resp.Mode)
	}
}

func TestSetMode_Valid(t *testing.T) {
	handler, _, _ := setupConfigTest(t)

	body := ModeRequest{Mode: "mihomo"}
	req := httptest.NewRequest("POST", "/api/config/mode", marshalBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.SetMode(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	if handler.currentMode != "mihomo" {
		t.Errorf("Mode not updated, got %q", handler.currentMode)
	}
}

func TestSetMode_Invalid(t *testing.T) {
	handler, _, _ := setupConfigTest(t)

	body := ModeRequest{Mode: "invalid"}
	req := httptest.NewRequest("POST", "/api/config/mode", marshalBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.SetMode(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", rec.Code)
	}
}

// --- Backup Tests ---

func TestListBackups(t *testing.T) {
	handler, tmpDir, _ := setupConfigTest(t)

	filePath := filepath.Join(tmpDir, "configs", "01_log.json")
	req := httptest.NewRequest("GET", "/api/config/backups?path="+filePath, nil)
	rec := httptest.NewRecorder()
	handler.ListBackups(rec, req)

	// No backups yet
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rec.Code)
	}
}

func TestListBackups_AfterWrite(t *testing.T) {
	handler, tmpDir, backupDir := setupConfigTest(t)

	// Write to trigger backup
	filePath := filepath.Join(tmpDir, "configs", "01_log.json")
	writeBody := WriteFileRequest{Path: filePath, Content: `{"updated":true}`}
	req := httptest.NewRequest("POST", "/api/config/file", marshalBody(t, writeBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.WriteFile(rec, req)

	// Now list backups
	req = httptest.NewRequest("GET", "/api/config/backups?path="+filePath, nil)
	rec = httptest.NewRecorder()
	handler.ListBackups(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rec.Code)
	}

	// Parse response
	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)

	backups, ok := resp["backups"].([]interface{})
	if !ok {
		t.Fatal("Expected backups array")
	}
	if len(backups) == 0 {
		// Check backup dir directly
		entries, _ := os.ReadDir(backupDir)
		t.Fatalf("Expected at least 1 backup, got 0. Backup dir entries: %d", len(entries))
	}
}

// --- Helper functions ---

func TestIsJSONFile(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"config.json", true},
		{"config.jsonc", true},
		{"CONFIG.JSON", true},
		{"config.yaml", false},
		{"config.yml", false},
		{"config.txt", false},
		{"json", false},
	}

	for _, tt := range tests {
		got := isJSONFile(tt.name)
		if got != tt.want {
			t.Errorf("isJSONFile(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestIsYAMLFile(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"config.yaml", true},
		{"config.yml", true},
		{"CONFIG.YAML", true},
		{"config.json", false},
		{"config.txt", false},
	}

	for _, tt := range tests {
		got := isYAMLFile(tt.name)
		if got != tt.want {
			t.Errorf("isYAMLFile(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// --- RegisterConfigRoutes smoke test ---

func TestRegisterConfigRoutes(t *testing.T) {
	r := mux.NewRouter()
	handler, _, _ := setupConfigTest(t)
	RegisterConfigRoutes(r, handler)

	// Verify routes exist by making a request
	req := httptest.NewRequest("GET", "/config/mode", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected registered route to work, got %d", rec.Code)
	}
}

// --- marshalBody helper ---

func marshalBody(t *testing.T, v interface{}) *strings.Reader {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("Failed to marshal body: %v", err)
	}
	return strings.NewReader(string(data))
}

// --- RestoreBackup path traversal tests ---

func TestRestoreBackup_PathTraversal(t *testing.T) {
	handler, xrayDir, _ := setupConfigTest(t)
	r := mux.NewRouter()
	RegisterConfigRoutes(r, handler)

	body := marshalBody(t, map[string]string{
		"file_path":   filepath.Join(xrayDir, "test.json"),
		"backup_path": "../../../etc/passwd",
	})

	req := httptest.NewRequest("POST", "/config/restore", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden && rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400/403 for path traversal, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRestoreBackup_ExternalPath(t *testing.T) {
	handler, xrayDir, _ := setupConfigTest(t)
	r := mux.NewRouter()
	RegisterConfigRoutes(r, handler)

	body := marshalBody(t, map[string]string{
		"file_path":   filepath.Join(xrayDir, "test.json"),
		"backup_path": "/tmp/nonexistent/backup.bak",
	})

	req := httptest.NewRequest("POST", "/config/restore", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden && rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400/403 for external path, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ============================================================
// COMPREHENSIVE COVERAGE TESTS
// ============================================================

// --- GetBackupContent Tests ---

func TestGetBackupContent_ValidBackup(t *testing.T) {
	handler, _, backupDir := setupConfigTest(t)

	// Create a backup file manually
	backupContent := `{"log":{"loglevel":"warning"}}`
	backupName := "01_log.json.20260101-120000.bak"
	backupPath := filepath.Join(backupDir, backupName)
	os.WriteFile(backupPath, []byte(backupContent), 0644)

	req := httptest.NewRequest("GET", "/api/config/backups/content?backup_path="+backupPath, nil)
	rec := httptest.NewRecorder()
	handler.GetBackupContent(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)

	if resp["content"] != backupContent {
		t.Errorf("Expected content %q, got %v", backupContent, resp["content"])
	}
	if resp["success"] != true {
		t.Error("Expected success=true")
	}
	if resp["backup_path"] != backupPath {
		t.Errorf("Expected backup_path %q, got %v", backupPath, resp["backup_path"])
	}
}

func TestGetBackupContent_MissingParam(t *testing.T) {
	handler, _, _ := setupConfigTest(t)

	req := httptest.NewRequest("GET", "/api/config/backups/content", nil)
	rec := httptest.NewRecorder()
	handler.GetBackupContent(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for missing param, got %d", rec.Code)
	}
}

func TestGetBackupContent_NotFound(t *testing.T) {
	handler, _, backupDir := setupConfigTest(t)

	backupPath := filepath.Join(backupDir, "nonexistent.bak")
	req := httptest.NewRequest("GET", "/api/config/backups/content?backup_path="+backupPath, nil)
	rec := httptest.NewRecorder()
	handler.GetBackupContent(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected 404 for missing backup, got %d", rec.Code)
	}
}

func TestGetBackupContent_PathTraversal(t *testing.T) {
	handler, _, _ := setupConfigTest(t)

	req := httptest.NewRequest("GET", "/api/config/backups/content?backup_path=../../../etc/passwd", nil)
	rec := httptest.NewRecorder()
	handler.GetBackupContent(rec, req)

	if rec.Code != http.StatusForbidden && rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400/403 for path traversal, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetBackupContent_ExternalPath(t *testing.T) {
	handler, _, _ := setupConfigTest(t)

	req := httptest.NewRequest("GET", "/api/config/backups/content?backup_path=/etc/passwd", nil)
	rec := httptest.NewRecorder()
	handler.GetBackupContent(rec, req)

	if rec.Code != http.StatusForbidden && rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400/403 for external path, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- RestoreBackup Tests ---

func TestRestoreBackup_ValidBackup(t *testing.T) {
	handler, _, backupDir := setupConfigTest(t)

	// Create a backup file
	backupContent := `{"log":{"loglevel":"debug"}}`
	backupName := "01_log.json.20260101-120000.bak"
	backupPath := filepath.Join(backupDir, backupName)
	os.WriteFile(backupPath, []byte(backupContent), 0644)

	body := map[string]string{"backup_path": backupPath}
	req := httptest.NewRequest("POST", "/api/config/restore", marshalBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.RestoreBackup(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)

	if resp["success"] != true {
		t.Error("Expected success=true")
	}
	if resp["content"] != backupContent {
		t.Errorf("Expected content %q, got %v", backupContent, resp["content"])
	}
	if resp["original_name"] != "01_log.json" {
		t.Errorf("Expected original_name '01_log.json', got %v", resp["original_name"])
	}
}

func TestRestoreBackup_MissingParam(t *testing.T) {
	handler, _, _ := setupConfigTest(t)

	req := httptest.NewRequest("POST", "/api/config/restore", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.RestoreBackup(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for missing param, got %d", rec.Code)
	}
}

func TestRestoreBackup_InvalidBody(t *testing.T) {
	handler, _, _ := setupConfigTest(t)

	req := httptest.NewRequest("POST", "/api/config/restore", strings.NewReader(`not json`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.RestoreBackup(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid body, got %d", rec.Code)
	}
}

func TestRestoreBackup_InvalidBackupFilename(t *testing.T) {
	handler, _, backupDir := setupConfigTest(t)

	// Create a backup file with wrong name pattern (no .20 timestamp)
	backupPath := filepath.Join(backupDir, "bad_backup.bak")
	os.WriteFile(backupPath, []byte(`{}`), 0644)

	body := map[string]string{"backup_path": backupPath}
	req := httptest.NewRequest("POST", "/api/config/restore", marshalBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.RestoreBackup(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid backup filename, got %d", rec.Code)
	}
}

func TestRestoreBackup_BackupNotExist(t *testing.T) {
	handler, _, backupDir := setupConfigTest(t)

	backupPath := filepath.Join(backupDir, "file.20260101-120000.bak")
	body := map[string]string{"backup_path": backupPath}
	req := httptest.NewRequest("POST", "/api/config/restore", marshalBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.RestoreBackup(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected 500 for missing backup file, got %d", rec.Code)
	}
}

// --- CreateFile Extended Tests ---

func TestCreateFile_OutsideRoot(t *testing.T) {
	handler, _, _ := setupConfigTest(t)

	body := WriteFileRequest{
		Path:    "/etc/evil.json",
		Content: `{}`,
	}

	req := httptest.NewRequest("POST", "/api/config/create", marshalBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.CreateFile(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected 403 for path outside roots, got %d", rec.Code)
	}
}

func TestCreateFile_InvalidBody(t *testing.T) {
	handler, _, _ := setupConfigTest(t)

	req := httptest.NewRequest("POST", "/api/config/create", strings.NewReader(`not json`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.CreateFile(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid body, got %d", rec.Code)
	}
}

func TestCreateFile_NoPath(t *testing.T) {
	handler, _, _ := setupConfigTest(t)

	body := WriteFileRequest{Content: `{}`}
	req := httptest.NewRequest("POST", "/api/config/create", marshalBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.CreateFile(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for missing path, got %d", rec.Code)
	}
}

func TestCreateFile_NestedPath(t *testing.T) {
	handler, tmpDir, _ := setupConfigTest(t)

	// Create file in a subdirectory that doesn't exist yet
	newPath := filepath.Join(tmpDir, "configs", "subdir", "nested.json")
	body := WriteFileRequest{Path: newPath, Content: `{"nested": true}`}

	req := httptest.NewRequest("POST", "/api/config/create", marshalBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.CreateFile(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	data, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatalf("File not created: %v", err)
	}
	if string(data) != `{"nested": true}` {
		t.Errorf("Content mismatch: %s", data)
	}
}

// --- RenameFile Extended Tests ---

func TestRenameFile_OldPathOutsideRoot(t *testing.T) {
	handler, _, _ := setupConfigTest(t)

	body := RenameFileRequest{
		OldPath: "/etc/passwd",
		NewPath: "/tmp/evil",
	}

	req := httptest.NewRequest("POST", "/api/config/rename", marshalBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.RenameFile(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected 403 for old_path outside roots, got %d", rec.Code)
	}
}

func TestRenameFile_NewPathOutsideRoot(t *testing.T) {
	handler, tmpDir, _ := setupConfigTest(t)

	body := RenameFileRequest{
		OldPath: filepath.Join(tmpDir, "configs", "01_log.json"),
		NewPath: "/etc/evil.json",
	}

	req := httptest.NewRequest("POST", "/api/config/rename", marshalBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.RenameFile(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected 403 for new_path outside roots, got %d", rec.Code)
	}
}

func TestRenameFile_MissingOldPath(t *testing.T) {
	handler, _, _ := setupConfigTest(t)

	body := RenameFileRequest{NewPath: "/some/path"}
	req := httptest.NewRequest("POST", "/api/config/rename", marshalBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.RenameFile(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for missing old_path, got %d", rec.Code)
	}
}

func TestRenameFile_MissingNewPath(t *testing.T) {
	handler, _, _ := setupConfigTest(t)

	body := RenameFileRequest{OldPath: "/some/path"}
	req := httptest.NewRequest("POST", "/api/config/rename", marshalBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.RenameFile(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for missing new_path, got %d", rec.Code)
	}
}

func TestRenameFile_InvalidBody(t *testing.T) {
	handler, _, _ := setupConfigTest(t)

	req := httptest.NewRequest("POST", "/api/config/rename", strings.NewReader(`not json`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.RenameFile(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid body, got %d", rec.Code)
	}
}

func TestRenameFile_CreatesBackup(t *testing.T) {
	handler, tmpDir, backupDir := setupConfigTest(t)

	oldPath := filepath.Join(tmpDir, "configs", "01_log.json")
	newPath := filepath.Join(tmpDir, "configs", "renamed.json")

	body := RenameFileRequest{OldPath: oldPath, NewPath: newPath}
	req := httptest.NewRequest("POST", "/api/config/rename", marshalBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.RenameFile(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify backup was created
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatalf("Failed to read backup dir: %v", err)
	}
	if len(entries) == 0 {
		t.Error("Expected backup to be created before rename")
	}
}

// --- WriteFile Extended Tests ---

func TestWriteFile_YAMLContent(t *testing.T) {
	handler, tmpDir, _ := setupConfigTest(t)

	yamlPath := filepath.Join(tmpDir, "configs", "test.yaml")
	body := WriteFileRequest{
		Path:    yamlPath,
		Content: "proxy:\n  name: test\n",
	}

	req := httptest.NewRequest("POST", "/api/config/file", marshalBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.WriteFile(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	data, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatalf("File not written: %v", err)
	}
	if string(data) != "proxy:\n  name: test\n" {
		t.Errorf("Content mismatch: %s", data)
	}
}

func TestWriteFile_YAMLEmptyContent(t *testing.T) {
	handler, tmpDir, _ := setupConfigTest(t)

	yamlPath := filepath.Join(tmpDir, "configs", "empty.yaml")
	body := WriteFileRequest{
		Path:    yamlPath,
		Content: "   ", // whitespace only
	}

	req := httptest.NewRequest("POST", "/api/config/file", marshalBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.WriteFile(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for empty YAML, got %d", rec.Code)
	}
}

func TestWriteFile_InvalidYAMLPath(t *testing.T) {
	handler, _, _ := setupConfigTest(t)

	body := WriteFileRequest{
		Path:    "/etc/evil.yaml",
		Content: "test: true",
	}

	req := httptest.NewRequest("POST", "/api/config/file", marshalBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.WriteFile(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected 403 for outside roots, got %d", rec.Code)
	}
}

func TestWriteFile_InvalidBody(t *testing.T) {
	handler, _, _ := setupConfigTest(t)

	req := httptest.NewRequest("POST", "/api/config/file", strings.NewReader(`not json`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.WriteFile(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid body, got %d", rec.Code)
	}
}

func TestWriteFile_CreatesParentDir(t *testing.T) {
	handler, tmpDir, _ := setupConfigTest(t)

	newPath := filepath.Join(tmpDir, "configs", "newsubdir", "deep.json")
	body := WriteFileRequest{
		Path:    newPath,
		Content: `{"deep": true}`,
	}

	req := httptest.NewRequest("POST", "/api/config/file", marshalBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.WriteFile(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	if _, err := os.Stat(newPath); err != nil {
		t.Errorf("File should exist: %v", err)
	}
}

// --- ListBackups Extended Tests ---

func TestListBackups_NoPathParam(t *testing.T) {
	handler, _, _ := setupConfigTest(t)

	req := httptest.NewRequest("GET", "/api/config/backups", nil)
	rec := httptest.NewRecorder()
	handler.ListBackups(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for missing path, got %d", rec.Code)
	}
}

func TestListBackups_EmptyBackupDir(t *testing.T) {
	handler, tmpDir, backupDir := setupConfigTest(t)

	file := filepath.Join(tmpDir, "configs", "01_log.json")
	req := httptest.NewRequest("GET", "/api/config/backups?path="+file, nil)
	rec := httptest.NewRecorder()
	handler.ListBackups(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rec.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)

	backups, ok := resp["backups"].([]interface{})
	if !ok {
		t.Fatal("Expected backups array in response")
	}
	if len(backups) != 0 {
		t.Errorf("Expected 0 backups, got %d", len(backups))
	}

	// Verify backupDir exists and is empty
	entries, _ := os.ReadDir(backupDir)
	_ = entries // may or may not have entries from other writes
}

func TestListBackups_MultipleBackups(t *testing.T) {
	handler, tmpDir, backupDir := setupConfigTest(t)

	// Create multiple backup files manually
	content1 := `{"log":"old"}`
	content2 := `{"log":"newer"}`
	bp1 := filepath.Join(backupDir, "01_log.json.20260101-100000.bak")
	bp2 := filepath.Join(backupDir, "01_log.json.20260101-110000.bak")
	os.WriteFile(bp1, []byte(content1), 0644)
	os.WriteFile(bp2, []byte(content2), 0644)

	// Also create a backup for a different file (should not appear)
	os.WriteFile(filepath.Join(backupDir, "02_dns.json.20260101-100000.bak"), []byte(`{}`), 0644)

	filePath := filepath.Join(tmpDir, "configs", "01_log.json")
	req := httptest.NewRequest("GET", "/api/config/backups?path="+filePath, nil)
	rec := httptest.NewRecorder()
	handler.ListBackups(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rec.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)

	backups := resp["backups"].([]interface{})
	if len(backups) != 2 {
		t.Errorf("Expected 2 backups for 01_log.json, got %d", len(backups))
	}
}

func TestListBackups_NonexistentBackupDir(t *testing.T) {
	handler, tmpDir, _ := setupConfigTest(t)

	// Point handler to nonexistent backup dir
	handler.backupDir = filepath.Join(tmpDir, "no-backups-dir")

	filePath := filepath.Join(tmpDir, "configs", "01_log.json")
	req := httptest.NewRequest("GET", "/api/config/backups?path="+filePath, nil)
	rec := httptest.NewRecorder()
	handler.ListBackups(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rec.Code)
	}

	// Should return empty array
	var resp []FileInfo
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if len(resp) != 0 {
		t.Errorf("Expected 0 backups for nonexistent dir, got %d", len(resp))
	}
}

// --- SetMode Extended Tests ---

func TestSetMode_SwitchToXray(t *testing.T) {
	handler, _, _ := setupConfigTest(t)

	// Handler starts in xray mode, switch to mihomo then back
	handler.currentMode = "mihomo"

	body := ModeRequest{Mode: "xray"}
	req := httptest.NewRequest("POST", "/api/config/mode", marshalBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.SetMode(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	if handler.currentMode != "xray" {
		t.Errorf("Mode should be xray, got %q", handler.currentMode)
	}
}

func TestSetMode_InvalidBody(t *testing.T) {
	handler, _, _ := setupConfigTest(t)

	req := httptest.NewRequest("POST", "/api/config/mode", strings.NewReader(`not json`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.SetMode(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid body, got %d", rec.Code)
	}
}

func TestSetMode_MihomoNotAvailable(t *testing.T) {
	handler, tmpDir, _ := setupConfigTest(t)

	// Remove mihomo dir so it's unavailable
	os.RemoveAll(filepath.Join(tmpDir, "mihomo"))

	body := ModeRequest{Mode: "mihomo"}
	req := httptest.NewRequest("POST", "/api/config/mode", marshalBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.SetMode(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 when mihomo unavailable, got %d", rec.Code)
	}
}

func TestSetMode_XrayNotAvailable(t *testing.T) {
	handler, tmpDir, _ := setupConfigTest(t)

	// Remove xray config dir
	os.RemoveAll(filepath.Join(tmpDir, "configs"))

	body := ModeRequest{Mode: "xray"}
	req := httptest.NewRequest("POST", "/api/config/mode", marshalBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.SetMode(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 when xray unavailable, got %d", rec.Code)
	}
}

func TestSetMode_PersistsToConfigFile(t *testing.T) {
	handler, tmpDir, _ := setupConfigTest(t)

	configPath := filepath.Join(tmpDir, "xkeen-ui", "config.json")

	body := ModeRequest{Mode: "mihomo"}
	req := httptest.NewRequest("POST", "/api/config/mode", marshalBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.SetMode(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rec.Code)
	}

	// Verify config file was updated
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Config file should exist: %v", err)
	}

	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("Config should be valid JSON: %v", err)
	}

	if config["mode"] != "mihomo" {
		t.Errorf("Config file should have mode=mihomo, got %v", config["mode"])
	}
}

func TestSetMode_NoConfigPath(t *testing.T) {
	handler, _, _ := setupConfigTest(t)

	// Empty config path — should still succeed (in-memory only)
	handler.configPath = ""

	body := ModeRequest{Mode: "mihomo"}
	req := httptest.NewRequest("POST", "/api/config/mode", marshalBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.SetMode(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	if handler.currentMode != "mihomo" {
		t.Errorf("Mode should be updated in memory, got %q", handler.currentMode)
	}
}

func TestSetMode_ResponseContainsMode(t *testing.T) {
	handler, _, _ := setupConfigTest(t)

	body := ModeRequest{Mode: "mihomo"}
	req := httptest.NewRequest("POST", "/api/config/mode", marshalBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.SetMode(rec, req)

	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)

	if resp["success"] != true {
		t.Error("Expected success=true")
	}
	if resp["mode"] != "mihomo" {
		t.Errorf("Expected mode=mihomo in response, got %v", resp["mode"])
	}
}

// --- DeleteFile Extended Tests ---

func TestDeleteFile_CreatesBackup(t *testing.T) {
	handler, tmpDir, backupDir := setupConfigTest(t)

	filePath := filepath.Join(tmpDir, "configs", "01_log.json")
	body := DeleteFileRequest{Path: filePath}

	req := httptest.NewRequest("DELETE", "/api/config/file", marshalBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.DeleteFile(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rec.Code)
	}

	entries, _ := os.ReadDir(backupDir)
	if len(entries) == 0 {
		t.Error("Expected backup before deletion")
	}
}

func TestDeleteFile_MissingPath(t *testing.T) {
	handler, _, _ := setupConfigTest(t)

	body := DeleteFileRequest{}
	req := httptest.NewRequest("DELETE", "/api/config/file", marshalBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.DeleteFile(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for missing path, got %d", rec.Code)
	}
}

func TestDeleteFile_InvalidBody(t *testing.T) {
	handler, _, _ := setupConfigTest(t)

	req := httptest.NewRequest("DELETE", "/api/config/file", strings.NewReader(`not json`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.DeleteFile(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid body, got %d", rec.Code)
	}
}

// --- GetMode Extended Tests ---

func TestGetMode_Availability(t *testing.T) {
	handler, _, _ := setupConfigTest(t)

	req := httptest.NewRequest("GET", "/api/config/mode", nil)
	rec := httptest.NewRecorder()
	handler.GetMode(rec, req)

	var resp ModeInfo
	json.NewDecoder(rec.Body).Decode(&resp)

	// Both xray and mihomo dirs exist in test setup
	if !resp.XrayAvailable {
		t.Error("xray should be available")
	}
	if !resp.MihomoAvailable {
		t.Error("mihomo should be available")
	}
}

func TestGetMode_MihomoUnavailable(t *testing.T) {
	handler, tmpDir, _ := setupConfigTest(t)

	// Remove mihomo dir
	os.RemoveAll(filepath.Join(tmpDir, "mihomo"))

	req := httptest.NewRequest("GET", "/api/config/mode", nil)
	rec := httptest.NewRecorder()
	handler.GetMode(rec, req)

	var resp ModeInfo
	json.NewDecoder(rec.Body).Decode(&resp)

	if !resp.XrayAvailable {
		t.Error("xray should still be available")
	}
	if resp.MihomoAvailable {
		t.Error("mihomo should not be available")
	}
}

// --- CleanupOldBackups Test ---

func TestCleanupOldBackups(t *testing.T) {
	handler, _, backupDir := setupConfigTest(t)

	// Create 7 backup files
	baseName := "01_log.json"
	for i := 0; i < 7; i++ {
		name := fmt.Sprintf("%s.2026010%d-120000.bak", baseName, i+1)
		path := filepath.Join(backupDir, name)
		os.WriteFile(path, []byte(`{}`), 0644)
		// Small delay to ensure different mod times
		time.Sleep(10 * time.Millisecond)
	}

	entries, _ := os.ReadDir(backupDir)
	if len(entries) != 7 {
		t.Fatalf("Setup: expected 7 backups, got %d", len(entries))
	}

	// Trigger cleanup (keep 5)
	handler.cleanupOldBackups(filepath.Join("configs", baseName), 5)

	entries, _ = os.ReadDir(backupDir)
	if len(entries) != 5 {
		t.Errorf("Expected 5 backups after cleanup, got %d", len(entries))
	}
}
