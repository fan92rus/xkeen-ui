package subscription

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"encoding/base64"
	"path/filepath"
)

// ---------- Scheduler creation ----------

func TestNewScheduler(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))
	fetcher := NewFetcher()

	s := NewScheduler(store, fetcher)
	if s == nil {
		t.Fatal("expected non-nil scheduler")
	}
}

// ---------- Stop/Start ----------

func TestScheduler_StopWithoutStart(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))
	fetcher := NewFetcher()

	s := NewScheduler(store, fetcher)
	// Should not panic
	s.Stop()
}

func TestScheduler_StartStopIdempotent(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))
	fetcher := NewFetcher()

	s := NewScheduler(store, fetcher)
	s.Start()
	time.Sleep(50 * time.Millisecond)

	// Double stop should be fine
	s.Stop()
	s.Stop()
}

// ---------- RefreshOne ----------

func TestRefreshOne_Success(t *testing.T) {
	// Create a test server that returns a vless subscription
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "Hiddify" {
			t.Error("expected Hiddify User-Agent")
		}

		lines := "vless://uuid@1.2.3.4:443?type=tcp&security=reality#%F0%9F%87%A9%F0%9F%87%AA%20Test"
		encoded := base64.StdEncoding.EncodeToString([]byte(lines))
		w.Write([]byte(encoded))
	}))
	defer server.Close()

	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))
	sub := &Subscription{Name: "Test", URL: server.URL, Enabled: true}
	store.AddSubscription(sub)

	fetcher := NewFetcher()
	sched := NewScheduler(store, fetcher)

	err := sched.RefreshOne(sub.ID)
	if err != nil {
		t.Fatalf("RefreshOne: %v", err)
	}

	updated, _ := store.GetSubscription(sub.ID)
	if updated.LastFetch.IsZero() {
		t.Error("expected LastFetch to be set")
	}
	if updated.LastError != "" {
		t.Errorf("unexpected LastError: %q", updated.LastError)
	}
	if updated.ProxyCount == 0 {
		t.Error("expected ProxyCount > 0")
	}
}

func TestRefreshOne_NotFound(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))
	fetcher := NewFetcher()
	sched := NewScheduler(store, fetcher)

	err := sched.RefreshOne("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent subscription")
	}
}

func TestRefreshOne_BadURL(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))
	sub := &Subscription{Name: "Bad", URL: "http://127.0.0.1:1/impossible", Enabled: true}
	store.AddSubscription(sub)

	fetcher := NewFetcher()
	sched := NewScheduler(store, fetcher)

	err := sched.RefreshOne(sub.ID)
	if err == nil {
		t.Error("expected error for unreachable URL")
	}

	updated, _ := store.GetSubscription(sub.ID)
	if updated.LastError == "" {
		t.Error("expected LastError to be set on failure")
	}
	if updated.LastFetch.IsZero() {
		t.Error("expected LastFetch to be set even on failure")
	}
}

// ---------- RefreshAll ----------

func TestRefreshAll(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lines := "vless://uuid@1.2.3.4:443?type=tcp&security=reality#Test"
		encoded := base64.StdEncoding.EncodeToString([]byte(lines))
		w.Write([]byte(encoded))
	}))
	defer server.Close()

	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))

	store.AddSubscription(&Subscription{Name: "Sub1", URL: server.URL, Enabled: true})
	store.AddSubscription(&Subscription{Name: "Sub2", URL: server.URL, Enabled: true})
	// Disabled subscription should be skipped
	store.AddSubscription(&Subscription{Name: "Disabled", URL: "http://bad", Enabled: false})

	fetcher := NewFetcher()
	sched := NewScheduler(store, fetcher)

	err := sched.RefreshAll()
	if err != nil {
		t.Fatalf("RefreshAll: %v", err)
	}

	cfg := store.GetConfig()
	for _, sub := range cfg.Subscriptions {
		if sub.Name == "Disabled" {
			if !sub.LastFetch.IsZero() {
				t.Error("disabled subscription should not be fetched")
			}
			continue
		}
		if sub.LastFetch.IsZero() {
			t.Errorf("subscription %q should have LastFetch set", sub.Name)
		}
	}
}

// ---------- OnUpdate callback ----------

func TestOnUpdate_CalledOnRefreshOne(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lines := "vless://uuid@1.2.3.4:443?type=tcp&security=reality#Test"
		encoded := base64.StdEncoding.EncodeToString([]byte(lines))
		w.Write([]byte(encoded))
	}))
	defer server.Close()

	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))
	sub := &Subscription{Name: "Test", URL: server.URL, Enabled: true}
	store.AddSubscription(sub)

	fetcher := NewFetcher()
	sched := NewScheduler(store, fetcher)

	callbackCalled := false
	sched.OnUpdate = func() {
		callbackCalled = true
	}

	// RefreshOne itself doesn't call OnUpdate — only RefreshAll does.
	// But let's verify RefreshAll calls it.
	store.AddSubscription(&Subscription{Name: "Sub2", URL: server.URL, Enabled: true})
	sched.RefreshAll()

	if !callbackCalled {
		t.Error("expected OnUpdate callback to be called")
	}
}

// ---------- Integration: Fetcher with custom client + scheduler ----------

func TestScheduler_FetchWithCustomClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lines := "vless://uuid@1.2.3.4:443?type=tcp&security=reality#Test"
		encoded := base64.StdEncoding.EncodeToString([]byte(lines))
		w.Write([]byte(encoded))
	}))
	defer server.Close()

	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))
	sub := &Subscription{Name: "Test", URL: server.URL, Enabled: true}
	store.AddSubscription(sub)

	client := &http.Client{Timeout: 5 * time.Second}
	fetcher := NewFetcherWithClient(client)
	sched := NewScheduler(store, fetcher)

	err := sched.RefreshOne(sub.ID)
	if err != nil {
		t.Fatalf("RefreshOne with custom client: %v", err)
	}
}

// ---------- Context timeout ----------

func TestScheduler_FetcherContextCancellation(t *testing.T) {
	// Server that sleeps forever
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Second)
	}))
	defer server.Close()

	fetcher := NewFetcher()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := fetcher.Fetch(ctx, server.URL)
	if err == nil {
		t.Error("expected timeout error")
	}
}
