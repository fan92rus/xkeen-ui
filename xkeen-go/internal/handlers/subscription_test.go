package handlers

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"github.com/fan92rus/xkeen-ui/internal/subscription"
)

// ---------- Helpers ----------

// newTestHandler creates a SubscriptionHandler with a temp store for testing.
func newTestHandler(t *testing.T) (*SubscriptionHandler, string) {
	t.Helper()
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "subscriptions.json")
	xrayDir := filepath.Join(tmpDir, "xray-configs")

	store, err := subscription.NewStore(storePath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	fetcher := subscription.NewFetcher()
	scheduler := subscription.NewScheduler(store, fetcher)
	t.Cleanup(func() { scheduler.Stop() })

	handler := NewSubscriptionHandler(store, fetcher, scheduler, xrayDir)
	return handler, xrayDir
}

// newTestRouter creates a mux.Router with subscription routes.
func newTestRouter(h *SubscriptionHandler) *mux.Router {
	r := mux.NewRouter()
	RegisterSubscriptionRoutes(r, h)
	return r
}

// doRequest executes an HTTP request against the router and returns response body.
func doRequest(t *testing.T, router *mux.Router, method, path string, body interface{}) *http.Response {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("failed to marshal body: %v", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, path, bodyReader)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec.Result()
}

// parseResponse parses the response body into a map.
func parseResponse(t *testing.T, resp *http.Response) map[string]interface{} {
	t.Helper()
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("failed to parse response JSON: %v\nbody: %s", err, string(data))
	}
	return result
}

// addTestSubscription adds a subscription via the store for test setup.
func addTestSubscription(t *testing.T, store *subscription.Store) string {
	t.Helper()
	sub := &subscription.Subscription{
		Name:    "Test Sub",
		URL:     "https://example.com/sub",
		Enabled: true,
	}
	if err := store.AddSubscription(sub); err != nil {
		t.Fatalf("failed to add test subscription: %v", err)
	}
	return sub.ID
}

// addTestSubscriptionWithProxies adds a subscription and sets proxies in the store.
func addTestSubscriptionWithProxies(t *testing.T, store *subscription.Store, count int) string {
	t.Helper()
	id := addTestSubscription(t, store)

	proxies := make([]*subscription.ProxyEntry, count)
	for i := 0; i < count; i++ {
		outbound := map[string]interface{}{
			"tag":      fmt.Sprintf("proxy-test-%d", i+1),
			"protocol": "vless",
			"settings": map[string]interface{}{
				"vnext": []map[string]interface{}{
					{
						"address": fmt.Sprintf("10.0.0.%d", i+1),
						"port":    443,
					},
				},
			},
		}
		outJSON, _ := json.Marshal(outbound)
		proxies[i] = &subscription.ProxyEntry{
			Tag:      fmt.Sprintf("proxy-test-%d", i+1),
			Protocol: "vless",
			Outbound: outJSON,
			Remarks:  fmt.Sprintf("Test Node %d", i+1),
			Country:  "DE",
			Marker:   "⚡",
		}
	}
	store.SetProxies(proxies)

	// Update proxy count on subscription
	sub, _ := store.GetSubscription(id)
	sub.ProxyCount = count
	_ = store.UpdateSubscription(sub)

	return id
}

// ---------- Tests ----------

func TestListSubscriptions_Empty(t *testing.T) {
	h, _ := newTestHandler(t)
	router := newTestRouter(h)

	resp := doRequest(t, router, "GET", "/subscriptions", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	result := parseResponse(t, resp)
	subs, ok := result["subscriptions"].([]interface{})
	if !ok {
		t.Fatalf("expected subscriptions array, got %T", result["subscriptions"])
	}
	if len(subs) != 0 {
		t.Fatalf("expected 0 subscriptions, got %d", len(subs))
	}
}

func TestAddSubscription_Success(t *testing.T) {
	h, _ := newTestHandler(t)
	router := newTestRouter(h)

	resp := doRequest(t, router, "POST", "/subscriptions", map[string]interface{}{
		"name":     "My Sub",
		"url":      "https://example.com/sub",
		"interval": 5,
		"enabled":  true,
	})
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 201, got %d: %s", resp.StatusCode, string(body))
	}

	result := parseResponse(t, resp)
	if result["success"] != true {
		t.Fatalf("expected success=true")
	}
	subMap, ok := result["subscription"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected subscription object")
	}
	if subMap["name"] != "My Sub" {
		t.Fatalf("expected name=My Sub, got %v", subMap["name"])
	}
	if subMap["id"] == nil || subMap["id"] == "" {
		t.Fatalf("expected auto-generated id")
	}
}

func TestAddSubscription_MissingURL(t *testing.T) {
	h, _ := newTestHandler(t)
	router := newTestRouter(h)

	resp := doRequest(t, router, "POST", "/subscriptions", map[string]interface{}{
		"name": "No URL Sub",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestAddSubscription_DefaultName(t *testing.T) {
	h, _ := newTestHandler(t)
	router := newTestRouter(h)

	resp := doRequest(t, router, "POST", "/subscriptions", map[string]interface{}{
		"url": "https://example.com/sub",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	result := parseResponse(t, resp)
	subMap := result["subscription"].(map[string]interface{})
	if subMap["name"] != "New Subscription" {
		t.Fatalf("expected default name, got %v", subMap["name"])
	}
}

func TestUpdateSubscription_Success(t *testing.T) {
	h, _ := newTestHandler(t)
	router := newTestRouter(h)

	// Add first
	id := addTestSubscription(t, h.store)

	resp := doRequest(t, router, "PUT", "/subscriptions/"+id, map[string]interface{}{
		"name":     "Updated",
		"url":      "https://new.example.com/sub",
		"interval": 10,
		"enabled":  false,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Verify update
	sub, _ := h.store.GetSubscription(id)
	if sub.Name != "Updated" {
		t.Fatalf("expected name=Updated, got %s", sub.Name)
	}
}

func TestUpdateSubscription_NotFound(t *testing.T) {
	h, _ := newTestHandler(t)
	router := newTestRouter(h)

	resp := doRequest(t, router, "PUT", "/subscriptions/nonexistent", map[string]interface{}{
		"name": "X",
		"url":  "https://x.com",
	})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestDeleteSubscription_Success(t *testing.T) {
	h, _ := newTestHandler(t)
	router := newTestRouter(h)

	id := addTestSubscription(t, h.store)

	resp := doRequest(t, router, "DELETE", "/subscriptions/"+id, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Verify deletion
	_, err := h.store.GetSubscription(id)
	if err == nil {
		t.Fatal("expected subscription to be deleted")
	}
}

func TestDeleteSubscription_NotFound(t *testing.T) {
	h, _ := newTestHandler(t)
	router := newTestRouter(h)

	resp := doRequest(t, router, "DELETE", "/subscriptions/nonexistent", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestGetFilters(t *testing.T) {
	h, _ := newTestHandler(t)
	router := newTestRouter(h)

	resp := doRequest(t, router, "GET", "/subscriptions/filters", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestUpdateFilters(t *testing.T) {
	h, _ := newTestHandler(t)
	router := newTestRouter(h)

	resp := doRequest(t, router, "PUT", "/subscriptions/filters", map[string]interface{}{
		"include_markers":   []string{"⚡"},
		"exclude_markers":   []string{"0.5X", "🎮"},
		"include_countries": []string{"DE", "NL"},
		"max_proxies":       50,
	})
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(body))
	}

	// Verify
	filters := h.store.GetFilters()
	if len(filters.IncludeMarkers) != 1 || filters.IncludeMarkers[0] != "⚡" {
		t.Fatalf("expected include_markers=[⚡], got %v", filters.IncludeMarkers)
	}
	if filters.MaxProxies != 50 {
		t.Fatalf("expected max_proxies=50, got %d", filters.MaxProxies)
	}
}

func TestGetStrategy(t *testing.T) {
	h, _ := newTestHandler(t)
	router := newTestRouter(h)

	resp := doRequest(t, router, "GET", "/subscriptions/strategy", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestUpdateStrategy_Valid(t *testing.T) {
	h, _ := newTestHandler(t)
	router := newTestRouter(h)

	for _, strategyType := range subscription.StrategyTypes {
		resp := doRequest(t, router, "PUT", "/subscriptions/strategy", map[string]interface{}{
			"type":         strategyType,
			"fallback_tag": "direct",
		})
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("strategy %q: expected 200, got %d: %s", strategyType, resp.StatusCode, string(body))
		}
	}
}

func TestUpdateStrategy_Invalid(t *testing.T) {
	h, _ := newTestHandler(t)
	router := newTestRouter(h)

	resp := doRequest(t, router, "PUT", "/subscriptions/strategy", map[string]interface{}{
		"type": "invalid-strategy",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestGetProxies_Empty(t *testing.T) {
	h, _ := newTestHandler(t)
	router := newTestRouter(h)

	resp := doRequest(t, router, "GET", "/subscriptions/proxies", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	result := parseResponse(t, resp)
	if result["total"].(float64) != 0 {
		t.Fatalf("expected 0 total, got %v", result["total"])
	}
}

func TestGetProxies_WithData(t *testing.T) {
	h, _ := newTestHandler(t)
	router := newTestRouter(h)

	addTestSubscriptionWithProxies(t, h.store, 5)

	resp := doRequest(t, router, "GET", "/subscriptions/proxies", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	result := parseResponse(t, resp)
	if result["total"].(float64) != 5 {
		t.Fatalf("expected 5 total, got %v", result["total"])
	}
}

func TestPreview_NoProxies(t *testing.T) {
	h, _ := newTestHandler(t)
	router := newTestRouter(h)

	resp := doRequest(t, router, "GET", "/subscriptions/preview", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	result := parseResponse(t, resp)
	if result["proxy_count"].(float64) != 0 {
		t.Fatalf("expected 0 proxy_count, got %v", result["proxy_count"])
	}
}

func TestPreview_WithProxies(t *testing.T) {
	h, _ := newTestHandler(t)
	router := newTestRouter(h)

	addTestSubscriptionWithProxies(t, h.store, 3)

	resp := doRequest(t, router, "GET", "/subscriptions/preview", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	result := parseResponse(t, resp)
	if result["proxy_count"].(float64) != 3 {
		t.Fatalf("expected 3 proxy_count, got %v", result["proxy_count"])
	}
	if result["outbounds"] == nil {
		t.Fatal("expected outbounds JSON")
	}
	if result["routing"] == nil {
		t.Fatal("expected routing JSON")
	}
}

func TestApply_NoProxies(t *testing.T) {
	h, _ := newTestHandler(t)
	router := newTestRouter(h)

	resp := doRequest(t, router, "POST", "/subscriptions/apply", map[string]interface{}{
		"restart": true,
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestApply_Success(t *testing.T) {
	h, xrayDir := newTestHandler(t)
	router := newTestRouter(h)

	addTestSubscriptionWithProxies(t, h.store, 3)

	resp := doRequest(t, router, "POST", "/subscriptions/apply", map[string]interface{}{
		"restart": true,
	})
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(body))
	}

	result := parseResponse(t, resp)
	if result["success"] != true {
		t.Fatalf("expected success=true")
	}
	if result["proxy_count"].(float64) != 3 {
		t.Fatalf("expected 3 proxy_count, got %v", result["proxy_count"])
	}

	// Verify files were written
	outboundsPath := filepath.Join(xrayDir, "04_outbounds.json")
	routingPath := filepath.Join(xrayDir, "05_routing.json")

	if _, err := os.Stat(outboundsPath); os.IsNotExist(err) {
		t.Fatal("expected 04_outbounds.json to be written")
	}
	if _, err := os.Stat(routingPath); os.IsNotExist(err) {
		t.Fatal("expected 05_routing.json to be written")
	}

	// Verify outbounds is valid JSON with "proxy" as first tag
	data, _ := os.ReadFile(outboundsPath)
	var outbounds map[string]interface{}
	if err := json.Unmarshal(data, &outbounds); err != nil {
		t.Fatalf("outbounds is not valid JSON: %v", err)
	}
	obList := outbounds["outbounds"].([]interface{})
	first := obList[0].(map[string]interface{})
	if first["tag"] != "proxy" {
		t.Fatalf("expected first outbound tag=proxy, got %v", first["tag"])
	}

	// Verify routing is valid JSON
	data, _ = os.ReadFile(routingPath)
	var routing map[string]interface{}
	if err := json.Unmarshal(data, &routing); err != nil {
		t.Fatalf("routing is not valid JSON: %v", err)
	}
}

func TestApply_WithObservatory(t *testing.T) {
	h, xrayDir := newTestHandler(t)
	router := newTestRouter(h)

	addTestSubscriptionWithProxies(t, h.store, 2)

	// Set strategy to leastping (requires observatory)
	_ = h.store.SetStrategy(&subscription.RoutingStrategy{
		Type:        "leastping",
		FallbackTag: "direct",
	})

	resp := doRequest(t, router, "POST", "/subscriptions/apply", nil)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(body))
	}

	// Verify observatory file was written
	obsPath := filepath.Join(xrayDir, "07_observatory.json")
	if _, err := os.Stat(obsPath); os.IsNotExist(err) {
		t.Fatal("expected 07_observatory.json to be written for leastping strategy")
	}

	data, _ := os.ReadFile(obsPath)
	var obs map[string]interface{}
	if err := json.Unmarshal(data, &obs); err != nil {
		t.Fatalf("observatory is not valid JSON: %v", err)
	}
	if _, ok := obs["observatory"]; !ok {
		t.Fatal("expected 'observatory' key in observatory JSON")
	}
}

func TestApply_RemoveObservatory(t *testing.T) {
	h, xrayDir := newTestHandler(t)
	router := newTestRouter(h)

	addTestSubscriptionWithProxies(t, h.store, 2)

	// First apply with leastping
	_ = h.store.SetStrategy(&subscription.RoutingStrategy{Type: "leastping"})
	resp := doRequest(t, router, "POST", "/subscriptions/apply", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("first apply failed: %d", resp.StatusCode)
	}

	obsPath := filepath.Join(xrayDir, "07_observatory.json")
	if _, err := os.Stat(obsPath); os.IsNotExist(err) {
		t.Fatal("expected observatory file after first apply")
	}

	// Now switch to "all" strategy (no observatory needed)
	_ = h.store.SetStrategy(&subscription.RoutingStrategy{Type: "all"})
	resp = doRequest(t, router, "POST", "/subscriptions/apply", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("second apply failed: %d", resp.StatusCode)
	}

	if _, err := os.Stat(obsPath); !os.IsNotExist(err) {
		t.Fatal("expected observatory file to be removed after switching to 'all' strategy")
	}
}

func TestApply_GeneratedAtUpdated(t *testing.T) {
	h, _ := newTestHandler(t)
	router := newTestRouter(h)

	addTestSubscriptionWithProxies(t, h.store, 2)

	before := time.Now()
	resp := doRequest(t, router, "POST", "/subscriptions/apply", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	cfg := h.store.GetConfig()
	if cfg.GeneratedAt.Before(before) {
		t.Fatalf("expected GeneratedAt to be updated, got %v", cfg.GeneratedAt)
	}
}

func TestListSubscriptions_WithData(t *testing.T) {
	h, _ := newTestHandler(t)
	router := newTestRouter(h)

	addTestSubscription(t, h.store)

	resp := doRequest(t, router, "GET", "/subscriptions", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	result := parseResponse(t, resp)
	subs := result["subscriptions"].([]interface{})
	if len(subs) != 1 {
		t.Fatalf("expected 1 subscription, got %d", len(subs))
	}

	sub := subs[0].(map[string]interface{})
	if sub["name"] != "Test Sub" {
		t.Fatalf("expected name=Test Sub, got %v", sub["name"])
	}
}

func TestFetchSubscription_NotFound(t *testing.T) {
	h, _ := newTestHandler(t)
	router := newTestRouter(h)

	resp := doRequest(t, router, "POST", "/subscriptions/nonexistent/fetch", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestFetchSubscription_FetchFails(t *testing.T) {
	h, _ := newTestHandler(t)
	router := newTestRouter(h)

	id := addTestSubscription(t, h.store)

	// Fetch will fail because the URL doesn't actually serve subscription content
	resp := doRequest(t, router, "POST", "/subscriptions/"+id+"/fetch", nil)
	// Should get 502 or similar since the URL is fake
	if resp.StatusCode == http.StatusOK {
		t.Fatal("expected non-200 for fake URL")
	}

	// Verify error is recorded on the subscription
	sub, _ := h.store.GetSubscription(id)
	if sub.LastError == "" {
		t.Fatal("expected LastError to be set")
	}
}

func TestRegisterSubscriptionRoutes(t *testing.T) {
	h, _ := newTestHandler(t)
	r := mux.NewRouter()
	RegisterSubscriptionRoutes(r, h)

	// Verify routes are registered by checking a few
	expectedRoutes := []struct {
		method string
		path   string
	}{
		{"GET", "/subscriptions"},
		{"POST", "/subscriptions"},
		{"GET", "/subscriptions/proxies"},
		{"GET", "/subscriptions/filters"},
		{"PUT", "/subscriptions/filters"},
		{"GET", "/subscriptions/strategy"},
		{"PUT", "/subscriptions/strategy"},
		{"POST", "/subscriptions/apply"},
		{"GET", "/subscriptions/preview"},
	}

	for _, route := range expectedRoutes {
		req, _ := http.NewRequest(route.method, route.path, nil)
		match := &mux.RouteMatch{}
		if !r.Match(req, match) {
			t.Errorf("route %s %s not registered", route.method, route.path)
		}
	}
}

func TestStop_Idempotent(t *testing.T) {
	h, _ := newTestHandler(t)
	// Should not panic
	h.Stop()
	h.Stop()
}

func TestPreview_ObservatoryForLeastPing(t *testing.T) {
	h, _ := newTestHandler(t)
	router := newTestRouter(h)

	addTestSubscriptionWithProxies(t, h.store, 2)
	_ = h.store.SetStrategy(&subscription.RoutingStrategy{Type: "leastping"})

	resp := doRequest(t, router, "GET", "/subscriptions/preview", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	result := parseResponse(t, resp)
	if result["observatory"] == nil {
		t.Fatal("expected observatory for leastping strategy")
	}
}

func TestPreview_NoObservatoryForAll(t *testing.T) {
	h, _ := newTestHandler(t)
	router := newTestRouter(h)

	addTestSubscriptionWithProxies(t, h.store, 2)
	_ = h.store.SetStrategy(&subscription.RoutingStrategy{Type: "all"})

	resp := doRequest(t, router, "GET", "/subscriptions/preview", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	result := parseResponse(t, resp)
	if result["observatory"] != nil {
		t.Fatal("expected no observatory for 'all' strategy")
	}
}

func TestApply_EmptyBody(t *testing.T) {
	h, _ := newTestHandler(t)
	router := newTestRouter(h)

	addTestSubscriptionWithProxies(t, h.store, 2)

	// Apply with empty body (no restart field)
	req, _ := http.NewRequest("POST", "/subscriptions/apply", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestNewSubscriptionHandler_NilFields(t *testing.T) {
	h := NewSubscriptionHandler(nil, nil, nil, "/tmp")
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
}

func TestFetchSubscription_WithMockServer(t *testing.T) {
	h, _ := newTestHandler(t)

	vlessURI := "vless://a1b2c3d4-e5f6-0012-abcd-ef1234567890@1.2.3.4:443?encryption=none&flow=xtls-rprx-vision&type=tcp&security=reality&sni=example.com&fp=chrome&pbk=testkey&sid=abcd1234#TestNode"

	// Use a real httptest.Server for the mock
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		encoded := base64.StdEncoding.EncodeToString([]byte(vlessURI))
		w.Write([]byte(encoded))
	}))
	defer mockServer.Close()

	// Replace fetcher with one that uses the mock server's client
	h.fetcher = subscription.NewFetcherWithClient(mockServer.Client())

	// Add subscription pointing to mock server
	sub := &subscription.Subscription{
		Name:    "Mock Sub",
		URL:     mockServer.URL,
		Enabled: true,
	}
	_ = h.store.AddSubscription(sub)

	router := newTestRouter(h)

	resp := doRequest(t, router, "POST", "/subscriptions/"+sub.ID+"/fetch", nil)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(body))
	}

	result := parseResponse(t, resp)
	if result["success"] != true {
		t.Fatalf("expected success=true")
	}

	totalCount := result["total"].(float64)
	if totalCount < 1 {
		t.Fatalf("expected at least 1 total proxy, got %v", totalCount)
	}
}

func TestApply_RoutingPreservesExisting(t *testing.T) {
	h, xrayDir := newTestHandler(t)
	router := newTestRouter(h)

	// Pre-create xray dir with existing routing
	os.MkdirAll(xrayDir, 0755)
	existingRouting := map[string]interface{}{
		"routing": map[string]interface{}{
			"domainStrategy": "AsIs",
			"rules": []interface{}{
				map[string]interface{}{
					"type":        "field",
					"domain":      []string{"geosite:category-ads-all"},
					"outboundTag": "block",
				},
				map[string]interface{}{
					"type":        "field",
					"domain":      []string{"geosite:google"},
					"outboundTag": "direct",
				},
			},
		},
	}
	routingData, _ := json.MarshalIndent(existingRouting, "", "  ")
	os.WriteFile(filepath.Join(xrayDir, "05_routing.json"), routingData, 0644)

	addTestSubscriptionWithProxies(t, h.store, 2)

	resp := doRequest(t, router, "POST", "/subscriptions/apply", nil)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(body))
	}

	// Read back routing and verify the "direct" rule was preserved
	data, _ := os.ReadFile(filepath.Join(xrayDir, "05_routing.json"))
	var routing map[string]interface{}
	json.Unmarshal(data, &routing)

	routingObj := routing["routing"].(map[string]interface{})
	rules := routingObj["rules"].([]interface{})

	foundDirect := false
	for _, r := range rules {
		rule := r.(map[string]interface{})
		if rule["outboundTag"] == "direct" {
			foundDirect = true
		}
	}
	if !foundDirect {
		t.Fatal("expected existing 'direct' rule to be preserved")
	}
}

func TestFetchSubscription_UpdatesProxyCount(t *testing.T) {
	h, _ := newTestHandler(t)

	vlessURI := "vless://uuid@1.2.3.4:443?encryption=none&type=tcp&security=reality#Node1\nvless://uuid@5.6.7.8:443?encryption=none&type=tcp&security=reality#Node2"
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(base64.StdEncoding.EncodeToString([]byte(vlessURI))))
	}))
	defer mockServer.Close()

	h.fetcher = subscription.NewFetcherWithClient(mockServer.Client())

	sub := &subscription.Subscription{
		Name:    "Mock Sub",
		URL:     mockServer.URL,
		Enabled: true,
	}
	_ = h.store.AddSubscription(sub)

	router := newTestRouter(h)
	resp := doRequest(t, router, "POST", "/subscriptions/"+sub.ID+"/fetch", nil)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(body))
	}

	updated, _ := h.store.GetSubscription(sub.ID)
	if updated.ProxyCount < 1 {
		t.Fatalf("expected ProxyCount >= 1, got %d", updated.ProxyCount)
	}
}


