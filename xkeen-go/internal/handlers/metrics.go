// Package handlers provides HTTP handlers for XKEEN-UI API endpoints.
package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
)

// MetricsHandler proxies Xray's metrics/observatory data to the frontend.
// It caches responses to avoid hammering Xray on every frontend poll.
type MetricsHandler struct {
	baseURL string         // e.g. "http://127.0.0.1:11111"
	client  *http.Client

	mu       sync.RWMutex
	cached   []byte // raw /debug/vars response
	cachedAt time.Time
	cacheTTL time.Duration
}

// NewMetricsHandler creates a MetricsHandler.
// baseURL is the Xray metrics listen address (e.g. "http://127.0.0.1:11111").
// timeout is the HTTP client timeout for requests to Xray.
func NewMetricsHandler(baseURL string, timeout time.Duration) *MetricsHandler {
	return &MetricsHandler{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		client: &http.Client{
			Timeout: timeout,
		},
		cacheTTL: 5 * time.Second,
	}
}

// fetchVars fetches /debug/vars from Xray, using cache if fresh.
func (h *MetricsHandler) fetchVars() ([]byte, bool, error) {
	// Try cache first
	h.mu.RLock()
	if h.cached != nil && time.Since(h.cachedAt) < h.cacheTTL {
		data := h.cached
		avail := true
		h.mu.RUnlock()
		return data, avail, nil
	}
	h.mu.RUnlock()

	// Cache miss — fetch from Xray
	resp, err := h.client.Get(h.baseURL + "/debug/vars")
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, err
	}

	// Update cache
	h.mu.Lock()
	h.cached = body
	h.cachedAt = time.Now()
	h.mu.Unlock()

	return body, true, nil
}

// GetStats returns inbound/outbound traffic statistics from Xray.
// GET /api/metrics/stats
func (h *MetricsHandler) GetStats(w http.ResponseWriter, r *http.Request) {
	body, available, err := h.fetchVars()
	if !available {
		respondJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"error":     fmt.Sprintf("metrics unavailable: %v", err),
			"available": false,
		})
		return
	}

	var vars map[string]interface{}
	if err := json.Unmarshal(body, &vars); err != nil {
		log.Printf("MetricsHandler: failed to parse /debug/vars: %v", err)
		respondJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"error":     "invalid metrics response",
			"available": false,
		})
		return
	}

	// expvar publishes stats as a JSON string — need double parse
	statsRaw, ok := vars["stats"]
	if !ok {
		respondJSON(w, http.StatusOK, map[string]interface{}{"available": true, "inbound": nil, "outbound": nil})
		return
	}
	var stats struct {
		Inbound  map[string]map[string]interface{} `json:"inbound"`
		Outbound map[string]map[string]interface{} `json:"outbound"`
	}
	switch v := statsRaw.(type) {
	case string:
		if err := json.Unmarshal([]byte(v), &stats); err != nil {
			log.Printf("MetricsHandler: failed to parse stats string: %v", err)
		}
	default:
		b, _ := json.Marshal(v)
		json.Unmarshal(b, &stats)
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"available": true,
		"inbound":   stats.Inbound,
		"outbound":  stats.Outbound,
	})
}

// GetObservatory returns observatory (proxy latency/health) data from Xray.
// GET /api/metrics/observatory
func (h *MetricsHandler) GetObservatory(w http.ResponseWriter, r *http.Request) {
	body, available, err := h.fetchVars()
	if !available {
		respondJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"error":     fmt.Sprintf("metrics unavailable: %v", err),
			"available": false,
		})
		return
	}

	var vars map[string]interface{}
	if err := json.Unmarshal(body, &vars); err != nil {
		log.Printf("MetricsHandler: failed to parse /debug/vars: %v", err)
		respondJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"error":     "invalid metrics response",
			"available": false,
		})
		return
	}

	// expvar publishes observatory as a JSON string — need double parse
	obsRaw, ok := vars["observatory"]
	if !ok {
		respondJSON(w, http.StatusOK, map[string]interface{}{"available": true, "results": nil})
		return
	}
	var observatory map[string]interface{}
	switch v := obsRaw.(type) {
	case string:
		if err := json.Unmarshal([]byte(v), &observatory); err != nil {
			log.Printf("MetricsHandler: failed to parse observatory string: %v", err)
		}
	default:
		b, _ := json.Marshal(v)
		json.Unmarshal(b, &observatory)
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"available": true,
		"results":   observatory,
	})
}

// RegisterMetricsRoutes registers metrics API routes.
func RegisterMetricsRoutes(r *mux.Router, handler *MetricsHandler) {
	r.HandleFunc("/metrics/stats", handler.GetStats).Methods("GET")
	r.HandleFunc("/metrics/observatory", handler.GetObservatory).Methods("GET")
}
