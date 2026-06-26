package subscription

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// AWGBaseMark is the starting fwmark number for AWG interfaces.
const AWGBaseMark = 100

// ScanAWGConfigs scans awgDir for *.conf files and returns the list of
// discovered config names. Already-tracked configs retain their marks;
// new configs get the next available mark starting from AWGBaseMark.
func (s *Store) ScanAWGConfigs(awgDir string) ([]AWGConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := os.ReadDir(awgDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read AWG directory %s: %w", awgDir, err)
	}

	// Collect .conf files sorted by name
	var confFiles []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), ".conf") && !strings.HasPrefix(e.Name(), ".") {
			confFiles = append(confFiles, e.Name())
		}
	}
	sort.Strings(confFiles)

	// Build a set of tracked configs by name
	tracked := make(map[string]bool)
	for _, c := range s.config.AWGConfigs {
		tracked[c.Name] = true
	}

	// Allocate marks: keep existing, assign next available to new ones
	nextMark := AWGBaseMark
	var result []AWGConfig
	for _, fn := range confFiles {
		name := strings.TrimSuffix(fn, ".conf")

		// Check if already tracked
		var existing *AWGConfig
		for i := range s.config.AWGConfigs {
			if s.config.AWGConfigs[i].Name == name {
				existing = &s.config.AWGConfigs[i]
				break
			}
		}

		if existing != nil {
			result = append(result, *existing)
			if existing.Mark >= nextMark {
				nextMark = existing.Mark + 1
			}
		} else {
			// New config — assign next available mark
			cfg := AWGConfig{Name: name, Mark: nextMark}
			result = append(result, cfg)
			nextMark++
		}
	}

	// Update store
	s.config.AWGConfigs = result
	if err := s.saveConfig(s.config); err != nil {
		return nil, fmt.Errorf("failed to save AWG configs: %w", err)
	}

	return result, nil
}

// GenerateAWGProxies creates ProxyEntry objects from AWG configs.
// Each AWG proxy generates a "freedom" outbound with sockopt.mark
// that routes traffic through the corresponding AWG interface.
func GenerateAWGProxies(configs []AWGConfig) []*ProxyEntry {
	proxies := make([]*ProxyEntry, 0, len(configs))
	for _, c := range configs {
		entry := &ProxyEntry{
			Tag:            fmt.Sprintf("awg-%s", c.Name),
			Protocol:       "awg",
			Remarks:        c.Name,
			Country:        "awg",
			SubscriptionID: ReservedAWGSubscriptionID,
		}

		// Build outbound: freedom with sockopt.mark
		outbound := map[string]interface{}{
			"protocol": "freedom",
			"settings": map[string]interface{}{},
			"streamSettings": map[string]interface{}{
				"sockopt": map[string]interface{}{
					"mark": c.Mark,
				},
			},
		}

		raw, _ := json.Marshal(outbound)
		entry.Outbound = raw

		proxies = append(proxies, entry)
	}
	return proxies
}

// RemoveAWGConfig removes an AWG config from the store's tracked list and
// returns the freed mark. Returns false if the config is not tracked.
func (s *Store) RemoveAWGConfig(name string) (mark int, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, c := range s.config.AWGConfigs {
		if c.Name == name {
			mark = c.Mark
			s.config.AWGConfigs = append(s.config.AWGConfigs[:i], s.config.AWGConfigs[i+1:]...)
			s.saveConfig(s.config)
			return mark, true
		}
	}
	return 0, false
}

// GetAWGConfig returns the tracked config for the given name.
func (s *Store) GetAWGConfig(name string) (*AWGConfig, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, c := range s.config.AWGConfigs {
		if c.Name == name {
			cp := c
			return &cp, true
		}
	}
	return nil, false
}

// ListAWGConfigs returns a copy of all tracked AWG configs.
func (s *Store) ListAWGConfigs() []AWGConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := make([]AWGConfig, len(s.config.AWGConfigs))
	copy(cp, s.config.AWGConfigs)
	return cp
}
