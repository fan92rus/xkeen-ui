package subscription

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// ---------- Store creation / persistence ----------

func TestNewStore_CreatesDefaultConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subscriptions.json")

	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	cfg := store.GetConfig()
	if len(cfg.Subscriptions) != 0 {
		t.Errorf("expected 0 subscriptions, got %d", len(cfg.Subscriptions))
	}
	if len(cfg.Profiles) == 0 || !cfg.Profiles[0].IsDefault {
		t.Fatal("expected default profile")
	}
	dp := cfg.Profiles[0]
	if dp.Strategy.Type != "all" {
		t.Errorf("expected default strategy 'all', got %q", dp.Strategy.Type)
	}
	if dp.Strategy.FallbackTag != "direct" {
		t.Errorf("expected fallback 'direct', got %q", dp.Strategy.FallbackTag)
	}
}

func TestNewStore_LoadsExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subscriptions.json")

	existing := &SubscriptionConfig{
		Subscriptions: []Subscription{
			{ID: "abc", Name: "Test", URL: "https://example.com/sub", Enabled: true, Interval: 5},
		},
		Filters:  &Filter{ExcludeMarkers: []string{"0.5X", "🎮"}},
		Strategy: &RoutingStrategy{Type: "random", FallbackTag: "direct"},
	}
	data, _ := json.MarshalIndent(existing, "", "    ")
	os.WriteFile(path, data, 0644)

	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	cfg := store.GetConfig()
	if len(cfg.Subscriptions) != 1 {
		t.Fatalf("expected 1 subscription, got %d", len(cfg.Subscriptions))
	}
	if cfg.Subscriptions[0].ID != "abc" {
		t.Errorf("expected id 'abc', got %q", cfg.Subscriptions[0].ID)
	}
	// Legacy fields should be migrated into default profile
	if len(cfg.Profiles) == 0 || !cfg.Profiles[0].IsDefault {
		t.Fatal("expected default profile after migration")
	}
	dp := cfg.Profiles[0]
	if dp.Strategy.Type != "random" {
		t.Errorf("expected strategy 'random', got %q", dp.Strategy.Type)
	}
	if len(dp.Filter.ExcludeMarkers) != 2 {
		t.Errorf("expected 2 exclude markers, got %d", len(dp.Filter.ExcludeMarkers))
	}
}

func TestNewStore_CorruptFileUsesDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subscriptions.json")
	os.WriteFile(path, []byte("not json at all"), 0644)

	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	cfg := store.GetConfig()
	if len(cfg.Profiles) == 0 || !cfg.Profiles[0].IsDefault {
		t.Fatal("expected default profile for corrupt file")
	}
	if cfg.Profiles[0].Strategy.Type != "all" {
		t.Errorf("expected default strategy for corrupt file, got %q", cfg.Profiles[0].Strategy.Type)
	}
}

// ---------- Subscription CRUD ----------

func TestAddSubscription_GeneratesID(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))

	sub := &Subscription{Name: "Test", URL: "https://example.com", Enabled: true}
	if err := store.AddSubscription(sub); err != nil {
		t.Fatalf("AddSubscription: %v", err)
	}

	if sub.ID == "" {
		t.Error("expected ID to be generated")
	}
	if len(sub.ID) != 32 { // 16 bytes hex = 32 chars
		t.Errorf("expected 32-char ID, got %d chars: %q", len(sub.ID), sub.ID)
	}

	// Verify persisted
	cfg := store.GetConfig()
	if len(cfg.Subscriptions) != 1 {
		t.Fatalf("expected 1 subscription, got %d", len(cfg.Subscriptions))
	}
	if cfg.Subscriptions[0].ID != sub.ID {
		t.Errorf("persisted ID mismatch: %q vs %q", cfg.Subscriptions[0].ID, sub.ID)
	}
}

func TestAddSubscription_RejectsDuplicateID(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))

	sub1 := &Subscription{ID: "dup", Name: "First", URL: "https://a.com", Enabled: true}
	if err := store.AddSubscription(sub1); err != nil {
		t.Fatalf("AddSubscription 1: %v", err)
	}

	sub2 := &Subscription{ID: "dup", Name: "Second", URL: "https://b.com", Enabled: true}
	err := store.AddSubscription(sub2)
	if err == nil {
		t.Error("expected error for duplicate ID")
	}
}

func TestUpdateSubscription(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))

	sub := &Subscription{Name: "Original", URL: "https://a.com", Enabled: true}
	store.AddSubscription(sub)

	sub.Name = "Updated"
	sub.Interval = 10
	if err := store.UpdateSubscription(sub); err != nil {
		t.Fatalf("UpdateSubscription: %v", err)
	}

	got, err := store.GetSubscription(sub.ID)
	if err != nil {
		t.Fatalf("GetSubscription: %v", err)
	}
	if got.Name != "Updated" {
		t.Errorf("expected name 'Updated', got %q", got.Name)
	}
	if got.Interval != 10 {
		t.Errorf("expected interval 10, got %d", got.Interval)
	}
}

func TestUpdateSubscription_NotFound(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))

	err := store.UpdateSubscription(&Subscription{ID: "nonexistent"})
	if err == nil {
		t.Error("expected error for nonexistent subscription")
	}
}

func TestDeleteSubscription(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))

	sub := &Subscription{Name: "ToDelete", URL: "https://a.com", Enabled: true}
	store.AddSubscription(sub)

	err := store.DeleteSubscription(sub.ID)
	if err != nil {
		t.Fatalf("DeleteSubscription: %v", err)
	}

	_, err = store.GetSubscription(sub.ID)
	if err == nil {
		t.Error("expected error after deletion")
	}
}

func TestDeleteSubscription_NotFound(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))

	err := store.DeleteSubscription("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent subscription")
	}
}

func TestGetSubscription(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))

	sub := &Subscription{Name: "MySub", URL: "https://a.com", Enabled: true, ProxyCount: 42}
	store.AddSubscription(sub)

	got, err := store.GetSubscription(sub.ID)
	if err != nil {
		t.Fatalf("GetSubscription: %v", err)
	}
	if got.Name != "MySub" {
		t.Errorf("expected 'MySub', got %q", got.Name)
	}
	if got.ProxyCount != 42 {
		t.Errorf("expected ProxyCount=42, got %d", got.ProxyCount)
	}
}

// ---------- Filters ----------

func TestSetGetFilters(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))

	f := &Filter{
		ExcludeMarkers:   []string{"0.5X", "🎮"},
		IncludeCountries: []string{"DE", "NL"},
		MaxProxies:       20,
	}
	if err := store.SetFilters(f); err != nil {
		t.Fatalf("SetFilters: %v", err)
	}

	got := store.GetFilters()
	if len(got.ExcludeMarkers) != 2 {
		t.Errorf("expected 2 exclude markers, got %d", len(got.ExcludeMarkers))
	}
	if got.MaxProxies != 20 {
		t.Errorf("expected MaxProxies=20, got %d", got.MaxProxies)
	}
}

func TestGetFilters_ReturnsCopy(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))

	f := &Filter{IncludeCountries: []string{"DE"}}
	store.SetFilters(f)

	got := store.GetFilters()
	got.IncludeCountries[0] = "US"

	original := store.GetFilters()
	if original.IncludeCountries[0] != "DE" {
		t.Error("GetFilters should return a copy")
	}
}

// ---------- Strategy ----------

func TestSetGetStrategy(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))

	s := &RoutingStrategy{Type: "random", FallbackTag: "direct"}
	if err := store.SetStrategy(s); err != nil {
		t.Fatalf("SetStrategy: %v", err)
	}

	got := store.GetStrategy()
	if got.Type != "random" {
		t.Errorf("expected 'random', got %q", got.Type)
	}
}

// ---------- Proxies cache ----------

func TestSetGetProxies(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))

	proxies := []*ProxyEntry{
		{Tag: "proxy-de-1", Protocol: "vless", Country: "DE"},
		{Tag: "proxy-nl-1", Protocol: "vless", Country: "NL"},
	}
	store.SetProxies(proxies)

	got := store.GetProxies()
	if len(got) != 2 {
		t.Fatalf("expected 2 proxies, got %d", len(got))
	}
	if got[0].Tag != "proxy-de-1" {
		t.Errorf("expected 'proxy-de-1', got %q", got[0].Tag)
	}
}

func TestSetGetProxies_ReturnsCopy(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))

	store.SetProxies([]*ProxyEntry{{Tag: "original"}})
	got := store.GetProxies()
	got[0].Tag = "modified"

	original := store.GetProxies()
	if original[0].Tag != "original" {
		t.Error("GetProxies should return a copy")
	}
}

// ---------- GeneratedAt ----------

func TestSetGeneratedAt(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))

	now := time.Now().Truncate(time.Second)
	if err := store.SetGeneratedAt(now); err != nil {
		t.Fatalf("SetGeneratedAt: %v", err)
	}

	cfg := store.GetConfig()
	if cfg.GeneratedAt.Format(time.RFC3339) != now.Format(time.RFC3339) {
		t.Errorf("expected %v, got %v", now, cfg.GeneratedAt)
	}
}

// ---------- Persistence ----------

func TestSaveCreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "deep", "subscriptions.json")

	store, _ := NewStore(path)
	store.AddSubscription(&Subscription{Name: "Test", URL: "https://a.com", Enabled: true})

	// File should exist
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("expected config file to be created")
	}
}

func TestSaveAtomic_NoLeftoverTmpOnSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subscriptions.json")

	store, _ := NewStore(path)
	store.AddSubscription(&Subscription{Name: "Test", URL: "https://a.com", Enabled: true})

	tmp := path + ".tmp"
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Error("temp file should be cleaned up after save")
	}
}

func TestPersistence_AcrossStoreInstances(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subscriptions.json")

	// First instance: add data
	store1, _ := NewStore(path)
	store1.AddSubscription(&Subscription{Name: "Persist", URL: "https://a.com", Enabled: true})
	store1.SetFilters(&Filter{MaxProxies: 15})

	// Second instance: load same file
	store2, _ := NewStore(path)
	cfg := store2.GetConfig()
	if len(cfg.Subscriptions) != 1 {
		t.Fatalf("expected 1 subscription, got %d", len(cfg.Subscriptions))
	}
	if cfg.Subscriptions[0].Name != "Persist" {
		t.Errorf("expected 'Persist', got %q", cfg.Subscriptions[0].Name)
	}
	if len(cfg.Profiles) == 0 || !cfg.Profiles[0].IsDefault {
		t.Fatal("expected default profile")
	}
	if cfg.Profiles[0].Filter.MaxProxies != 15 {
		t.Errorf("expected MaxProxies=15, got %d", cfg.Profiles[0].Filter.MaxProxies)
	}
}

// ---------- Concurrency ----------

func TestConcurrentAccess(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			name := string(rune('A' + n))
			store.AddSubscription(&Subscription{Name: name, URL: "https://a.com", Enabled: true})
		}(i)
	}
	wg.Wait()

	cfg := store.GetConfig()
	if len(cfg.Subscriptions) != 10 {
		t.Errorf("expected 10 subscriptions, got %d", len(cfg.Subscriptions))
	}
}

func TestConcurrentReadWrite(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))
	store.AddSubscription(&Subscription{Name: "Base", URL: "https://a.com", Enabled: true})

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			store.GetConfig()
		}()
		go func(n int) {
			defer wg.Done()
			store.SetProxies([]*ProxyEntry{{Tag: string(rune('A' + n))}})
		}(i)
	}
	wg.Wait()
	// Just verify no panics occurred
}

func TestGetFilters_NeverReturnsNilSlices(t *testing.T) {
	// Regression test: GetFilters must never return nil slices
	// (frontend crashes on .length of null)
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))

	filters := store.GetFilters()
	for _, slice := range [][]string{
		filters.IncludeMarkers,
		filters.ExcludeMarkers,
		filters.IncludeCountries,
		filters.ExcludeCountries,
	} {
		if slice == nil {
			t.Error("expected empty slice, got nil")
		}
	}
}

func TestGetFilters_NilSlicesAfterSetFilters(t *testing.T) {
	// Ensure SetFilters with nil slices → GetFilters returns empty slices
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))

	store.SetFilters(&Filter{
		IncludeMarkers:   nil,
		ExcludeMarkers:   nil,
		IncludeCountries: nil,
		ExcludeCountries: nil,
	})

	filters := store.GetFilters()
	for _, slice := range [][]string{
		filters.IncludeMarkers,
		filters.ExcludeMarkers,
		filters.IncludeCountries,
		filters.ExcludeCountries,
	} {
		if slice == nil {
			t.Error("expected empty slice, got nil after SetFilters with nil")
		}
	}
}

func TestConcurrentProxies(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func(n int) {
			defer wg.Done()
			proxies := make([]*ProxyEntry, n+1)
			for j := range proxies {
				proxies[j] = &ProxyEntry{Tag: fmt.Sprintf("p-%d-%d", n, j)}
			}
			store.SetProxies(proxies)
		}(i)
		go func() {
			defer wg.Done()
			store.GetProxies()
		}()
	}
	wg.Wait()
}

func TestStore_PersistenceWithFilters(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subscriptions.json")

	store1, _ := NewStore(path)
	store1.AddSubscription(&Subscription{Name: "Test", URL: "https://a.com", Enabled: true})
	store1.SetFilters(&Filter{
		ExcludeMarkers:   []string{"mobile"},
		MaxProxies:       50,
		IncludeRegexes:  []string{"speed"},
	})
	store1.SetStrategy(&RoutingStrategy{Type: "random"})

	// Create new store instance — should load from disk
	store2, _ := NewStore(path)
	filters := store2.GetFilters()
	if len(filters.ExcludeMarkers) != 1 || filters.ExcludeMarkers[0] != "mobile" {
		t.Errorf("exclude_markers not persisted: %v", filters.ExcludeMarkers)
	}
	if filters.MaxProxies != 50 {
		t.Errorf("max_proxies not persisted: %d", filters.MaxProxies)
	}
	if len(filters.IncludeRegexes) != 1 || filters.IncludeRegexes[0] != "speed" {
		t.Errorf("include_regexes not persisted: %q", filters.IncludeRegexes[0])
	}

	strategy := store2.GetStrategy()
	if strategy.Type != "random" {
		t.Errorf("strategy type not persisted: %q", strategy.Type)
	}
}

