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
		Filters:  &Filter{ExcludeCountries: []string{"RU"}},
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
	if len(dp.Filter.ExcludeCountries) != 1 {
		t.Errorf("expected 1 exclude country, got %d", len(dp.Filter.ExcludeCountries))
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
		ExcludeCountries: []string{"RU"},
		IncludeCountries: []string{"DE", "NL"},
		MaxProxies:       20,
	}
	if err := store.SetFilters(f); err != nil {
		t.Fatalf("SetFilters: %v", err)
	}

	got := store.GetFilters()
	if len(got.ExcludeCountries) != 1 {
		t.Errorf("expected 1 exclude country, got %d", len(got.ExcludeCountries))
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
		filters.ExcludeCountries,
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
		IncludeCountries: nil,
		ExcludeCountries: nil,
	})

	filters := store.GetFilters()
	for _, slice := range [][]string{
		filters.ExcludeCountries,
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

// ---------- Profile CRUD ----------

func TestGetProfile_Existing(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))

	// Default profile should exist
	dp, err := store.GetProfile("default")
	if err != nil {
		t.Fatalf("GetProfile('default'): %v", err)
	}
	if !dp.IsDefault {
		t.Error("expected IsDefault=true")
	}
	if dp.Name != "По умолчанию" {
		t.Errorf("expected name 'По умолчанию', got %q", dp.Name)
	}
	if dp.Strategy.Type != "all" {
		t.Errorf("expected strategy 'all', got %q", dp.Strategy.Type)
	}
	if dp.Strategy.FallbackTag != "direct" {
		t.Errorf("expected fallback 'direct', got %q", dp.Strategy.FallbackTag)
	}
}

func TestGetProfile_NonExistent(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))

	_, err := store.GetProfile("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent profile")
	}
	if !containsStr(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}

func TestGetProfile_NonDefault(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))

	p := &Profile{Name: "Custom", Enabled: true, Filter: emptyFilter(), Strategy: RoutingStrategy{Type: "random"}}
	if err := store.AddProfile(p); err != nil {
		t.Fatalf("AddProfile: %v", err)
	}

	got, err := store.GetProfile(p.ID)
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	if got.Name != "Custom" {
		t.Errorf("expected 'Custom', got %q", got.Name)
	}
	if got.IsDefault {
		t.Error("non-default profile should not have IsDefault=true")
	}
	if got.Strategy.Type != "random" {
		t.Errorf("expected strategy 'random', got %q", got.Strategy.Type)
	}
}

func TestAddProfile_Valid(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))

	before := store.GetProfiles()
	nBefore := len(before)

	p := &Profile{
		Name:     "Test Profile",
		Enabled:  true,
		Filter:   Filter{ExcludeCountries: []string{"RU"}, MaxProxies: 20},
		Strategy: RoutingStrategy{Type: "leastping"},
	}
	if err := store.AddProfile(p); err != nil {
		t.Fatalf("AddProfile: %v", err)
	}
	if p.ID == "" {
		t.Error("expected ID to be generated")
	}
	if p.ID == "default" {
		t.Error("generated ID should not be 'default'")
	}

	after := store.GetProfiles()
	if len(after) != nBefore+1 {
		t.Fatalf("expected %d profiles, got %d", nBefore+1, len(after))
	}

	// Verify the new profile
	got := after[len(after)-1]
	if got.Name != "Test Profile" {
		t.Errorf("expected name 'Test Profile', got %q", got.Name)
	}
	if got.Filter.MaxProxies != 20 {
		t.Errorf("expected MaxProxies=20, got %d", got.Filter.MaxProxies)
	}
	if len(got.Filter.ExcludeCountries) != 1 || got.Filter.ExcludeCountries[0] != "RU" {
		t.Errorf("exclude_countries not persisted: %v", got.Filter.ExcludeCountries)
	}
}

func TestAddProfile_RejectsDefaultID(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))

	err := store.AddProfile(&Profile{ID: "default", Name: "Evil"})
	if err == nil {
		t.Error("expected error for ID 'default'")
	}
	if !containsStr(err.Error(), "reserved") {
		t.Errorf("error should mention 'reserved', got: %v", err)
	}
}

func TestAddProfile_EnforcesMaxProfiles(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))

	// Add MaxProfiles-1 profiles (default already exists = 1)
	for i := 0; i < MaxProfiles-1; i++ {
		err := store.AddProfile(&Profile{
			Name:     fmt.Sprintf("Profile-%d", i),
			Strategy: RoutingStrategy{Type: "random"},
		})
		if err != nil {
			t.Fatalf("AddProfile(%d): %v", i, err)
		}
	}

	// MaxProfiles should now be reached
	err := store.AddProfile(&Profile{Name: "Overflow"})
	if err == nil {
		t.Errorf("expected error when exceeding MaxProfiles=%d", MaxProfiles)
	}
	if !containsStr(err.Error(), "maximum") {
		t.Errorf("error should mention 'maximum', got: %v", err)
	}

	// Verify we have exactly MaxProfiles profiles
	profiles := store.GetProfiles()
	if len(profiles) != MaxProfiles {
		t.Errorf("expected %d profiles, got %d", MaxProfiles, len(profiles))
	}
}

func TestAddProfile_SetsIsDefaultFalse(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))

	// Even if caller sets IsDefault=true, store should override
	p := &Profile{Name: "Hijack", IsDefault: true}
	if err := store.AddProfile(p); err != nil {
		t.Fatalf("AddProfile: %v", err)
	}

	got, err := store.GetProfile(p.ID)
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	if got.IsDefault {
		t.Error("AddProfile should force IsDefault=false")
	}
}

func TestAddProfile_PreservesProvidedID(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))

	p := &Profile{ID: "custom-id", Name: "Custom"}
	if err := store.AddProfile(p); err != nil {
		t.Fatalf("AddProfile: %v", err)
	}
	if p.ID != "custom-id" {
		t.Errorf("expected ID 'custom-id', got %q", p.ID)
	}
}

func TestUpdateProfile_ChangesFields(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))

	p := &Profile{Name: "Original", Strategy: RoutingStrategy{Type: "random"}}
	store.AddProfile(p)

	p.Name = "Updated"
	p.Strategy.Type = "roundrobin"
	p.Filter.ExcludeCountries = []string{"RU"}
	p.Enabled = true
	if err := store.UpdateProfile(p); err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}

	got, err := store.GetProfile(p.ID)
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	if got.Name != "Updated" {
		t.Errorf("expected name 'Updated', got %q", got.Name)
	}
	if got.Strategy.Type != "roundrobin" {
		t.Errorf("expected strategy 'roundrobin', got %q", got.Strategy.Type)
	}
	if len(got.Filter.ExcludeCountries) != 1 || got.Filter.ExcludeCountries[0] != "RU" {
		t.Errorf("exclude_countries not updated: %v", got.Filter.ExcludeCountries)
	}
}

func TestUpdateProfile_PreservesIsDefault(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))

	// Get default profile and try to change IsDefault to false
	dp, _ := store.GetProfile("default")
	dp.IsDefault = false // attempt to remove default status
	dp.Name = "Hacked"
	if err := store.UpdateProfile(dp); err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}

	got, _ := store.GetProfile("default")
	if !got.IsDefault {
		t.Error("UpdateProfile should preserve IsDefault=true")
	}

	// Also test non-default: try to set IsDefault=true
	p := &Profile{Name: "NonDefault"}
	store.AddProfile(p)
	p.IsDefault = true // attempt to hijack default status
	store.UpdateProfile(p)

	got2, _ := store.GetProfile(p.ID)
	if got2.IsDefault {
		t.Error("UpdateProfile should preserve IsDefault=false for non-default")
	}
}

func TestUpdateProfile_NotFound(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))

	err := store.UpdateProfile(&Profile{ID: "ghost", Name: "Ghost"})
	if err == nil {
		t.Error("expected error for nonexistent profile")
	}
}

func TestDeleteProfile_NonDefault(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))

	p := &Profile{Name: "ToDelete", Strategy: RoutingStrategy{Type: "random"}}
	store.AddProfile(p)

	before := len(store.GetProfiles())

	err := store.DeleteProfile(p.ID)
	if err != nil {
		t.Fatalf("DeleteProfile: %v", err)
	}

	after := store.GetProfiles()
	if len(after) != before-1 {
		t.Fatalf("expected %d profiles, got %d", before-1, len(after))
	}

	// Verify deleted
	_, err = store.GetProfile(p.ID)
	if err == nil {
		t.Error("expected error for deleted profile")
	}
}

func TestDeleteProfile_RefusesDefault(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))

	err := store.DeleteProfile("default")
	if err == nil {
		t.Error("expected error when deleting default profile")
	}
	if !containsStr(err.Error(), "cannot delete") {
		t.Errorf("error should mention 'cannot delete', got: %v", err)
	}

	// Default should still exist
	dp, err := store.GetProfile("default")
	if err != nil {
		t.Fatalf("default profile should still exist: %v", err)
	}
	if !dp.IsDefault {
		t.Error("default profile should still be IsDefault=true")
	}
}

func TestDeleteProfile_NotFound(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))

	err := store.DeleteProfile("ghost")
	if err == nil {
		t.Error("expected error for nonexistent profile")
	}
}

func TestDeleteProfile_PersistsRemoval(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subscriptions.json")

	store1, _ := NewStore(path)
	p := &Profile{Name: "Temp", Strategy: RoutingStrategy{Type: "random"}}
	store1.AddProfile(p)

	store1.DeleteProfile(p.ID)

	// Reload from disk
	store2, _ := NewStore(path)
	_, err := store2.GetProfile(p.ID)
	if err == nil {
		t.Error("deleted profile should not survive reload")
	}
	// Default should survive
	dp, err := store2.GetProfile("default")
	if err != nil {
		t.Fatalf("default profile should survive: %v", err)
	}
	if !dp.IsDefault {
		t.Error("default profile should be IsDefault")
	}
}

// ---------- Save ----------

func TestSave_PersistsToDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subscriptions.json")

	store, _ := NewStore(path)
	store.AddSubscription(&Subscription{Name: "Test", URL: "https://a.com", Enabled: true})
	store.SetFilters(&Filter{MaxProxies: 42})

	// Save explicitly
	if err := store.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Read the file directly and verify
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	var cfg SubscriptionConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(cfg.Subscriptions) != 1 {
		t.Errorf("expected 1 subscription, got %d", len(cfg.Subscriptions))
	}
	if len(cfg.Profiles) == 0 || !cfg.Profiles[0].IsDefault {
		t.Error("expected default profile")
	}
	if cfg.Profiles[0].Filter.MaxProxies != 42 {
		t.Errorf("expected MaxProxies=42, got %d", cfg.Profiles[0].Filter.MaxProxies)
	}
}

func TestSave_CreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deep", "nested", "config.json")

	store, _ := NewStore(path)
	if err := store.Save(); err != nil {
		t.Fatalf("Save with nested dirs: %v", err)
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("config file should be created in nested dir")
	}
}

// ---------- defaultProfile ----------

func TestDefaultProfile_CreatedOnNewStore(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))

	profiles := store.GetProfiles()
	if len(profiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(profiles))
	}
	dp := profiles[0]
	if dp.ID != "default" {
		t.Errorf("expected ID 'default', got %q", dp.ID)
	}
	if !dp.Enabled {
		t.Error("default should be enabled")
	}
	if !dp.IsDefault {
		t.Error("expected IsDefault=true")
	}
	if dp.Name != "По умолчанию" {
		t.Errorf("expected 'По умолчанию', got %q", dp.Name)
	}
}

func TestDefaultProfile_FilterNeverNilSlices(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))

	dp, err := store.GetProfile("default")
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	for name, slice := range map[string][]string{
		"ExcludeCountries":   dp.Filter.ExcludeCountries,
		"IncludeCountries": dp.Filter.IncludeCountries,
		"IncludeRegexes":   dp.Filter.IncludeRegexes,
		"ExcludeRegexes":   dp.Filter.ExcludeRegexes,
	} {
		if slice == nil {
			t.Errorf("default profile Filter.%s should not be nil", name)
		}
	}
}

// ---------- GetProfiles deep copy ----------

func TestGetProfiles_DeepCopyIsolation(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))

	// Modify default profile filters to have data
	store.SetFilters(&Filter{ExcludeCountries: []string{"original"}})

	profiles1 := store.GetProfiles()
	// Modify returned slice
	profiles1[0].Filter.ExcludeCountries[0] = "tampered"
	profiles1[0].Name = "hacked"

	// Get fresh copy — should be unaffected
	profiles2 := store.GetProfiles()
	if profiles2[0].Filter.ExcludeCountries[0] != "original" {
		t.Errorf("deep copy isolation failed: got %q", profiles2[0].Filter.ExcludeCountries[0])
	}
	if profiles2[0].Name != "По умолчанию" {
		t.Errorf("deep copy isolation failed: got %q", profiles2[0].Name)
	}
}

func TestGetProfiles_IncludesAddedProfiles(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))

	store.AddProfile(&Profile{Name: "Custom1"})
	store.AddProfile(&Profile{Name: "Custom2"})

	profiles := store.GetProfiles()
	if len(profiles) != 3 { // default + 2 custom
		t.Fatalf("expected 3 profiles, got %d", len(profiles))
	}
	names := make(map[string]bool)
	for _, p := range profiles {
		names[p.Name] = true
	}
	if !names["По умолчанию"] || !names["Custom1"] || !names["Custom2"] {
		t.Errorf("expected 3 profiles, got names: %v", names)
	}
}

func TestGetProfile_ReturnsCopy(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))

	got1, _ := store.GetProfile("default")
	got1.Name = "mutated"

	got2, _ := store.GetProfile("default")
	if got2.Name != "По умолчанию" {
		t.Error("GetProfile should return a copy, not a pointer to internal state")
	}
}

// ---------- Profile persistence ----------

func TestProfilePersistence_AcrossStoreInstances(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subscriptions.json")

	store1, _ := NewStore(path)
	p := &Profile{
		Name:     "Persist",
		Enabled:  true,
		Filter:   Filter{ExcludeCountries: []string{"US"}, MaxProxies: 100},
		Strategy: RoutingStrategy{Type: "roundrobin"},
	}
	store1.AddProfile(p)

	store2, _ := NewStore(path)
	got, err := store2.GetProfile(p.ID)
	if err != nil {
		t.Fatalf("profile not persisted: %v", err)
	}
	if got.Name != "Persist" {
		t.Errorf("expected 'Persist', got %q", got.Name)
	}
	if got.Strategy.Type != "roundrobin" {
		t.Errorf("expected 'roundrobin', got %q", got.Strategy.Type)
	}
	if len(got.Filter.ExcludeCountries) != 1 || got.Filter.ExcludeCountries[0] != "US" {
		t.Errorf("exclude_countries not persisted: %v", got.Filter.ExcludeCountries)
	}
	if got.Filter.MaxProxies != 100 {
		t.Errorf("expected MaxProxies=100, got %d", got.Filter.MaxProxies)
	}
	if got.IsDefault {
		t.Error("non-default profile should not be IsDefault after reload")
	}
}

func TestProfilePersistence_DefaultPreserved(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subscriptions.json")

	store1, _ := NewStore(path)
	store1.SetFilters(&Filter{MaxProxies: 77})
	store1.SetStrategy(&RoutingStrategy{Type: "leastload"})

	store2, _ := NewStore(path)
	dp, _ := store2.GetProfile("default")
	if dp.Filter.MaxProxies != 77 {
		t.Errorf("expected MaxProxies=77, got %d", dp.Filter.MaxProxies)
	}
	if dp.Strategy.Type != "leastload" {
		t.Errorf("expected 'leastload', got %q", dp.Strategy.Type)
	}
}

// ---------- Profile concurrent access ----------

func TestConcurrentProfileAccess(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))

	var wg sync.WaitGroup
	errs := make(chan error, MaxProfiles-1)

	// Concurrently add profiles
	for i := 0; i < MaxProfiles-1; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			err := store.AddProfile(&Profile{
				Name:     fmt.Sprintf("Concurrent-%d", n),
				Strategy: RoutingStrategy{Type: "random"},
			})
			if err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent AddProfile error: %v", err)
	}

	// Should have MaxProfiles profiles total (default + 9)
	profiles := store.GetProfiles()
	if len(profiles) != MaxProfiles {
		t.Errorf("expected %d profiles, got %d", MaxProfiles, len(profiles))
	}
}

func TestConcurrentProfileReadWrite(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))

	p := &Profile{Name: "RWTest", Strategy: RoutingStrategy{Type: "random"}}
	store.AddProfile(p)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			store.GetProfiles()
		}()
		go func() {
			defer wg.Done()
			store.GetProfile(p.ID)
		}()
		go func(n int) {
			defer wg.Done()
			up := &Profile{ID: p.ID, Name: fmt.Sprintf("Updated-%d", n), Strategy: RoutingStrategy{Type: "random"}}
			store.UpdateProfile(up)
		}(i)
	}
	wg.Wait()
	// No panic = success
}

// ---------- Helpers ----------

func emptyFilter() Filter {
	return Filter{
		ExcludeCountries: []string{},
		IncludeCountries: []string{},
		IncludeRegexes:   []string{},
		ExcludeRegexes:   []string{},
	}
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || containsStrHelper(s, substr))
}

func containsStrHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestStore_PersistenceWithFilters(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subscriptions.json")

	store1, _ := NewStore(path)
	store1.AddSubscription(&Subscription{Name: "Test", URL: "https://a.com", Enabled: true})
	store1.SetFilters(&Filter{
		ExcludeCountries: []string{"RU"},
		MaxProxies:       50,
		IncludeRegexes:  []string{"speed"},
	})
	store1.SetStrategy(&RoutingStrategy{Type: "random"})

	// Create new store instance — should load from disk
	store2, _ := NewStore(path)
	filters := store2.GetFilters()
	if len(filters.ExcludeCountries) != 1 || filters.ExcludeCountries[0] != "RU" {
		t.Errorf("exclude_countries not persisted: %v", filters.ExcludeCountries)
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

