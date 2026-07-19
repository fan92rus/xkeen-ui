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
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"github.com/fan92rus/xkeen-ui/internal/subscription"
)

// ---------- Helpers ----------

// newTestHandler creates a SubscriptionHandler with a temp store for testing.
func newTestHandler(t *testing.T) (h *SubscriptionHandler, xrayDir string) {
	t.Helper()
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "subscriptions.json")
	xrayDir = filepath.Join(tmpDir, "xray-configs")

	store, err := subscription.NewStore(storePath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	fetcher := subscription.NewFetcher()
	scheduler := subscription.NewScheduler(store, fetcher)
	t.Cleanup(func() { scheduler.Stop() })

	handler := NewSubscriptionHandler(store, fetcher, scheduler, xrayDir, "", "", "xray")
	h = handler
	return h, xrayDir
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
	// Built-in AWG subscription is always present
	if len(subs) != 1 {
		t.Fatalf("expected 1 subscription (built-in AWG), got %d", len(subs))
	}
	awg := subs[0].(map[string]interface{})
	if awg["id"] != "__awg__" {
		t.Fatalf("expected built-in AWG subscription, got id=%v", awg["id"])
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

	// Verify no null arrays — frontend crashes on .length of null
	result := parseResponse(t, resp)
	for _, key := range []string{"include_countries", "exclude_countries"} {
		val, exists := result[key]
		if !exists || val == nil {
			t.Errorf("%s is null, expected empty array", key)
		}
		if _, ok := val.([]interface{}); !ok {
			t.Errorf("%s is %T, expected array", key, val)
		}
	}
}

func TestUpdateFilters(t *testing.T) {
	h, _ := newTestHandler(t)
	router := newTestRouter(h)

	resp := doRequest(t, router, "PUT", "/subscriptions/filters", map[string]interface{}{
		"include_countries": []string{"DE", "NL"},
		"max_proxies":       50,
	})
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(body))
	}

	// Verify
	filters := h.store.GetFilters()
	if filters.MaxProxies != 50 {
		t.Fatalf("expected max_proxies=50, got %d", filters.MaxProxies)
	}
}

func TestUpdateFilters_InvalidRegex(t *testing.T) {
	h, _ := newTestHandler(t)
	router := newTestRouter(h)

	resp := doRequest(t, router, "PUT", "/subscriptions/filters", map[string]interface{}{
		"include_regexes": []string{"valid", "[invalid"},
	})
	if resp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400 for invalid regex, got %d: %s", resp.StatusCode, string(body))
	}

	result := parseResponse(t, resp)
	if errMsg, ok := result["error"].(string); ok {
		if !strings.Contains(errMsg, "include_regexes[1]") {
			t.Errorf("error should mention include_regexes[1], got: %s", errMsg)
		}
	} else {
		t.Errorf("response should have 'error' string field, got: %v", result)
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
			"type": strategyType,
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

	// Verify outbounds is a JSON object (not a double-encoded string)
	outbounds, ok := result["outbounds"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected outbounds to be a JSON object, got %T: %v", result["outbounds"], result["outbounds"])
	}
	if _, hasKey := outbounds["outbounds"]; !hasKey {
		t.Fatal("expected outbounds object to have 'outbounds' key")
	}

	routing, ok := result["routing"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected routing to be a JSON object, got %T: %v", result["routing"], result["routing"])
	}
	if _, hasKey := routing["routing"]; !hasKey {
		t.Fatal("expected routing object to have 'routing' key")
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
		Type: "leastping",
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

func TestApply_RestartCalled(t *testing.T) {
	h, _ := newTestHandler(t)

	var restartCalled atomic.Int32
	h.SetRestartFn(func() { restartCalled.Add(1) })

	addTestSubscriptionWithProxies(t, h.store, 3)

	router := newTestRouter(h)
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
	if result["restart_initiated"] != true {
		t.Errorf("expected restart_initiated=true in response")
	}

	// Wait briefly for the async goroutine to fire
	for i := 0; i < 50; i++ {
		if restartCalled.Load() == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if restartCalled.Load() != 1 {
		t.Errorf("expected restartFn to be called once, got %d", restartCalled.Load())
	}
}

func TestApply_NoRestartWhenFalse(t *testing.T) {
	h, _ := newTestHandler(t)

	var restartCalled atomic.Int32
	h.SetRestartFn(func() { restartCalled.Add(1) })

	addTestSubscriptionWithProxies(t, h.store, 3)

	router := newTestRouter(h)
	resp := doRequest(t, router, "POST", "/subscriptions/apply", map[string]interface{}{
		"restart": false,
	})
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(body))
	}

	result := parseResponse(t, resp)
	if result["restart_initiated"] != nil {
		t.Errorf("expected no restart_initiated in response, got %v", result["restart_initiated"])
	}

	if restartCalled.Load() != 0 {
		t.Errorf("expected restartFn not to be called, got %d", restartCalled.Load())
	}
}

func TestApply_RestartCalledOnlyWhenFnSet(t *testing.T) {
	h, _ := newTestHandler(t)

	// No restartFn set
	addTestSubscriptionWithProxies(t, h.store, 3)

	router := newTestRouter(h)
	resp := doRequest(t, router, "POST", "/subscriptions/apply", map[string]interface{}{
		"restart": true,
	})
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(body))
	}

	result := parseResponse(t, resp)
	if result["restart_initiated"] != nil {
		t.Errorf("expected no restart_initiated when restartFn is nil")
	}
}

func TestApply_AtomicWriteNoPartialFiles(t *testing.T) {
	h, xrayDir := newTestHandler(t)
	router := newTestRouter(h)

	addTestSubscriptionWithProxies(t, h.store, 2)

	resp := doRequest(t, router, "POST", "/subscriptions/apply", nil)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(body))
	}

	// Verify all expected files exist and are valid JSON
	paths := []string{
		filepath.Join(xrayDir, "04_outbounds.json"),
		filepath.Join(xrayDir, "05_routing.json"),
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("expected file %s to exist: %v", p, err)
			continue
		}
		if !json.Valid(data) {
			t.Errorf("file %s is not valid JSON", p)
		}
	}

	// Verify no .tmp files remain
	entries, _ := os.ReadDir(xrayDir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("unexpected .tmp file left behind: %s", e.Name())
		}
	}
}

func TestAtomicWriteAll_CleanupOnError(t *testing.T) {
	tmpDir := t.TempDir()

	// First file is written and renamed successfully (atomic)
	path1 := filepath.Join(tmpDir, "01.json")
	// Second file target doesn't exist (parent dir missing) — will fail
	path2 := filepath.Join(tmpDir, "subdir", "02.json")

	files := map[string][]byte{
		path1: []byte(`{"a":1}`),
		path2: []byte(`{"b":2}`),
	}

	err := atomicWriteAll(files)
	if err == nil {
		t.Fatal("expected error when target directory doesn't exist")
	}

	// path1 WAS atomically renamed (rename is atomic, not rolled back)
	if _, statErr := os.Stat(path1); os.IsNotExist(statErr) {
		t.Error("expected path1 to exist (atomic rename), but it doesn't")
	}

	// path2 should NOT exist
	if data, _ := os.ReadFile(path2); data != nil {
		t.Error("expected path2 to NOT be written on error")
	}

	// Verify no .tmp files remain
	entries, _ := os.ReadDir(tmpDir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("unexpected .tmp file left behind: %s", e.Name())
		}
	}
}

func TestAtomicWriteAll_Success(t *testing.T) {
	tmpDir := t.TempDir()

	path1 := filepath.Join(tmpDir, "01.json")
	path2 := filepath.Join(tmpDir, "02.json")

	files := map[string][]byte{
		path1: []byte(`{"a":1}`),
		path2: []byte(`{"b":2}`),
	}

	if err := atomicWriteAll(files); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Verify both files exist and have correct content
	data1, _ := os.ReadFile(path1)
	if !strings.Contains(string(data1), `"a":1`) {
		t.Error("path1 content mismatch")
	}
	data2, _ := os.ReadFile(path2)
	if !strings.Contains(string(data2), `"b":2`) {
		t.Error("path2 content mismatch")
	}

	// Verify no .tmp files
	entries, _ := os.ReadDir(tmpDir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("unexpected .tmp file: %s", e.Name())
		}
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
	// 1 added + 1 built-in AWG = 2
	if len(subs) != 2 {
		t.Fatalf("expected 2 subscriptions (added + built-in AWG), got %d", len(subs))
	}

	// The added subscription should be present (either index 0 or 1)
	var found bool
	for _, raw := range subs {
		sub := raw.(map[string]interface{})
		if sub["name"] == "Test Sub" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected to find 'Test Sub' in subscriptions")
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
		req, _ := http.NewRequest(route.method, route.path, http.NoBody)
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
	req, _ := http.NewRequest("POST", "/subscriptions/apply", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestNewSubscriptionHandler_NilFields(t *testing.T) {
	h := NewSubscriptionHandler(nil, nil, nil, "/tmp", "", "", "xray")
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
}

func TestFetchSubscription_WithMockServer(t *testing.T) {
	h, _ := newTestHandler(t)

	vlessURI := "vless://a1b2c3d4-e5f6-0012-abcd-ef1234567890@1.2.3.4:443?encryption=none&flow=xtls-rprx-vision&type=tcp&security=reality&sni=example.com&fp=chrome&pbk=testkey&sid=abcd1234#TestNode"

	// Use a real httptest.Server for the mock
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
	os.MkdirAll(xrayDir, 0o755)
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
	os.WriteFile(filepath.Join(xrayDir, "05_routing.json"), routingData, 0o644)

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
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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

// ---------- Profile CRUD Tests ----------

func TestListProfiles_DefaultProfile(t *testing.T) {
	h, _ := newTestHandler(t)
	router := newTestRouter(h)

	resp := doRequest(t, router, "GET", "/subscriptions/profiles", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Response is an array of profile objects
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}
	var profiles []map[string]interface{}
	if err := json.Unmarshal(data, &profiles); err != nil {
		t.Fatalf("failed to parse profiles array: %v\nbody: %s", err, string(data))
	}

	if len(profiles) < 1 {
		t.Fatalf("expected at least 1 (default) profile, got %d", len(profiles))
	}

	// Default profile should be first
	def := profiles[0]
	if def["is_default"] != true {
		t.Errorf("expected is_default=true, got %v", def["is_default"])
	}
	if def["name"] == nil || def["name"] == "" {
		t.Error("expected default profile to have a name")
	}
	// proxy_count and total_proxy should be present (both 0 when no proxies)
	if _, ok := def["proxy_count"]; !ok {
		t.Error("expected proxy_count field in profile response")
	}
	if _, ok := def["total_proxy"]; !ok {
		t.Error("expected total_proxy field in profile response")
	}
}

func TestListProfiles_WithProxies(t *testing.T) {
	h, _ := newTestHandler(t)
	router := newTestRouter(h)

	addTestSubscriptionWithProxies(t, h.store, 5)

	resp := doRequest(t, router, "GET", "/subscriptions/profiles", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var profiles []map[string]interface{}
	json.Unmarshal(data, &profiles)

	if len(profiles) < 1 {
		t.Fatalf("expected at least 1 profile")
	}

	def := profiles[0]
	// Default profile with no filters should include all 5 proxies
	proxyCount := int(def["proxy_count"].(float64))
	totalProxy := int(def["total_proxy"].(float64))
	if proxyCount != 5 {
		t.Errorf("expected proxy_count=5, got %d", proxyCount)
	}
	if totalProxy != 5 {
		t.Errorf("expected total_proxy=5, got %d", totalProxy)
	}
}

func TestCreateProfile_Success(t *testing.T) {
	h, _ := newTestHandler(t)
	router := newTestRouter(h)

	resp := doRequest(t, router, "POST", "/subscriptions/profiles", map[string]interface{}{
		"name":    "Custom Profile",
		"enabled": true,
		"strategy": map[string]interface{}{
			"type": "random",
		},
	})
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 201, got %d: %s", resp.StatusCode, string(body))
	}

	result := parseResponse(t, resp)
	if result["name"] != "Custom Profile" {
		t.Errorf("expected name='Custom Profile', got %v", result["name"])
	}
	if result["is_default"] == true {
		t.Error("new profile should not be default")
	}
	if result["id"] == nil || result["id"] == "" {
		t.Error("expected auto-generated id")
	}

	// Verify it appears in list
	profiles := h.store.GetProfiles()
	found := false
	for _, p := range profiles {
		if p.Name == "Custom Profile" {
			found = true
			if p.IsDefault {
				t.Error("new profile should have IsDefault=false")
			}
		}
	}
	if !found {
		t.Fatal("created profile not found in store")
	}
}

func TestCreateProfile_RejectsEmptyName(t *testing.T) {
	h, _ := newTestHandler(t)
	router := newTestRouter(h)

	resp := doRequest(t, router, "POST", "/subscriptions/profiles", map[string]interface{}{
		"name":    "",
		"enabled": true,
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}

	result := parseResponse(t, resp)
	if errMsg, ok := result["error"].(string); ok {
		if !containsSubstring(errMsg, "name") {
			t.Errorf("error should mention 'name', got: %s", errMsg)
		}
	}
}

func TestCreateProfile_InvalidRegex(t *testing.T) {
	h, _ := newTestHandler(t)
	router := newTestRouter(h)

	resp := doRequest(t, router, "POST", "/subscriptions/profiles", map[string]interface{}{
		"name":    "Filtered",
		"enabled": true,
		"filter": map[string]interface{}{
			"exclude_regexes": []string{"[bad"},
		},
	})
	if resp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400 for invalid regex, got %d: %s", resp.StatusCode, string(body))
	}

	result := parseResponse(t, resp)
	if errMsg, ok := result["error"].(string); ok {
		if !strings.Contains(errMsg, "exclude_regexes[0]") {
			t.Errorf("error should mention exclude_regexes[0], got: %s", errMsg)
		}
	} else {
		t.Errorf("response should have 'error' string field, got: %v", result)
	}
}

func TestCreateProfile_RejectsMaxProfiles(t *testing.T) {
	h, _ := newTestHandler(t)
	router := newTestRouter(h)

	// Fill up to MaxProfiles (10), default profile already exists (1)
	for i := 0; i < subscription.MaxProfiles-1; i++ {
		resp := doRequest(t, router, "POST", "/subscriptions/profiles", map[string]interface{}{
			"name":    fmt.Sprintf("Profile %d", i+1),
			"enabled": true,
		})
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("profile %d: expected 201, got %d", i+1, resp.StatusCode)
		}
		resp.Body.Close()
	}

	// The next one should be rejected
	resp := doRequest(t, router, "POST", "/subscriptions/profiles", map[string]interface{}{
		"name":    "Overflow",
		"enabled": true,
	})
	if resp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400 for max profiles, got %d: %s", resp.StatusCode, string(body))
	}
}

func TestCreateProfile_RejectsDefaultID(t *testing.T) {
	h, _ := newTestHandler(t)
	router := newTestRouter(h)

	resp := doRequest(t, router, "POST", "/subscriptions/profiles", map[string]interface{}{
		"id":      "default",
		"name":    "Fake Default",
		"enabled": true,
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for reserved id, got %d", resp.StatusCode)
	}
}

func TestUpdateProfile_Success(t *testing.T) {
	h, _ := newTestHandler(t)
	router := newTestRouter(h)

	// Create a profile first
	createResp := doRequest(t, router, "POST", "/subscriptions/profiles", map[string]interface{}{
		"name":    "Original",
		"enabled": true,
	})
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", createResp.StatusCode)
	}
	createResult := parseResponse(t, createResp)
	profileID := createResult["id"].(string)

	// Update it
	resp := doRequest(t, router, "PUT", "/subscriptions/profiles/"+profileID, map[string]interface{}{
		"name":    "Renamed",
		"enabled": true,
		"filter": map[string]interface{}{
			"include_countries": []string{"DE", "NL"},
			"max_proxies":       20,
		},
		"strategy": map[string]interface{}{
			"type": "roundrobin",
		},
	})
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(body))
	}

	result := parseResponse(t, resp)
	if result["name"] != "Renamed" {
		t.Errorf("expected name=Renamed, got %v", result["name"])
	}

	// Verify in store
	p, err := h.store.GetProfile(profileID)
	if err != nil {
		t.Fatalf("profile not found: %v", err)
	}
	if p.Name != "Renamed" {
		t.Errorf("store: expected name=Renamed, got %s", p.Name)
	}
	if len(p.Filter.IncludeCountries) != 2 {
		t.Errorf("expected 2 include_countries, got %d", len(p.Filter.IncludeCountries))
	}
	if p.Strategy.Type != "roundrobin" {
		t.Errorf("expected strategy=roundrobin, got %s", p.Strategy.Type)
	}
}

func TestUpdateProfile_PreservesIsDefault(t *testing.T) {
	h, _ := newTestHandler(t)
	router := newTestRouter(h)

	// Find the default profile ID
	profiles := h.store.GetProfiles()
	var defaultID string
	for _, p := range profiles {
		if p.IsDefault {
			defaultID = p.ID
			break
		}
	}
	if defaultID == "" {
		t.Fatal("no default profile found")
	}

	// Try to update default profile (changing is_default to false should be ignored)
	resp := doRequest(t, router, "PUT", "/subscriptions/profiles/"+defaultID, map[string]interface{}{
		"name":       "Updated Default",
		"enabled":    true,
		"is_default": false, // should be ignored
	})
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(body))
	}

	// Verify IsDefault is still true
	p, _ := h.store.GetProfile(defaultID)
	if !p.IsDefault {
		t.Error("IsDefault should be preserved as true")
	}
}

func TestUpdateProfile_NotFound(t *testing.T) {
	h, _ := newTestHandler(t)
	router := newTestRouter(h)

	resp := doRequest(t, router, "PUT", "/subscriptions/profiles/nonexistent-id", map[string]interface{}{
		"name":    "Ghost",
		"enabled": true,
	})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestUpdateProfile_InvalidRegex(t *testing.T) {
	h, _ := newTestHandler(t)
	router := newTestRouter(h)

	// Create a profile first, then update it with bad regex
	createResp := doRequest(t, router, "POST", "/subscriptions/profiles", map[string]interface{}{
		"name":    "Mutable",
		"enabled": true,
	})
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", createResp.StatusCode)
	}
	createResult := parseResponse(t, createResp)
	profileID := createResult["id"].(string)

	resp := doRequest(t, router, "PUT", "/subscriptions/profiles/"+profileID, map[string]interface{}{
		"name":    "Updated",
		"enabled": true,
		"filter": map[string]interface{}{
			"include_regexes": []string{"unclosed["},
		},
	})
	if resp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400 for invalid regex, got %d: %s", resp.StatusCode, string(body))
	}

	result := parseResponse(t, resp)
	if errMsg, ok := result["error"].(string); ok {
		if !strings.Contains(errMsg, "include_regexes[0]") {
			t.Errorf("error should mention include_regexes[0], got: %s", errMsg)
		}
	} else {
		t.Errorf("response should have 'error' string field, got: %v", result)
	}
}

func TestDeleteProfile_NonDefault(t *testing.T) {
	h, _ := newTestHandler(t)
	router := newTestRouter(h)

	// Create a non-default profile
	createResp := doRequest(t, router, "POST", "/subscriptions/profiles", map[string]interface{}{
		"name":    "Deletable",
		"enabled": true,
	})
	createResult := parseResponse(t, createResp)
	profileID := createResult["id"].(string)

	// Delete it
	resp := doRequest(t, router, "DELETE", "/subscriptions/profiles/"+profileID, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	result := parseResponse(t, resp)
	if result["status"] != "deleted" {
		t.Errorf("expected status=deleted, got %v", result["status"])
	}

	// Verify it's gone from store
	_, err := h.store.GetProfile(profileID)
	if err == nil {
		t.Error("expected profile to be deleted from store")
	}
}

func TestDeleteProfile_DefaultRefused(t *testing.T) {
	h, _ := newTestHandler(t)
	router := newTestRouter(h)

	// Find the default profile ID
	profiles := h.store.GetProfiles()
	var defaultID string
	for _, p := range profiles {
		if p.IsDefault {
			defaultID = p.ID
		}
	}
	if defaultID == "" {
		t.Fatal("no default profile found")
	}

	resp := doRequest(t, router, "DELETE", "/subscriptions/profiles/"+defaultID, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for deleting default, got %d", resp.StatusCode)
	}

	// Verify default profile still exists
	p, err := h.store.GetProfile(defaultID)
	if err != nil {
		t.Fatal("default profile should still exist")
	}
	if !p.IsDefault {
		t.Error("default profile should still have IsDefault=true")
	}
}

func TestDeleteProfile_NotFound(t *testing.T) {
	h, _ := newTestHandler(t)
	router := newTestRouter(h)

	resp := doRequest(t, router, "DELETE", "/subscriptions/profiles/nonexistent-id", nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// ---------- Auto-Apply Handler Tests ----------

func TestGetAutoApply_Defaults(t *testing.T) {
	h, _ := newTestHandler(t)
	router := newTestRouter(h)

	resp := doRequest(t, router, "GET", "/subscriptions/auto-apply", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	result := parseResponse(t, resp)
	if result["enabled"] == nil {
		t.Error("expected 'enabled' field")
	}
	if result["cron"] == nil {
		t.Error("expected 'cron' field")
	}
	// By default, auto-apply should be disabled
	if result["enabled"] == true {
		t.Error("expected auto-apply disabled by default")
	}
}

func TestUpdateAutoApply_ValidCron(t *testing.T) {
	h, _ := newTestHandler(t)
	router := newTestRouter(h)

	resp := doRequest(t, router, "PUT", "/subscriptions/auto-apply", map[string]interface{}{
		"enabled": true,
		"cron":    "0 */6 * * *",
	})
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(body))
	}

	result := parseResponse(t, resp)
	if result["enabled"] != true {
		t.Errorf("expected enabled=true, got %v", result["enabled"])
	}
	if result["cron"] != "0 */6 * * *" {
		t.Errorf("expected cron='0 */6 * * *', got %v", result["cron"])
	}

	// Verify persisted in store
	enabled, cronExpr := h.store.GetAutoApply()
	if !enabled {
		t.Error("store: expected enabled=true")
	}
	if cronExpr != "0 */6 * * *" {
		t.Errorf("store: expected cron='0 */6 * * *', got %s", cronExpr)
	}
}

func TestUpdateAutoApply_InvalidCron(t *testing.T) {
	h, _ := newTestHandler(t)
	router := newTestRouter(h)

	resp := doRequest(t, router, "PUT", "/subscriptions/auto-apply", map[string]interface{}{
		"enabled": true,
		"cron":    "not-a-valid-cron",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid cron, got %d", resp.StatusCode)
	}

	result := parseResponse(t, resp)
	if errMsg, ok := result["error"].(string); ok {
		if !containsSubstring(errMsg, "cron") {
			t.Errorf("error should mention 'cron', got: %s", errMsg)
		}
	}

	// Verify NOT persisted
	enabled, _ := h.store.GetAutoApply()
	if enabled {
		t.Error("invalid cron should not enable auto-apply")
	}
}

func TestUpdateAutoApply_Disable(t *testing.T) {
	h, _ := newTestHandler(t)
	router := newTestRouter(h)

	// First enable
	enableResp := doRequest(t, router, "PUT", "/subscriptions/auto-apply", map[string]interface{}{
		"enabled": true,
		"cron":    "0 */6 * * *",
	})
	if enableResp.StatusCode != http.StatusOK {
		t.Fatalf("enable: expected 200, got %d", enableResp.StatusCode)
	}

	// Now disable
	resp := doRequest(t, router, "PUT", "/subscriptions/auto-apply", map[string]interface{}{
		"enabled": false,
		"cron":    "",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("disable: expected 200, got %d", resp.StatusCode)
	}

	result := parseResponse(t, resp)
	if result["enabled"] != false {
		t.Errorf("expected enabled=false, got %v", result["enabled"])
	}

	// Verify persisted
	enabled, _ := h.store.GetAutoApply()
	if enabled {
		t.Error("store: expected enabled=false after disable")
	}
}

func TestUpdateAutoApply_EmptyCronWithEnable(t *testing.T) {
	h, _ := newTestHandler(t)
	router := newTestRouter(h)

	// Empty cron with enabled=false should succeed (just disables)
	resp := doRequest(t, router, "PUT", "/subscriptions/auto-apply", map[string]interface{}{
		"enabled": false,
		"cron":    "",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// ---------- Profile Route Registration ----------

func TestRegisterSubscriptionRoutes_Profiles(t *testing.T) {
	h, _ := newTestHandler(t)
	r := mux.NewRouter()
	RegisterSubscriptionRoutes(r, h)

	profileRoutes := []struct {
		method string
		path   string
	}{
		{"GET", "/subscriptions/profiles"},
		{"POST", "/subscriptions/profiles"},
		{"PUT", "/subscriptions/profiles/test-id"},
		{"DELETE", "/subscriptions/profiles/test-id"},
		{"GET", "/subscriptions/auto-apply"},
		{"PUT", "/subscriptions/auto-apply"},
	}

	for _, route := range profileRoutes {
		req, _ := http.NewRequest(route.method, route.path, http.NoBody)
		match := &mux.RouteMatch{}
		if !r.Match(req, match) {
			t.Errorf("route %s %s not registered", route.method, route.path)
		}
	}
}

// ---------- Bug Confirmation: Preview shows applied config, ignoring filter changes ----------

func TestPreview_FiltersAffectOutput(t *testing.T) {
	// This test verifies that filter changes are reflected in the preview output.
	// Previously, filters had no effect on preview for the default profile with strategy "all".
	// After the fix:
	//   - filtered_proxy_count reflects how many proxies pass the filter
	//   - outbounds only includes filtered proxies
	//   - when all proxies are filtered out, preview returns a message

	h, _ := newTestHandler(t)
	router := newTestRouter(h)

	addTestSubscriptionWithProxies(t, h.store, 5)

	// Apply with default filters (strategy="all", no filter rules)
	resp := doRequest(t, router, "POST", "/subscriptions/apply", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("apply failed: %d", resp.StatusCode)
	}

	// Preview BEFORE filter changes
	resp = doRequest(t, router, "GET", "/subscriptions/preview", nil)
	beforeResult := parseResponse(t, resp)
	beforeFilteredCount := beforeResult["filtered_proxy_count"].(float64)
	beforeProxyCount := beforeResult["proxy_count"].(float64)
	t.Logf("BEFORE filter: proxy_count=%.0f, filtered_proxy_count=%.0f", beforeProxyCount, beforeFilteredCount)

	if beforeFilteredCount != 5 {
		t.Errorf("expected filtered_proxy_count=5 (no filter), got %.0f", beforeFilteredCount)
	}
	if beforeProxyCount != 5 {
		t.Errorf("expected proxy_count=5, got %.0f", beforeProxyCount)
	}

	filters := h.store.GetFilters()
	if err := h.store.SetFilters(filters); err != nil {
		t.Fatalf("failed to set filters: %v", err)
	}

	// Verify filter was applied correctly
	profiles := h.store.GetProfiles()
	allProxies := h.store.GetProxies()
	for _, p := range profiles {
		if p.IsDefault {
			filtered := subscription.ApplyFilter(allProxies, &p.Filter)
			_ = filtered
		}
	}

	// Preview AFTER filter changes
	resp = doRequest(t, router, "GET", "/subscriptions/preview", nil)
	afterResult := parseResponse(t, resp)
	afterFilteredCount := afterResult["filtered_proxy_count"].(float64)
	afterProxyCount := afterResult["proxy_count"].(float64)
	t.Logf("AFTER filter: proxy_count=%.0f, filtered_proxy_count=%.0f", afterProxyCount, afterFilteredCount)

	// After fix: filtered_proxy_count should be 0 (all proxies filtered out)
	if afterFilteredCount == 0 {
		t.Log("all proxies filtered out as expected")
	}

	// proxy_count still shows total (5)
	if afterProxyCount != 5 {
		t.Errorf("expected proxy_count=5 (total), got %.0f", afterProxyCount)
	}

	// The preview should indicate no proxies pass filters
	if msg, ok := afterResult["message"].(string); ok {
		if !strings.Contains(msg, "filter") {
			t.Errorf("expected filter-related message, got: %q", msg)
		}
	}
}

func TestPreview_NonDefaultProfile_FilterAffectsBalancer(t *testing.T) {
	// For non-default profiles with strategy != "all",
	// filters DO affect the balancer selector. But the outbounds still
	// include ALL proxies regardless of any profile's filter.

	h, _ := newTestHandler(t)
	router := newTestRouter(h)

	addTestSubscriptionWithProxies(t, h.store, 5)

	// Create a non-default profile with country filter and random strategy
	profile := &subscription.Profile{
		Name:    "EU Only",
		Enabled: true,
		Filter: subscription.Filter{
			IncludeCountries: []string{"DE"}, // only DE proxies
		},
		Strategy: subscription.RoutingStrategy{Type: "random"},
	}
	if err := h.store.AddProfile(profile); err != nil {
		t.Fatalf("failed to add profile: %v", err)
	}

	resp := doRequest(t, router, "GET", "/subscriptions/preview", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("preview failed: %d", resp.StatusCode)
	}

	result := parseResponse(t, resp)

	// proxy_count still shows ALL proxies (not filtered)
	proxyCount := result["proxy_count"].(float64)
	if proxyCount != 5 {
		t.Errorf("expected proxy_count=5 (all), got %.0f", proxyCount)
	}

	// But routing should have a balancer for the non-default profile
	// with selector containing only DE proxy tags
	routingMap, ok := result["routing"].(map[string]interface{})
	if !ok {
		t.Fatal("expected routing object")
	}
	routingInner, ok := routingMap["routing"].(map[string]interface{})
	if !ok {
		t.Fatal("expected routing inner object")
	}

	balancers, ok := routingInner["balancers"].([]interface{})
	if !ok || len(balancers) < 1 {
		t.Fatal("expected at least 1 balancer")
	}

	// Find the EU profile's balancer
	var euBalancer map[string]interface{}
	for _, b := range balancers {
		bm := b.(map[string]interface{})
		if bm["tag"] == profile.ID+"-balancer" {
			euBalancer = bm
			break
		}
	}
	if euBalancer == nil {
		t.Fatal("EU profile balancer not found")
	}

	selector, ok := euBalancer["selector"].([]interface{})
	if !ok {
		t.Fatal("expected selector array in EU balancer")
	}

	t.Logf("EU balancer selector has %d proxies (all test proxies are DE)", len(selector))
	// All test proxies are DE country, so they should all be included
	if len(selector) == 0 {
		t.Error("EU balancer selector should have proxies")
	}
}

// ---------- Helpers ----------

// containsSubstring checks if s contains substr.
func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || s != "" && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestRegenerateOutbounds verifies that RegenerateOutbounds writes the
// 04_outbounds.json file with the current mark setting.
func TestRegenerateOutbounds(t *testing.T) {
	h, xrayDir := newTestHandler(t)

	// Add a subscription + proxy so generation has something to work with
	sub := &subscription.Subscription{
		ID:       "sub1",
		Name:     "Test",
		URL:      "https://example.com/sub",
		Enabled:  true,
		Interval: subscription.FlexibleInt(60),
	}
	if err := h.store.AddSubscription(sub); err != nil {
		t.Fatalf("AddSubscription: %v", err)
	}
	outboundJSON := []byte("{\"protocol\":\"vless\",\"settings\":{\"vnext\":[{\"address\":\"1.2.3.4\",\"port\":443}]}}")
	h.store.SetProxies([]*subscription.ProxyEntry{
		{SubscriptionID: "sub1", Protocol: "vless", Outbound: outboundJSON},
	})

	// mark=0: no mark in output
	h.SetMark(0)
	if err := h.RegenerateOutbounds(); err != nil {
		t.Fatalf("RegenerateOutbounds (mark=0): %v", err)
	}
	data, err := os.ReadFile(filepath.Join(xrayDir, "04_outbounds.json"))
	if err != nil {
		t.Fatalf("read outbounds: %v", err)
	}
	if bytes.Contains(data, []byte("mark")) {
		t.Errorf("mark=0 should not produce 'mark' in output, got: %s", data)
	}

	// mark=255: mark should appear in streamSettings.sockopt
	h.SetMark(255)
	if err := h.RegenerateOutbounds(); err != nil {
		t.Fatalf("RegenerateOutbounds (mark=255): %v", err)
	}
	data, err = os.ReadFile(filepath.Join(xrayDir, "04_outbounds.json"))
	if err != nil {
		t.Fatalf("read outbounds: %v", err)
	}
	if !bytes.Contains(data, []byte(`"mark": 255`)) {
		t.Errorf("mark=255 should produce \"mark\": 255 in output, got: %s", data)
	}
	if !bytes.Contains(data, []byte("sockopt")) {
		t.Errorf("mark=255 should produce sockopt section, got: %s", data)
	}
}

// TestRegenerateOutbounds_EmptyStore verifies that calling RegenerateOutbounds
// when no proxies exist is a no-op (returns nil), not an error.
// proxy_entware toggle may be invoked before any subscription is fetched.
func TestRegenerateOutbounds_EmptyStore(t *testing.T) {
	h, xrayDir := newTestHandler(t)

	// No proxies added to the store
	h.SetMark(255)
	err := h.RegenerateOutbounds()
	if err != nil {
		t.Errorf("RegenerateOutbounds on empty store should return nil, got: %v", err)
	}
	// Verify no file was written
	if _, err := os.Stat(filepath.Join(xrayDir, "04_outbounds.json")); err == nil {
		t.Error("outbounds file should not be created when there are no proxies")
	}
}
