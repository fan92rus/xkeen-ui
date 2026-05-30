package subscription

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Store manages subscription configuration persistence.
type Store struct {
	mu     sync.RWMutex
	path   string
	config *SubscriptionConfig

	// proxies is the in-memory cache of all parsed proxies from all subscriptions.
	// Updated by the scheduler after a successful fetch cycle.
	proxies []*ProxyEntry
}

// NewStore creates a new store. If the file exists it loads it;
// otherwise it returns a store with sensible defaults.
func NewStore(path string) (*Store, error) {
	s := &Store{
		path: path,
		config: &SubscriptionConfig{
			Subscriptions: []Subscription{},
			Filters:       Filter{},
			Strategy:      RoutingStrategy{Type: "all", FallbackTag: "direct"},
		},
	}

	data, err := os.ReadFile(path)
	if err == nil {
		var cfg SubscriptionConfig
		if jsonErr := json.Unmarshal(data, &cfg); jsonErr == nil {
			s.config = &cfg
		}
		// If the file exists but can't be parsed, keep defaults.
	}

	return s, nil
}

// GetConfig returns a deep copy of the current config.
func (s *Store) GetConfig() *SubscriptionConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneConfig(s.config)
}

// Save persists the current config to disk.
func (s *Store) Save() error {
	s.mu.RLock()
	cfg := cloneConfig(s.config)
	s.mu.RUnlock()

	return s.saveConfig(cfg)
}

// saveConfig writes a config to disk (caller handles locking if needed).
func (s *Store) saveConfig(cfg *SubscriptionConfig) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "    ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Write atomically via temp file.
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	if err := os.Rename(tmp, s.path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("failed to rename config: %w", err)
	}

	return nil
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

// DeleteSubscription removes a subscription by ID.
func (s *Store) DeleteSubscription(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, existing := range s.config.Subscriptions {
		if existing.ID == id {
			s.config.Subscriptions = append(
				s.config.Subscriptions[:i],
				s.config.Subscriptions[i+1:]...,
			)
			return s.saveConfig(s.config)
		}
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

// ---------- Filters ----------

// SetFilters replaces the current filter rules and saves.
func (s *Store) SetFilters(filters *Filter) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.config.Filters = *filters
	return s.saveConfig(s.config)
}

// GetFilters returns a deep copy of the current filters.
func (s *Store) GetFilters() *Filter {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneFilter(&s.config.Filters)
}

// cloneFilter returns a deep copy of a Filter with non-nil slices.
func cloneFilter(f *Filter) *Filter {
	cp := *f
	cp.IncludeMarkers = safeSlice(f.IncludeMarkers)
	cp.ExcludeMarkers = safeSlice(f.ExcludeMarkers)
	cp.IncludeCountries = safeSlice(f.IncludeCountries)
	cp.ExcludeCountries = safeSlice(f.ExcludeCountries)
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

// ---------- Strategy ----------

// SetStrategy replaces the routing strategy and saves.
func (s *Store) SetStrategy(strategy *RoutingStrategy) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.config.Strategy = *strategy
	return s.saveConfig(s.config)
}

// GetStrategy returns a copy of the current routing strategy.
func (s *Store) GetStrategy() *RoutingStrategy {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := s.config.Strategy
	return &cp
}

// ---------- Proxies (in-memory cache) ----------

// SetProxies replaces the in-memory proxy cache. Does NOT persist to disk.
func (s *Store) SetProxies(proxies []*ProxyEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.proxies = proxies
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
func cloneConfig(cfg *SubscriptionConfig) *SubscriptionConfig {
	data, err := json.Marshal(cfg)
	if err != nil {
		// Should never happen with our types; return shallow copy.
		cp := *cfg
		return &cp
	}
	var cp SubscriptionConfig
	json.Unmarshal(data, &cp)
	return &cp
}
