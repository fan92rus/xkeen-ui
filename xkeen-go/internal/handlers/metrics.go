// Package handlers provides HTTP handlers for XKEEN-UI API endpoints.
package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

const (
	// metricsHistoryCapacity stores ~20 min of data at 20-second intervals (60 entries).
	metricsHistoryCapacity = 60
	// metricsHistoryInterval is the interval between history samples.
	metricsHistoryInterval = 20 * time.Second
	// metricsLiveInterval is the interval for live polling (fast updates for WS clients).
	metricsLiveInterval = 2 * time.Second
)

// MetricsSnapshot represents a single point-in-time metrics reading.
type MetricsSnapshot struct {
	Timestamp  int64                  `json:"ts"`       // Unix seconds
	Inbound    interface{}            `json:"inbound"`  // map[string]map[string]interface{} or {}
	Outbound   interface{}            `json:"outbound"` // map[string]map[string]interface{} or {}
	Observable interface{}            `json:"observatory,omitempty"`
	Available  bool                   `json:"available"`
	Debug      string                 `json:"debug,omitempty"`
}

// WSMessage is a message sent over the metrics WebSocket.
type WSMessage struct {
	Type    string          `json:"type"`    // "history", "snapshot", "error", "ping"
	History []MetricsSnapshot `json:"history,omitempty"`
	Snap    *MetricsSnapshot  `json:"snap,omitempty"`
	Error   string            `json:"error,omitempty"`
}

// MetricsHandler proxies Xray's metrics/observatory data to the frontend
// via WebSocket. It runs a background worker that polls Xray periodically,
// stores a sparse history (20-second intervals, ~20 min), and streams live
// updates (2-second intervals) to connected WebSocket clients.
type MetricsHandler struct {
	baseURL string
	client  *http.Client

	// Cache for /debug/vars responses
	mu       sync.RWMutex
	cached   []byte
	cachedAt time.Time
	cacheTTL time.Duration

	// History ring buffer (sparse, every 20s, ~20 min)
	histMu   sync.RWMutex
	history  []MetricsSnapshot
	histIdx  int // next write position (ring buffer)

	// WebSocket clients
	clients   map[*websocket.Conn]bool
	clientsMu sync.RWMutex
	broadcast chan WSMessage

	// Background worker lifecycle
	doneCh chan struct{}
	cancel func()
	wg     sync.WaitGroup

	// WebSocket upgrader
	upgrader       websocket.Upgrader
	allowedOrigins map[string]bool
}

// NewMetricsHandler creates a MetricsHandler.
// baseURL is the Xray metrics listen address (e.g. "http://127.0.0.1:11111").
// timeout is the HTTP client timeout for requests to Xray.
func NewMetricsHandler(baseURL string, timeout time.Duration) *MetricsHandler {
	return NewMetricsHandlerWithOrigins(baseURL, timeout, nil)
}

// NewMetricsHandlerWithOrigins creates a MetricsHandler with CORS origins for WebSocket.
func NewMetricsHandlerWithOrigins(baseURL string, timeout time.Duration, allowedOrigins []string) *MetricsHandler {
	originsMap := make(map[string]bool)
	for _, o := range allowedOrigins {
		originsMap[o] = true
	}

	h := &MetricsHandler{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		client: &http.Client{
			Timeout: timeout,
		},
		cacheTTL:       2 * time.Second,
		clients:        make(map[*websocket.Conn]bool),
		broadcast:      make(chan WSMessage, 64),
		history:        make([]MetricsSnapshot, 0, metricsHistoryCapacity),
		allowedOrigins: originsMap,
	}

	h.upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 4096,
		CheckOrigin:     h.checkOrigin,
	}

	// Start background workers
	h.startWorkers()

	return h
}

// NewMetricsHandlerHTTPOnly creates a MetricsHandler for HTTP-only use (no background workers).
// Used for testing the legacy HTTP endpoints without goroutine interference.
func NewMetricsHandlerHTTPOnly(baseURL string, timeout time.Duration) *MetricsHandler {
	h := &MetricsHandler{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		client: &http.Client{
			Timeout: timeout,
		},
		cacheTTL:       5 * time.Second,
		clients:        make(map[*websocket.Conn]bool),
		broadcast:      make(chan WSMessage),
		history:        make([]MetricsSnapshot, 0, metricsHistoryCapacity),
		allowedOrigins: make(map[string]bool),
	}

	h.upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 4096,
		CheckOrigin:     func(r *http.Request) bool { return true },
	}

	return h
}

// Close gracefully stops all background goroutines.
func (h *MetricsHandler) Close() {
	if h.cancel != nil {
		h.cancel()
	}
	close(h.broadcast)
	h.wg.Wait()
}

// startWorkers launches the background polling goroutines.
func (h *MetricsHandler) startWorkers() {
	done := make(chan struct{})
	h.doneCh = done
	h.cancel = func() { close(done) }

	// Broadcast goroutine
	h.wg.Add(1)
	go h.runBroadcast()

	// History sampler (every 20s, populates the sparse ring buffer)
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		ticker := time.NewTicker(metricsHistoryInterval)
		defer ticker.Stop()

		// Take an initial sample immediately
		snap := h.collectSnapshot()
		h.appendHistory(snap)

		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				snap := h.collectSnapshot()
				h.appendHistory(snap)
			}
		}
	}()

	// Live polling (every 2s, sends to WS clients)
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		ticker := time.NewTicker(metricsLiveInterval)
		defer ticker.Stop()

		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				snap := h.collectSnapshot()
				msg := WSMessage{
					Type: "snapshot",
					Snap: &snap,
				}
				select {
				case h.broadcast <- msg:
				default:
					// Channel full, skip
				}
			}
		}
	}()
}

// appendHistory adds a snapshot to the ring buffer.
func (h *MetricsHandler) appendHistory(snap MetricsSnapshot) {
	h.histMu.Lock()
	defer h.histMu.Unlock()

	if len(h.history) < metricsHistoryCapacity {
		h.history = append(h.history, snap)
	} else {
		h.history[h.histIdx] = snap
	}
	h.histIdx = (h.histIdx + 1) % metricsHistoryCapacity
}

// getHistory returns all stored history in chronological order.
func (h *MetricsHandler) getHistory() []MetricsSnapshot {
	h.histMu.RLock()
	defer h.histMu.RUnlock()

	n := len(h.history)
	if n == 0 {
		return nil
	}

	// Ring buffer: if not full, histIdx is the end; if full, oldest is at histIdx
	if n < metricsHistoryCapacity {
		result := make([]MetricsSnapshot, n)
		copy(result, h.history[:n])
		return result
	}

	// Full ring buffer — reorder so oldest is first
	result := make([]MetricsSnapshot, n)
	copy(result, h.history[h.histIdx:])
	copy(result[n-h.histIdx:], h.history[:h.histIdx])
	return result
}

// fetchVars fetches /debug/vars from Xray, using cache if fresh.
func (h *MetricsHandler) fetchVars() ([]byte, bool, error) {
	h.mu.RLock()
	if h.cached != nil && time.Since(h.cachedAt) < h.cacheTTL {
		data := h.cached
		h.mu.RUnlock()
		return data, true, nil
	}
	h.mu.RUnlock()

	resp, err := h.client.Get(h.baseURL + "/debug/vars")
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, err
	}

	h.mu.Lock()
	h.cached = body
	h.cachedAt = time.Now()
	h.mu.Unlock()

	return body, true, nil
}

// collectSnapshot fetches data from Xray and builds a MetricsSnapshot.
func (h *MetricsHandler) collectSnapshot() MetricsSnapshot {
	snap := MetricsSnapshot{
		Timestamp: time.Now().Unix(),
	}

	body, available, err := h.fetchVars()
	if !available {
		snap.Available = false
		snap.Debug = fmt.Sprintf("metrics unavailable: %v", err)
		return snap
	}

	var vars map[string]interface{}
	if err := json.Unmarshal(body, &vars); err != nil {
		snap.Available = false
		snap.Debug = "invalid metrics response"
		return snap
	}

	snap.Available = true

	// Parse stats
	statsRaw, statsKeyExists := vars["stats"]
	if !statsKeyExists {
		snap.Inbound = nil
		snap.Outbound = nil
	} else if statsRaw == nil {
		snap.Inbound = map[string]interface{}{}
		snap.Outbound = map[string]interface{}{}
	} else {
		var stats struct {
			Inbound  map[string]map[string]interface{} `json:"inbound"`
			Outbound map[string]map[string]interface{} `json:"outbound"`
		}
		switch v := statsRaw.(type) {
		case string:
			json.Unmarshal([]byte(v), &stats)
		default:
			b, _ := json.Marshal(v)
			json.Unmarshal(b, &stats)
		}
		snap.Inbound = stats.Inbound
		snap.Outbound = stats.Outbound
	}

	// Parse observatory
	if obsRaw, ok := vars["observatory"]; ok {
		var observatory map[string]interface{}
		switch v := obsRaw.(type) {
		case string:
			json.Unmarshal([]byte(v), &observatory)
		default:
			b, _ := json.Marshal(v)
			json.Unmarshal(b, &observatory)
		}
		snap.Observable = observatory
	}

	return snap
}

// checkOrigin validates the origin of WebSocket connections.
func (h *MetricsHandler) checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	if h.allowedOrigins[origin] {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return u.Host == r.Host
}

// runBroadcast sends messages to all connected WebSocket clients.
func (h *MetricsHandler) runBroadcast() {
	defer h.wg.Done()

	for msg := range h.broadcast {
		h.clientsMu.RLock()
		var dead []*websocket.Conn
		for client := range h.clients {
			if err := client.WriteJSON(msg); err != nil {
				dead = append(dead, client)
			}
		}
		h.clientsMu.RUnlock()

		if len(dead) > 0 {
			h.clientsMu.Lock()
			for _, c := range dead {
				c.Close()
				delete(h.clients, c)
			}
			h.clientsMu.Unlock()
		}
	}
}

// WebSocket handles WebSocket connections for real-time metrics.
// GET /ws/metrics
//
// Protocol:
//   - On connect, server sends { "type": "history", "history": [...] } with stored backend history.
//   - Then every ~2s server sends { "type": "snapshot", "snap": {...} } with live data.
//   - Pings are sent every 30s: { "type": "ping" }.
func (h *MetricsHandler) WebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("MetricsHandler: WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	h.clientsMu.Lock()
	h.clients[conn] = true
	clientCount := len(h.clients)
	h.clientsMu.Unlock()

	log.Printf("MetricsHandler: WebSocket client connected. Total: %d", clientCount)

	// Send history on connect
	history := h.getHistory()
	if len(history) > 0 {
		conn.WriteJSON(WSMessage{
			Type:    "history",
			History: history,
		})
	}

	// Read loop for close detection + ping ticker
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	}()

	pingTicker := time.NewTicker(30 * time.Second)
	defer pingTicker.Stop()

	for {
		select {
		case <-done:
			h.clientsMu.Lock()
			delete(h.clients, conn)
			h.clientsMu.Unlock()
			log.Printf("MetricsHandler: WebSocket client disconnected. Total: %d", len(h.clients))
			return
		case <-pingTicker.C:
			if err := conn.WriteJSON(WSMessage{Type: "ping"}); err != nil {
				h.clientsMu.Lock()
				delete(h.clients, conn)
				h.clientsMu.Unlock()
				return
			}
		}
	}
}

// ── Legacy HTTP endpoints (kept for backward compatibility) ──

// GetStats returns inbound/outbound traffic statistics from Xray.
// GET /api/metrics/stats
func (h *MetricsHandler) GetStats(w http.ResponseWriter, r *http.Request) {
	snap := h.collectSnapshot()
	if !snap.Available {
		respondJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"error":     snap.Debug,
			"available": false,
		})
		return
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"available": true,
		"inbound":   snap.Inbound,
		"outbound":  snap.Outbound,
		"debug":     snap.Debug,
	})
}

// GetObservatory returns observatory (proxy latency/health) data from Xray.
// GET /api/metrics/observatory
func (h *MetricsHandler) GetObservatory(w http.ResponseWriter, r *http.Request) {
	snap := h.collectSnapshot()
	if !snap.Available {
		respondJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"error":     snap.Debug,
			"available": false,
		})
		return
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"available": true,
		"results":   snap.Observable,
	})
}

// RegisterMetricsRoutes registers metrics API routes (HTTP).
func RegisterMetricsRoutes(r *mux.Router, handler *MetricsHandler) {
	r.HandleFunc("/metrics/stats", handler.GetStats).Methods("GET")
	r.HandleFunc("/metrics/observatory", handler.GetObservatory).Methods("GET")
}

// RegisterMetricsWSRoute registers the WebSocket route for metrics.
func RegisterMetricsWSRoute(r *mux.Router, handler *MetricsHandler, authMiddleware func(http.Handler) http.Handler) {
	wsRouter := r.PathPrefix("/ws").Subrouter()
	wsRouter.Use(authMiddleware)
	wsRouter.HandleFunc("/metrics", handler.WebSocket).Methods("GET")
}

func sortedKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
