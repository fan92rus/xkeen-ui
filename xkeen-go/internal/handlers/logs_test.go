package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"

	"github.com/fan92rus/xkeen-ui/internal/utils"
)

// helper: create a LogsHandler with a temp dir as the allowed root
// Does NOT start tail goroutines to avoid hangs in test.
func newTestLogsHandler(t *testing.T) (*LogsHandler, string) {
	t.Helper()
	tmpDir := t.TempDir()

	validator, err := utils.NewPathValidator([]string{tmpDir})
	if err != nil {
		t.Fatalf("failed to create path validator: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	h := &LogsHandler{
		validator:  validator,
		logFiles:   []string{},
		clients:    make(map[*websocket.Conn]bool),
		broadcast:  make(chan LogMessage, 100),
		ctx:        ctx,
		cancel:     cancel,
		upgrader:   websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }},
	}

	// Start only the broadcast goroutine (no tailFile goroutines)
	h.wg.Add(1)
	go h.runBroadcast()

	t.Cleanup(func() { h.Close() })
	return h, tmpDir
}

// helper: write lines to a file
func writeLines(t *testing.T, path string, lines []string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
}

// helper: decode logs response
func decodeLogsResponse(t *testing.T, rec *httptest.ResponseRecorder) (string, []LogMessage) {
	t.Helper()
	var result map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	pathVal, _ := result["path"].(string)

	// Re-marshal entries and unmarshal as []LogMessage
	entriesRaw, err := json.Marshal(result["entries"])
	if err != nil {
		t.Fatalf("failed to re-marshal entries: %v", err)
	}
	var entries []LogMessage
	if err := json.Unmarshal(entriesRaw, &entries); err != nil {
		t.Fatalf("failed to unmarshal entries: %v", err)
	}
	return pathVal, entries
}

// helper: decode error response
func decodeErrorResponse(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var errResp struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	return errResp.Error
}

// === HandleLogs (ReadLogs) ===

func TestReadLogs_FileNotFound(t *testing.T) {
	h, tmpDir := newTestLogsHandler(t)

	req := httptest.NewRequest("GET", "/api/logs/xray?path="+filepath.Join(tmpDir, "nonexistent.log"), nil)
	rec := httptest.NewRecorder()
	h.ReadLogs(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestReadLogs_EmptyFile(t *testing.T) {
	h, tmpDir := newTestLogsHandler(t)
	logPath := filepath.Join(tmpDir, "empty.log")
	writeLines(t, logPath, []string{})

	req := httptest.NewRequest("GET", "/api/logs/xray?path="+logPath, nil)
	rec := httptest.NewRecorder()
	h.ReadLogs(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	_, entries := decodeLogsResponse(t, rec)
	if len(entries) != 0 {
		t.Errorf("expected 0 entries for empty file, got %d", len(entries))
	}
}

func TestReadLogs_WithContent(t *testing.T) {
	h, tmpDir := newTestLogsHandler(t)
	logPath := filepath.Join(tmpDir, "test.log")
	writeLines(t, logPath, []string{
		"line 1 info message",
		"line 2 error message",
		"line 3 warn message",
	})

	req := httptest.NewRequest("GET", "/api/logs/xray?path="+logPath, nil)
	rec := httptest.NewRecorder()
	h.ReadLogs(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	pathVal, entries := decodeLogsResponse(t, rec)
	if pathVal != logPath {
		t.Errorf("expected path %q, got %q", logPath, pathVal)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	// Verify content preserved
	if entries[0].Message != "line 1 info message" {
		t.Errorf("entry 0 message: got %q", entries[0].Message)
	}
	if entries[0].File != "test.log" {
		t.Errorf("entry 0 file: got %q", entries[0].File)
	}
}

func TestReadLogs_RespectsLimit(t *testing.T) {
	h, tmpDir := newTestLogsHandler(t)
	logPath := filepath.Join(tmpDir, "many.log")

	var lines []string
	for i := 0; i < 200; i++ {
		lines = append(lines, "log line content")
	}
	writeLines(t, logPath, lines)

	req := httptest.NewRequest("GET", "/api/logs/xray?path="+logPath+"&lines=10", nil)
	rec := httptest.NewRecorder()
	h.ReadLogs(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	_, entries := decodeLogsResponse(t, rec)
	if len(entries) != 10 {
		t.Errorf("expected 10 entries with lines=10, got %d", len(entries))
	}
}

func TestReadLogs_DefaultLimit100(t *testing.T) {
	h, tmpDir := newTestLogsHandler(t)
	logPath := filepath.Join(tmpDir, "default.log")

	var lines []string
	for i := 0; i < 150; i++ {
		lines = append(lines, "line")
	}
	writeLines(t, logPath, lines)

	req := httptest.NewRequest("GET", "/api/logs/xray?path="+logPath, nil)
	rec := httptest.NewRecorder()
	h.ReadLogs(rec, req)

	_, entries := decodeLogsResponse(t, rec)
	if len(entries) != 100 {
		t.Errorf("expected 100 entries (default limit), got %d", len(entries))
	}
}

func TestReadLogs_MaxLimit1000(t *testing.T) {
	h, tmpDir := newTestLogsHandler(t)
	logPath := filepath.Join(tmpDir, "max.log")

	var lines []string
	for i := 0; i < 1500; i++ {
		lines = append(lines, "line")
	}
	writeLines(t, logPath, lines)

	req := httptest.NewRequest("GET", "/api/logs/xray?path="+logPath+"&lines=5000", nil)
	rec := httptest.NewRecorder()
	h.ReadLogs(rec, req)

	_, entries := decodeLogsResponse(t, rec)
	if len(entries) != 1000 {
		t.Errorf("expected 1000 entries (max limit), got %d", len(entries))
	}
}

func TestReadLogs_InvalidLimitUsesDefault(t *testing.T) {
	h, tmpDir := newTestLogsHandler(t)
	logPath := filepath.Join(tmpDir, "invalid.log")

	var lines []string
	for i := 0; i < 150; i++ {
		lines = append(lines, "line")
	}
	writeLines(t, logPath, lines)

	req := httptest.NewRequest("GET", "/api/logs/xray?path="+logPath+"&lines=0", nil)
	rec := httptest.NewRecorder()
	h.ReadLogs(rec, req)

	_, entries := decodeLogsResponse(t, rec)
	// lines < 1 resets to default 100
	if len(entries) != 100 {
		t.Errorf("expected 100 entries (invalid limit resets to default), got %d", len(entries))
	}
}

func TestReadLogs_InvalidPathTraversal(t *testing.T) {
	h, _ := newTestLogsHandler(t)

	req := httptest.NewRequest("GET", "/api/logs/xray?path=../../etc/passwd", nil)
	rec := httptest.NewRecorder()
	h.ReadLogs(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 for path traversal, got %d", rec.Code)
	}
}

func TestReadLogs_DefaultPathWhenEmpty(t *testing.T) {
	h, _ := newTestLogsHandler(t)

	req := httptest.NewRequest("GET", "/api/logs/xray", nil)
	rec := httptest.NewRecorder()
	h.ReadLogs(rec, req)

	// Default path /opt/var/log/xray/access.log won't exist, should error
	// but the path validation should fail because it's outside allowed roots
	if rec.Code != http.StatusForbidden {
		// Could be 500 if file not found, or 403 if path validation fails
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("expected non-200 for default path, got %d", rec.Code)
		}
	}
}

func TestReadLogs_ContentTypeJSON(t *testing.T) {
	h, tmpDir := newTestLogsHandler(t)
	logPath := filepath.Join(tmpDir, "ct.log")
	writeLines(t, logPath, []string{"line"})

	req := httptest.NewRequest("GET", "/api/logs/xray?path="+logPath, nil)
	rec := httptest.NewRecorder()
	h.ReadLogs(rec, req)

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected application/json, got %q", ct)
	}
}

// === parseLogLine ===

func TestParseLogLine_InfoLevel(t *testing.T) {
	h, _ := newTestLogsHandler(t)
	msg := h.parseLogLine("2024/01/01 12:00:00 some info message", "/var/log/xray/access.log")

	if msg.Level != "info" {
		t.Errorf("expected level info, got %q", msg.Level)
	}
}

func TestParseLogLine_ErrorLevel(t *testing.T) {
	h, _ := newTestLogsHandler(t)
	msg := h.parseLogLine("2024/01/01 12:00:00 ERROR: something failed", "/var/log/xray/access.log")

	if msg.Level != "error" {
		t.Errorf("expected level error, got %q", msg.Level)
	}
}

func TestParseLogLine_WarnLevel(t *testing.T) {
	h, _ := newTestLogsHandler(t)
	msg := h.parseLogLine("2024/01/01 12:00:00 WARNING: something suspicious", "/var/log/xray/access.log")

	if msg.Level != "warn" {
		t.Errorf("expected level warn, got %q", msg.Level)
	}
}

func TestParseLogLine_DebugLevel(t *testing.T) {
	h, _ := newTestLogsHandler(t)
	msg := h.parseLogLine("2024/01/01 12:00:00 DEBUG: debug info", "/var/log/xray/access.log")

	if msg.Level != "debug" {
		t.Errorf("expected level debug, got %q", msg.Level)
	}
}

func TestParseLogLine_LevelFromFilename(t *testing.T) {
	h, _ := newTestLogsHandler(t)

	// Line without recognizable level keyword, but file is error.log
	msg := h.parseLogLine("some generic message", "/var/log/xray/error.log")
	if msg.Level != "error" {
		t.Errorf("expected level error from filename, got %q", msg.Level)
	}

	// Access log without level → default info
	msg2 := h.parseLogLine("some generic message", "/var/log/xray/access.log")
	if msg2.Level != "info" {
		t.Errorf("expected level info from filename, got %q", msg2.Level)
	}
}

func TestParseLogLine_ErrorMessageOverridesFilename(t *testing.T) {
	h, _ := newTestLogsHandler(t)

	// ERROR keyword overrides access.log default
	msg := h.parseLogLine("ERROR: something bad", "/var/log/xray/access.log")
	if msg.Level != "error" {
		t.Errorf("expected error level to override filename, got %q", msg.Level)
	}
}

func TestParseLogLine_FileField(t *testing.T) {
	h, _ := newTestLogsHandler(t)
	msg := h.parseLogLine("line", "/opt/var/log/xray/access.log")
	if msg.File != "access.log" {
		t.Errorf("expected file 'access.log', got %q", msg.File)
	}
}

func TestParseLogLine_TimestampPresent(t *testing.T) {
	h, _ := newTestLogsHandler(t)
	msg := h.parseLogLine("line", "/var/log/xray/access.log")
	if msg.Timestamp == "" {
		t.Error("expected non-empty timestamp")
	}
}

// === readLastLines ===

func TestReadLastLines_EmptyFile(t *testing.T) {
	h, _ := newTestLogsHandler(t)
	tmpFile := filepath.Join(t.TempDir(), "empty.log")
	os.WriteFile(tmpFile, []byte{}, 0644)

	entries, err := h.readLastLines(tmpFile, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestReadLastLines_SmallFile(t *testing.T) {
	h, _ := newTestLogsHandler(t)
	tmpFile := filepath.Join(t.TempDir(), "small.log")
	os.WriteFile(tmpFile, []byte("line1\nline2\nline3\n"), 0644)

	entries, err := h.readLastLines(tmpFile, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	if entries[0].Message != "line1" {
		t.Errorf("entry 0: got %q", entries[0].Message)
	}
	if entries[2].Message != "line3" {
		t.Errorf("entry 2: got %q", entries[2].Message)
	}
}

func TestReadLastLines_TruncatesToN(t *testing.T) {
	h, _ := newTestLogsHandler(t)
	tmpFile := filepath.Join(t.TempDir(), "trunc.log")
	var content strings.Builder
	for i := 0; i < 20; i++ {
		content.WriteString("line\n")
	}
	os.WriteFile(tmpFile, []byte(content.String()), 0644)

	entries, err := h.readLastLines(tmpFile, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 5 {
		t.Fatalf("expected 5 entries, got %d", len(entries))
	}
}

func TestReadLastLines_ExactLineCount(t *testing.T) {
	h, _ := newTestLogsHandler(t)
	tmpFile := filepath.Join(t.TempDir(), "exact.log")
	os.WriteFile(tmpFile, []byte("line1\nline2\nline3\n"), 0644)

	entries, err := h.readLastLines(tmpFile, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 3 {
		t.Errorf("expected 3 entries, got %d", len(entries))
	}
}

func TestReadLastLines_FileNotFound(t *testing.T) {
	h, _ := newTestLogsHandler(t)

	_, err := h.readLastLines("/nonexistent/path/file.log", 10)
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestReadLastLines_VeryLongLine(t *testing.T) {
	h, _ := newTestLogsHandler(t)
	tmpFile := filepath.Join(t.TempDir(), "longline.log")

	longLine := strings.Repeat("x", 10000)
	os.WriteFile(tmpFile, []byte(longLine+"\n"), 0644)

	entries, err := h.readLastLines(tmpFile, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if len(entries[0].Message) != 10000 {
		t.Errorf("expected 10000 char message, got %d", len(entries[0].Message))
	}
}

func TestReadLastLines_NoTrailingNewline(t *testing.T) {
	h, _ := newTestLogsHandler(t)
	tmpFile := filepath.Join(t.TempDir(), "notrail.log")
	os.WriteFile(tmpFile, []byte("line1\nline2\nline3"), 0644) // no trailing \n

	entries, err := h.readLastLines(tmpFile, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
}

func TestReadLastLines_BinaryContent(t *testing.T) {
	h, _ := newTestLogsHandler(t)
	tmpFile := filepath.Join(t.TempDir(), "binary.log")

	// Binary content with null bytes and non-UTF8
	data := []byte{0x48, 0x65, 0x6c, 0x6c, 0x6f, 0x0a, 0x00, 0x01, 0x02, 0x0a}
	os.WriteFile(tmpFile, data, 0644)

	entries, err := h.readLastLines(tmpFile, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should have 2 lines (split by \n)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
}

// === checkOrigin ===

func TestCheckOrigin_EmptyOrigin(t *testing.T) {
	h, _ := newTestLogsHandler(t)

	req := httptest.NewRequest("GET", "/ws/logs", nil)
	req.Header.Del("Origin")

	if !h.checkOrigin(req) {
		t.Error("empty origin should be allowed")
	}
}

func TestCheckOrigin_SameOriginHTTP(t *testing.T) {
	h, _ := newTestLogsHandler(t)

	req := httptest.NewRequest("GET", "/ws/logs", nil)
	req.Header.Set("Origin", "http://localhost:8089")
	req.Host = "localhost:8089"

	if !h.checkOrigin(req) {
		t.Error("same-origin HTTP should be allowed")
	}
}

func TestCheckOrigin_SameOriginHTTPS(t *testing.T) {
	h, _ := newTestLogsHandler(t)

	req := httptest.NewRequest("GET", "/ws/logs", nil)
	req.Header.Set("Origin", "https://myrouter:8089")
	req.Host = "myrouter:8089"

	if !h.checkOrigin(req) {
		t.Error("same-origin HTTPS should be allowed")
	}
}

func TestCheckOrigin_AllowedOrigin(t *testing.T) {
	h := &LogsHandler{
		allowedOrigins: map[string]bool{"http://trusted.example.com": true},
		clients:        make(map[*websocket.Conn]bool),
		broadcast:      make(chan LogMessage, 100),
	}

	req := httptest.NewRequest("GET", "/ws/logs", nil)
	req.Header.Set("Origin", "http://trusted.example.com")
	req.Host = "other-host"

	if !h.checkOrigin(req) {
		t.Error("explicitly allowed origin should pass")
	}
}

func TestCheckOrigin_RejectedOrigin(t *testing.T) {
	h := &LogsHandler{
		allowedOrigins: map[string]bool{"http://trusted.example.com": true},
		clients:        make(map[*websocket.Conn]bool),
		broadcast:      make(chan LogMessage, 100),
	}

	req := httptest.NewRequest("GET", "/ws/logs", nil)
	req.Header.Set("Origin", "http://evil.example.com")
	req.Host = "myrouter:8089"

	if h.checkOrigin(req) {
		t.Error("untrusted origin should be rejected")
	}
}

// === RegisterLogsRoutes ===

func TestRegisterLogsRoutes(t *testing.T) {
	h, tmpDir := newTestLogsHandler(t)
	logPath := filepath.Join(tmpDir, "route.log")
	writeLines(t, logPath, []string{"test line"})

	router := mux.NewRouter()
	RegisterLogsRoutes(router, h)

	req := httptest.NewRequest("GET", "/logs/xray?path="+logPath, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRegisterLogsRoutes_MethodNotAllowed(t *testing.T) {
	h, _ := newTestLogsHandler(t)

	router := mux.NewRouter()
	RegisterLogsRoutes(router, h)

	req := httptest.NewRequest("POST", "/logs/xray", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code == http.StatusOK {
		t.Error("POST should not be allowed on /logs/xray")
	}
}

// === truncate ===

func TestTruncate_ShortString(t *testing.T) {
	result := truncate("hello", 10)
	if result != "hello" {
		t.Errorf("expected 'hello', got %q", result)
	}
}

func TestTruncate_ExactLength(t *testing.T) {
	result := truncate("hello", 5)
	if result != "hello" {
		t.Errorf("expected 'hello', got %q", result)
	}
}

func TestTruncate_LongString(t *testing.T) {
	result := truncate("hello world foo bar", 5)
	if result != "hello..." {
		t.Errorf("expected 'hello...', got %q", result)
	}
}

func TestTruncate_ZeroMaxLen(t *testing.T) {
	result := truncate("hello", 0)
	// 0 max len: len("hello")=5 > 0, so truncate to s[:0]+"..." = "..."
	if result != "..." {
		t.Errorf("expected '...' for zero maxLen, got %q", result)
	}
}

// === LogsHandler construction ===

func TestNewLogsHandler_DefaultLogFiles(t *testing.T) {
	// Verify that empty LogFiles triggers the default paths
	// We can't call NewLogsHandler directly (it starts tail goroutines)
	// so we verify the logic by checking defaultLogFiles
	if len(defaultLogFiles) == 0 {
		t.Error("expected default log files to be defined")
	}
	for _, f := range defaultLogFiles {
		if !strings.HasPrefix(f, "/opt/") {
			t.Errorf("expected default path under /opt/, got %q", f)
		}
	}
}

func TestNewLogsHandler_CustomLogFiles(t *testing.T) {
	tmpDir := t.TempDir()
	customPath := filepath.Join(tmpDir, "custom.log")

	ctx, cancel := context.WithCancel(context.Background())
	h := &LogsHandler{
		validator: nil,
		logFiles:  []string{customPath},
		clients:   make(map[*websocket.Conn]bool),
		broadcast: make(chan LogMessage, 100),
		ctx:       ctx,
		cancel:    cancel,
	}
	defer h.Close()

	if len(h.logFiles) != 1 {
		t.Fatalf("expected 1 custom log file, got %d", len(h.logFiles))
	}
	if h.logFiles[0] != customPath {
		t.Errorf("expected custom log file %q, got %q", customPath, h.logFiles[0])
	}
}

// === LogMessage ===

func TestLogMessage_JSONSerialization(t *testing.T) {
	msg := LogMessage{
		Timestamp: "2024-01-01T00:00:00Z",
		Level:     "info",
		Message:   "test message",
		File:      "access.log",
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded LogMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Timestamp != msg.Timestamp {
		t.Errorf("timestamp mismatch: got %q", decoded.Timestamp)
	}
	if decoded.Level != msg.Level {
		t.Errorf("level mismatch: got %q", decoded.Level)
	}
	if decoded.Message != msg.Message {
		t.Errorf("message mismatch: got %q", decoded.Message)
	}
	if decoded.File != msg.File {
		t.Errorf("file mismatch: got %q", decoded.File)
	}
}
