package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fan92rus/xkeen-ui/internal/version"
)

// mockStableReleaseAPI returns a httptest server whose /releases/latest
// endpoint serves a release with the given tag and prerelease flag.
func mockStableReleaseAPI(t *testing.T, tag string, prerelease bool) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/fan92rus/xkeen-ui/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(GitHubRelease{
			TagName:    tag,
			Name:       tag,
			Prerelease: prerelease,
			HTMLURL:    "https://example.com/release",
		})
	})
	return httptest.NewServer(mux)
}

// TestCheckOnce_UpToDate verifies that no update is performed when the running
// version is >= the latest release.
func TestCheckOnce_UpToDate(t *testing.T) {
	version.SetVersion("2.0.0", "", "")
	defer version.SetVersion("dev", "", "")

	server := mockStableReleaseAPI(t, "2.0.0", false)
	defer server.Close()

	h := newUpdateHandler(t)
	h.apiBaseURL = server.URL
	h.httpClient = server.Client()

	var updateCalled int32
	c := NewUpdateChecker(h)
	c.doUpdate = func(context.Context, string) error {
		atomic.StoreInt32(&updateCalled, 1)
		return nil
	}

	c.checkOnce(context.Background())

	if atomic.LoadInt32(&updateCalled) == 1 {
		t.Error("doUpdate should NOT be called when up to date")
	}
}

// TestCheckOnce_NewVersionTriggersUpdate verifies an update is triggered when
// the latest release is newer than the running version.
func TestCheckOnce_NewVersionTriggersUpdate(t *testing.T) {
	version.SetVersion("1.0.0", "", "")
	defer version.SetVersion("dev", "", "")

	server := mockStableReleaseAPI(t, "1.2.0", false)
	defer server.Close()

	h := newUpdateHandler(t)
	h.apiBaseURL = server.URL
	h.httpClient = server.Client()

	var updateCalled int32
	var updateTag string
	c := NewUpdateChecker(h)
	c.doUpdate = func(_ context.Context, tag string) error {
		atomic.StoreInt32(&updateCalled, 1)
		updateTag = tag
		return nil
	}

	c.checkOnce(context.Background())

	if atomic.LoadInt32(&updateCalled) != 1 {
		t.Fatal("doUpdate should be called for a newer version")
	}
	if updateTag != "1.2.0" {
		t.Errorf("update tag = %q, want 1.2.0", updateTag)
	}
}

// TestCheckOnce_PrereleaseSkipped verifies that prereleases are never applied,
// even if newer than the running version.
func TestCheckOnce_PrereleaseSkipped(t *testing.T) {
	version.SetVersion("1.0.0", "", "")
	defer version.SetVersion("dev", "", "")

	server := mockStableReleaseAPI(t, "9.9.9", true) // prerelease!
	defer server.Close()

	h := newUpdateHandler(t)
	h.apiBaseURL = server.URL
	h.httpClient = server.Client()

	var updateCalled int32
	c := NewUpdateChecker(h)
	c.doUpdate = func(context.Context, string) error {
		atomic.StoreInt32(&updateCalled, 1)
		return nil
	}

	c.checkOnce(context.Background())

	if atomic.LoadInt32(&updateCalled) == 1 {
		t.Error("doUpdate should NOT be called for a prerelease")
	}
}

// TestCheckOnce_APIErrorNoUpdate verifies that a failed GitHub API call does
// not trigger an update.
func TestCheckOnce_APIErrorNoUpdate(t *testing.T) {
	version.SetVersion("1.0.0", "", "")
	defer version.SetVersion("dev", "", "")

	// Server that returns 500
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer server.Close()

	h := newUpdateHandler(t)
	h.apiBaseURL = server.URL
	h.httpClient = server.Client()

	var updateCalled int32
	c := NewUpdateChecker(h)
	c.doUpdate = func(context.Context, string) error {
		atomic.StoreInt32(&updateCalled, 1)
		return nil
	}

	c.checkOnce(context.Background()) // should not panic, should not update

	if atomic.LoadInt32(&updateCalled) == 1 {
		t.Error("doUpdate should NOT be called on API error")
	}
}

// TestUpdateChecker_StartStop verifies the goroutine starts and stops cleanly.
func TestUpdateChecker_StartStop(t *testing.T) {
	h := newUpdateHandler(t)
	// Point at a server that returns "up to date" so the immediate check is fast.
	version.SetVersion("9.9.9", "", "")
	defer version.SetVersion("dev", "", "")
	server := mockStableReleaseAPI(t, "1.0.0", false)
	defer server.Close()
	h.apiBaseURL = server.URL
	h.httpClient = server.Client()

	c := NewUpdateChecker(h)

	if c.IsActive() {
		t.Fatal("checker should not be active before Start")
	}

	c.Start()
	if !c.IsActive() {
		t.Fatal("checker should be active after Start")
	}

	// Start is idempotent
	c.Start()
	if !c.IsActive() {
		t.Fatal("checker should still be active after double Start")
	}

	c.Stop()
	if c.IsActive() {
		t.Fatal("checker should not be active after Stop")
	}

	// Stop is idempotent
	c.Stop()
}

// TestUpdateChecker_StopCancelsInFlight verifies that Stop aborts an in-flight
// check (the doUpdate call) via context cancellation.
func TestUpdateChecker_StopCancelsInFlight(t *testing.T) {
	version.SetVersion("1.0.0", "", "")
	defer version.SetVersion("dev", "", "")

	server := mockStableReleaseAPI(t, "9.9.9", false)
	defer server.Close()

	h := newUpdateHandler(t)
	h.apiBaseURL = server.URL
	h.httpClient = server.Client()

	c := NewUpdateChecker(h)
	updateStarted := make(chan struct{})
	c.doUpdate = func(ctx context.Context, _ string) error {
		close(updateStarted)
		<-ctx.Done() // block until context is canceled
		return ctx.Err()
	}

	c.Start()

	// Wait for the immediate check to reach doUpdate, then Stop.
	select {
	case <-updateStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("doUpdate was not called in time")
	}

	c.Stop() // should cancel the in-flight context and let the goroutine exit
	// If Stop returned, the goroutine exited — test passes.
}
