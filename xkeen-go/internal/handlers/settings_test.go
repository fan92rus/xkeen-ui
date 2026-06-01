package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"github.com/fan92rus/xkeen-ui/internal/config"
)

// ---------------------------------------------------------------------------
// Test setup helpers
// ---------------------------------------------------------------------------

// setupSettingsTest creates a temporary directory structure and SettingsHandler.
// Returns handler, xrayConfigDir, backupDir.
func setupSettingsTest(t *testing.T) (*SettingsHandler, string, string) {
	t.Helper()

	tmpDir := t.TempDir()

	xrayDir := filepath.Join(tmpDir, "xray-configs")
	backupDir := filepath.Join(tmpDir, "backups")
	if err := os.MkdirAll(xrayDir, 0755); err != nil {
		t.Fatalf("failed to create xray dir: %v", err)
	}
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		t.Fatalf("failed to create backup dir: %v", err)
	}

	handler := NewSettingsHandler(
		[]string{tmpDir}, xrayDir, backupDir,
		config.DefaultConfig(),
		filepath.Join(tmpDir, "config.json"),
		nil, // no metrics callback in most tests
	)
	return handler, xrayDir, backupDir
}

// newSettingsRouter creates a mux.Router with settings routes.
func newSettingsRouter(h *SettingsHandler) *mux.Router {
	r := mux.NewRouter()
	RegisterSettingsRoutes(r, h)
	return r
}

// settingsRequest is a helper for making HTTP requests against settings routes.
func settingsRequest(t *testing.T, router *mux.Router, method, path string, body interface{}) *http.Response {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("failed to marshal body: %v", err)
		}
		bodyReader = strings.NewReader(string(data))
	}

	req, err := http.NewRequest(method, path, bodyReader)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec.Result()
}

// parseSettingsResponse parses a JSON response body into a map.
func parseSettingsResponse(t *testing.T, resp *http.Response) map[string]interface{} {
	t.Helper()
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("failed to parse JSON response: %s", string(data))
	}
	return result
}

// writeLogConfig writes a log config file to the xray config directory.
func writeLogConfig(t *testing.T, xrayDir string, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(xrayDir, "01_log.json"), []byte(content), 0644); err != nil {
		t.Fatalf("failed to write log config: %v", err)
	}
}

// readLogConfig reads the log config file from xray config directory.
func readLogConfig(t *testing.T, xrayDir string) XrayLogConfigFile {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(xrayDir, "01_log.json"))
	if err != nil {
		t.Fatalf("failed to read log config: %v", err)
	}
	var config XrayLogConfigFile
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("failed to parse log config: %v", err)
	}
	return config
}

// ---------------------------------------------------------------------------
// NewSettingsHandler tests
// ---------------------------------------------------------------------------

func TestNewSettingsHandler_ValidRoots(t *testing.T) {
	tmpDir := t.TempDir()
	handler := NewSettingsHandler(
		[]string{tmpDir},
		filepath.Join(tmpDir, "xray"),
		filepath.Join(tmpDir, "bak"),
		config.DefaultConfig(),
		filepath.Join(tmpDir, "config.json"),
		nil,
	)

	if handler == nil {
		t.Fatal("expected non-nil handler")
	}
	if handler.validator == nil {
		t.Error("expected path validator to be created")
	}
	if handler.logConfigPath != filepath.Join(tmpDir, "xray", "01_log.json") {
		t.Errorf("logConfigPath = %q, unexpected", handler.logConfigPath)
	}
}

func TestNewSettingsHandler_EmptyRoots(t *testing.T) {
	handler := NewSettingsHandler([]string{}, "/opt/xray", "/opt/backups", config.DefaultConfig(), "/opt/config.json", nil)
	if handler == nil {
		t.Fatal("expected non-nil handler even with empty roots")
	}
}

// ---------------------------------------------------------------------------
// GetXraySettings tests
// ---------------------------------------------------------------------------

func TestGetXraySettings_DefaultsWhenNoConfigFile(t *testing.T) {
	handler, _, _ := setupSettingsTest(t)
	router := newSettingsRouter(handler)

	resp := settingsRequest(t, router, "GET", "/xray/settings", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	result := parseSettingsResponse(t, resp)

	if result["log_level"] != "none" {
		t.Errorf("expected default log_level 'none', got %v", result["log_level"])
	}

	levels, ok := result["log_levels"].([]interface{})
	if !ok || len(levels) != 5 {
		t.Errorf("expected 5 log levels, got %v", result["log_levels"])
	}

	if result["access_log"] != "/opt/var/log/xray/access.log" {
		t.Errorf("expected default access_log path, got %v", result["access_log"])
	}
	if result["error_log"] != "/opt/var/log/xray/error.log" {
		t.Errorf("expected default error_log path, got %v", result["error_log"])
	}
}

func TestGetXraySettings_ReadsExistingConfig(t *testing.T) {
	handler, xrayDir, _ := setupSettingsTest(t)
	router := newSettingsRouter(handler)

	writeLogConfig(t, xrayDir, `{"log":{"loglevel":"debug","access":"/var/log/access.log","error":"/var/log/error.log"}}`)

	resp := settingsRequest(t, router, "GET", "/xray/settings", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	result := parseSettingsResponse(t, resp)
	if result["log_level"] != "debug" {
		t.Errorf("expected log_level 'debug', got %v", result["log_level"])
	}
	if result["access_log"] != "/var/log/access.log" {
		t.Errorf("expected access_log '/var/log/access.log', got %v", result["access_log"])
	}
	if result["error_log"] != "/var/log/error.log" {
		t.Errorf("expected error_log '/var/log/error.log', got %v", result["error_log"])
	}
}

func TestGetXraySettings_PreservesEmptyLogLevelAsDefault(t *testing.T) {
	handler, xrayDir, _ := setupSettingsTest(t)
	router := newSettingsRouter(handler)

	writeLogConfig(t, xrayDir, `{"log":{"access":"/tmp/a.log"}}`)

	resp := settingsRequest(t, router, "GET", "/xray/settings", nil)
	result := parseSettingsResponse(t, resp)

	if result["log_level"] != "none" {
		t.Errorf("expected default 'none' when loglevel empty, got %v", result["log_level"])
	}
}

func TestGetXraySettings_MalformedJSON(t *testing.T) {
	handler, xrayDir, _ := setupSettingsTest(t)
	router := newSettingsRouter(handler)

	writeLogConfig(t, xrayDir, `{this is not valid json}`)

	resp := settingsRequest(t, router, "GET", "/xray/settings", nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed JSON, got %d", resp.StatusCode)
	}
}

func TestGetXraySettings_JSONCWithComments(t *testing.T) {
	handler, xrayDir, _ := setupSettingsTest(t)
	router := newSettingsRouter(handler)

	writeLogConfig(t, xrayDir, `{
		// Log configuration
		"log": {
			"loglevel": "info"
		}
	}`)

	resp := settingsRequest(t, router, "GET", "/xray/settings", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	result := parseSettingsResponse(t, resp)
	if result["log_level"] != "info" {
		t.Errorf("expected log_level 'info', got %v", result["log_level"])
	}
}

func TestGetXraySettings_InvalidJSONAfterJSONCStrip(t *testing.T) {
	handler, xrayDir, _ := setupSettingsTest(t)
	router := newSettingsRouter(handler)

	writeLogConfig(t, xrayDir, `// just a comment
[not an object]`)

	resp := settingsRequest(t, router, "GET", "/xray/settings", nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// UpdateLogLevel tests
// ---------------------------------------------------------------------------

func TestUpdateLogLevel_AllValidLevels(t *testing.T) {
	for _, level := range []string{"debug", "info", "warning", "error", "none"} {
		t.Run(level, func(t *testing.T) {
			handler, xrayDir, _ := setupSettingsTest(t)
			router := newSettingsRouter(handler)

			body := map[string]string{"log_level": level}
			resp := settingsRequest(t, router, "POST", "/xray/settings/log-level", body)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("expected 200 for level %q, got %d", level, resp.StatusCode)
			}

			config := readLogConfig(t, xrayDir)
			if config.Log.LogLevel != level {
				t.Errorf("got level %q, want %q", config.Log.LogLevel, level)
			}
		})
	}
}

func TestUpdateLogLevel_PreservesExistingPaths(t *testing.T) {
	handler, xrayDir, _ := setupSettingsTest(t)
	router := newSettingsRouter(handler)

	writeLogConfig(t, xrayDir, `{"log":{"loglevel":"warning","access":"/custom/access.log","error":"/custom/error.log"}}`)

	body := map[string]string{"log_level": "none"}
	resp := settingsRequest(t, router, "POST", "/xray/settings/log-level", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	config := readLogConfig(t, xrayDir)
	if config.Log.Access != "/custom/access.log" {
		t.Errorf("access path not preserved: %q", config.Log.Access)
	}
	if config.Log.Error != "/custom/error.log" {
		t.Errorf("error path not preserved: %q", config.Log.Error)
	}
	if config.Log.LogLevel != "none" {
		t.Errorf("log level not updated: %q", config.Log.LogLevel)
	}
}

func TestUpdateLogLevel_InvalidLevel(t *testing.T) {
	handler, _, _ := setupSettingsTest(t)
	router := newSettingsRouter(handler)

	body := map[string]string{"log_level": "verbose"}
	resp := settingsRequest(t, router, "POST", "/xray/settings/log-level", body)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid level, got %d", resp.StatusCode)
	}

	result := parseSettingsResponse(t, resp)
	if _, hasErr := result["error"]; !hasErr {
		t.Error("expected error message")
	}
}

func TestUpdateLogLevel_EmptyLevel(t *testing.T) {
	handler, _, _ := setupSettingsTest(t)
	router := newSettingsRouter(handler)

	body := map[string]string{"log_level": ""}
	resp := settingsRequest(t, router, "POST", "/xray/settings/log-level", body)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty level, got %d", resp.StatusCode)
	}
}

func TestUpdateLogLevel_InvalidRequestBody(t *testing.T) {
	handler, _, _ := setupSettingsTest(t)
	router := newSettingsRouter(handler)

	req, _ := http.NewRequest("POST", "/xray/settings/log-level", strings.NewReader(`not json`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestUpdateLogLevel_CreatesFileIfNotExists(t *testing.T) {
	handler, xrayDir, _ := setupSettingsTest(t)
	router := newSettingsRouter(handler)

	configPath := filepath.Join(xrayDir, "01_log.json")
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatal("config file should not exist initially")
	}

	body := map[string]string{"log_level": "info"}
	resp := settingsRequest(t, router, "POST", "/xray/settings/log-level", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatal("config file should have been created")
	}

	config := readLogConfig(t, xrayDir)
	if config.Log.LogLevel != "info" {
		t.Errorf("got level %q, want 'info'", config.Log.LogLevel)
	}
	if config.Log.Access != "/opt/var/log/xray/access.log" {
		t.Errorf("expected default access path, got %q", config.Log.Access)
	}
	if config.Log.Error != "/opt/var/log/xray/error.log" {
		t.Errorf("expected default error path, got %q", config.Log.Error)
	}
}

func TestUpdateLogLevel_CreatesBackupBeforeOverwrite(t *testing.T) {
	handler, xrayDir, backupDir := setupSettingsTest(t)
	router := newSettingsRouter(handler)

	originalContent := `{"log":{"loglevel":"warning"}}`
	writeLogConfig(t, xrayDir, originalContent)

	body := map[string]string{"log_level": "debug"}
	resp := settingsRequest(t, router, "POST", "/xray/settings/log-level", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	entries, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatalf("failed to read backup dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected backup file to be created")
	}

	backupData, _ := os.ReadFile(filepath.Join(backupDir, entries[0].Name()))
	if string(backupData) != originalContent {
		t.Errorf("backup content = %q, want %q", string(backupData), originalContent)
	}
}

func TestUpdateLogLevel_NoBackupWhenNoExistingFile(t *testing.T) {
	handler, _, backupDir := setupSettingsTest(t)
	router := newSettingsRouter(handler)

	body := map[string]string{"log_level": "info"}
	resp := settingsRequest(t, router, "POST", "/xray/settings/log-level", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	entries, _ := os.ReadDir(backupDir)
	if len(entries) != 0 {
		t.Errorf("expected no backups when no existing file, got %d", len(entries))
	}
}

func TestUpdateLogLevel_ResponseBody(t *testing.T) {
	handler, _, _ := setupSettingsTest(t)
	router := newSettingsRouter(handler)

	body := map[string]string{"log_level": "debug"}
	resp := settingsRequest(t, router, "POST", "/xray/settings/log-level", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	result := parseSettingsResponse(t, resp)
	if result["success"] != true {
		t.Errorf("expected success=true, got %v", result["success"])
	}
	if result["log_level"] != "debug" {
		t.Errorf("expected log_level 'debug', got %v", result["log_level"])
	}
	if msg, ok := result["message"].(string); !ok || !strings.Contains(msg, "debug") {
		t.Errorf("expected message containing 'debug', got %v", result["message"])
	}
}

// ---------------------------------------------------------------------------
// ListBackupsForLogConfig tests
// ---------------------------------------------------------------------------

func TestListBackups_EmptyWhenNoBackups(t *testing.T) {
	handler, _, _ := setupSettingsTest(t)
	router := newSettingsRouter(handler)

	resp := settingsRequest(t, router, "GET", "/xray/settings/backups", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	result := parseSettingsResponse(t, resp)
	backups, ok := result["backups"].([]interface{})
	if !ok {
		t.Fatalf("expected backups array, got %T", result["backups"])
	}
	if len(backups) != 0 {
		t.Errorf("expected 0 backups, got %d", len(backups))
	}
}

func TestListBackups_ReturnsExistingBackups(t *testing.T) {
	handler, xrayDir, _ := setupSettingsTest(t)
	router := newSettingsRouter(handler)

	configPath := filepath.Join(xrayDir, "01_log.json")

	// First config + backup
	writeLogConfig(t, xrayDir, `{"log":{"loglevel":"warning"}}`)
	os.Chtimes(configPath, time.Date(2025, 1, 1, 10, 0, 0, 0, time.Local), time.Date(2025, 1, 1, 10, 0, 0, 0, time.Local))
	settingsRequest(t, router, "POST", "/xray/settings/log-level", map[string]string{"log_level": "debug"})

	// Second config + backup (force different mtime so backup name differs)
	os.Chtimes(configPath, time.Date(2025, 1, 1, 11, 0, 0, 0, time.Local), time.Date(2025, 1, 1, 11, 0, 0, 0, time.Local))
	settingsRequest(t, router, "POST", "/xray/settings/log-level", map[string]string{"log_level": "info"})

	resp := settingsRequest(t, router, "GET", "/xray/settings/backups", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	result := parseSettingsResponse(t, resp)
	backups, ok := result["backups"].([]interface{})
	if !ok {
		t.Fatalf("expected backups array, got %T", result["backups"])
	}
	if len(backups) != 2 {
		t.Errorf("expected 2 backups, got %d", len(backups))
	}
}

func TestListBackups_SortedNewestFirst(t *testing.T) {
	handler, xrayDir, _ := setupSettingsTest(t)
	router := newSettingsRouter(handler)

	configPath := filepath.Join(xrayDir, "01_log.json")

	writeLogConfig(t, xrayDir, `{"log":{"loglevel":"warning"}}`)
	os.Chtimes(configPath, time.Date(2025, 1, 1, 10, 0, 0, 0, time.Local), time.Date(2025, 1, 1, 10, 0, 0, 0, time.Local))
	settingsRequest(t, router, "POST", "/xray/settings/log-level", map[string]string{"log_level": "debug"})

	os.Chtimes(configPath, time.Date(2025, 1, 1, 11, 0, 0, 0, time.Local), time.Date(2025, 1, 1, 11, 0, 0, 0, time.Local))
	settingsRequest(t, router, "POST", "/xray/settings/log-level", map[string]string{"log_level": "info"})

	resp := settingsRequest(t, router, "GET", "/xray/settings/backups", nil)
	result := parseSettingsResponse(t, resp)
	backups := result["backups"].([]interface{})

	if len(backups) < 2 {
		t.Fatalf("need at least 2 backups, got %d", len(backups))
	}

	b1 := backups[0].(map[string]interface{})
	b2 := backups[1].(map[string]interface{})
	if b1["modified"].(float64) < b2["modified"].(float64) {
		t.Error("backups should be sorted newest first")
	}
}

func TestListBackups_IgnoresNonMatchingFiles(t *testing.T) {
	handler, _, backupDir := setupSettingsTest(t)
	router := newSettingsRouter(handler)

	os.WriteFile(filepath.Join(backupDir, "other_file.bak"), []byte("data"), 0644)
	os.WriteFile(filepath.Join(backupDir, "01_log.json.readme"), []byte("data"), 0644)

	resp := settingsRequest(t, router, "GET", "/xray/settings/backups", nil)
	result := parseSettingsResponse(t, resp)
	backups := result["backups"].([]interface{})

	if len(backups) != 0 {
		t.Errorf("expected 0 backups for non-matching files, got %d", len(backups))
	}
}

func TestListBackups_BackupHasCorrectFields(t *testing.T) {
	handler, xrayDir, _ := setupSettingsTest(t)
	router := newSettingsRouter(handler)

	writeLogConfig(t, xrayDir, `{"log":{"loglevel":"warning"}}`)
	settingsRequest(t, router, "POST", "/xray/settings/log-level", map[string]string{"log_level": "debug"})

	resp := settingsRequest(t, router, "GET", "/xray/settings/backups", nil)
	result := parseSettingsResponse(t, resp)
	backups := result["backups"].([]interface{})

	if len(backups) == 0 {
		t.Fatal("expected at least 1 backup")
	}

	backup := backups[0].(map[string]interface{})
	for _, field := range []string{"name", "path", "size", "modified", "is_dir"} {
		if _, ok := backup[field]; !ok {
			t.Errorf("backup missing field %q", field)
		}
	}

	if backup["is_dir"].(bool) {
		t.Error("backup should not be a directory")
	}
	if backup["size"].(float64) == 0 {
		t.Error("backup size should be > 0")
	}

	name := backup["name"].(string)
	if !strings.HasPrefix(name, "01_log.json.") || !strings.HasSuffix(name, ".bak") {
		t.Errorf("backup name %q doesn't match expected pattern", name)
	}
}

func TestListBackups_IncludesFilePath(t *testing.T) {
	handler, xrayDir, _ := setupSettingsTest(t)
	router := newSettingsRouter(handler)

	resp := settingsRequest(t, router, "GET", "/xray/settings/backups", nil)
	result := parseSettingsResponse(t, resp)

	expectedFile := filepath.Join(xrayDir, "01_log.json")
	if result["file"] != expectedFile {
		t.Errorf("expected file=%q, got %v", expectedFile, result["file"])
	}
}

func TestListBackups_WithMixedFiles(t *testing.T) {
	handler, xrayDir, backupDir := setupSettingsTest(t)
	router := newSettingsRouter(handler)

	// Create one valid backup by updating log level
	writeLogConfig(t, xrayDir, `{"log":{"loglevel":"warning"}}`)
	settingsRequest(t, router, "POST", "/xray/settings/log-level", map[string]string{"log_level": "debug"})

	// Create unrelated files
	os.WriteFile(filepath.Join(backupDir, "random.txt"), []byte("data"), 0644)
	os.WriteFile(filepath.Join(backupDir, "other.json.bak"), []byte("data"), 0644)

	resp := settingsRequest(t, router, "GET", "/xray/settings/backups", nil)
	result := parseSettingsResponse(t, resp)
	backups := result["backups"].([]interface{})

	if len(backups) != 1 {
		t.Errorf("expected exactly 1 matching backup, got %d", len(backups))
	}
}

// ---------------------------------------------------------------------------
// createBackup tests
// ---------------------------------------------------------------------------

func TestCreateBackup_Success(t *testing.T) {
	handler, xrayDir, _ := setupSettingsTest(t)

	content := `{"log":{"loglevel":"warning"}}`
	writeLogConfig(t, xrayDir, content)

	backupPath, err := handler.createBackup(filepath.Join(xrayDir, "01_log.json"))
	if err != nil {
		t.Fatalf("createBackup failed: %v", err)
	}
	if backupPath == "" {
		t.Fatal("expected backup path")
	}

	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		t.Fatal("backup file not created")
	}

	data, _ := os.ReadFile(backupPath)
	if string(data) != content {
		t.Errorf("backup content = %q, want %q", string(data), content)
	}

	name := filepath.Base(backupPath)
	if !strings.HasPrefix(name, "01_log.json.") || !strings.HasSuffix(name, ".bak") {
		t.Errorf("backup name %q doesn't match pattern", name)
	}
}

func TestCreateBackup_FileNotExist(t *testing.T) {
	handler, xrayDir, _ := setupSettingsTest(t)

	backupPath, err := handler.createBackup(filepath.Join(xrayDir, "nonexistent.json"))
	if err != nil {
		t.Fatalf("expected nil error for nonexistent file, got %v", err)
	}
	if backupPath != "" {
		t.Errorf("expected empty path for nonexistent file, got %q", backupPath)
	}
}

func TestCreateBackup_CreatesBackupDir(t *testing.T) {
	handler, xrayDir, _ := setupSettingsTest(t)

	newBackupDir := filepath.Join(t.TempDir(), "new-backups")
	handler.backupDir = newBackupDir

	writeLogConfig(t, xrayDir, `{"log":{"loglevel":"info"}}`)

	backupPath, err := handler.createBackup(filepath.Join(xrayDir, "01_log.json"))
	if err != nil {
		t.Fatalf("createBackup failed: %v", err)
	}

	if _, err := os.Stat(newBackupDir); os.IsNotExist(err) {
		t.Fatal("backup directory should have been created")
	}
	if backupPath == "" {
		t.Fatal("expected backup path")
	}
}

func TestCreateBackup_MultipleBackupsDifferentTimestamps(t *testing.T) {
	handler, xrayDir, _ := setupSettingsTest(t)

	writeLogConfig(t, xrayDir, `{"log":{"loglevel":"warning"}}`)

	configPath := filepath.Join(xrayDir, "01_log.json")
	modTime1 := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	os.Chtimes(configPath, modTime1, modTime1)

	backupPath1, err := handler.createBackup(configPath)
	if err != nil {
		t.Fatalf("first backup failed: %v", err)
	}

	writeLogConfig(t, xrayDir, `{"log":{"loglevel":"debug"}}`)
	modTime2 := time.Date(2025, 1, 2, 12, 0, 0, 0, time.UTC)
	os.Chtimes(configPath, modTime2, modTime2)

	backupPath2, err := handler.createBackup(configPath)
	if err != nil {
		t.Fatalf("second backup failed: %v", err)
	}

	if backupPath1 == backupPath2 {
		t.Error("two backups should have different names")
	}
}

// ---------------------------------------------------------------------------
// getFileModTime tests
// ---------------------------------------------------------------------------

func TestGetFileModTime_ExistingFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.json")
	os.WriteFile(path, []byte("{}"), 0644)

	result := getFileModTime(path)
	if result == "" || result == "unknown" {
		t.Errorf("expected formatted timestamp, got %q", result)
	}
	if len(result) != 15 {
		t.Errorf("expected YYYYMMDD-HHMMSS (15 chars), got %q (%d chars)", result, len(result))
	}
}

func TestGetFileModTime_NonexistentFile(t *testing.T) {
	result := getFileModTime("/nonexistent/path/file.json")
	if result != "unknown" {
		t.Errorf("expected 'unknown', got %q", result)
	}
}

func TestGetFileModTime_UsesModTime(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "timed.json")
	os.WriteFile(path, []byte("{}"), 0644)

	localTime := time.Date(2025, 6, 15, 10, 30, 45, 0, time.Local)
	os.Chtimes(path, localTime, localTime)

	expected := localTime.Format("20060102-150405")
	result := getFileModTime(path)
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

// ---------------------------------------------------------------------------
// RegisterSettingsRoutes smoke test
// ---------------------------------------------------------------------------

func TestRegisterSettingsRoutes(t *testing.T) {
	r := mux.NewRouter()
	handler, _, _ := setupSettingsTest(t)
	RegisterSettingsRoutes(r, handler)

	req := httptest.NewRequest("GET", "/xray/settings", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected registered route to work, got %d", rec.Code)
	}
}

func TestRegisterSettingsRoutes_RouteMethods(t *testing.T) {
	r := mux.NewRouter()
	handler, _, _ := setupSettingsTest(t)
	RegisterSettingsRoutes(r, handler)

	tests := []struct {
		name   string
		method string
		path   string
		want   int
	}{
		{"GET settings", "GET", "/xray/settings", 200},
		{"POST log-level no body", "POST", "/xray/settings/log-level", 400},
		{"GET backups", "GET", "/xray/settings/backups", 200},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			if rec.Code != tt.want {
				t.Errorf("expected %d, got %d", tt.want, rec.Code)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Integration: update then read roundtrip
// ---------------------------------------------------------------------------

func TestUpdateThenRead_Roundtrip(t *testing.T) {
	handler, _, _ := setupSettingsTest(t)
	router := newSettingsRouter(handler)

	// No config initially → defaults
	resp := settingsRequest(t, router, "GET", "/xray/settings", nil)
	result := parseSettingsResponse(t, resp)
	if result["log_level"] != "none" {
		t.Errorf("expected default 'none', got %v", result["log_level"])
	}

	// Update to debug
	settingsRequest(t, router, "POST", "/xray/settings/log-level", map[string]string{"log_level": "debug"})

	// Read back
	resp = settingsRequest(t, router, "GET", "/xray/settings", nil)
	result = parseSettingsResponse(t, resp)
	if result["log_level"] != "debug" {
		t.Errorf("expected 'debug' after update, got %v", result["log_level"])
	}
}

func TestUpdateThenRead_PreservesCustomPaths(t *testing.T) {
	handler, xrayDir, _ := setupSettingsTest(t)
	router := newSettingsRouter(handler)

	writeLogConfig(t, xrayDir, `{"log":{"loglevel":"warning","access":"/custom/a.log","error":"/custom/e.log"}}`)

	settingsRequest(t, router, "POST", "/xray/settings/log-level", map[string]string{"log_level": "none"})

	resp := settingsRequest(t, router, "GET", "/xray/settings", nil)
	result := parseSettingsResponse(t, resp)

	if result["access_log"] != "/custom/a.log" {
		t.Errorf("access_log = %v, want /custom/a.log", result["access_log"])
	}
	if result["error_log"] != "/custom/e.log" {
		t.Errorf("error_log = %v, want /custom/e.log", result["error_log"])
	}
	if result["log_level"] != "none" {
		t.Errorf("log_level = %v, want none", result["log_level"])
	}
}

// ---------------------------------------------------------------------------
// Edge cases
// ---------------------------------------------------------------------------

func TestUpdateLogLevel_JSONCExistingConfig(t *testing.T) {
	handler, xrayDir, _ := setupSettingsTest(t)
	router := newSettingsRouter(handler)

	writeLogConfig(t, xrayDir, `{
		// Comments here
		"log": {
			"loglevel": "info",
			/* block comment */
			"access": "/custom/access.log"
		}
	}`)

	body := map[string]string{"log_level": "warning"}
	resp := settingsRequest(t, router, "POST", "/xray/settings/log-level", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	config := readLogConfig(t, xrayDir)
	if config.Log.Access != "/custom/access.log" {
		t.Errorf("access path not preserved from JSONC: %q", config.Log.Access)
	}
	if config.Log.LogLevel != "warning" {
		t.Errorf("log level not updated: %q", config.Log.LogLevel)
	}
}

func TestUpdateLogLevel_BackupContainsPreviousContent(t *testing.T) {
	handler, xrayDir, backupDir := setupSettingsTest(t)
	router := newSettingsRouter(handler)

	initialContent := `{"log":{"loglevel":"warning","access":"/test/access.log"}}`
	writeLogConfig(t, xrayDir, initialContent)

	settingsRequest(t, router, "POST", "/xray/settings/log-level", map[string]string{"log_level": "error"})

	entries, _ := os.ReadDir(backupDir)
	if len(entries) == 0 {
		t.Fatal("no backups created")
	}

	sort.Slice(entries, func(i, j int) bool {
		infoI, _ := entries[i].Info()
		infoJ, _ := entries[j].Info()
		return infoI.ModTime().After(infoJ.ModTime())
	})

	backupData, _ := os.ReadFile(filepath.Join(backupDir, entries[0].Name()))
	if string(backupData) != initialContent {
		t.Errorf("backup = %q, want %q", string(backupData), initialContent)
	}
}

func TestUpdateLogLevel_WrittenFileHasTrailingNewline(t *testing.T) {
	handler, xrayDir, _ := setupSettingsTest(t)
	router := newSettingsRouter(handler)

	settingsRequest(t, router, "POST", "/xray/settings/log-level", map[string]string{"log_level": "info"})

	data, _ := os.ReadFile(filepath.Join(xrayDir, "01_log.json"))
	if len(data) == 0 || data[len(data)-1] != '\n' {
		t.Error("config file should have trailing newline")
	}
}

func TestUpdateLogLevel_WrittenFileIsValidJSON(t *testing.T) {
	handler, xrayDir, _ := setupSettingsTest(t)
	router := newSettingsRouter(handler)

	settingsRequest(t, router, "POST", "/xray/settings/log-level", map[string]string{"log_level": "warning"})

	data, _ := os.ReadFile(filepath.Join(xrayDir, "01_log.json"))
	if !json.Valid(data) {
		t.Errorf("written file is not valid JSON: %s", string(data))
	}
}
