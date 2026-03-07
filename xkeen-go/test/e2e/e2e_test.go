// Package e2e provides end-to-end tests for XKEEN-GO API.
package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/user/xkeen-go/internal/config"
	"github.com/user/xkeen-go/internal/server"
)

const testPort = 18089
const baseURL = "http://localhost:18089"

var testDir string
var configsDir string
var logsDir string
var backupDir string
var testServer *server.Server

// mockWebFS implements fs.FS for testing
type mockWebFS struct{}

func (m *mockWebFS) Open(name string) (fs.File, error) {
	// Return minimal HTML files for testing
	var content string
	switch name {
	case "login.html":
		content = `<!DOCTYPE html><html><body><h1>Login</h1></body></html>`
	case "index.html":
		content = `<!DOCTYPE html><html><body><h1>Dashboard</h1></body></html>`
	case "static/app.js":
		content = `console.log("test");`
	default:
		return nil, os.ErrNotExist
	}
	return &mockFile{content: content}, nil
}

type mockFile struct {
	content string
	offset  int
}

func (m *mockFile) Stat() (fs.FileInfo, error) { return nil, nil }
func (m *mockFile) Close() error               { return nil }
func (m *mockFile) Read(b []byte) (int, error) {
	if m.offset >= len(m.content) {
		return 0, io.EOF
	}
	n := copy(b, m.content[m.offset:])
	m.offset += n
	return n, nil
}

// TestMain sets up the test server and runs all tests
func TestMain(m *testing.M) {
	// Create temp directories
	var err error
	testDir, err = os.MkdirTemp("", "xkeen-go-e2e-*")
	if err != nil {
		fmt.Printf("Failed to create temp dir: %v\n", err)
		os.Exit(1)
	}

	configsDir = filepath.Join(testDir, "configs")
	logsDir = filepath.Join(testDir, "logs")
	backupDir = filepath.Join(testDir, "backups")

	for _, dir := range []string{configsDir, logsDir, backupDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			fmt.Printf("Failed to create dir %s: %v\n", dir, err)
			os.Exit(1)
		}
	}

	// Create test config file
	testConfig := `{
		// Test config with JSONC comments
		"log": {"loglevel": "debug"},
		"inbounds": [{"port": 1080, "protocol": "socks"}],
		"outbounds": [{"protocol": "freedom"}]
	}`
	if err := os.WriteFile(filepath.Join(configsDir, "test.json"), []byte(testConfig), 0644); err != nil {
		fmt.Printf("Failed to create test config: %v\n", err)
		os.Exit(1)
	}

	// Create test log file
	testLog := `2024-03-04 12:00:01 INFO Starting xray core
2024-03-04 12:00:02 INFO Listening on 0.0.0.0:1080
2024-03-04 12:00:05 DEBUG Connection from 192.168.1.100
2024-03-04 12:00:15 WARN Rate limit exceeded
2024-03-04 12:00:20 ERROR Connection timeout
`
	if err := os.WriteFile(filepath.Join(logsDir, "access.log"), []byte(testLog), 0644); err != nil {
		fmt.Printf("Failed to create test log: %v\n", err)
		os.Exit(1)
	}

	// Create config
	cfg := config.DefaultConfig()
	cfg.Port = testPort
	cfg.XrayConfigDir = configsDir
	cfg.AllowedRoots = []string{testDir}
	cfg.SessionSecret = "test-secret-key-for-e2e-testing"
	cfg.Auth.Username = "admin"
	cfg.Auth.PasswordHash = "" // Will use default "admin"

	// Create server with mock web FS
	var webFS fs.FS = &mockWebFS{}
	testServer, err = server.NewServer(cfg, webFS)
	if err != nil {
		fmt.Printf("Failed to create server: %v\n", err)
		os.Exit(1)
	}

	// Start server in goroutine
	go func() {
		if err := testServer.Start(); err != nil {
			fmt.Printf("Server error: %v\n", err)
		}
	}()

	// Wait for server to start
	time.Sleep(500 * time.Millisecond)

	// Run tests
	code := m.Run()

	// Cleanup
	testServer.Stop()
	os.RemoveAll(testDir)

	os.Exit(code)
}

// APIClient helps with API requests
type APIClient struct {
	client    *http.Client
	baseURL   string
	csrfToken string
	t         *testing.T
}

func NewAPIClient(t *testing.T) *APIClient {
	jar, _ := cookiejar.New(nil)
	return &APIClient{
		client:  &http.Client{Jar: jar, Timeout: 10 * time.Second},
		baseURL: baseURL,
		t:       t,
	}
}

func (c *APIClient) Login(username, password string) map[string]interface{} {
	return c.Post("/api/auth/login", map[string]string{
		"username": username,
		"password": password,
	}, false)
}

func (c *APIClient) Get(path string) map[string]interface{} {
	resp, err := c.client.Get(c.baseURL + path)
	if err != nil {
		c.t.Fatalf("GET %s failed: %v", path, err)
	}
	defer resp.Body.Close()
	return c.parseResponse(resp)
}

func (c *APIClient) Post(path string, data interface{}, needCSRF bool) map[string]interface{} {
	var body []byte
	if data != nil {
		body, _ = json.Marshal(data)
	} else {
		body = []byte("{}")
	}

	req, err := http.NewRequest("POST", c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		c.t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if needCSRF && c.csrfToken != "" {
		req.Header.Set("X-CSRF-Token", c.csrfToken)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		c.t.Fatalf("POST %s failed: %v", path, err)
	}
	defer resp.Body.Close()
	return c.parseResponse(resp)
}

func (c *APIClient) Delete(path string, data interface{}) map[string]interface{} {
	var body []byte
	if data != nil {
		body, _ = json.Marshal(data)
	} else {
		body = []byte("{}")
	}

	req, err := http.NewRequest("DELETE", c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		c.t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.csrfToken != "" {
		req.Header.Set("X-CSRF-Token", c.csrfToken)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		c.t.Fatalf("DELETE %s failed: %v", path, err)
	}
	defer resp.Body.Close()
	return c.parseResponse(resp)
}

func (c *APIClient) parseResponse(resp *http.Response) map[string]interface{} {
	body, _ := io.ReadAll(resp.Body)

	// Handle non-JSON responses
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		return map[string]interface{}{
			"status": resp.StatusCode,
			"body":   string(body),
		}
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		c.t.Logf("Response body: %s", string(body))
		c.t.Fatalf("Failed to parse JSON response: %v", err)
	}

	// Extract CSRF token if present
	if token, ok := result["csrf_token"].(string); ok {
		c.csrfToken = token
	}

	return result
}

// ============== TESTS ==============

func TestHealthCheck(t *testing.T) {
	client := NewAPIClient(t)

	resp, err := client.client.Get(baseURL + "/health")
	if err != nil {
		t.Fatalf("Failed to connect to server: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}

func TestAuth_Login(t *testing.T) {
	client := NewAPIClient(t)

	// Test successful login
	resp := client.Login("admin", "admin")
	if !resp["ok"].(bool) {
		t.Errorf("Login failed: %v", resp)
	}

	if client.csrfToken == "" {
		t.Error("CSRF token not received")
	}

	t.Logf("Login successful, CSRF token: %s...", client.csrfToken[:min(20, len(client.csrfToken))])
}

func TestAuth_LoginInvalid(t *testing.T) {
	client := NewAPIClient(t)

	resp := client.Login("admin", "wrongpassword")
	if resp["ok"].(bool) {
		t.Error("Login should fail with wrong password")
	}

	t.Logf("Invalid login correctly rejected: %v", resp["error"])
}

func TestConfig_ListFiles(t *testing.T) {
	client := NewAPIClient(t)
	client.Login("admin", "admin")

	resp := client.Get("/api/config/files?path=" + configsDir)

	path, ok := resp["path"].(string)
	if !ok {
		t.Fatalf("No path in response: %v", resp)
	}

	if !strings.Contains(path, "configs") {
		t.Errorf("Unexpected path: %s", path)
	}

	files, ok := resp["files"].([]interface{})
	if !ok {
		t.Fatalf("No files in response: %v", resp)
	}

	if len(files) == 0 {
		t.Error("Expected at least one file")
	}

	t.Logf("Found %d files", len(files))
}

func TestConfig_ReadFile(t *testing.T) {
	client := NewAPIClient(t)
	client.Login("admin", "admin")

	filePath := filepath.Join(configsDir, "test.json")
	resp := client.Get("/api/config/file?path=" + filePath)

	content, ok := resp["content"].(string)
	if !ok {
		t.Fatalf("No content in response: %v", resp)
	}

	if !strings.Contains(content, "inbounds") {
		t.Errorf("Unexpected content: %s", content[:min(100, len(content))])
	}

	valid, _ := resp["valid"].(bool)
	if !valid {
		t.Error("File should be valid JSON")
	}

	t.Logf("Read file successfully, %d bytes", len(content))
}

func TestConfig_CreateFile(t *testing.T) {
	client := NewAPIClient(t)
	client.Login("admin", "admin")

	newFilePath := filepath.Join(configsDir, "new-test.json")
	resp := client.Post("/api/config/create", map[string]interface{}{
		"path":    newFilePath,
		"content": `{"created": true}`,
	}, true)

	success, _ := resp["success"].(bool)
	if !success {
		t.Fatalf("Failed to create file: %v", resp)
	}

	// Verify file exists
	if _, err := os.Stat(newFilePath); os.IsNotExist(err) {
		t.Error("File was not created")
	}

	t.Logf("Created file: %s", newFilePath)
}

func TestConfig_WriteFile(t *testing.T) {
	client := NewAPIClient(t)
	client.Login("admin", "admin")

	filePath := filepath.Join(configsDir, "test.json")
	resp := client.Post("/api/config/file", map[string]interface{}{
		"path":    filePath,
		"content": `{"log": {"loglevel": "info"}, "updated": true}`,
	}, true)

	success, _ := resp["success"].(bool)
	if !success {
		t.Fatalf("Failed to write file: %v", resp)
	}

	// Verify backup was created
	backup, _ := resp["backup"].(string)
	if backup != "" {
		t.Logf("Backup created: %s", backup)
	}
}

func TestConfig_RenameFile(t *testing.T) {
	client := NewAPIClient(t)
	client.Login("admin", "admin")

	oldPath := filepath.Join(configsDir, "new-test.json")
	newPath := filepath.Join(configsDir, "renamed-test.json")

	resp := client.Post("/api/config/rename", map[string]interface{}{
		"old_path": oldPath,
		"new_path": newPath,
	}, true)

	success, _ := resp["success"].(bool)
	if !success {
		t.Fatalf("Failed to rename file: %v", resp)
	}

	// Verify old file doesn't exist
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Error("Old file should not exist")
	}

	// Verify new file exists
	if _, err := os.Stat(newPath); os.IsNotExist(err) {
		t.Error("New file should exist")
	}

	t.Logf("Renamed file successfully")
}

func TestConfig_DeleteFile(t *testing.T) {
	client := NewAPIClient(t)
	client.Login("admin", "admin")

	filePath := filepath.Join(configsDir, "renamed-test.json")
	resp := client.Delete("/api/config/file", map[string]interface{}{
		"path": filePath,
	})

	success, _ := resp["success"].(bool)
	if !success {
		t.Fatalf("Failed to delete file: %v", resp)
	}

	// Verify file doesn't exist
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Error("File should not exist after deletion")
	}

	t.Logf("Deleted file successfully")
}

func TestConfig_ListBackups(t *testing.T) {
	client := NewAPIClient(t)
	client.Login("admin", "admin")

	filePath := filepath.Join(configsDir, "test.json")
	resp := client.Get("/api/config/backups?path=" + filePath)

	// Response should have backups array
	_, ok := resp["backups"]
	if !ok {
		t.Logf("No backups field in response (might be empty): %v", resp)
	}

	t.Logf("Backups response: %v", resp)
}

func TestLogs_ReadLogs(t *testing.T) {
	client := NewAPIClient(t)
	client.Login("admin", "admin")

	logPath := filepath.Join(logsDir, "access.log")
	resp := client.Get("/api/logs/xray?path=" + logPath + "&lines=10")

	entries, ok := resp["entries"].([]interface{})
	if !ok {
		t.Fatalf("No entries in response: %v", resp)
	}

	if len(entries) == 0 {
		t.Error("Expected at least one log entry")
	}

	// Check first entry has expected fields
	firstEntry := entries[0].(map[string]interface{})
	if _, ok := firstEntry["message"]; !ok {
		t.Error("Log entry should have message field")
	}

	t.Logf("Read %d log entries", len(entries))
}

func TestLogs_ParseLevels(t *testing.T) {
	client := NewAPIClient(t)
	client.Login("admin", "admin")

	logPath := filepath.Join(logsDir, "access.log")
	resp := client.Get("/api/logs/xray?path=" + logPath + "&lines=10")

	entries := resp["entries"].([]interface{})

	levels := make(map[string]int)
	for _, e := range entries {
		entry := e.(map[string]interface{})
		level := entry["level"].(string)
		levels[level]++
	}

	t.Logf("Log levels found: %v", levels)

	// Should have at least info, warn, error, debug
	if levels["error"] == 0 {
		t.Error("Expected to find ERROR level logs")
	}
	if levels["warn"] == 0 {
		t.Error("Expected to find WARN level logs")
	}
}

func TestService_Status(t *testing.T) {
	client := NewAPIClient(t)
	client.Login("admin", "admin")

	resp := client.Get("/api/xkeen/status")

	// On Windows/CI, xkeen service won't exist, but endpoint should work
	success, _ := resp["success"].(bool)
	if !success {
		t.Logf("Status check returned error (expected on non-Keenetic): %v", resp["message"])
	}

	t.Logf("Status response: %v", resp)
}

func TestAuth_Logout(t *testing.T) {
	client := NewAPIClient(t)
	client.Login("admin", "admin")

	resp := client.Post("/api/auth/logout", nil, true)

	ok, _ := resp["ok"].(bool)
	if !ok {
		t.Errorf("Logout failed: %v", resp)
	}

	t.Logf("Logout successful")
}

func TestAuth_Unauthorized(t *testing.T) {
	client := NewAPIClient(t)
	// Don't login

	resp := client.Get("/api/config/files?path=" + configsDir)

	// Should redirect or return error
	if resp["error"] != nil {
		t.Logf("Unauthorized request correctly rejected: %v", resp["error"])
	}
}

func TestConfig_PathTraversal(t *testing.T) {
	client := NewAPIClient(t)
	client.Login("admin", "admin")

	// Try to access file outside allowed roots
	resp := client.Get("/api/config/file?path=/etc/passwd")

	if resp["error"] == nil {
		t.Error("Path traversal should be blocked")
	}

	t.Logf("Path traversal correctly blocked: %v", resp["error"])
}

func TestConfig_InvalidJSON(t *testing.T) {
	client := NewAPIClient(t)
	client.Login("admin", "admin")

	filePath := filepath.Join(configsDir, "invalid.json")
	resp := client.Post("/api/config/file", map[string]interface{}{
		"path":    filePath,
		"content": `{invalid json}`,
	}, true)

	if resp["error"] == nil {
		t.Error("Invalid JSON should be rejected")
	}

	t.Logf("Invalid JSON correctly rejected: %v", resp["error"])
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
