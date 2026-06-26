// Package handlers provides HTTP handlers for XKEEN-UI API endpoints.
package handlers

import (
	"context"
	"log"
	"os/exec"
	"sync"
	"time"
)

// CommandRegistry is the runtime-loaded whitelist of xkeen CLI commands.
//
// Commands are discovered by executing `<xkeenPath> -help` and parsing the
// output (see parseHelp). The result is cached for CacheTTL (300 min).
// After expiry the next access triggers a re-load. The cache also resets on
// server restart (process lifetime). Call Refresh() to force a re-load.
//
// If xkeen is unavailable, fails, or times out, the registry serves an EMPTY
// command set (by design — no hardcoded fallback), so the UI simply shows no
// commands until xkeen is installed/available.
//
// CommandRegistry is safe for concurrent use.

// CacheTTL is how long the command list stays cached before a refresh.
const CacheTTL = 300 * time.Minute

type CommandRegistry struct {
	mu        sync.RWMutex
	xkeenPath string
	loader    func() (map[string]CommandConfig, error) // injectable for tests
	cache     map[string]CommandConfig
	loaded    bool
	loadedAt  time.Time
	loadErr   string
}

// NewCommandRegistry creates a registry that loads commands from
// `<xkeenPath> -help` lazily on first access. Pass DefaultXKeenPath for the
// standard router install path.
func NewCommandRegistry(xkeenPath string) *CommandRegistry {
	r := &CommandRegistry{
		xkeenPath: xkeenPath,
		cache:     map[string]CommandConfig{},
	}
	r.loader = r.loadFromXkeen
	return r
}

// newCommandRegistryWithLoader creates a registry with a custom loader (tests).
// The loader is invoked once lazily; returning an error yields an empty set.
func newCommandRegistryWithLoader(loader func() (map[string]CommandConfig, error)) *CommandRegistry {
	return &CommandRegistry{
		loader: loader,
		cache:  map[string]CommandConfig{},
		loaded: false, // explicitly unloaded; ensureLoaded() will load on first call
	}
}

// ensureLoaded loads or re-loads the command list if the cache is stale.
// The cache is valid for CacheTTL (300 min). After expiry, the next access
// triggers a re-load. On first-load error we cache the empty result for the
// full TTL to avoid hammering xkeen. Use Refresh() to force a retry.
func (r *CommandRegistry) ensureLoaded() {
	r.mu.RLock()
	if r.loaded && time.Since(r.loadedAt) < CacheTTL {
		r.mu.RUnlock()
		return
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.loaded && time.Since(r.loadedAt) < CacheTTL {
		return // double-check after acquiring write lock
	}
	cmds, err := r.loader()
	if err != nil {
		if r.loaded {
			// Refresh error — keep existing cache, don't extend TTL
			log.Printf("CommandRegistry: refresh failed (%s -help): %v — keeping previous cache", r.xkeenPath, err)
			r.loadErr = err.Error()
			return
		}
		// First load error — cache the empty set for the full TTL
		log.Printf("CommandRegistry: load failed (%s -help): %v — serving empty command set", r.xkeenPath, err)
		r.cache = map[string]CommandConfig{}
		r.loadErr = err.Error()
		r.loaded = true
		r.loadedAt = time.Now()
		return
	}
	if cmds == nil {
		r.cache = map[string]CommandConfig{}
	} else {
		r.cache = cmds
	}
	r.loadErr = ""
	r.loaded = true
	r.loadedAt = time.Now()
}

// loadFromXkeen runs `<xkeenPath> -help` with a timeout and parses the output.
func (r *CommandRegistry) loadFromXkeen() (map[string]CommandConfig, error) {
	ctx, cancel := context.WithTimeout(context.Background(), HelpTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, r.xkeenPath, "-help")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, err
	}
	return parseHelp(stripANSI(string(output))), nil
}

// Get returns the config for a command flag and whether it exists.
func (r *CommandRegistry) Get(cmd string) (CommandConfig, bool) {
	r.ensureLoaded()
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.cache[cmd]
	return c, ok
}

// All returns all registered commands (order is not guaranteed).
func (r *CommandRegistry) All() []CommandConfig {
	r.ensureLoaded()
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]CommandConfig, 0, len(r.cache))
	for _, c := range r.cache {
		out = append(out, c)
	}
	return out
}

// Count returns the number of registered commands (forces a load).
func (r *CommandRegistry) Count() int {
	r.ensureLoaded()
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.cache)
}

// LoadError returns the last load error message (empty if load succeeded).
func (r *CommandRegistry) LoadError() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.loadErr
}

// Refresh re-runs the loader and replaces the cache. Useful after xkeen is
// updated (e.g. via -uk) so new commands become available without a restart.
func (r *CommandRegistry) Refresh() {
	r.mu.Lock()
	defer r.mu.Unlock()
	cmds, err := r.loader()
	if err != nil {
		r.loadErr = err.Error()
		log.Printf("CommandRegistry: refresh failed (%s -help): %v — keeping previous command set", r.xkeenPath, err)
		return // keep existing cache on refresh error
	}
	if cmds == nil {
		r.cache = map[string]CommandConfig{}
	} else {
		r.cache = cmds
	}
	r.loadErr = ""
	r.loaded = true
	r.loadedAt = time.Now()
}
