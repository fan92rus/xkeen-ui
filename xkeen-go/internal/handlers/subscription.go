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
		"filters":       cfg.Filters,
		"strategy":      cfg.Strategy,
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

	// Store ALL proxies unfiltered — filtering happens only at display/apply time
	h.store.SetProxies(entries)

	// Extract markers from unfiltered data
	markers := subscription.ExtractAllMarkers(entries)

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"success":     true,
		"proxy_count": len(entries),
		"total":       len(entries),
		"proxies":     entries,
		"markers":     markers,
	})
}

// ---------- Proxies ----------

// GetProxies returns all cached proxies.
// GET /api/subscriptions/proxies
func (h *SubscriptionHandler) GetProxies(w http.ResponseWriter, r *http.Request) {
	allProxies := h.store.GetProxies()
	markers := subscription.ExtractAllMarkers(allProxies)

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"total":    len(allProxies),
		"markers":  markers,
		"proxies":  allProxies,
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

	// Get filtered proxies
	allProxies := h.store.GetProxies()
	filters := h.store.GetFilters()
	filtered := subscription.ApplyFilter(allProxies, filters)

	if len(filtered) == 0 {
		respondError(w, http.StatusBadRequest, "no proxies available after filtering; fetch subscriptions first")
		return
	}

	strategy := h.store.GetStrategy()

	// Generate outbounds
	outboundsJSON, err := subscription.GenerateOutboundsJSON(filtered)
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

	// Generate routing
	routingJSON, err := subscription.GenerateRoutingJSON(filtered, *strategy, existingRouting)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to generate routing: %v", err))
		return
	}

	// Generate observatory if needed
	var observatoryJSON []byte
	if subscription.NeedsObservatory(strategy.Type) {
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

	log.Printf("[subscription] applied %d proxies with strategy %q: %s, %s", len(filtered), strategy.Type, outboundsPath, routingPath)

	response := map[string]interface{}{
		"success":     true,
		"proxy_count": len(filtered),
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
	filters := h.store.GetFilters()
	filtered := subscription.ApplyFilter(allProxies, filters)

	if len(filtered) == 0 {
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"proxy_count": 0,
			"outbounds":   nil,
			"routing":     nil,
			"observatory": nil,
			"message":     "no proxies available",
		})
		return
	}

	strategy := h.store.GetStrategy()

	outboundsJSON, err := subscription.GenerateOutboundsJSON(filtered)
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

	routingJSON, err := subscription.GenerateRoutingJSON(filtered, *strategy, existingRouting)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to generate routing: %v", err))
		return
	}

	var observatoryJSON json.RawMessage
	if subscription.NeedsObservatory(strategy.Type) {
		if obs, err := subscription.GenerateObservatoryJSON(); err == nil {
			observatoryJSON = obs
		}
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"proxy_count": len(filtered),
		"outbounds":   json.RawMessage(outboundsJSON),
		"routing":     json.RawMessage(routingJSON),
		"observatory": observatoryJSON,
		"strategy":    strategy.Type,
	})
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

	// Auto-apply cron settings
	r.HandleFunc("/subscriptions/auto-apply", handler.GetAutoApply).Methods("GET")
	r.HandleFunc("/subscriptions/auto-apply", handler.UpdateAutoApply).Methods("PUT")

	// {id} routes last
	r.HandleFunc("/subscriptions/{id}", handler.UpdateSubscription).Methods("PUT")
	r.HandleFunc("/subscriptions/{id}", handler.DeleteSubscription).Methods("DELETE")
	r.HandleFunc("/subscriptions/{id}/fetch", handler.FetchSubscription).Methods("POST")
}

