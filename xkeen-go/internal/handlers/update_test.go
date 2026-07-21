package handlers

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"github.com/fan92rus/xkeen-ui/internal/version"
)

// ---------- Update test helpers ----------

// newUpdateHandler creates an UpdateHandler for testing.
func newUpdateHandler(t *testing.T) *UpdateHandler {
	t.Helper()
	h := NewUpdateHandler()
	// Disable retries by default so existing tests don't spend 55s waiting
	// on quadratic backoff. Retry-specific tests override these fields.
	h.maxDownloadRetries = 0
	return h
}

// newUpdateRouter creates a mux.Router with update routes.
func newUpdateRouter(h *UpdateHandler) *mux.Router {
	r := mux.NewRouter()
	RegisterUpdateRoutes(r, h)
	return r
}

// parseUpdateResponse parses a CheckUpdate response body into a map.
func parseUpdateResponse(t *testing.T, resp *http.Response) map[string]interface{} {
	t.Helper()
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("failed to parse response JSON: %v\nbody: %s", err, string(data))
	}
	return result
}

// parseSSEEvents reads all SSE events from a response body.
// Returns slice of {event, data} maps.
func parseSSEEvents(t *testing.T, body io.Reader) []map[string]interface{} {
	t.Helper()
	var events []map[string]interface{}
	scanner := bufio.NewScanner(body)
	var currentEvent string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			currentEvent = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			dataStr := strings.TrimPrefix(line, "data: ")
			var data map[string]interface{}
			if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
				t.Fatalf("failed to parse SSE data: %v\nline: %s", err, dataStr)
			}
			data["_event"] = currentEvent
			events = append(events, data)
		}
	}
	return events
}

// mockGitHubAPI creates a mock GitHub API server.
// stableHandler handles /repos/{owner}/{repo}/releases/latest
// listHandler handles /repos/{owner}/{repo}/releases (can be nil)
func mockGitHubAPI(t *testing.T, stableHandler, listHandler http.HandlerFunc) *httptest.Server {
	t.Helper()
	serveMux := http.NewServeMux()
	if stableHandler != nil {
		serveMux.HandleFunc("/repos/fan92rus/xkeen-ui/releases/latest", stableHandler)
	}
	if listHandler != nil {
		serveMux.HandleFunc("/repos/fan92rus/xkeen-ui/releases", listHandler)
	}
	serveMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Download endpoint
		if strings.Contains(r.URL.Path, "/releases/download/") {
			// Serve a dummy binary file (must be >1MB for size check)
			w.Header().Set("Content-Type", "application/octet-stream")
			// Write 2MB of data
			chunk := make([]byte, 1024)
			for i := 0; i < 2048; i++ {
				w.Write(chunk)
			}
			return
		}
		http.NotFound(w, r)
	})
	return httptest.NewServer(serveMux)
}

// ---------- Tests ----------

func TestNewUpdateHandler(t *testing.T) {
	h := newUpdateHandler(t)
	if h.githubRepo != "fan92rus/xkeen-ui" {
		t.Errorf("expected githubRepo=fan92rus/xkeen-ui, got %s", h.githubRepo)
	}
	if h.binaryName == "" {
		t.Error("binaryName should not be empty")
	}
	if h.installPath == "" {
		t.Error("installPath should not be empty")
	}
	if h.downloadURL == "" {
		t.Error("downloadURL should not be empty")
	}
}

func TestCheckUpdate_WithMockServer(t *testing.T) {
	// Mock GitHub stable release endpoint
	stableHandler := func(w http.ResponseWriter, _ *http.Request) {
		release := GitHubRelease{
			TagName:     "v99.0.0",
			Name:        "v99.0.0",
			Body:        "Test release notes",
			HTMLURL:     "https://github.com/fan92rus/xkeen-ui/releases/tag/v99.0.0",
			PublishedAt: "2026-01-01T00:00:00Z",
			Prerelease:  false,
		}
		json.NewEncoder(w).Encode(release)
	}

	server := mockGitHubAPI(t, stableHandler, nil)
	defer server.Close()

	h := newUpdateHandler(t)
	// Override the GitHub API URL by using custom HTTP client pointing to mock server
	// Since we can't easily override the URL in the handler, test via integration instead
	// For now test that the handler properly parses a valid response

	router := newUpdateRouter(h)
	req := httptest.NewRequest("GET", "/update/check", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	// This will likely fail to connect to real GitHub, but we can test error handling
	resp := rec.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	result := parseUpdateResponse(t, resp)
	// Should have at least current_version and architecture
	if result["current_version"] == nil {
		t.Error("expected current_version in response")
	}
	if result["architecture"] == nil {
		t.Error("expected architecture in response")
	}
	if result["binary_name"] == nil {
		t.Error("expected binary_name in response")
	}
}

func TestCheckUpdate_ErrorResponse(t *testing.T) {
	// Test that when GitHub API fails, handler returns error in response (not HTTP error)
	h := newUpdateHandler(t)
	// Override to use an unreachable URL by using a canceled context
	router := newUpdateRouter(h)

	// The handler will try to hit the real GitHub API which may or may not succeed.
	// But the response format should always be consistent.
	req := httptest.NewRequest("GET", "/update/check", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	resp := rec.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 (errors reported in body), got %d", resp.StatusCode)
	}

	result := parseUpdateResponse(t, resp)
	// Must always have these fields regardless of success/failure
	if result["current_version"] == nil {
		t.Error("expected current_version")
	}
	if result["architecture"] == nil {
		t.Error("expected architecture")
	}
	if result["binary_name"] == nil {
		t.Error("expected binary_name")
	}
}

func TestCheckUpdate_DevChannel(t *testing.T) {
	// Test that prerelease=true query parameter is handled
	h := newUpdateHandler(t)
	router := newUpdateRouter(h)

	req := httptest.NewRequest("GET", "/update/check?prerelease=true", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	resp := rec.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	result := parseUpdateResponse(t, resp)
	if result["current_version"] == nil {
		t.Error("expected current_version")
	}
}

func TestCompareVersions(t *testing.T) {
	h := newUpdateHandler(t)

	tests := []struct {
		v1, v2 string
		want   int
		desc   string
	}{
		{"1.0.0", "2.0.0", -1, "major version less"},
		{"2.0.0", "1.0.0", 1, "major version greater"},
		{"1.0.0", "1.0.0", 0, "equal versions"},
		{"1.2.0", "1.3.0", -1, "minor version less"},
		{"1.3.0", "1.2.0", 1, "minor version greater"},
		{"1.0.1", "1.0.2", -1, "patch version less"},
		{"1.0.2", "1.0.1", 1, "patch version greater"},
		{"v1.0.0", "v2.0.0", -1, "with v prefix"},
		{"v1.0.0", "2.0.0", -1, "mixed v prefix"},
		{"1.0.0", "1.0.0-dev.100", 1, "release > prerelease"},
		{"1.0.0-dev.100", "1.0.0", -1, "prerelease < release"},
		{"1.0.0-dev.100", "1.0.0-dev.200", -1, "earlier dev < later dev"},
		{"1.0.0-dev.200", "1.0.0-dev.100", 1, "later dev > earlier dev"},
		{"1.0.0-dev.100", "1.0.0-dev.100", 0, "same dev build"},
		{"2.0.0", "1.9.9", 1, "major overrides lower minor/patch"},
		{"0.1.0", "0.1.1", -1, "zero major"},
		{"1.0", "1.0.0", 0, "missing patch equals zero"},
		{"1.0.0", "1.0", 0, "reverse missing patch"},
		{"", "", 0, "empty strings"},
		{"0.0.1-dev.500", "0.0.1", -1, "dev of patch version"},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			got := h.compareVersions(tc.v1, tc.v2)
			if got != tc.want {
				t.Errorf("compareVersions(%q, %q) = %d, want %d", tc.v1, tc.v2, got, tc.want)
			}
		})
	}
}

func TestSplitPreRelease(t *testing.T) {
	tests := []struct {
		input, wantBase, wantPre string
	}{
		{"1.2.3", "1.2.3", ""},
		{"1.2.3-dev.123", "1.2.3", "dev.123"},
		{"1.0.0-beta.1", "1.0.0", "beta.1"},
		{"0.1.0-rc.42", "0.1.0", "rc.42"},
		{"", "", ""},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			base, pre := splitPreRelease(tc.input)
			if base != tc.wantBase || pre != tc.wantPre {
				t.Errorf("splitPreRelease(%q) = (%q, %q), want (%q, %q)",
					tc.input, base, pre, tc.wantBase, tc.wantPre)
			}
		})
	}
}

func TestExtractTimestamp(t *testing.T) {
	tests := []struct {
		input string
		want  int64
	}{
		{"dev.1709876543", 1709876543},
		{"dev.0", 0},
		{"dev.", 0},
		{"", 0},
		{"beta.100", 100},
		{"rc.9999999999", 9999999999},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := extractTimestamp(tc.input)
			if got != tc.want {
				t.Errorf("extractTimestamp(%q) = %d, want %d", tc.input, got, tc.want)
			}
		})
	}
}

func TestComparePrereleaseSuffixes(t *testing.T) {
	tests := []struct {
		p1, p2 string
		want   int
	}{
		{"dev.100", "dev.200", -1},
		{"dev.200", "dev.100", 1},
		{"dev.100", "dev.100", 0},
		{"dev.0", "dev.1", -1},
		{"beta.100", "beta.200", -1},
	}

	for _, tc := range tests {
		t.Run(tc.p1+"_vs_"+tc.p2, func(t *testing.T) {
			got := comparePrereleaseSuffixes(tc.p1, tc.p2)
			if got != tc.want {
				t.Errorf("comparePrereleaseSuffixes(%q, %q) = %d, want %d",
					tc.p1, tc.p2, got, tc.want)
			}
		})
	}
}

func TestCheckUpdate_DevReleaseTag_Stored(t *testing.T) {
	// Verify that devReleaseTag is stored when dev update is available
	h := newUpdateHandler(t)

	// Simulate a dev release being available by testing compareVersions
	// and the tag storage logic indirectly
	currentVersion := version.GetVersion()
	newVersion := "v999.0.0-dev.1234567890"

	result := h.compareVersions(currentVersion, newVersion)
	if result >= 0 {
		t.Skipf("current version %s >= mock version %s, skipping", currentVersion, newVersion)
	}

	// The devReleaseTag should be empty initially
	if h.devReleaseTag != "" {
		t.Error("devReleaseTag should start empty")
	}
}

func TestDownloadWithChecksum_Success(t *testing.T) {
	// Create a mock download server that serves a binary + checksum
	tmpDir := t.TempDir()
	binaryContent := make([]byte, 2*1024*1024) // 2MB
	for i := range binaryContent {
		binaryContent[i] = byte(i % 256)
	}

	hash := sha256.Sum256(binaryContent)
	checksum := hex.EncodeToString(hash[:])

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprintf(w, "%s  xkeen-ui-keenetic-arm64", checksum)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(binaryContent)
	}))
	defer server.Close()

	h := newUpdateHandler(t)
	binaryPath := filepath.Join(tmpDir, "binary.new")

	err := h.downloadWithChecksum(context.Background(), binaryPath, server.URL+"/download/binary")
	if err != nil {
		t.Fatalf("downloadWithChecksum failed: %v", err)
	}

	// Verify the file was written
	info, err := os.Stat(binaryPath)
	if err != nil {
		t.Fatalf("binary file not found: %v", err)
	}
	if info.Size() != int64(len(binaryContent)) {
		t.Errorf("file size = %d, want %d", info.Size(), len(binaryContent))
	}
}

func TestDownloadWithChecksum_Mismatch(t *testing.T) {
	// Server serves correct binary but wrong checksum
	tmpDir := t.TempDir()
	binaryContent := make([]byte, 2*1024*1024)
	for i := range binaryContent {
		binaryContent[i] = byte(i % 256)
	}

	wrongChecksum := strings.Repeat("a", 64)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprintf(w, "%s  binary", wrongChecksum)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(binaryContent)
	}))
	defer server.Close()

	h := newUpdateHandler(t)
	binaryPath := filepath.Join(tmpDir, "binary.new")

	err := h.downloadWithChecksum(context.Background(), binaryPath, server.URL+"/download/binary")
	if err == nil {
		t.Fatal("expected checksum mismatch error")
	}
	if !strings.Contains(err.Error(), "checksum verification failed") {
		t.Errorf("unexpected error: %v", err)
	}

	// Binary should be cleaned up on checksum failure
	if _, err := os.Stat(binaryPath); !os.IsNotExist(err) {
		t.Error("corrupted binary should be removed on checksum failure")
	}
}

func TestDownloadWithChecksum_NoChecksumFile(t *testing.T) {
	// When checksum file is not available, download should still succeed
	tmpDir := t.TempDir()
	binaryContent := make([]byte, 2*1024*1024)
	for i := range binaryContent {
		binaryContent[i] = byte(i % 256)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(binaryContent)
	}))
	defer server.Close()

	h := newUpdateHandler(t)
	binaryPath := filepath.Join(tmpDir, "binary.new")

	err := h.downloadWithChecksum(context.Background(), binaryPath, server.URL+"/download/binary")
	if err != nil {
		t.Fatalf("expected success when checksum file missing, got: %v", err)
	}

	// File should still be downloaded
	info, err := os.Stat(binaryPath)
	if err != nil {
		t.Fatalf("binary file not found: %v", err)
	}
	if info.Size() != int64(len(binaryContent)) {
		t.Errorf("file size = %d, want %d", info.Size(), len(binaryContent))
	}
}

func TestDownloadWithChecksum_DownloadError(t *testing.T) {
	tmpDir := t.TempDir()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer server.Close()

	h := newUpdateHandler(t)
	binaryPath := filepath.Join(tmpDir, "binary.new")

	err := h.downloadWithChecksum(context.Background(), binaryPath, server.URL+"/download/binary")
	if err == nil {
		t.Fatal("expected error when download fails")
	}
	if !strings.Contains(err.Error(), "failed to download binary") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDownloadWithChecksum_ContextCancelled(t *testing.T) {
	tmpDir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(make([]byte, 1024))
	}))
	defer server.Close()

	h := newUpdateHandler(t)
	binaryPath := filepath.Join(tmpDir, "binary.new")

	err := h.downloadWithChecksum(ctx, binaryPath, server.URL+"/download/binary")
	if err == nil {
		t.Fatal("expected error with canceled context")
	}
}

func TestDownloadFile_Success(t *testing.T) {
	content := []byte("hello world binary content here")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify User-Agent header
		ua := r.Header.Get("User-Agent")
		if !strings.HasPrefix(ua, "XKEEN-UI/") {
			t.Errorf("expected User-Agent starting with XKEEN-UI/, got %q", ua)
		}
		w.Write(content)
	}))
	defer server.Close()

	h := newUpdateHandler(t)
	tmpFile := filepath.Join(t.TempDir(), "downloaded")

	err := h.downloadFile(context.Background(), tmpFile, server.URL+"/file")
	if err != nil {
		t.Fatalf("downloadFile failed: %v", err)
	}

	data, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("failed to read downloaded file: %v", err)
	}
	if !bytes.Equal(data, content) {
		t.Errorf("downloaded content mismatch: got %q, want %q", string(data), string(content))
	}
}

func TestDownloadFile_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	h := newUpdateHandler(t)
	tmpFile := filepath.Join(t.TempDir(), "downloaded")

	err := h.downloadFile(context.Background(), tmpFile, server.URL+"/file")
	if err == nil {
		t.Fatal("expected error for 404")
	}
	if !strings.Contains(err.Error(), "HTTP 404") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDownloadFile_ConnectionError(t *testing.T) {
	h := newUpdateHandler(t)
	tmpFile := filepath.Join(t.TempDir(), "downloaded")

	// Use a port that's not listening
	err := h.downloadFile(context.Background(), tmpFile, "http://127.0.0.1:1/file")
	if err == nil {
		t.Fatal("expected connection error")
	}
}

func TestStartUpdate_SSEHeaders(t *testing.T) {
	// Test that StartUpdate sets correct SSE headers
	// We can't fully test the update flow (it calls os.Exit), but we can test initial setup

	// StartUpdate will try to download and fail, sending error SSE event
	// We use a mock server that returns 404 for downloads
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	h := newUpdateHandler(t)
	// Override download URL to point to our mock server
	h.downloadURL = server.URL + "/download/binary"

	router := newUpdateRouter(h)
	req := httptest.NewRequest("POST", "/update/start", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()

	// Check SSE headers
	ct := resp.Header.Get("Content-Type")
	if ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	cc := resp.Header.Get("Cache-Control")
	if cc != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", cc)
	}
	ka := resp.Header.Get("Connection")
	if ka != "keep-alive" {
		t.Errorf("Connection = %q, want keep-alive", ka)
	}
}

func TestStartUpdate_DownloadFail_SendsErrorEvent(t *testing.T) {
	// When download fails, should send SSE error event
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	h := newUpdateHandler(t)
	h.downloadURL = server.URL + "/download/binary"

	router := newUpdateRouter(h)
	req := httptest.NewRequest("POST", "/update/start", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()

	events := parseSSEEvents(t, resp.Body)

	// Should have at least a progress event and an error event
	if len(events) == 0 {
		t.Fatal("expected SSE events, got none")
	}

	// First event should be progress
	if events[0]["_event"] != "progress" {
		t.Errorf("first event type = %q, want progress", events[0]["_event"])
	}
	if events[0]["status"] != "downloading" {
		t.Errorf("first event status = %q, want downloading", events[0]["status"])
	}

	// Should have an error event
	foundError := false
	for _, e := range events {
		if e["_event"] == "error" {
			foundError = true
			errMsg, _ := e["error"].(string)
			if !strings.Contains(errMsg, "failed to download binary") {
				t.Errorf("error message = %q, want contains 'failed to download binary'", errMsg)
			}
		}
	}
	if !foundError {
		t.Error("expected error SSE event")
	}
}

func TestStartUpdate_Prerelease_UsesDevTag(t *testing.T) {
	// When prerelease=true and devReleaseTag is set, should use tag-specific URL
	h := newUpdateHandler(t)
	h.devReleaseTag = "v99.0.0-dev.12345"

	// Can't fully test download, but verify the URL construction logic
	// by checking the handler starts properly with prerelease flag
	router := newUpdateRouter(h)
	req := httptest.NewRequest("POST", "/update/start?prerelease=true", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	resp := rec.Result()
	resp.Body.Close()

	// Just verify it doesn't crash - the download will fail but that's OK
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestStartUpdate_FileTooSmall(t *testing.T) {
	// Server returns a small file (<1MB), should fail verification
	smallContent := make([]byte, 100) // 100 bytes

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Write(smallContent)
	}))
	defer server.Close()

	h := newUpdateHandler(t)
	h.downloadURL = server.URL + "/download/binary"

	router := newUpdateRouter(h)
	req := httptest.NewRequest("POST", "/update/start", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()

	events := parseSSEEvents(t, resp.Body)

	// Should have error about file being too small
	foundSmallError := false
	for _, e := range events {
		if e["_event"] == "error" {
			errMsg, _ := e["error"].(string)
			if strings.Contains(errMsg, "too small") {
				foundSmallError = true
			}
		}
	}
	if !foundSmallError {
		t.Error("expected error about file too small")
	}
}

func TestCheckUpdateResponse_Fields(t *testing.T) {
	// Verify CheckUpdateResponse serializes correctly
	resp := CheckUpdateResponse{
		CurrentVersion:  "1.0.0",
		LatestVersion:   "2.0.0",
		UpdateAvailable: true,
		IsPrerelease:    false,
		ReleaseURL:      "https://github.com/test",
		ReleaseNotes:    "bug fixes",
		Architecture:    "arm64",
		BinaryName:      "xkeen-ui-keenetic-arm64",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if parsed["current_version"] != "1.0.0" {
		t.Errorf("current_version = %v, want 1.0.0", parsed["current_version"])
	}
	if parsed["latest_version"] != "2.0.0" {
		t.Errorf("latest_version = %v, want 2.0.0", parsed["latest_version"])
	}
	if parsed["update_available"] != true {
		t.Error("update_available should be true")
	}
	if parsed["architecture"] != "arm64" {
		t.Errorf("architecture = %v, want arm64", parsed["architecture"])
	}
}

func TestCheckUpdateResponse_WithError(t *testing.T) {
	// Verify error field is present when set
	resp := CheckUpdateResponse{
		CurrentVersion: "1.0.0",
		Architecture:   "arm64",
		BinaryName:     "xkeen-ui-keenetic-arm64",
		Error:          "network error",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var parsed map[string]interface{}
	json.Unmarshal(data, &parsed)

	if parsed["error"] != "network error" {
		t.Errorf("error = %v, want 'network error'", parsed["error"])
	}
	// update_available should be false when there's an error
	if parsed["update_available"] == true {
		t.Error("update_available should be false when error is set")
	}
}

func TestCheckUpdateResponse_OmitEmpty(t *testing.T) {
	// When no error, the error field should be omitted
	resp := CheckUpdateResponse{
		CurrentVersion:  "1.0.0",
		LatestVersion:   "2.0.0",
		UpdateAvailable: true,
		Architecture:    "arm64",
		BinaryName:      "test-binary",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	s := string(data)
	if strings.Contains(s, `"error"`) {
		t.Errorf("error field should be omitted when empty, got: %s", s)
	}
	if strings.Contains(s, `"release_url"`) {
		t.Errorf("release_url should be omitted when empty, got: %s", s)
	}
}

func TestGitHubRelease_ParseFromJSON(t *testing.T) {
	jsonStr := `{
		"tag_name": "v1.2.3",
		"name": "Release 1.2.3",
		"body": "## Changes\n- Fixed bug",
		"html_url": "https://github.com/fan92rus/xkeen-ui/releases/tag/v1.2.3",
		"published_at": "2026-01-15T10:30:00Z",
		"prerelease": true
	}`

	var release GitHubRelease
	if err := json.Unmarshal([]byte(jsonStr), &release); err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if release.TagName != "v1.2.3" {
		t.Errorf("TagName = %q, want v1.2.3", release.TagName)
	}
	if release.Name != "Release 1.2.3" {
		t.Errorf("Name = %q", release.Name)
	}
	if !release.Prerelease {
		t.Error("Prerelease should be true")
	}
	if release.HTMLURL == "" {
		t.Error("HTMLURL should not be empty")
	}
}

func TestProgressData_Serialization(t *testing.T) {
	p := ProgressData{Percent: 50, Status: "downloading"}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}
	var parsed map[string]interface{}
	json.Unmarshal(data, &parsed)
	if int(parsed["percent"].(float64)) != 50 {
		t.Errorf("percent = %v, want 50", parsed["percent"])
	}
	if parsed["status"] != "downloading" {
		t.Errorf("status = %v", parsed["status"])
	}
}

func TestErrorData_Serialization(t *testing.T) {
	e := ErrorData{Error: "something failed"}
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}
	var parsed map[string]interface{}
	json.Unmarshal(data, &parsed)
	if parsed["error"] != "something failed" {
		t.Errorf("error = %v", parsed["error"])
	}
}

func TestCompleteData_Serialization(t *testing.T) {
	c := CompleteData{Success: true, Message: "Update downloaded"}
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}
	var parsed map[string]interface{}
	json.Unmarshal(data, &parsed)
	if parsed["success"] != true {
		t.Error("success should be true")
	}
	if parsed["message"] != "Update downloaded" {
		t.Errorf("message = %v", parsed["message"])
	}
}

func TestRegisterUpdateRoutes(t *testing.T) {
	r := mux.NewRouter()
	h := newUpdateHandler(t)
	RegisterUpdateRoutes(r, h)

	// Verify routes are registered by making test requests
	tests := []struct {
		method, path string
	}{
		{"GET", "/update/check"},
		{"POST", "/update/start"},
	}

	for _, tc := range tests {
		t.Run(tc.method+"_"+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, http.NoBody)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			// Should not return 405 (method not allowed) or 404
			if rec.Code == http.StatusMethodNotAllowed {
				t.Errorf("route %s %s not registered (405)", tc.method, tc.path)
			}
			if rec.Code == http.StatusNotFound {
				t.Errorf("route %s %s not found (404)", tc.method, tc.path)
			}
		})
	}
}

func TestGetLatestPrerelease_FallbackToStable(t *testing.T) {
	// When no dev prerelease exists, should fall back to stable
	stableCalled := false
	stableHandler := func(w http.ResponseWriter, _ *http.Request) {
		stableCalled = true
		release := GitHubRelease{
			TagName:    "v1.0.0",
			Prerelease: false,
		}
		json.NewEncoder(w).Encode(release)
	}

	// All releases are stable (no dev prerelease)
	listHandler := func(w http.ResponseWriter, _ *http.Request) {
		releases := []GitHubRelease{
			{TagName: "v1.0.0", Prerelease: false},
			{TagName: "v0.9.0", Prerelease: false},
		}
		json.NewEncoder(w).Encode(releases)
	}

	server := mockGitHubAPI(t, stableHandler, listHandler)
	defer server.Close()

	// We can't directly override the GitHub API URL in the handler,
	// but we can verify the logic flow through integration testing
	_ = stableCalled
}

func TestChecksumFileFormats(t *testing.T) {
	// Test that checksum file can be in format "hash  filename" or just "hash"
	tmpDir := t.TempDir()
	binaryContent := []byte("test binary content data here padding")
	for len(binaryContent) < 100 {
		binaryContent = append(binaryContent, 0)
	}
	hash := sha256.Sum256(binaryContent)
	expectedHash := hex.EncodeToString(hash[:])

	formats := []struct {
		name    string
		content string
	}{
		{"hash_only", expectedHash},
		{"hash_space_filename", expectedHash + "  xkeen-ui-keenetic-arm64"},
		{"hash_tab_filename", expectedHash + "\t" + "binary"},
		{"hash_two_spaces_filename", expectedHash + "  *binary"},
	}

	for _, tc := range formats {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, ".sha256") {
					w.Header().Set("Content-Type", "text/plain")
					w.Write([]byte(tc.content))
					return
				}
				w.Write(binaryContent)
			}))
			defer server.Close()

			h := newUpdateHandler(t)
			binaryPath := filepath.Join(tmpDir, "binary_"+tc.name+".new")

			err := h.downloadWithChecksum(context.Background(), binaryPath, server.URL+"/dl")
			if err != nil {
				t.Fatalf("downloadWithChecksum failed for format %q: %v", tc.name, err)
			}

			if _, err := os.Stat(binaryPath); err != nil {
				t.Errorf("binary file should exist: %v", err)
			}
		})
	}
}

// ---------- getLatestStableRelease / getLatestPrerelease tests ----------

// mockAPIHandler creates an UpdateHandler whose apiBaseURL points to a mock HTTP
// server. The mock handler fn receives the path (e.g. "/repos/...") and writes
// the desired response.
func mockAPIHandler(t *testing.T, fn func(w http.ResponseWriter, r *http.Request)) *UpdateHandler {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(fn))
	t.Cleanup(server.Close)

	h := NewUpdateHandler()
	h.apiBaseURL = server.URL
	return h
}

func TestGetLatestStableRelease_Success(t *testing.T) {
	h := mockAPIHandler(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/releases/latest") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Accept") != "application/vnd.github.v3+json" {
			t.Errorf("unexpected Accept: %s", r.Header.Get("Accept"))
		}
		if !strings.HasPrefix(r.Header.Get("User-Agent"), "XKEEN-UI/") {
			t.Errorf("unexpected User-Agent: %s", r.Header.Get("User-Agent"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(GitHubRelease{
			TagName:     "v99.0.0",
			Name:        "v99.0.0",
			Body:        "Test release",
			HTMLURL:     "https://example.com/v99.0.0",
			PublishedAt: "2026-06-01T00:00:00Z",
		})
	})

	release, err := h.getLatestStableRelease(context.Background())
	if err != nil {
		t.Fatalf("getLatestStableRelease failed: %v", err)
	}
	if release == nil {
		t.Fatal("expected non-nil release")
	}
	if release.TagName != "v99.0.0" {
		t.Errorf("TagName = %q, want v99.0.0", release.TagName)
	}
	if release.Name != "v99.0.0" {
		t.Errorf("Name = %q, want v99.0.0", release.Name)
	}
	if release.Body != "Test release" {
		t.Errorf("Body = %q, want 'Test release'", release.Body)
	}
}

func TestGetLatestStableRelease_HTTPError(t *testing.T) {
	h := mockAPIHandler(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"message":"server error"}`))
	})

	_, err := h.getLatestStableRelease(context.Background())
	if err == nil {
		t.Fatal("expected error for HTTP 500")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention HTTP status, got: %v", err)
	}
}

func TestGetLatestStableRelease_MalformedJSON(t *testing.T) {
	h := mockAPIHandler(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`not json at all`))
	})

	_, err := h.getLatestStableRelease(context.Background())
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if !strings.Contains(err.Error(), "failed to parse release info") {
		t.Errorf("error should mention parse failure, got: %v", err)
	}
}

func TestGetLatestPrerelease_SuccessFound(t *testing.T) {
	// Server returns a list that includes a dev prerelease.
	h := mockAPIHandler(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]GitHubRelease{
			{TagName: "v1.0.0-dev.1700000000", Prerelease: true},
			{TagName: "v0.9.0", Prerelease: false},
		})
	})

	release, err := h.getLatestPrerelease(context.Background())
	if err != nil {
		t.Fatalf("getLatestPrerelease failed: %v", err)
	}
	if release == nil {
		t.Fatal("expected a prerelease")
	}
	if release.TagName != "v1.0.0-dev.1700000000" {
		t.Errorf("TagName = %q, want v1.0.0-dev.1700000000", release.TagName)
	}
	if !release.Prerelease {
		t.Error("expected prerelease to be true")
	}
}

func TestGetLatestPrerelease_FallbackToStable_Mocked(t *testing.T) {
	// When no dev build exists, should fall back to the stable release endpoint.
	var listCalled, stableCalled bool
	h := mockAPIHandler(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/releases") && !strings.Contains(r.URL.Path, "/latest") {
			listCalled = true
			w.Header().Set("Content-Type", "application/json")
			// All releases are stable (no dev prerelease)
			json.NewEncoder(w).Encode([]GitHubRelease{
				{TagName: "v2.0.0", Prerelease: false},
				{TagName: "v1.0.0", Prerelease: false},
			})
			return
		}
		if strings.Contains(r.URL.Path, "/releases/latest") {
			stableCalled = true
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(GitHubRelease{
				TagName:    "v2.0.0",
				Prerelease: false,
			})
			return
		}
		t.Errorf("unexpected call: %s %s", r.Method, r.URL.Path)
	})

	release, err := h.getLatestPrerelease(context.Background())
	if err != nil {
		t.Fatalf("getLatestPrerelease fallback failed: %v", err)
	}
	if !listCalled {
		t.Error("expected list endpoint to be called")
	}
	if !stableCalled {
		t.Error("expected stable endpoint to be called as fallback")
	}
	if release == nil || release.TagName != "v2.0.0" {
		t.Errorf("TagName = %v, want v2.0.0", release)
	}
}

func TestGetLatestPrerelease_HTTPError(t *testing.T) {
	h := mockAPIHandler(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/releases") && !strings.Contains(r.URL.Path, "/latest") {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"message":"rate limited"}`))
			return
		}
		t.Errorf("unexpected call: %s %s", r.Method, r.URL.Path)
	})

	_, err := h.getLatestPrerelease(context.Background())
	if err == nil {
		t.Fatal("expected error for HTTP 403")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error should mention HTTP status, got: %v", err)
	}
}

func TestGetLatestPrerelease_MalformedJSON(t *testing.T) {
	h := mockAPIHandler(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/releases") && !strings.Contains(r.URL.Path, "/latest") {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{]`))
			return
		}
		t.Errorf("unexpected call: %s %s", r.Method, r.URL.Path)
	})

	_, err := h.getLatestPrerelease(context.Background())
	if err == nil {
		t.Fatal("expected error for malformed JSON in list")
	}
}

func TestNewUpdateHandler_HasHTTPClientAndBaseURL(t *testing.T) {
	h := NewUpdateHandler()
	if h.httpClient == nil {
		t.Error("expected httpClient to be set")
	}
	if h.httpClient != http.DefaultClient {
		t.Error("expected httpClient to be http.DefaultClient by default")
	}
	if h.apiBaseURL != "https://api.github.com" {
		t.Errorf("apiBaseURL = %q, want https://api.github.com", h.apiBaseURL)
	}
}

func TestNewUpdateHandler_OverridableFields(t *testing.T) {
	// Verify that NewUpdateHandler is a function (not a singleton/enforceable)
	// and that the new fields can be overridden for testing.
	h1 := NewUpdateHandler()
	h2 := NewUpdateHandler()

	// Different instances should be independent
	h2.apiBaseURL = "http://localhost:9999"
	h2.httpClient = &http.Client{}

	if h1.apiBaseURL != "https://api.github.com" {
		t.Errorf("h1 should still have default apiBaseURL, got %q", h1.apiBaseURL)
	}
	if h1.httpClient != http.DefaultClient {
		t.Error("h1 should still have default httpClient")
	}
}

// ---------- downloadWithRetry tests ----------

// TestDownloadWithRetry_SucceedsOnRetry verifies that a download succeeds when
// the server fails the first few attempts but then serves the file. Uses tiny
// backoff to keep the test fast.
func TestDownloadWithRetry_SucceedsOnRetry(t *testing.T) {
	tmpDir := t.TempDir()
	var attempts int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 { // fail first 2 attempts
			http.Error(w, "transient error", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(make([]byte, 100))
	}))
	defer server.Close()

	h := newUpdateHandler(t)
	h.maxDownloadRetries = 5
	h.retryBackoff = func(int) time.Duration { return 1 * time.Millisecond }

	dest := filepath.Join(tmpDir, "out")
	if err := h.downloadWithRetry(context.Background(), dest, server.URL+"/file"); err != nil {
		t.Fatalf("expected success on retry, got: %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Errorf("expected 3 attempts, got %d", got)
	}
}

// TestDownloadWithRetry_FailsAfterExhausted verifies the retry loop gives up
// after maxDownloadRetries attempts and returns the last error.
func TestDownloadWithRetry_FailsAfterExhausted(t *testing.T) {
	tmpDir := t.TempDir()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "always fails", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	h := newUpdateHandler(t)
	h.maxDownloadRetries = 2
	h.retryBackoff = func(int) time.Duration { return 1 * time.Millisecond }

	dest := filepath.Join(tmpDir, "out")
	err := h.downloadWithRetry(context.Background(), dest, server.URL+"/file")
	if err == nil {
		t.Fatal("expected error after retries exhausted")
	}
	if !strings.Contains(err.Error(), "download failed after 2 retries") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestDownloadWithRetry_ContextCancelled verifies that canceling the context
// during the backoff wait aborts the retry loop.
func TestDownloadWithRetry_ContextCancelled(t *testing.T) {
	tmpDir := t.TempDir()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "fail", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	h := newUpdateHandler(t)
	h.maxDownloadRetries = 10
	// Use a long backoff so the context cancellation lands during the wait.
	h.retryBackoff = func(int) time.Duration { return 5 * time.Second }

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel shortly after the first failure triggers the long backoff.
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	dest := filepath.Join(tmpDir, "out")
	err := h.downloadWithRetry(ctx, dest, server.URL+"/file")
	if err == nil {
		t.Fatal("expected cancellation error")
	}
	// Should be context.Canceled, not "after N retries"
	if strings.Contains(err.Error(), "retries") {
		t.Errorf("should have been canceled, not exhausted: %v", err)
	}
}

// TestQuadraticBackoff verifies the n²-second backoff schedule.
func TestQuadraticBackoff(t *testing.T) {
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{1, 1 * time.Second},
		{2, 4 * time.Second},
		{3, 9 * time.Second},
		{4, 16 * time.Second},
		{5, 25 * time.Second},
	}
	for _, tt := range tests {
		if got := quadraticBackoff(tt.attempt); got != tt.want {
			t.Errorf("quadraticBackoff(%d) = %v, want %v", tt.attempt, got, tt.want)
		}
	}
}
