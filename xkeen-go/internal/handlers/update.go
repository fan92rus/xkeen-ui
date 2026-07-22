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
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"

	"github.com/fan92rus/xkeen-ui/internal/utils"
	"github.com/fan92rus/xkeen-ui/internal/version"
)

// UpdateShutdownCh is used to signal main goroutine that the update
// has completed and the server should perform a graceful shutdown
// before the binary is replaced.
// minBinarySize is the minimum acceptable binary size (2MB) for update verification.
// Uncompressed Go binary with embedded frontend is ~6.4MB; UPX-compressed is ~1.9MB.
const minBinarySize = 2_000_000

// UpdateShutdownCh is used to signal the main process to shut down for updates.
var UpdateShutdownCh = make(chan struct{}, 1)

// UpdateHandler handles application update operations.
type UpdateHandler struct {
	githubRepo   string
	binaryName   string
	installPath  string
	initScript   string
	updateScript string
	downloadURL  string

	httpClient        *http.Client
	apiBaseURL        string
	maxDownloadRetries int
	retryBackoff      func(attempt int) time.Duration

	mu            sync.Mutex
	devReleaseTag string
	updateMu      sync.Mutex

	branchesCache   *BranchListResponse
	branchesCachedAt time.Time
	branchesCacheMu sync.Mutex
}

// NewUpdateHandler creates a new UpdateHandler.
func NewUpdateHandler() *UpdateHandler {
	repo := "fan92rus/xkeen-ui"
	binaryName := utils.GetBinaryNameForArch()
	return &UpdateHandler{
		githubRepo:         repo,
		binaryName:         binaryName,
		installPath:        "/opt/bin/" + binaryName,
		initScript:         "/opt/etc/init.d/xkeen-ui",
		updateScript:       "/opt/etc/xkeen-ui/update.sh",
		downloadURL:        fmt.Sprintf("https://github.com/%s/releases/latest/download/%s", repo, binaryName),
		httpClient:         http.DefaultClient,
		apiBaseURL:         "https://api.github.com",
		maxDownloadRetries: 5,
		retryBackoff:       quadraticBackoff,
	}
}

func quadraticBackoff(attempt int) time.Duration {
	n := int64(attempt)
	return time.Duration(n*n) * time.Second
}

// ── Types ──

type GitHubRelease struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	Body        string `json:"body"`
	HTMLURL     string `json:"html_url"`
	PublishedAt string `json:"published_at"`
	Prerelease  bool   `json:"prerelease"`
}

type CheckUpdateResponse struct {
	CurrentVersion  string `json:"current_version"`
	LatestVersion   string `json:"latest_version"`
	UpdateAvailable bool   `json:"update_available"`
	IsPrerelease    bool   `json:"is_prerelease"`
	ReleaseURL      string `json:"release_url,omitempty"`
	ReleaseNotes    string `json:"release_notes,omitempty"`
	Architecture    string `json:"architecture"`
	BinaryName      string `json:"binary_name"`
	Branch          string `json:"branch,omitempty"`
	Error           string `json:"error,omitempty"`
}

type BranchInfo struct {
	Name            string `json:"name"`
	LatestVersion   string `json:"latest_version"`
	UpdateAvailable bool   `json:"update_available"`
	ReleaseURL      string `json:"release_url"`
	PublishedAt     string `json:"published_at"`
}

type BranchListResponse struct {
	CurrentBranch string       `json:"current_branch"`
	Branches      []BranchInfo `json:"branches"`
	Error         string       `json:"error,omitempty"`
}

// ── CheckUpdate ──

// CheckUpdate checks GitHub for the latest release.
// GET /api/update/check?prerelease=true&branch=feat/routing-ui
func (h *UpdateHandler) CheckUpdate(w http.ResponseWriter, r *http.Request) {
	currentVersion := version.GetVersion()
	checkPrerelease := r.URL.Query().Get("prerelease") == "true"
	branch := r.URL.Query().Get("branch")

	var release *GitHubRelease
	var err error

	if branch != "" {
		release, err = h.getLatestForBranch(r.Context(), branch)
	} else if checkPrerelease {
		release, err = h.getLatestPrerelease(r.Context())
	} else {
		release, err = h.getLatestStableRelease(r.Context())
	}

	if err != nil {
		respondJSON(w, http.StatusOK, CheckUpdateResponse{
			CurrentVersion: currentVersion,
			Architecture:   runtime.GOARCH,
			BinaryName:     h.binaryName,
			Error:          err.Error(),
			Branch:         branch,
		})
		return
	}

	updateAvailable := h.compareVersions(currentVersion, release.TagName) < 0

	if checkPrerelease && updateAvailable {
		h.setDevReleaseTag(release.TagName)
	}

	respondJSON(w, http.StatusOK, CheckUpdateResponse{
		CurrentVersion:  currentVersion,
		LatestVersion:   release.TagName,
		UpdateAvailable: updateAvailable,
		IsPrerelease:    release.Prerelease,
		ReleaseURL:      release.HTMLURL,
		ReleaseNotes:    release.Body,
		Architecture:    runtime.GOARCH,
		BinaryName:      h.binaryName,
		Branch:          branch,
	})
}

// ── CheckBranches ──

// CheckBranches lists all branches with available dev builds.
// GET /api/update/branches
func (h *UpdateHandler) CheckBranches(w http.ResponseWriter, r *http.Request) {
	resp := h.getBranchesCached()
	respondJSON(w, http.StatusOK, resp)
}

func (h *UpdateHandler) getBranchesCached() *BranchListResponse {
	h.branchesCacheMu.Lock()
	defer h.branchesCacheMu.Unlock()

	if h.branchesCache != nil && time.Since(h.branchesCachedAt) < 60*time.Second {
		return h.branchesCache
	}

	resp := h.fetchBranches()
	h.branchesCache = resp
	h.branchesCachedAt = time.Now()
	return resp
}

func (h *UpdateHandler) fetchBranches() *BranchListResponse {
	releases, err := h.fetchAllReleases(context.Background())
	if err != nil {
		return &BranchListResponse{
			CurrentBranch: version.GetBuildBranch(),
			Error:         err.Error(),
		}
	}

	// Group releases by branch, keep latest per branch
	branchMap := make(map[string]*GitHubRelease)
	for i := range releases {
		if !releases[i].Prerelease || !strings.Contains(releases[i].TagName, "-dev.") {
			continue
		}
		br := extractBranchFromRelease(releases[i])
		if _, ok := branchMap[br]; ok {
			// Keep the newer one (GitHub returns newest first, so first match wins)
			continue
		}
		branchMap[br] = &releases[i]
	}

	// Collect feature branches (non-master) and master separately
	var featureBranches []BranchInfo
	var masterBranch *BranchInfo

	for name, rel := range branchMap {
		bi := BranchInfo{
			Name:          name,
			LatestVersion: rel.TagName,
			ReleaseURL:    rel.HTMLURL,
			PublishedAt:   rel.PublishedAt,
		}
		if name == "master" {
			masterBranch = &bi
		} else {
			featureBranches = append(featureBranches, bi)
		}
	}

	// Sort feature branches alphabetically
	for i := 1; i < len(featureBranches); i++ {
		for j := i; j > 0 && featureBranches[j].Name < featureBranches[j-1].Name; j-- {
			featureBranches[j], featureBranches[j-1] = featureBranches[j-1], featureBranches[j]
		}
	}

	// Prepend master to the sorted feature branches
	branches := make([]BranchInfo, 0, len(branchMap))
	if masterBranch != nil {
		branches = append(branches, *masterBranch)
	}
	branches = append(branches, featureBranches...)

	return &BranchListResponse{
		CurrentBranch: version.GetBuildBranch(),
		Branches:      branches,
	}
}

// ── GitHub API calls ──

func (h *UpdateHandler) getLatestStableRelease(ctx context.Context) (*GitHubRelease, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/repos/%s/releases/latest", h.apiBaseURL, h.githubRepo), http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "XKEEN-UI/"+version.GetVersion())

	resp, err := h.httpClient.Do(req)
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

func (h *UpdateHandler) getLatestPrerelease(ctx context.Context) (*GitHubRelease, error) {
	return h.getLatestForBranch(ctx, "")
}

// getLatestForBranch fetches the latest prerelease for a specific branch.
// If branch is empty, returns the latest prerelease regardless of branch.
func (h *UpdateHandler) getLatestForBranch(ctx context.Context, branch string) (*GitHubRelease, error) {
	releases, err := h.fetchAllReleases(ctx)
	if err != nil {
		return nil, err
	}

	for i := range releases {
		if !releases[i].Prerelease || !strings.Contains(releases[i].TagName, "-dev.") {
			continue
		}
		if branch == "" {
			return &releases[i], nil
		}
		if extractBranchFromRelease(releases[i]) == branch {
			return &releases[i], nil
		}
	}

	// No matching prerelease found, try stable
	return h.getLatestStableRelease(ctx)
}

func (h *UpdateHandler) fetchAllReleases(ctx context.Context) ([]GitHubRelease, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/repos/%s/releases?per_page=100", h.apiBaseURL, h.githubRepo), http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "XKEEN-UI/"+version.GetVersion())

	resp, err := h.httpClient.Do(req)
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
	return releases, nil
}

// extractBranchFromRelease extracts branch name from a release.
// Priority: release body (| Branch | xxx |) → tag name parsing → "master".
func extractBranchFromRelease(release GitHubRelease) string {
	// Try release body first
	for _, line := range strings.Split(release.Body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "| Branch |") {
			parts := strings.Split(line, "|")
			if len(parts) >= 3 {
				branch := strings.TrimSpace(parts[2])
				if branch != "" {
					return branch
				}
			}
		}
	}

	// Fallback: parse tag v{base}-dev.{sanitized-branch}.{ts}
	tag := release.TagName
	if !strings.Contains(tag, "-dev.") {
		return "master"
	}
	parts := strings.SplitAfterN(tag, "-dev.", 2)
	if len(parts) < 2 {
		return "master"
	}
	suffix := parts[1] // "feat-routing-ui.1724921234" or "1724921234"
	sub := strings.Split(suffix, ".")
	if len(sub) < 2 {
		return "master"
	}
	// If exactly 2 parts and first is numeric (timestamp), it's master
	if len(sub) == 2 {
		if _, err := strconv.ParseInt(sub[0], 10, 64); err == nil {
			return "master"
		}
	}
	// Branch name = everything except the last part (timestamp)
	return strings.Join(sub[:len(sub)-1], ".")
}

// ── Version comparison ──

func (h *UpdateHandler) compareVersions(v1, v2 string) int {
	v1 = strings.TrimPrefix(v1, "v")
	v2 = strings.TrimPrefix(v2, "v")

	base1, pre1 := splitPreRelease(v1)
	base2, pre2 := splitPreRelease(v2)

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

	if pre1 != "" && pre2 == "" {
		return -1
	}
	if pre1 == "" && pre2 != "" {
		return 1
	}
	if pre1 != "" && pre2 != "" {
		return comparePrereleaseSuffixes(pre1, pre2)
	}
	return 0
}

func splitPreRelease(v string) (base, pre string) {
	idx := strings.Index(v, "-")
	if idx == -1 {
		return v, ""
	}
	return v[:idx], v[idx+1:]
}

func comparePrereleaseSuffixes(p1, p2 string) int {
	ts1 := extractTimestamp(p1)
	ts2 := extractTimestamp(p2)
	if ts1 < ts2 {
		return -1
	} else if ts1 > ts2 {
		return 1
	}
	return 0
}

func extractTimestamp(pre string) int64 {
	parts := strings.Split(pre, ".")
	if len(parts) == 0 {
		return 0
	}
	ts, _ := strconv.ParseInt(parts[len(parts)-1], 10, 64)
	return ts
}

// ── Progress / StartUpdate ──

type ProgressData struct {
	Percent int    `json:"percent"`
	Status  string `json:"status"`
}

type ErrorData struct {
	Error string `json:"error"`
}

type CompleteData struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// StartUpdate starts the update process with SSE progress.
// POST /api/update/start?prerelease=true&branch=feat/routing-ui
func (h *UpdateHandler) StartUpdate(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), SSEStreamTimeout)
	defer cancel()

	sse, ok := NewSSEWriter(w, "Access-Control-Allow-Origin: *")
	if !ok {
		return
	}

	sendEvent := func(event string, data interface{}) {
		if sse.Send(event, data) != nil {
			cancel()
		}
	}

	prerelease := r.URL.Query().Get("prerelease") == "true"

	downloadURL := h.downloadURL
	if prerelease {
		tag := h.getDevReleaseTag()
		if tag != "" {
			downloadURL = fmt.Sprintf("https://github.com/%s/releases/download/%s/%s",
				h.githubRepo, tag, h.binaryName)
		}
	}

	if !h.updateMu.TryLock() {
		sendEvent("error", ErrorData{Error: "another update is already in progress"})
		return
	}
	defer h.updateMu.Unlock()

	// Step 1: Download and verify checksum
	sendEvent("progress", ProgressData{Percent: 5, Status: "downloading"})

	tmpFile := "/tmp/" + h.binaryName + ".new"
	if err := h.downloadWithChecksum(ctx, tmpFile, downloadURL); err != nil {
		sendEvent("error", ErrorData{Error: fmt.Sprintf("Download/verification failed: %v", err)})
		return
	}

	sendEvent("progress", ProgressData{Percent: 40, Status: "download complete, checksum verified"})

	// Step 2: Set permissions
	sendEvent("progress", ProgressData{Percent: 45, Status: "setting permissions"})
	//nolint:gosec
	if err := os.Chmod(tmpFile, 0o755); err != nil {
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
	if info.Size() < minBinarySize {
		sendEvent("error", ErrorData{Error: "Downloaded file too small, likely corrupted"})
		return
	}

	sendEvent("progress", ProgressData{Percent: 60, Status: "verified"})

	// Step 4: Launch update script in background
	sendEvent("progress", ProgressData{Percent: 70, Status: "preparing update"})

	currentPID := os.Getpid()
	shellCmd := fmt.Sprintf("(sh %s %s %d </dev/null >>/opt/var/log/xkeen-ui.log 2>&1 &)", h.updateScript, h.binaryName, currentPID)
	updateCmd := exec.Command("sh", "-c", shellCmd)
	if err := updateCmd.Run(); err != nil {
		_ = os.Remove(tmpFile)
		sendEvent("error", ErrorData{Error: fmt.Sprintf("Failed to start update script: %v", err)})
		return
	}

	log.Printf("Update script started, current process %d will terminate", currentPID)

	sendEvent("progress", ProgressData{Percent: 90, Status: "restarting"})
	sendEvent("complete", CompleteData{
		Success: true,
		Message: "Update downloaded. Service is restarting...",
	})

	go func() {
		time.Sleep(1 * time.Second)
		log.Printf("Update: graceful shutdown requested")
		select {
		case UpdateShutdownCh <- struct{}{}:
		default:
		}
	}()
}

// ── Download helpers ──

func (h *UpdateHandler) downloadFile(ctx context.Context, path, url string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, http.NoBody)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "XKEEN-UI/"+version.GetVersion())

	resp, err := h.httpClient.Do(req)
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
	defer func() { _ = out.Close() }()

	_, err = io.Copy(out, resp.Body)
	return err
}

func (h *UpdateHandler) downloadWithChecksum(ctx context.Context, binaryPath, binaryURL string) error {
	if err := h.downloadWithRetry(ctx, binaryPath, binaryURL); err != nil {
		return fmt.Errorf("failed to download binary: %w", err)
	}

	checksumURL := binaryURL + ".sha256"
	checksumPath := binaryPath + ".sha256"

	checksumErr := h.downloadFile(ctx, checksumPath, checksumURL)
	if checksumErr != nil {
		log.Printf("WARNING: Checksum file not available: %v", checksumErr)
		log.Printf("WARNING: Skipping checksum verification (downloaded from HTTPS)")
		return nil
	}

	expectedChecksumBytes, err := os.ReadFile(checksumPath)
	if err != nil {
		return fmt.Errorf("failed to read checksum file: %w", err)
	}
	expectedChecksum := strings.TrimSpace(string(expectedChecksumBytes))
	if parts := strings.Fields(expectedChecksum); len(parts) > 0 {
		expectedChecksum = parts[0]
	}

	binaryData, err := os.ReadFile(binaryPath)
	if err != nil {
		return fmt.Errorf("failed to read binary for checksum: %w", err)
	}
	hash := sha256.Sum256(binaryData)
	actualChecksum := hex.EncodeToString(hash[:])

	if subtle.ConstantTimeCompare([]byte(expectedChecksum), []byte(actualChecksum)) != 1 {
		_ = os.Remove(binaryPath)
		_ = os.Remove(checksumPath)
		return fmt.Errorf("checksum verification failed: expected %s, got %s", expectedChecksum, actualChecksum)
	}

	log.Printf("Checksum verified successfully: %s", actualChecksum[:16]+"...")
	_ = os.Remove(checksumPath)
	return nil
}

func (h *UpdateHandler) downloadWithRetry(ctx context.Context, path, url string) error {
	var lastErr error
	for attempt := 0; attempt <= h.maxDownloadRetries; attempt++ {
		if attempt > 0 {
			wait := h.retryBackoff(attempt)
			log.Printf("[update] download retry %d/%d after %v: %v", attempt, h.maxDownloadRetries, wait, lastErr)
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		_ = os.Remove(path)
		if err := h.downloadFile(ctx, path, url); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return fmt.Errorf("download failed after %d retries: %w", h.maxDownloadRetries, lastErr)
}

func (h *UpdateHandler) setDevReleaseTag(tag string) {
	h.mu.Lock()
	h.devReleaseTag = tag
	h.mu.Unlock()
}

func (h *UpdateHandler) getDevReleaseTag() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.devReleaseTag
}

// RegisterUpdateRoutes registers update-related routes.
func RegisterUpdateRoutes(r *mux.Router, handler *UpdateHandler) {
	r.HandleFunc("/update/check", handler.CheckUpdate).Methods("GET")
	r.HandleFunc("/update/start", handler.StartUpdate).Methods("POST")
	r.HandleFunc("/update/branches", handler.CheckBranches).Methods("GET")
}
