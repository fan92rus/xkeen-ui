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
type SubscriptionHandler struct {
	store     *subscription.Store
	fetcher   *subscription.Fetcher
	scheduler *subscription.Scheduler
	xrayDir   string // xray config directory for writing generated files
}

// NewSubscriptionHandler creates a new SubscriptionHandler.
func NewSubscriptionHandler(store *subscription.Store, fetcher *subscription.Fetcher, scheduler *subscription.Scheduler, xrayDir string) *SubscriptionHandler {
	return &SubscriptionHandler{
		store:     store,
		fetcher:   fetcher,
		scheduler: scheduler,
		xrayDir:   xrayDir,
	}
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
func (h *SubscriptionHandler) ListSubscriptions(w http.ResponseWriter, r *http.Request) {
	cfg := h.store.GetConfig()
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"subscriptions": cfg.Subscriptions,
		"filters":       h.store.GetFilters(),
		"strategy":      h.store.GetStrategy(),
		"generated_at":  cfg.GeneratedAt,
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

	respondJSON(w, http.StatusCreated, map[string]interface{}{
		"success":      true,
		"subscription": req,
	})
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

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"success":      true,
		"subscription": req,
	})
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

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"id":      id,
	})
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

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	entries, err := h.fetcher.Fetch(ctx, sub.URL)
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
	sub.ProxyCount = len(entries)
	_ = h.store.UpdateSubscription(sub)

	// Tag entries with subscription ID
	for _, e := range entries {
		e.SubscriptionID = id
	}

	// Replace proxies for this subscription, keep others (skip orphaned entries without subscription_id)
	existing := h.store.GetProxies()
	merged := make([]*subscription.ProxyEntry, 0, len(existing)+len(entries))
	for _, p := range existing {
		if p.SubscriptionID == id {
			continue // remove old proxies from this subscription
		}
		if p.SubscriptionID == "" {
			continue // remove orphaned proxies from previous versions
		}
		merged = append(merged, p)
	}
	merged = append(merged, entries...)
	h.store.SetProxies(merged)

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"success":     true,
		"proxy_count": len(entries),
		"total":       len(entries),
		"proxies":     entries,
	})
}

// ---------- Proxies ----------

// GetProxies returns all cached proxies.
// GET /api/subscriptions/proxies
func (h *SubscriptionHandler) GetProxies(w http.ResponseWriter, r *http.Request) {
	allProxies := h.store.GetProxies()

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"total":   len(allProxies),
		"proxies": allProxies,
	})
}

// ---------- Filters ----------

// GetFilters returns current filter rules.
// GET /api/subscriptions/filters
func (h *SubscriptionHandler) GetFilters(w http.ResponseWriter, r *http.Request) {
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

	if err := h.store.SetFilters(&req); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to save filters: %v", err))
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"filters": req,
	})
}

// ---------- Strategy ----------

// GetStrategy returns current routing strategy.
// GET /api/subscriptions/strategy
func (h *SubscriptionHandler) GetStrategy(w http.ResponseWriter, r *http.Request) {
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

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"strategy": req,
	})
}

// ---------- Apply / Preview ----------

// Apply generates outbounds, routing, and observatory files and writes them to disk.
// POST /api/subscriptions/apply
func (h *SubscriptionHandler) Apply(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Restart bool `json:"restart"`
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

	// Generate outbounds for filtered proxies only
	outboundsJSON, err := subscription.GenerateOutboundsJSON(filteredProxies)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to generate outbounds: %v", err))
		return
	}

	// Read existing routing (if any)
	var existingRouting json.RawMessage
	routingPath := h.xrayDir + "/05_routing.json"
	if data, err := os.ReadFile(routingPath); err == nil {
		existingRouting = data
	}

	// Generate routing with all profiles
	routingJSON, err := subscription.GenerateRoutingJSON(allProxies, profiles, existingRouting)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to generate routing: %v", err))
		return
	}

	// Generate observatory if any profile needs it
	var observatoryJSON []byte
	if subscription.NeedsObservatory(profiles) {
		observatoryJSON, err = subscription.GenerateObservatoryJSON()
		if err != nil {
			respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to generate observatory: %v", err))
			return
		}
	}

	// Ensure directory exists
	if err := os.MkdirAll(h.xrayDir, 0755); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create config directory: %v", err))
		return
	}

	// Write outbounds
	outboundsPath := h.xrayDir + "/04_outbounds.json"
	if err := os.WriteFile(outboundsPath, outboundsJSON, 0644); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to write outbounds: %v", err))
		return
	}

	// Write routing
	if err := os.WriteFile(routingPath, routingJSON, 0644); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to write routing: %v", err))
		return
	}

	// Write/remove observatory
	observatoryPath := h.xrayDir + "/07_observatory.json"
	if observatoryJSON != nil {
		if err := os.WriteFile(observatoryPath, observatoryJSON, 0644); err != nil {
			respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to write observatory: %v", err))
			return
		}
	} else {
		// Remove observatory file if it exists but is no longer needed
		os.Remove(observatoryPath)
	}

	// Update generated timestamp
	_ = h.store.SetGeneratedAt(time.Now())

	log.Printf("[subscription] applied %d/%d proxies with %d profiles: %s, %s", len(filteredProxies), len(allProxies), len(profiles), outboundsPath, routingPath)

	response := map[string]interface{}{
		"success":     true,
		"proxy_count": len(filteredProxies),
		"files": map[string]string{
			"outbounds":   outboundsPath,
			"routing":     routingPath,
			"observatory": "",
		},
	}
	if observatoryJSON != nil {
		response["files"].(map[string]string)["observatory"] = observatoryPath
	}

	respondJSON(w, http.StatusOK, response)
}

// Preview returns a dry-run of what Apply would generate, without writing files.
// GET /api/subscriptions/preview
func (h *SubscriptionHandler) Preview(w http.ResponseWriter, r *http.Request) {
	allProxies := h.store.GetProxies()
	profiles := h.store.GetProfiles()

	if len(allProxies) == 0 {
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"proxy_count":         0,
			"filtered_proxy_count": 0,
			"outbounds":           nil,
			"routing":             nil,
			"observatory":         nil,
			"message":             "no proxies available",
		})
		return
	}

	// Collect only proxies needed by enabled profiles (respecting filters)
	filteredProxies := subscription.CollectFilteredProxies(allProxies, profiles)

	if len(filteredProxies) == 0 {
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"proxy_count":         len(allProxies),
			"filtered_proxy_count": 0,
			"outbounds":           nil,
			"routing":             nil,
			"observatory":         nil,
			"message":             "no proxies pass the current filters",
		})
		return
	}

	outboundsJSON, err := subscription.GenerateOutboundsJSON(filteredProxies)
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

	routingJSON, err := subscription.GenerateRoutingJSON(allProxies, profiles, existingRouting)
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

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"proxy_count":         len(allProxies),
		"filtered_proxy_count": len(filteredProxies),
		"outbounds":           json.RawMessage(outboundsJSON),
		"routing":             json.RawMessage(routingJSON),
		"observatory":         observatoryJSON,
		"profiles":            profiles,
	})
}

// ---------- Profiles ----------

// ListProfiles returns all profiles with proxy counts.
// GET /api/subscriptions/profiles
func (h *SubscriptionHandler) ListProfiles(w http.ResponseWriter, r *http.Request) {
	profiles := h.store.GetProfiles()
	allProxies := h.store.GetProxies()

	type profileWithCount struct {
		subscription.Profile
		ProxyCount  int `json:"proxy_count"`
		TotalProxy  int `json:"total_proxy"`
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
func (h *SubscriptionHandler) GetAutoApply(w http.ResponseWriter, r *http.Request) {
	enabled, cronExpr := h.store.GetAutoApply()
	nextRun := h.scheduler.GetNextRun()

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"enabled":   enabled,
		"cron":      cronExpr,
		"next_run":  nextRun,
	})
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
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"enabled":  req.Enabled,
		"cron":     req.Cron,
		"next_run": nextRun,
	})
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

