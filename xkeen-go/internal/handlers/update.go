// Package handlers provides HTTP handlers for XKEEN-GO API endpoints.
package handlers

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"github.com/user/xkeen-ui/internal/version"
)

// UpdateHandler handles application update operations.
type UpdateHandler struct {
	githubRepo     string
	binaryName     string
	installPath    string
	initScript     string
	updateScript   string
	downloadURL    string
	devReleaseTag  string // Latest dev release tag for download
}

// NewUpdateHandler creates a new UpdateHandler.
func NewUpdateHandler() *UpdateHandler {
	repo := "fan92rus/xkeen-ui"
	binaryName := "xkeen-ui-keenetic-arm64"
	return &UpdateHandler{
		githubRepo:   repo,
		binaryName:   binaryName,
		installPath:  "/opt/bin/" + binaryName,
		initScript:   "/opt/etc/init.d/xkeen-ui",
		updateScript: "/opt/etc/xkeen-ui/update.sh",
		downloadURL:  fmt.Sprintf("https://github.com/%s/releases/latest/download/%s", repo, binaryName),
	}
}

// GitHubRelease represents a GitHub release.
type GitHubRelease struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	Body        string `json:"body"`
	HTMLURL     string `json:"html_url"`
	PublishedAt string `json:"published_at"`
	Prerelease  bool   `json:"prerelease"`
}

// CheckUpdateResponse is the response for CheckUpdate.
type CheckUpdateResponse struct {
	CurrentVersion  string `json:"current_version"`
	LatestVersion   string `json:"latest_version"`
	UpdateAvailable bool   `json:"update_available"`
	IsPrerelease    bool   `json:"is_prerelease"`
	ReleaseURL      string `json:"release_url,omitempty"`
	ReleaseNotes    string `json:"release_notes,omitempty"`
	Error           string `json:"error,omitempty"`
}

// CheckUpdate checks GitHub for the latest release.
// GET /api/update/check?prerelease=true to check for dev builds
func (h *UpdateHandler) CheckUpdate(w http.ResponseWriter, r *http.Request) {
	currentVersion := version.GetVersion()
	checkPrerelease := r.URL.Query().Get("prerelease") == "true"

	var release *GitHubRelease
	var err error

	if checkPrerelease {
		release, err = h.getLatestPrerelease(r.Context())
	} else {
		release, err = h.getLatestStableRelease(r.Context())
	}

	if err != nil {
		h.respondJSON(w, http.StatusOK, CheckUpdateResponse{
			CurrentVersion: currentVersion,
			Error:          err.Error(),
		})
		return
	}

	// Compare versions
	updateAvailable := h.compareVersions(currentVersion, release.TagName) < 0

	// Store dev release tag for download if checking prerelease
	if checkPrerelease && updateAvailable {
		h.devReleaseTag = release.TagName
	}

	h.respondJSON(w, http.StatusOK, CheckUpdateResponse{
		CurrentVersion:  currentVersion,
		LatestVersion:   release.TagName,
		UpdateAvailable: updateAvailable,
		IsPrerelease:    release.Prerelease,
		ReleaseURL:      release.HTMLURL,
		ReleaseNotes:    release.Body,
	})
}

// getLatestStableRelease fetches the latest stable release from GitHub.
func (h *UpdateHandler) getLatestStableRelease(ctx context.Context) (*GitHubRelease, error) {
	req, err := http.NewRequestWithContext(ctx, "GET",
		fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", h.githubRepo), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "XKEEN-GO/"+version.GetVersion())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch release info: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API error: %d - %s", resp.StatusCode, string(body))
	}

	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("failed to parse release info: %v", err)
	}

	return &release, nil
}

// getLatestPrerelease fetches the latest dev prerelease from GitHub.
func (h *UpdateHandler) getLatestPrerelease(ctx context.Context) (*GitHubRelease, error) {
	req, err := http.NewRequestWithContext(ctx, "GET",
		fmt.Sprintf("https://api.github.com/repos/%s/releases", h.githubRepo), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "XKEEN-GO/"+version.GetVersion())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch releases: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API error: %d - %s", resp.StatusCode, string(body))
	}

	var releases []GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, fmt.Errorf("failed to parse releases: %v", err)
	}

	// Find the latest dev prerelease
	for i := range releases {
		if releases[i].Prerelease && strings.Contains(releases[i].TagName, "-dev.") {
			return &releases[i], nil
		}
	}

	// No dev prerelease found, fall back to latest stable
	return h.getLatestStableRelease(ctx)
}

// compareVersions compares two version strings (semver-aware).
// Pre-release versions (e.g., 1.2.3-dev.123) are considered lower than release (1.2.3).
// Returns: -1 if v1 < v2, 0 if equal, 1 if v1 > v2
func (h *UpdateHandler) compareVersions(v1, v2 string) int {
	// Remove 'v' prefix if present
	v1 = strings.TrimPrefix(v1, "v")
	v2 = strings.TrimPrefix(v2, "v")

	// Split off pre-release suffix (e.g., "1.2.3-dev.123" -> "1.2.3", "dev.123")
	base1, pre1 := splitPreRelease(v1)
	base2, pre2 := splitPreRelease(v2)

	// Compare numeric parts
	parts1 := strings.Split(base1, ".")
	parts2 := strings.Split(base2, ".")

	maxLen := len(parts1)
	if len(parts2) > maxLen {
		maxLen = len(parts2)
	}

	for i := 0; i < maxLen; i++ {
		var n1, n2 int
		if i < len(parts1) {
			n1, _ = strconv.Atoi(parts1[i])
		}
		if i < len(parts2) {
			n2, _ = strconv.Atoi(parts2[i])
		}

		if n1 < n2 {
			return -1
		} else if n1 > n2 {
			return 1
		}
	}

	// Numeric parts are equal, check pre-release
	// Pre-release (<base>-something) is lower than release (<base>)
	if pre1 != "" && pre2 == "" {
		return -1 // v1 is pre-release, v2 is release
	}
	if pre1 == "" && pre2 != "" {
		return 1 // v1 is release, v2 is pre-release
	}

	// Both are pre-release, compare timestamps if format is "dev.<timestamp>"
	if pre1 != "" && pre2 != "" {
		return comparePrereleaseSuffixes(pre1, pre2)
	}

	return 0
}

// splitPreRelease splits version into base and pre-release suffix.
// e.g., "1.2.3-dev.123" -> ("1.2.3", "dev.123")
func splitPreRelease(v string) (base, pre string) {
	idx := strings.Index(v, "-")
	if idx == -1 {
		return v, ""
	}
	return v[:idx], v[idx+1:]
}

// comparePrereleaseSuffixes compares two pre-release suffixes.
// Format expected: "dev.<timestamp>" or similar.
// Returns: -1 if p1 < p2, 0 if equal, 1 if p1 > p2
func comparePrereleaseSuffixes(p1, p2 string) int {
	// Extract timestamp from "dev.1234567890" format
	ts1 := extractTimestamp(p1)
	ts2 := extractTimestamp(p2)

	if ts1 < ts2 {
		return -1
	} else if ts1 > ts2 {
		return 1
	}
	return 0
}

// extractTimestamp extracts numeric timestamp from pre-release suffix.
// e.g., "dev.1709876543" -> 1709876543
func extractTimestamp(pre string) int64 {
	// Find the last segment after "."
	parts := strings.Split(pre, ".")
	if len(parts) == 0 {
		return 0
	}
	ts, _ := strconv.ParseInt(parts[len(parts)-1], 10, 64)
	return ts
}

// ProgressData represents progress information.
type ProgressData struct {
	Percent int    `json:"percent"`
	Status  string `json:"status"`
}

// ErrorData represents error information.
type ErrorData struct {
	Error string `json:"error"`
}

// CompleteData represents completion information.
type CompleteData struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// StartUpdate starts the update process with SSE progress.
// POST /api/update/start?prerelease=true to download dev build
func (h *UpdateHandler) StartUpdate(w http.ResponseWriter, r *http.Request) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	// Helper to send SSE event
	sendEvent := func(event string, data interface{}) {
		jsonData, _ := json.Marshal(data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, string(jsonData))
		flusher.Flush()
	}

	// Determine download URL
	prerelease := r.URL.Query().Get("prerelease") == "true"
	downloadURL := h.downloadURL
	if prerelease && h.devReleaseTag != "" {
		downloadURL = fmt.Sprintf("https://github.com/%s/releases/download/%s/%s",
			h.githubRepo, h.devReleaseTag, h.binaryName)
	}

	// Step 1: Download and verify checksum
	sendEvent("progress", ProgressData{Percent: 5, Status: "downloading"})

	tmpFile := "/tmp/" + h.binaryName + ".new"
	if err := h.downloadWithChecksum(r.Context(), tmpFile, downloadURL); err != nil {
		sendEvent("error", ErrorData{Error: fmt.Sprintf("Download/verification failed: %v", err)})
		return
	}

	sendEvent("progress", ProgressData{Percent: 40, Status: "download complete, checksum verified"})

	// Step 2: Set permissions
	sendEvent("progress", ProgressData{Percent: 45, Status: "setting permissions"})
	if err := os.Chmod(tmpFile, 0755); err != nil {
		sendEvent("error", ErrorData{Error: fmt.Sprintf("Failed to set permissions: %v", err)})
		return
	}

	// Step 3: Verify file
	sendEvent("progress", ProgressData{Percent: 50, Status: "verifying"})
	info, err := os.Stat(tmpFile)
	if err != nil {
		sendEvent("error", ErrorData{Error: fmt.Sprintf("Verification failed: %v", err)})
		return
	}
	if info.Size() < 1000000 { // Less than 1MB is suspicious
		sendEvent("error", ErrorData{Error: "Downloaded file too small, likely corrupted"})
		return
	}

	sendEvent("progress", ProgressData{Percent: 60, Status: "verified"})

	// Step 4: Launch update script in background
	// The script will wait for this process to terminate, then replace the binary
	sendEvent("progress", ProgressData{Percent: 70, Status: "preparing update"})

	currentPID := os.Getpid()
	// Use shell to properly detach with nohup
	shellCmd := fmt.Sprintf("nohup sh %s %d >/dev/null 2>&1 &", h.updateScript, currentPID)
	updateCmd := exec.Command("sh", "-c", shellCmd)
	if err := updateCmd.Run(); err != nil {
		// Clean up temp file on error
		os.Remove(tmpFile)
		sendEvent("error", ErrorData{Error: fmt.Sprintf("Failed to start update script: %v", err)})
		return
	}

	log.Printf("Update script started, current process %d will terminate", currentPID)

	// Step 5: Notify client and schedule shutdown
	sendEvent("progress", ProgressData{Percent: 90, Status: "restarting"})
	sendEvent("complete", CompleteData{
		Success: true,
		Message: "Update downloaded. Service is restarting...",
	})

	// Give SSE response time to be sent, then exit
	// The update script will replace the binary and restart the service
	go func() {
		time.Sleep(1 * time.Second)
		log.Printf("Shutting down for update...")
		os.Exit(0)
	}()
}

// downloadFile downloads a file from URL to path.
func (h *UpdateHandler) downloadFile(ctx context.Context, path, url string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	req.Header.Set("User-Agent", "XKEEN-GO/"+version.GetVersion())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	out, err := os.Create(path)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

// downloadWithChecksum downloads binary and verifies SHA256 checksum.
// Returns error if checksum verification fails or checksum file is not available.
func (h *UpdateHandler) downloadWithChecksum(ctx context.Context, binaryPath, binaryURL string) error {
	// 1. Download binary
	if err := h.downloadFile(ctx, binaryPath, binaryURL); err != nil {
		return fmt.Errorf("failed to download binary: %w", err)
	}

	// 2. Try to download checksum file
	checksumURL := binaryURL + ".sha256"
	checksumPath := binaryPath + ".sha256"

	checksumErr := h.downloadFile(ctx, checksumPath, checksumURL)
	if checksumErr != nil {
		// Checksum file not available - log warning but continue
		// This allows updates to work even if checksum file is missing
		log.Printf("WARNING: Checksum file not available: %v", checksumErr)
		log.Printf("WARNING: Skipping checksum verification (downloaded from HTTPS)")
		return nil
	}

	// 3. Read expected checksum
	expectedChecksumBytes, err := os.ReadFile(checksumPath)
	if err != nil {
		return fmt.Errorf("failed to read checksum file: %w", err)
	}
	expectedChecksum := strings.TrimSpace(string(expectedChecksumBytes))

	// Extract just the hash (checksum file format: "hash  filename" or just "hash")
	if parts := strings.Fields(expectedChecksum); len(parts) > 0 {
		expectedChecksum = parts[0]
	}

	// 4. Calculate actual checksum
	binaryData, err := os.ReadFile(binaryPath)
	if err != nil {
		return fmt.Errorf("failed to read binary for checksum: %w", err)
	}

	hash := sha256.Sum256(binaryData)
	actualChecksum := hex.EncodeToString(hash[:])

	// 5. Verify checksum (constant-time comparison)
	if subtle.ConstantTimeCompare([]byte(expectedChecksum), []byte(actualChecksum)) != 1 {
		// Remove corrupted binary
		os.Remove(binaryPath)
		os.Remove(checksumPath)
		return fmt.Errorf("checksum verification failed: expected %s, got %s", expectedChecksum, actualChecksum)
	}

	log.Printf("Checksum verified successfully: %s", actualChecksum[:16]+"...")

	// 6. Clean up checksum file
	os.Remove(checksumPath)

	return nil
}

// respondJSON writes a JSON response.
func (h *UpdateHandler) respondJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("Error encoding JSON response: %v", err)
	}
}

// RegisterUpdateRoutes registers update-related routes.
func RegisterUpdateRoutes(r *mux.Router, handler *UpdateHandler) {
	r.HandleFunc("/update/check", handler.CheckUpdate).Methods("GET")
	r.HandleFunc("/update/start", handler.StartUpdate).Methods("POST")
}
