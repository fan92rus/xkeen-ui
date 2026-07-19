package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/fan92rus/xkeen-ui/internal/subscription"
	"github.com/gorilla/mux"
)

// DiagnosticsHandler provides network diagnostic endpoints.
type DiagnosticsHandler struct {
	fetcher *subscription.Fetcher
}

// ipCheckDomain is the domain used for exit-IP verification. Exposed in the
// API response so the user can add it to Xray routing rules to control
// whether the check goes through VPN or direct.
const ipCheckDomain = "api.ipify.org"

// NewDiagnosticsHandler creates a new DiagnosticsHandler.
func NewDiagnosticsHandler(fetcher *subscription.Fetcher) *DiagnosticsHandler {
	return &DiagnosticsHandler{fetcher: fetcher}
}

// RegisterDiagnosticsRoutes registers diagnostics API routes.
func RegisterDiagnosticsRoutes(r *mux.Router, h *DiagnosticsHandler) {
	r.HandleFunc("/diagnostics/network", h.CheckNetwork).Methods("GET")
}

// exitIPResponse is the JSON response for the network check endpoint.
type exitIPResponse struct {
	ExitIP       string `json:"exit_ip"`
	Source       string `json:"source"`
	Latency      int64  `json:"latency_ms"`
	CheckDomain  string `json:"check_domain"`
	Error        string `json:"error,omitempty"`
}

// CheckNetwork performs an HTTP request to api.ipify.org through the same
// fetcher cascade (proxy → direct) used for subscription downloads, then
// returns the exit IP. This tells the user whether their subscription
// fetches go through VPN or directly.
func (h *DiagnosticsHandler) CheckNetwork(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	result := h.checkExitIP(ctx)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

// checkExitIP does the actual network request and returns the result.
func (h *DiagnosticsHandler) checkExitIP(ctx context.Context) exitIPResponse {

	client := h.fetcher.HTTPClient()
	if client == nil {
		return exitIPResponse{Error: "fetcher not initialized"}
	}

	start := time.Now()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+ipCheckDomain, http.NoBody)
	if err != nil {
		return exitIPResponse{Error: "failed to create request: " + err.Error()}
	}
	req.Header.Set("User-Agent", "xkeen-go-diagnostic")

	resp, err := client.Do(req)
	if err != nil {
		source := h.fetcher.ProxyStatus()
		return exitIPResponse{
			Source: source,
			Error:  err.Error(),
		}
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 256))
	if err != nil {
		return exitIPResponse{Error: "failed to read response: " + err.Error()}
	}

	latency := time.Since(start).Milliseconds()

	ip := string(body)

	source := h.fetcher.ProxyStatus()

	return exitIPResponse{
		ExitIP:      ip,
		Source:      source,
		Latency:     latency,
		CheckDomain: ipCheckDomain,
	}
}
