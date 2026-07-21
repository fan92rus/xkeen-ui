package handlers

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/fan92rus/xkeen-ui/internal/version"
)

// checkInterval is how often the auto-update checker polls GitHub for releases.
const checkInterval = 10 * time.Minute

// UpdateChecker periodically checks GitHub for new STABLE releases and applies
// them automatically. It is controlled by the auto_update config flag and can be
// started/stopped at runtime via Start/Stop (wired to the settings toggle).
//
// Lifecycle:
//   - Start() launches a goroutine that ticks every checkInterval.
//   - The goroutine selects on ctx.Done() (toggle off) and ticker.C (tick).
//   - Stop() cancels the context and waits for the goroutine to exit.
//
// Only stable (non-prerelease) releases trigger an update. The update itself
// reuses the same download→verify→replace path as the manual handler, guarded
// by updateMu so the two can't overlap.
type UpdateChecker struct {
	handler *UpdateHandler

	// doUpdate applies an update for the given tag. Defaults to
	// handler.performAutoUpdate; overridable in tests to avoid exec/shutdown.
	doUpdate func(ctx context.Context, tag string) error

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu     sync.Mutex
	active bool
}

// NewUpdateChecker creates a checker bound to the given handler.
func NewUpdateChecker(h *UpdateHandler) *UpdateChecker {
	c := &UpdateChecker{handler: h}
	c.doUpdate = h.performAutoUpdate
	return c
}

// Start launches the background checker goroutine. It is a no-op if already
// running. Safe to call from the settings toggle handler.
func (c *UpdateChecker) Start() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active {
		return
	}
	c.ctx, c.cancel = context.WithCancel(context.Background())
	c.active = true
	c.wg.Add(1)
	go c.loop()
	log.Println("[auto-update] checker started (10m interval, stable releases only)")
}

// Stop signals the checker goroutine to exit and waits for it. It is a no-op if
// not running. The in-flight HTTP request (if any) is aborted via context
// cancellation; if the update script already launched it is too late to cancel
// (detached process) — but that's an edge case.
func (c *UpdateChecker) Stop() {
	c.mu.Lock()
	if !c.active {
		c.mu.Unlock()
		return
	}
	c.active = false
	if c.cancel != nil {
		c.cancel()
	}
	c.mu.Unlock()

	c.wg.Wait()
	log.Println("[auto-update] checker stopped")
}

// IsActive returns true if the checker goroutine is currently running.
func (c *UpdateChecker) IsActive() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.active
}

// loop is the background goroutine. It checks immediately on start, then every
// checkInterval. Exits when ctx is canceled.
func (c *UpdateChecker) loop() {
	defer c.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[auto-update] recovered panic in checker loop: %v", r)
		}
	}()

	// Check immediately on start (don't wait 10 min for first check).
	c.checkOnce(c.ctx)

	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.checkOnce(c.ctx)
		}
	}
}

// checkOnce queries GitHub for the latest stable release and, if newer than the
// running version, triggers an update.
func (c *UpdateChecker) checkOnce(ctx context.Context) {
	current := version.GetVersion()

	release, err := c.handler.getLatestStableRelease(ctx)
	if err != nil {
		log.Printf("[auto-update] check failed: %v", err)
		return
	}

	// Only stable releases (prereleases are always skipped).
	if release.Prerelease {
		log.Printf("[auto-update] latest release %s is a prerelease, skipping", release.TagName)
		return
	}

	if c.handler.compareVersions(current, release.TagName) >= 0 {
		log.Printf("[auto-update] up to date (%s)", current)
		return
	}

	log.Printf("[auto-update] new stable version %s available (current %s), updating...",
		release.TagName, current)

	if err := c.doUpdate(ctx, release.TagName); err != nil {
		log.Printf("[auto-update] update to %s failed: %v", release.TagName, err)
		return
	}

	// performAutoUpdate launches the update script and signals shutdown; if we
	// reach here without error the process will terminate shortly.
	log.Printf("[auto-update] update to %s applied, restarting...", release.TagName)
}

// performAutoUpdate downloads the binary for the given tag, verifies it, and
// launches the update script (which replaces the binary and restarts the
// service). It reuses the same download path (with retries) as the manual
// handler. The updateMu lock prevents overlap with a concurrent manual update.
//
// Unlike StartUpdate (the HTTP handler), this does NOT send SSE events — it
// logs progress instead.
func (h *UpdateHandler) performAutoUpdate(ctx context.Context, tag string) error {
	// Acquire the update lock; bail out if a manual update is in progress.
	if !h.updateMu.TryLock() {
		return fmt.Errorf("another update is already in progress")
	}
	defer h.updateMu.Unlock()

	tmpFile := "/tmp/" + h.binaryName + ".new"
	downloadURL := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s",
		h.githubRepo, tag, h.binaryName)

	log.Printf("[auto-update] downloading %s ...", downloadURL)
	if err := h.downloadWithChecksum(ctx, tmpFile, downloadURL); err != nil {
		return fmt.Errorf("download: %w", err)
	}

	// Set permissions
	if err := os.Chmod(tmpFile, 0o755); err != nil { //nolint:gosec // binary needs execute permission
		_ = os.Remove(tmpFile)
		return fmt.Errorf("chmod: %w", err)
	}

	// Verify file size (guard against truncated downloads)
	info, err := os.Stat(tmpFile)
	if err != nil {
		_ = os.Remove(tmpFile)
		return fmt.Errorf("stat: %w", err)
	}
	if info.Size() < minBinarySize {
		_ = os.Remove(tmpFile)
		return fmt.Errorf("downloaded file too small: %d bytes", info.Size())
	}

	// Launch the detached update script. It waits for this PID to exit, then
	// replaces the binary and restarts the service.
	currentPID := os.Getpid()
	shellCmd := fmt.Sprintf("(sh %s %s %d </dev/null >>/opt/var/log/xkeen-ui.log 2>&1 &)",
		h.updateScript, h.binaryName, currentPID)
	if err := exec.Command("sh", "-c", shellCmd).Run(); err != nil {
		_ = os.Remove(tmpFile)
		return fmt.Errorf("update script: %w", err)
	}

	log.Printf("[auto-update] update script started, process %d will terminate", currentPID)

	// Signal the main goroutine to shut down so the script can replace the binary.
	select {
	case UpdateShutdownCh <- struct{}{}:
	default:
	}
	return nil
}
