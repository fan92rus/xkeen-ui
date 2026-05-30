package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
