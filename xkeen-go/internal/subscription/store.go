package subscription

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Store manages subscription configuration persistence.
type Store struct {
	mu     sync.RWMutex
	path   string
	config *Config

	// proxies is the in-memory cache of all parsed proxies from all subscriptions.
	// Updated by the scheduler after a successful fetch cycle.
	proxies []*ProxyEntry
}

// NewStore creates a new store. If the file exists it loads it;
// otherwise it returns a store with sensible defaults.
func NewStore(path string) (*Store, error) {
	s := &Store{
		path: path,
		config: &Config{
			Subscriptions: []Subscription{},
			Profiles:      []Profile{},
		},
	}

	data, err := os.ReadFile(path)
	if err == nil {
		var cfg Config
		if jsonErr := json.Unmarshal(data, &cfg); jsonErr == nil {
			s.config = &cfg
		}
		// If the file exists but can't be parsed, keep defaults.
	}

	// Migrate legacy Filters/Strategy into default profile
	s.migrateProfiles()

	// Migrate legacy string regex fields to slices
	s.migrateRegexFields()

	// Load cached proxy data (non-critical)
	s.loadProxyCache()

	// Ensure built-in subscriptions exist
	s.initBuiltinSubscriptions()

	return s, nil
}

// GetConfig returns a deep copy of the current config.
func (s *Store) GetConfig() *Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneConfig(s.config)
}

// Save persists the current config to disk. Holds RLock through disk write
// to prevent concurrent writers from modifying s.config during save.
func (s *Store) Save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cfg := cloneConfig(s.config)
	return s.saveConfig(cfg)
}

// saveConfig writes a config to disk (caller handles locking if needed).
func (s *Store) saveConfig(cfg *Config) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "    ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Write atomically via temp file.
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("failed to rename config: %w", err)
	}

	return nil
}

// ---------- Built-in subscriptions ----------

// initBuiltinSubscriptions ensures system subscriptions (AWG) exist in the
// subscription list. Called once after loading/creating the config.
func (s *Store) initBuiltinSubscriptions() {
	// Check if AWG subscription already exists
	for _, sub := range s.config.Subscriptions {
		if sub.ID == ReservedAWGSubscriptionID {
			return
		}
	}

	// Create built-in AWG subscription
	s.config.Subscriptions = append(s.config.Subscriptions, Subscription{
		ID:        ReservedAWGSubscriptionID,
		Name:      "AWG (AmneziaWG)",
		URL:       "",
		Enabled:   true,
		IsBuiltin: true,
	})
	if err := s.saveConfig(s.config); err != nil {
		log.Printf("[store] failed to save init config: %v", err)
	}
}

// IsBuiltinSubscription returns true if the subscription ID is a built-in
// system subscription that cannot be deleted.
func (s *Store) IsBuiltinSubscription(id string) bool {
	if id == ReservedAWGSubscriptionID {
		return true
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, sub := range s.config.Subscriptions {
		if sub.ID == id && sub.IsBuiltin {
			return true
		}
	}
	return false
}

// ---------- Subscription CRUD ----------

// AddSubscription adds a new subscription. Generates an ID if empty.
func (s *Store) AddSubscription(sub *Subscription) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if sub.ID == "" {
		id, err := generateID()
		if err != nil {
			return fmt.Errorf("failed to generate id: %w", err)
		}
		sub.ID = id
	}

	// Check for duplicate ID
	for _, existing := range s.config.Subscriptions {
		if existing.ID == sub.ID {
			return fmt.Errorf("subscription with id %s already exists", sub.ID)
		}
	}

	s.config.Subscriptions = append(s.config.Subscriptions, *sub)
	return s.saveConfig(s.config)
}

// UpdateSubscription updates an existing subscription by ID.
func (s *Store) UpdateSubscription(sub *Subscription) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, existing := range s.config.Subscriptions {
		if existing.ID == sub.ID {
			s.config.Subscriptions[i] = *sub
			return s.saveConfig(s.config)
		}
	}

	return fmt.Errorf("subscription %s not found", sub.ID)
}

// DeleteSubscription removes a subscription by ID and cleans up its proxies from cache.
func (s *Store) DeleteSubscription(id string) error {
	// Cannot delete built-in system subscriptions
	if id == ReservedAWGSubscriptionID {
		return fmt.Errorf("cannot delete built-in subscription: %s", id)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for i, existing := range s.config.Subscriptions {
		if existing.ID != id {
			continue
		}
		if existing.IsBuiltin {
			return fmt.Errorf("cannot delete built-in subscription: %s", id)
		}

		s.config.Subscriptions = append(
			s.config.Subscriptions[:i],
			s.config.Subscriptions[i+1:]...,
		)

		// Remove proxies belonging to the deleted subscription
		filtered := make([]*ProxyEntry, 0, len(s.proxies))
		for _, p := range s.proxies {
			if p.SubscriptionID != id {
				filtered = append(filtered, p)
			}
		}
		s.proxies = filtered
		s.saveProxyCache(s.proxies)

		return s.saveConfig(s.config)
	}

	return fmt.Errorf("subscription %s not found", id)
}

// GetSubscription returns a single subscription by ID.
func (s *Store) GetSubscription(id string) (*Subscription, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, sub := range s.config.Subscriptions {
		if sub.ID == id {
			cp := sub
			return &cp, nil
		}
	}

	return nil, fmt.Errorf("subscription %s not found", id)
}

// ---------- Filters / Strategy (delegate to default profile) ----------

// SetFilters replaces the default profile's filter rules.
func (s *Store) SetFilters(filters *Filter) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	dp := s.defaultProfile()
	dp.Filter = *filters
	return s.saveConfig(s.config)
}

// GetFilters returns the default profile's filter rules.
func (s *Store) GetFilters() *Filter {
	s.mu.RLock()
	defer s.mu.RUnlock()
	dp := s.findDefaultProfile()
	if dp == nil {
		return &Filter{
			IncludeCountries: []string{},
			ExcludeCountries: []string{},
			IncludeRegexes:   []string{},
			ExcludeRegexes:   []string{},
		}
	}
	return cloneFilter(&dp.Filter)
}

// SetStrategy replaces the default profile's routing strategy.
func (s *Store) SetStrategy(strategy *RoutingStrategy) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	dp := s.defaultProfile()
	dp.Strategy = *strategy
	return s.saveConfig(s.config)
}

// GetStrategy returns the default profile's routing strategy.
func (s *Store) GetStrategy() *RoutingStrategy {
	s.mu.RLock()
	defer s.mu.RUnlock()
	dp := s.findDefaultProfile()
	if dp == nil {
		return &RoutingStrategy{Type: "all"}
	}
	cp := dp.Strategy
	return &cp
}

// cloneFilter returns a deep copy of a Filter with non-nil slices.
func cloneFilter(f *Filter) *Filter {
	cp := *f
	cp.IncludeCountries = safeSlice(f.IncludeCountries)
	cp.ExcludeCountries = safeSlice(f.ExcludeCountries)
	cp.IncludeRegexes = safeSlice(f.IncludeRegexes)
	cp.ExcludeRegexes = safeSlice(f.ExcludeRegexes)
	return &cp
}

// safeSlice returns a copy of the slice, or an empty slice if nil.
func safeSlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	cp := make([]string, len(s))
	copy(cp, s)
	return cp
}

// ---------- Profiles ----------

// GetProfiles returns a deep copy of all profiles.
func (s *Store) GetProfiles() []Profile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	data, err := json.Marshal(s.config.Profiles)
	if err != nil {
		cp := make([]Profile, len(s.config.Profiles))
		copy(cp, s.config.Profiles)
		return cp
	}
	var cp []Profile
	if err := json.Unmarshal(data, &cp); err != nil {
		// Fallback: shallow copy
		cp = make([]Profile, len(s.config.Profiles))
		copy(cp, s.config.Profiles)
	}
	return cp
}

// GetProfile returns a single profile by ID.
func (s *Store) GetProfile(id string) (*Profile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.config.Profiles {
		if p.ID == id {
			cp := p
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("profile %s not found", id)
}

// AddProfile creates a new profile. Max MaxProfiles allowed.
func (s *Store) AddProfile(p *Profile) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.config.Profiles) >= MaxProfiles {
		return fmt.Errorf("maximum %d profiles allowed", MaxProfiles)
	}
	if p.ID == "" {
		id, err := generateID()
		if err != nil {
			return err
		}
		p.ID = id
	}
	if p.ID == "default" {
		return fmt.Errorf("'default' id is reserved")
	}
	p.IsDefault = false
	s.config.Profiles = append(s.config.Profiles, *p)
	return s.saveConfig(s.config)
}

// UpdateProfile updates an existing profile by ID.
func (s *Store) UpdateProfile(p *Profile) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, existing := range s.config.Profiles {
		if existing.ID == p.ID {
			// Preserve IsDefault flag
			p.IsDefault = existing.IsDefault
			s.config.Profiles[i] = *p
			return s.saveConfig(s.config)
		}
	}
	return fmt.Errorf("profile %s not found", p.ID)
}

// DeleteProfile removes a profile. Cannot delete the default profile.
func (s *Store) DeleteProfile(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, p := range s.config.Profiles {
		if p.ID == id {
			if p.IsDefault {
				return fmt.Errorf("cannot delete the default profile")
			}
			s.config.Profiles = append(s.config.Profiles[:i], s.config.Profiles[i+1:]...)
			return s.saveConfig(s.config)
		}
	}
	return fmt.Errorf("profile %s not found", id)
}

// findDefaultProfile returns a pointer to the existing default profile, or nil.
// Unlike defaultProfile(), this does NOT mutate the config — safe for read paths.
// Caller must hold at least s.mu.RLock.
func (s *Store) findDefaultProfile() *Profile {
	for i := range s.config.Profiles {
		if s.config.Profiles[i].IsDefault {
			return &s.config.Profiles[i]
		}
	}
	return nil
}

// defaultProfile returns a pointer to the default profile (creates one if missing).
// Caller must hold s.mu.
func (s *Store) defaultProfile() *Profile {
	for i := range s.config.Profiles {
		if s.config.Profiles[i].IsDefault {
			return &s.config.Profiles[i]
		}
	}
	// No default found — create one
	s.config.Profiles = append(s.config.Profiles, Profile{
		ID:        "default",
		Name:      "По умолчанию",
		Enabled:   true,
		IsDefault: true,
		Filter: Filter{
			IncludeCountries: []string{},
			ExcludeCountries: []string{},
			IncludeRegexes:   []string{},
			ExcludeRegexes:   []string{},
		},
		Strategy: RoutingStrategy{Type: "all", Fallback: "direct"},
	})
	return &s.config.Profiles[len(s.config.Profiles)-1]
}

// migrateProfiles converts legacy Filters/Strategy into a default profile.
func (s *Store) migrateProfiles() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.config.Profiles) > 0 {
		return // already migrated
	}

	f := s.config.Filters
	st := s.config.Strategy

	// Build default profile from legacy fields
	dp := Profile{
		ID:        "default",
		Name:      "По умолчанию",
		Enabled:   true,
		IsDefault: true,
	}
	if f != nil {
		dp.Filter = *f
	} else {
		dp.Filter = Filter{
			IncludeCountries: []string{},
			ExcludeCountries: []string{},
			IncludeRegexes:   []string{},
			ExcludeRegexes:   []string{},
		}
	}
	if st != nil {
		dp.Strategy = *st
	} else {
		dp.Strategy = RoutingStrategy{Type: "all"}
	}

	s.config.Profiles = []Profile{dp}

	// Clear legacy fields
	s.config.Filters = nil
	s.config.Strategy = nil
	if err := s.saveConfig(s.config); err != nil {
		log.Printf("[store] failed to clear legacy config: %v", err)
	}
}

// migrateRegexFields converts legacy single-string regex fields to string slices.
// Called once after loading the config. After migration, the legacy fields are cleared.
func (s *Store) migrateRegexFields() {
	s.mu.Lock()
	defer s.mu.Unlock()

	dirty := false
	for i := range s.config.Profiles {
		p := &s.config.Profiles[i]
		if p.Filter.LegacyIncludeRegex != "" && len(p.Filter.IncludeRegexes) == 0 {
			p.Filter.IncludeRegexes = []string{p.Filter.LegacyIncludeRegex}
			p.Filter.LegacyIncludeRegex = ""
			dirty = true
		}
		if p.Filter.LegacyExcludeRegex != "" && len(p.Filter.ExcludeRegexes) == 0 {
			p.Filter.ExcludeRegexes = []string{p.Filter.LegacyExcludeRegex}
			p.Filter.LegacyExcludeRegex = ""
			dirty = true
		}
		// Ensure non-nil slices
		if p.Filter.IncludeRegexes == nil {
			p.Filter.IncludeRegexes = []string{}
		}
		if p.Filter.ExcludeRegexes == nil {
			p.Filter.ExcludeRegexes = []string{}
		}
	}
	if dirty {
		if err := s.saveConfig(s.config); err != nil {
			log.Printf("[store] failed to save regex migration: %v", err)
		}
	}
}

// ---------- Proxies (cached to proxy-cache.json) ----------

// atomicWriteFile writes data to path atomically via temp file + rename.
// Mirror of saveConfig's tmp+rename pattern, usable for other file writes.
func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp) // cleanup on rename failure
		return fmt.Errorf("failed to rename temp file: %w", err)
	}
	return nil
}

// proxyCachePath returns the path for the proxy cache file,
// derived from the store path (subscriptions.json → proxy-cache.json).
func (s *Store) proxyCachePath() string {
	return filepath.Join(filepath.Dir(s.path), "proxy-cache.json")
}

// loadProxyCache reads the proxy cache from disk. Non-critical: errors are ignored.
func (s *Store) loadProxyCache() {
	data, err := os.ReadFile(s.proxyCachePath())
	if err != nil {
		return // no file or unreadable — start empty
	}
	var proxies []*ProxyEntry
	if json.Unmarshal(data, &proxies) != nil {
		return // corrupted — start empty
	}
	s.proxies = proxies
}

// saveProxyCache writes the current proxy cache to disk. Non-critical: errors are logged.
func (s *Store) saveProxyCache(proxies []*ProxyEntry) {
	cachePath := s.proxyCachePath()
	if len(proxies) == 0 {
		_ = os.Remove(cachePath) // clean up empty cache
		return
	}
	data, err := json.Marshal(proxies)
	if err != nil {
		log.Printf("[store] failed to marshal proxy cache: %v", err)
		return
	}
	if err := atomicWriteFile(cachePath, data, 0o600); err != nil {
		log.Printf("[store] failed to write proxy cache: %v", err)
	}
}

// SetProxies replaces the in-memory proxy cache and persists it to disk.
func (s *Store) SetProxies(proxies []*ProxyEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.proxies = proxies
	s.saveProxyCache(proxies)
}

// ReplaceProxiesForSubscription atomically replaces all proxies belonging to
// subscriptionID with newEntries, preserving proxies from other subscriptions.
// The optional transform callback (when non-nil) is invoked on the merged
// slice while the store lock is held, so it can mutate the entries in place
// (e.g. regenerate tags) before the result is persisted.
//
// Proxies with an empty SubscriptionID (orphans from previous versions) are
// removed in the process. The entire read-modify-write cycle runs under the
// store lock, making it safe to call concurrently from multiple goroutines
// (e.g. parallel subscription refreshes).
//
// This is the atomic equivalent of the previous GetProxies → filter → append
// → SetProxies pattern, which was subject to a TOCTOU race that silently
// dropped freshly-fetched proxies when two refreshes overlapped.
func (s *Store) ReplaceProxiesForSubscription(subscriptionID string, newEntries []*ProxyEntry, transform func(merged []*ProxyEntry)) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	merged := make([]*ProxyEntry, 0, len(s.proxies)+len(newEntries))
	for _, p := range s.proxies {
		if p.SubscriptionID == subscriptionID {
			continue // drop old proxies of the refreshed subscription
		}
		if p.SubscriptionID == "" {
			continue // drop orphans
		}
		merged = append(merged, p)
	}
	merged = append(merged, newEntries...)

	if transform != nil {
		transform(merged)
	}

	s.proxies = merged
	s.saveProxyCache(merged)
	return nil
}

// GetProxies returns a deep copy of the in-memory proxy cache.
func (s *Store) GetProxies() []*ProxyEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := make([]*ProxyEntry, len(s.proxies))
	for i, p := range s.proxies {
		pc := *p
		cp[i] = &pc
	}
	return cp
}

// ---------- Auto-Apply ----------

// GetAutoApply returns the current auto-apply configuration.
func (s *Store) GetAutoApply() (enabled bool, cronExpr string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config.AutoApplyEnabled, s.config.AutoApplyCron
}

// SetAutoApply updates the auto-apply configuration and saves.
func (s *Store) SetAutoApply(enabled bool, cronExpr string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config.AutoApplyEnabled = enabled
	s.config.AutoApplyCron = cronExpr
	return s.saveConfig(s.config)
}

// ---------- Generated state ----------

// SetGeneratedAt records when outbounds/routing were last generated and saves.
func (s *Store) SetGeneratedAt(t time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.config.GeneratedAt = t
	return s.saveConfig(s.config)
}

// ---------- Helpers ----------

// generateID creates a random 16-byte hex string.
func generateID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to read random bytes: %w", err)
	}
	return fmt.Sprintf("%x", b), nil
}

// cloneConfig returns a deep copy of cfg via JSON round-trip.
func cloneConfig(cfg *Config) *Config {
	data, err := json.Marshal(cfg)
	if err != nil {
		// Should never happen with our types; return shallow copy.
		cp := *cfg
		return &cp
	}
	var cp Config
	if err := json.Unmarshal(data, &cp); err != nil {
		// Fallback: shallow copy
		cp = *cfg
	}
	return &cp
}
