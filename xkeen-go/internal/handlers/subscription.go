package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/mux"

	"github.com/fan92rus/xkeen-ui/internal/subscription"
)

// SubscriptionHandler handles subscription management API endpoints.

// API response types for subscription handlers.

type listSubscriptionsResponse struct {
	Subscriptions interface{} `json:"subscriptions"`
	Filters       interface{} `json:"filters"`
	Strategy      interface{} `json:"strategy"`
	GeneratedAt   interface{} `json:"generated_at,omitempty"`
}

type subSuccessResponse struct {
	Success      bool        `json:"success"`
	Subscription interface{} `json:"subscription,omitempty"`
}

type subDeleteResponse struct {
	Success bool   `json:"success"`
	ID      string `json:"id"`
}

type subFetchResponse struct {
	Success    bool        `json:"success"`
	ProxyCount int         `json:"proxy_count"`
	Total      int         `json:"total"`
	Proxies    interface{} `json:"proxies"`
}

type subProxiesResponse struct {
	Total   int         `json:"total"`
	Proxies interface{} `json:"proxies"`
}

type subFiltersResponse struct {
	Success bool        `json:"success"`
	Filters interface{} `json:"filters"`
}

type subStrategyResponse struct {
	Success  bool        `json:"success"`
	Strategy interface{} `json:"strategy"`
}

type subApplyResponse struct {
	Success          bool              `json:"success"`
	ProxyCount       int               `json:"proxy_count"`
	Files            map[string]string `json:"files"`
	RestartInitiated bool              `json:"restart_initiated,omitempty"`
}

type subPreviewResponse struct {
	ProxyCount         int         `json:"proxy_count"`
	FilteredProxyCount int         `json:"filtered_proxy_count"`
	Outbounds          interface{} `json:"outbounds,omitempty"`
	Routing            interface{} `json:"routing,omitempty"`
	Observatory        interface{} `json:"observatory,omitempty"`
	Message            string      `json:"message,omitempty"`
	Profiles           interface{} `json:"profiles,omitempty"`
}

type subScheduleResponse struct {
	Enabled bool        `json:"enabled"`
	Cron    string      `json:"cron"`
	NextRun interface{} `json:"next_run,omitempty"`
}

// SubscriptionHandler handles subscription management endpoints.
type SubscriptionHandler struct {
	store       *subscription.Store
	fetcher     *subscription.Fetcher
	scheduler   *subscription.Scheduler
	xrayDir     string // xray config directory for writing generated files
	mihomoDir   string // mihomo config directory for writing generated files
	awgDir      string // awg config directory for scanning .conf files
	currentMode string // "xray" or "mihomo" — set on construction from config
	restartFn   func() // optional restart function wired from server.go
	mark        int    // sockopt.mark for outbounds (0 = none, 255 = proxy_entware on)
}

// NewSubscriptionHandler creates a new SubscriptionHandler.
func NewSubscriptionHandler(store *subscription.Store, fetcher *subscription.Fetcher, scheduler *subscription.Scheduler, xrayDir, mihomoDir, awgDir, mode string) *SubscriptionHandler {
	return &SubscriptionHandler{
		store:       store,
		fetcher:     fetcher,
		scheduler:   scheduler,
		xrayDir:     xrayDir,
		mihomoDir:   mihomoDir,
		awgDir:      awgDir,
		currentMode: mode,
	}
}

// SetRestartFn sets the restart function called when Apply receives restart:true.
func (h *SubscriptionHandler) SetRestartFn(fn func()) {
	h.restartFn = fn
}

// SetMark sets the sockopt.mark value applied to all outbounds during generation.
// 0 disables marking; 255 enables Entware traffic proxy mode.
func (h *SubscriptionHandler) SetMark(mark int) {
	h.mark = mark
}

// RegenerateOutbounds regenerates only the 04_outbounds.json file with the current
// mark setting and writes it to disk. Does NOT touch routing/observatory.
// Does NOT restart xray (caller's responsibility — e.g. xkeen -pr restarts it).
//
// Used by the proxy_entware toggle: the mark must be written to the config file
// BEFORE xkeen -pr on restarts xray, otherwise xray loads old unmarked outbounds
// and the iptables `--mark 255 -j RETURN` rule won't match Xray-originated packets.
func (h *SubscriptionHandler) RegenerateOutbounds() error {
	allProxies := h.store.GetProxies()
	outboundsJSON, err := subscription.GenerateOutboundsJSON(allProxies, h.mark)
	if err != nil {
		return fmt.Errorf("generate outbounds: %w", err)
	}
	outboundsPath := h.xrayDir + "/04_outbounds.json"
	if err := os.MkdirAll(h.xrayDir, 0o750); err != nil {
		return fmt.Errorf("mkdir %s: %w", h.xrayDir, err)
	}
	if err := atomicWriteAll(map[string][]byte{outboundsPath: outboundsJSON}); err != nil {
		return fmt.Errorf("write %s: %w", outboundsPath, err)
	}
	return nil
}

// Stop gracefully stops the scheduler.
func (h *SubscriptionHandler) Stop() {
	if h.scheduler != nil {
		h.scheduler.Stop()
	}
}

// ---------- CRUD ----------

// ListSubscriptions returns all subscriptions, filters, and strategy.
// GET /api/subscriptions
func (h *SubscriptionHandler) ListSubscriptions(w http.ResponseWriter, _ *http.Request) {
	cfg := h.store.GetConfig()
	respondJSON(w, http.StatusOK, &listSubscriptionsResponse{
		Subscriptions: cfg.Subscriptions,
		Filters:       h.store.GetFilters(),
		Strategy:      h.store.GetStrategy(),
		GeneratedAt:   cfg.GeneratedAt,
	})
}

// AddSubscription adds a new subscription source.
// POST /api/subscriptions
func (h *SubscriptionHandler) AddSubscription(w http.ResponseWriter, r *http.Request) {
	var req subscription.Subscription
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
		return
	}

	if req.URL == "" {
		respondError(w, http.StatusBadRequest, "url is required")
		return
	}
	if req.Name == "" {
		req.Name = "New Subscription"
	}

	if err := h.store.AddSubscription(&req); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to add subscription: %v", err))
		return
	}

	respondJSON(w, http.StatusCreated, &subSuccessResponse{Success: true, Subscription: req})
}

// UpdateSubscription updates an existing subscription.
// PUT /api/subscriptions/{id}
func (h *SubscriptionHandler) UpdateSubscription(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if id == "" {
		respondError(w, http.StatusBadRequest, "subscription id is required")
		return
	}

	var req subscription.Subscription
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
		return
	}

	req.ID = id

	if err := h.store.UpdateSubscription(&req); err != nil {
		respondError(w, http.StatusNotFound, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, &subSuccessResponse{Success: true, Subscription: req})
}

// DeleteSubscription removes a subscription.
// DELETE /api/subscriptions/{id}
func (h *SubscriptionHandler) DeleteSubscription(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if id == "" {
		respondError(w, http.StatusBadRequest, "subscription id is required")
		return
	}

	if err := h.store.DeleteSubscription(id); err != nil {
		respondError(w, http.StatusNotFound, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, &subDeleteResponse{Success: true, ID: id})
}

// ---------- Fetch ----------

// FetchSubscription manually fetches a single subscription and returns proxies.
// POST /api/subscriptions/{id}/fetch
func (h *SubscriptionHandler) FetchSubscription(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if id == "" {
		respondError(w, http.StatusBadRequest, "subscription id is required")
		return
	}

	sub, err := h.store.GetSubscription(id)
	if err != nil {
		respondError(w, http.StatusNotFound, err.Error())
		return
	}

	// Handle built-in AWG subscription: scan .conf files instead of HTTP fetch
	if id == subscription.ReservedAWGSubscriptionID {
		h.fetchAWG(w, r, sub)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	result, err := h.fetcher.FetchWithCascade(ctx, sub.URL)
	if err != nil {
		// Update error state
		sub.LastError = err.Error()
		sub.LastFetch = time.Now()
		_ = h.store.UpdateSubscription(sub)

		respondError(w, http.StatusBadGateway, fmt.Sprintf("fetch failed: %v", err))
		return
	}

	// Update subscription metadata
	sub.LastFetch = time.Now()
	sub.LastError = ""
	sub.ProxyCount = len(result.Entries)
	sub.LastSource = result.Source
	_ = h.store.UpdateSubscription(sub)

	// Tag entries with subscription ID
	for _, e := range result.Entries {
		e.SubscriptionID = id
	}

	// Atomically replace proxies for this subscription while keeping others.
	// Uses a store-level lock to avoid the TOCTOU race that a manual
	// GetProxies → modify → SetProxies sequence would introduce when
	// multiple refreshes overlap (manual + auto-apply).
	if err := h.store.ReplaceProxiesForSubscription(id, result.Entries, func(merged []*subscription.ProxyEntry) {
		subscription.GenerateTags(merged)
	}); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to update proxy cache: %v", err))
		return
	}

	respondJSON(w, http.StatusOK, &subFetchResponse{Success: true, ProxyCount: len(result.Entries), Total: len(result.Entries), Proxies: result.Entries})
}

// fetchAWG handles fetching for the built-in AWG subscription — scans .conf files.
func (h *SubscriptionHandler) fetchAWG(w http.ResponseWriter, _ *http.Request, sub *subscription.Subscription) {
	configs, err := h.store.ScanAWGConfigs(h.awgDir)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to scan AWG configs: %v", err))
		return
	}

	entries := subscription.GenerateAWGProxies(configs)

	// Update subscription metadata
	sub.LastFetch = time.Now()
	sub.LastError = ""
	sub.ProxyCount = len(entries)
	sub.LastSource = "awg-local" // AWG reads local .conf files, no network fetch
	_ = h.store.UpdateSubscription(sub)

	// Tag entries with subscription ID
	for _, e := range entries {
		e.SubscriptionID = subscription.ReservedAWGSubscriptionID
	}

	// Atomically replace AWG proxies in the pool, keep others.
	// Same TOCTOU-safe pattern as FetchSubscription.
	if err := h.store.ReplaceProxiesForSubscription(subscription.ReservedAWGSubscriptionID, entries, func(merged []*subscription.ProxyEntry) {
		subscription.GenerateTags(merged)
	}); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to update AWG proxy cache: %v", err))
		return
	}

	respondJSON(w, http.StatusOK, &subFetchResponse{
		Success:    true,
		ProxyCount: len(entries),
		Total:      len(entries),
		Proxies:    entries,
	})
}

// ---------- Proxies ----------

// GetProxies returns all cached proxies.
// GET /api/subscriptions/proxies
func (h *SubscriptionHandler) GetProxies(w http.ResponseWriter, _ *http.Request) {
	allProxies := h.store.GetProxies()

	respondJSON(w, http.StatusOK, &subProxiesResponse{Total: len(allProxies), Proxies: allProxies})
}

// ---------- Filters ----------

// GetFilters returns current filter rules.
// GET /api/subscriptions/filters
func (h *SubscriptionHandler) GetFilters(w http.ResponseWriter, _ *http.Request) {
	filters := h.store.GetFilters()
	respondJSON(w, http.StatusOK, filters)
}

// UpdateFilters replaces filter rules.
// PUT /api/subscriptions/filters
func (h *SubscriptionHandler) UpdateFilters(w http.ResponseWriter, r *http.Request) {
	var req subscription.Filter
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
		return
	}

	if err := subscription.ValidateRegexes(&req); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := h.store.SetFilters(&req); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to save filters: %v", err))
		return
	}

	respondJSON(w, http.StatusOK, &subFiltersResponse{Success: true, Filters: req})
}

// ---------- Strategy ----------

// GetStrategy returns current routing strategy.
// GET /api/subscriptions/strategy
func (h *SubscriptionHandler) GetStrategy(w http.ResponseWriter, _ *http.Request) {
	strategy := h.store.GetStrategy()
	respondJSON(w, http.StatusOK, strategy)
}

// UpdateStrategy replaces routing strategy.
// PUT /api/subscriptions/strategy
func (h *SubscriptionHandler) UpdateStrategy(w http.ResponseWriter, r *http.Request) {
	var req subscription.RoutingStrategy
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
		return
	}

	// Validate strategy type
	valid := false
	for _, t := range subscription.StrategyTypes {
		if req.Type == t {
			valid = true
			break
		}
	}
	if !valid {
		respondError(w, http.StatusBadRequest, fmt.Sprintf("invalid strategy type %q; must be one of %v", req.Type, subscription.StrategyTypes))
		return
	}

	if err := h.store.SetStrategy(&req); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to save strategy: %v", err))
		return
	}

	respondJSON(w, http.StatusOK, &subStrategyResponse{Success: true, Strategy: req})
}

// ---------- Apply / Preview ----------

// Apply generates outbounds, routing, and observatory files and writes them to disk.
// POST /api/subscriptions/apply
func (h *SubscriptionHandler) Apply(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Restart            bool `json:"restart"`
		ConvertXrayRouting bool `json:"convert_xray_routing,omitempty"`
	}
	// Body is optional
	if r.Body != nil && r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
			return
		}
	}

	// Get all proxies and profiles
	allProxies := h.store.GetProxies()
	profiles := h.store.GetProfiles()

	if len(allProxies) == 0 {
		respondError(w, http.StatusBadRequest, "no proxies available; fetch subscriptions first")
		return
	}

	// Collect only proxies needed by enabled profiles (respecting filters)
	filteredProxies := subscription.CollectFilteredProxies(allProxies, profiles)

	if len(filteredProxies) == 0 {
		respondError(w, http.StatusBadRequest, "no proxies pass the current filters; adjust filter settings")
		return
	}

	if h.currentMode == "mihomo" {
		h.applyMihomo(w, r, filteredProxies, profiles, req.ConvertXrayRouting, req.Restart)
		return
	}

	// ── Xray mode (existing behavior) ──
	var (
		observatoryJSON []byte
		outboundsPath   = h.xrayDir + "/04_outbounds.json"
		routingPath     = h.xrayDir + "/05_routing.json"
		observatoryPath = h.xrayDir + "/07_observatory.json"
	)

	if err := h.scheduler.WithApplyLock(func() error {
		outboundsJSON, err := subscription.GenerateOutboundsJSON(filteredProxies, h.mark)
		if err != nil {
			return fmt.Errorf("failed to generate outbounds: %v", err)
		}

		var existingRouting json.RawMessage
		if data, err := os.ReadFile(routingPath); err == nil {
			existingRouting = data
		}

		routingJSON, err := subscription.GenerateRoutingJSON(filteredProxies, profiles, existingRouting)
		if err != nil {
			return fmt.Errorf("failed to generate routing: %v", err)
		}

		if subscription.NeedsObservatory(profiles) {
			observatoryJSON, err = subscription.GenerateObservatoryJSON()
			if err != nil {
				return fmt.Errorf("failed to generate observatory: %v", err)
			}
		}

		if err := os.MkdirAll(h.xrayDir, 0o750); err != nil {
			return fmt.Errorf("failed to create config directory: %v", err)
		}

		files := map[string][]byte{
			outboundsPath: outboundsJSON,
			routingPath:   routingJSON,
		}
		if observatoryJSON != nil {
			files[observatoryPath] = observatoryJSON
		}
		if err := atomicWriteAll(files); err != nil {
			return fmt.Errorf("failed to write config files: %v", err)
		}

		return nil
	}); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if observatoryJSON == nil {
		_ = os.Remove(observatoryPath)
	}

	_ = h.store.SetGeneratedAt(time.Now())
	if req.Restart && h.restartFn != nil {
		go h.restartFn()
	}

	log.Printf("[subscription] applied %d/%d proxies with %d profiles: %s, %s", len(filteredProxies), len(allProxies), len(profiles), outboundsPath, routingPath)

	files := map[string]string{
		"outbounds":   outboundsPath,
		"routing":     routingPath,
		"observatory": "",
	}
	if observatoryJSON != nil {
		files["observatory"] = observatoryPath
	}
	resp := &subApplyResponse{Success: true, ProxyCount: len(filteredProxies), Files: files}
	if req.Restart && h.restartFn != nil {
		resp.RestartInitiated = true
	}

	respondJSON(w, http.StatusOK, resp)
}

// applyMihomo generates and writes a Mihomo config.yaml from subscription data.
func (h *SubscriptionHandler) applyMihomo(w http.ResponseWriter, _ *http.Request, proxies []*subscription.ProxyEntry, profiles []subscription.Profile, convertRouting, restart bool) {
	configPath := h.mihomoDir + "/config.yaml"

	var (
		existingConfig string
		xrayRouting    []byte
	)

	// Read existing Mihomo config for mix-in
	if data, err := os.ReadFile(configPath); err == nil {
		existingConfig = string(data)
	}

	// Read existing Xray routing if routing conversion is requested
	if convertRouting {
		xrayRouting, _ = os.ReadFile(h.xrayDir + "/05_routing.json")
	}

	var finalYAML string
	if err := h.scheduler.WithApplyLock(func() error {
		// Step 1: Generate Mihomo proxies, groups, and rules from subscription data
		genYAML, err := subscription.GenerateMihomoConfig(proxies, profiles, xrayRouting)
		if err != nil {
			return fmt.Errorf("failed to generate Mihomo config: %v", err)
		}

		// Step 2: Merge with existing config.yaml (preserve dns, tun, port, etc.)
		merged, err := subscription.MergeMihomoConfig(genYAML, existingConfig)
		if err != nil {
			return fmt.Errorf("failed to merge with existing config: %v", err)
		}
		finalYAML = merged

		if err := os.MkdirAll(h.mihomoDir, 0o750); err != nil {
			return fmt.Errorf("failed to create Mihomo config directory: %v", err)
		}

		if err := os.WriteFile(configPath, []byte(finalYAML), 0o600); err != nil {
			return fmt.Errorf("failed to write Mihomo config: %v", err)
		}
		return nil
	}); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	_ = h.store.SetGeneratedAt(time.Now())
	if restart && h.restartFn != nil {
		go h.restartFn()
	}

	log.Printf("[subscription] applied %d proxies to Mihomo config: %s", len(proxies), configPath)

	files := map[string]string{"config": configPath}
	resp := &subApplyResponse{Success: true, ProxyCount: len(proxies), Files: files}
	if restart && h.restartFn != nil {
		resp.RestartInitiated = true
	}

	respondJSON(w, http.StatusOK, resp)
}

// Preview returns a dry-run of what Apply would generate, without writing files.
// GET /api/subscriptions/preview
func (h *SubscriptionHandler) Preview(w http.ResponseWriter, _ *http.Request) {
	allProxies := h.store.GetProxies()
	profiles := h.store.GetProfiles()

	if len(allProxies) == 0 {
		respondJSON(w, http.StatusOK, &subPreviewResponse{ProxyCount: 0, FilteredProxyCount: 0, Message: "no proxies available"})
		return
	}

	// Collect only proxies needed by enabled profiles (respecting filters)
	filteredProxies := subscription.CollectFilteredProxies(allProxies, profiles)

	if len(filteredProxies) == 0 {
		respondJSON(w, http.StatusOK, &subPreviewResponse{
			ProxyCount: len(allProxies), FilteredProxyCount: 0,
			Message: "no proxies pass the current filters",
		})
		return
	}

	outboundsJSON, err := subscription.GenerateOutboundsJSON(filteredProxies, h.mark)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to generate outbounds: %v", err))
		return
	}

	// Read existing routing for context
	var existingRouting json.RawMessage
	routingPath := h.xrayDir + "/05_routing.json"
	if data, err := os.ReadFile(routingPath); err == nil {
		existingRouting = data
	}

	// Generate routing from the same filtered list as outbounds (see Apply handler).
	routingJSON, err := subscription.GenerateRoutingJSON(filteredProxies, profiles, existingRouting)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to generate routing: %v", err))
		return
	}

	var observatoryJSON json.RawMessage
	if subscription.NeedsObservatory(profiles) {
		if obs, err := subscription.GenerateObservatoryJSON(); err == nil {
			observatoryJSON = obs
		}
	}

	respondJSON(w, http.StatusOK, &subPreviewResponse{
		ProxyCount: len(allProxies), FilteredProxyCount: len(filteredProxies),
		Outbounds:   json.RawMessage(outboundsJSON),
		Routing:     json.RawMessage(routingJSON),
		Observatory: observatoryJSON,
		Profiles:    profiles,
	})
}

// atomicWriteAll writes a set of files atomically using tmp + rename.
// Each file is written to path+".tmp", then renamed atomically via os.Rename.
// If any write or rename fails, leftover .tmp files are cleaned up and the error is returned.
// Already-renamed files are NOT rolled back (each rename is atomic).
func atomicWriteAll(files map[string][]byte) error {
	// Track tmp files for cleanup on error
	tmpFiles := make([]string, 0, len(files))

	for path, data := range files {
		tmpPath := path + ".tmp"
		if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
			// Clean up all tmp files
			for _, tf := range tmpFiles {
				_ = os.Remove(tf)
			}
			return fmt.Errorf("failed to write %s: %w", tmpPath, err)
		}
		tmpFiles = append(tmpFiles, tmpPath)

		if err := os.Rename(tmpPath, path); err != nil {
			// Clean up all tmp files
			for _, tf := range tmpFiles {
				_ = os.Remove(tf)
			}
			return fmt.Errorf("failed to rename %s -> %s: %w", tmpPath, path, err)
		}
	}

	return nil
}

// ---------- Profiles ----------

// ListProfiles returns all profiles with proxy counts.
// GET /api/subscriptions/profiles
func (h *SubscriptionHandler) ListProfiles(w http.ResponseWriter, _ *http.Request) {
	profiles := h.store.GetProfiles()
	allProxies := h.store.GetProxies()

	type profileWithCount struct {
		subscription.Profile
		ProxyCount int `json:"proxy_count"`
		TotalProxy int `json:"total_proxy"`
	}

	result := make([]profileWithCount, len(profiles))
	for i, p := range profiles {
		filtered := subscription.ApplyFilter(allProxies, &p.Filter)
		result[i] = profileWithCount{Profile: p, ProxyCount: len(filtered), TotalProxy: len(allProxies)}
	}

	respondJSON(w, http.StatusOK, result)
}

// CreateProfile adds a new profile.
// POST /api/subscriptions/profiles
func (h *SubscriptionHandler) CreateProfile(w http.ResponseWriter, r *http.Request) {
	var p subscription.Profile
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		respondError(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
		return
	}
	if p.Name == "" {
		respondError(w, http.StatusBadRequest, "name is required")
		return
	}
	if err := subscription.ValidateRegexes(&p.Filter); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.store.AddProfile(&p); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusCreated, p)
}

// UpdateProfile updates an existing profile.
// PUT /api/subscriptions/profiles/{id}
func (h *SubscriptionHandler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	var p subscription.Profile
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		respondError(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
		return
	}
	p.ID = id
	if err := subscription.ValidateRegexes(&p.Filter); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.store.UpdateProfile(&p); err != nil {
		respondError(w, http.StatusNotFound, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, p)
}

// DeleteProfile removes a profile (cannot delete default).
// DELETE /api/subscriptions/profiles/{id}
func (h *SubscriptionHandler) DeleteProfile(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if err := h.store.DeleteProfile(id); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ---------- Registration ----------

// ---------- Auto-Apply ----------

// GetAutoApply returns the current auto-apply configuration.
// GET /api/subscriptions/auto-apply
func (h *SubscriptionHandler) GetAutoApply(w http.ResponseWriter, _ *http.Request) {
	enabled, cronExpr := h.store.GetAutoApply()
	nextRun := h.scheduler.GetNextRun()

	respondJSON(w, http.StatusOK, &subScheduleResponse{Enabled: enabled, Cron: cronExpr, NextRun: nextRun})
}

// UpdateAutoApplyRequest is the request body for UpdateAutoApply.
type UpdateAutoApplyRequest struct {
	Enabled bool   `json:"enabled"`
	Cron    string `json:"cron"`
}

// UpdateAutoApply updates the auto-apply configuration.
// PUT /api/subscriptions/auto-apply
func (h *SubscriptionHandler) UpdateAutoApply(w http.ResponseWriter, r *http.Request) {
	var req UpdateAutoApplyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
		return
	}

	// Validate cron expression before saving
	if req.Enabled && req.Cron != "" {
		if err := h.scheduler.UpdateAutoApply(req.Enabled, req.Cron); err != nil {
			respondError(w, http.StatusBadRequest, fmt.Sprintf("invalid cron expression: %v", err))
			return
		}
	} else {
		_ = h.scheduler.UpdateAutoApply(false, "")
	}

	// Persist to store
	if err := h.store.SetAutoApply(req.Enabled, req.Cron); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to save auto-apply: %v", err))
		return
	}

	nextRun := h.scheduler.GetNextRun()
	respondJSON(w, http.StatusOK, &subScheduleResponse{Enabled: req.Enabled, Cron: req.Cron, NextRun: nextRun})
}

// RegisterSubscriptionRoutes registers subscription-related routes.
func RegisterSubscriptionRoutes(r *mux.Router, handler *SubscriptionHandler) {
	// IMPORTANT: specific literal routes MUST be registered before {id} routes,
	// otherwise mux matches /subscriptions/strategy as {id}="strategy"
	r.HandleFunc("/subscriptions", handler.ListSubscriptions).Methods("GET")
	r.HandleFunc("/subscriptions", handler.AddSubscription).Methods("POST")

	// Specific paths before {id} catch-all
	r.HandleFunc("/subscriptions/proxies", handler.GetProxies).Methods("GET")
	r.HandleFunc("/subscriptions/filters", handler.GetFilters).Methods("GET")
	r.HandleFunc("/subscriptions/filters", handler.UpdateFilters).Methods("PUT")
	r.HandleFunc("/subscriptions/strategy", handler.GetStrategy).Methods("GET")
	r.HandleFunc("/subscriptions/strategy", handler.UpdateStrategy).Methods("PUT")
	r.HandleFunc("/subscriptions/apply", handler.Apply).Methods("POST")
	r.HandleFunc("/subscriptions/preview", handler.Preview).Methods("GET")

	// Profiles
	r.HandleFunc("/subscriptions/profiles", handler.ListProfiles).Methods("GET")
	r.HandleFunc("/subscriptions/profiles", handler.CreateProfile).Methods("POST")
	r.HandleFunc("/subscriptions/profiles/{id}", handler.UpdateProfile).Methods("PUT")
	r.HandleFunc("/subscriptions/profiles/{id}", handler.DeleteProfile).Methods("DELETE")

	// Auto-apply cron settings
	r.HandleFunc("/subscriptions/auto-apply", handler.GetAutoApply).Methods("GET")
	r.HandleFunc("/subscriptions/auto-apply", handler.UpdateAutoApply).Methods("PUT")

	// {id} routes last
	r.HandleFunc("/subscriptions/{id}", handler.UpdateSubscription).Methods("PUT")
	r.HandleFunc("/subscriptions/{id}", handler.DeleteSubscription).Methods("DELETE")
	r.HandleFunc("/subscriptions/{id}/fetch", handler.FetchSubscription).Methods("POST")
}
