package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// mockXrayMetricsServer creates a test server that mimics Xray's /debug/vars endpoint.
func mockXrayMetricsServer(body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/debug/vars" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(body))
		} else {
			http.NotFound(w, r)
		}
	}))
}

// xrayFullResponse is a representative /debug/vars response from Xray.
const xrayFullResponse = `{
	"stats": {
		"inbound": {
			"tproxy_tcp_inbound": {"downlink": 4739161, "uplink": 1568869},
			"http_inbound": {"downlink": 74460, "uplink": 10231}
		},
		"outbound": {
			"proxy-DE-1": {"downlink": 23873238, "uplink": 1049595},
			"direct": {"downlink": 97714548, "uplink": 3234617}
		}
	},
	"observatory": {
		"proxy-DE-1": {
			"alive": true,
			"delay": 782,
			"outbound_tag": "proxy-DE-1",
			"last_seen_time": 1648477189,
			"last_try_time": 1648477189
		}
	}
}`

func TestGetStats_Success(t *testing.T) {
	server := mockXrayMetricsServer(xrayFullResponse)
	defer server.Close()

	handler := NewMetricsHandlerHTTPOnly(server.URL, 5*time.Second)

	req := httptest.NewRequest("GET", "/api/metrics/stats", nil)
	w := httptest.NewRecorder()
	handler.GetStats(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if body["available"] != true {
		t.Errorf("expected available=true, got %v", body["available"])
	}
	if body["inbound"] == nil {
		t.Error("expected inbound data")
	}
	if body["outbound"] == nil {
		t.Error("expected outbound data")
	}
}

func TestGetStats_Cache(t *testing.T) {
	server := mockXrayMetricsServer(xrayFullResponse)
	defer server.Close()

	// Count how many times the server is actually hit
	var hitCount atomic.Int32
	wrapped := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitCount.Add(1)
		server.Config.Handler.ServeHTTP(w, r)
	}))
	defer wrapped.Close()

	handler := NewMetricsHandlerHTTPOnly(wrapped.URL, 5*time.Second)

	// First request — should hit the server
	req1 := httptest.NewRequest("GET", "/api/metrics/stats", nil)
	w1 := httptest.NewRecorder()
	handler.GetStats(w1, req1)

	if w1.Result().StatusCode != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", w1.Result().StatusCode)
	}

	// Second request immediately — should use cache
	req2 := httptest.NewRequest("GET", "/api/metrics/stats", nil)
	w2 := httptest.NewRecorder()
	handler.GetStats(w2, req2)

	if w2.Result().StatusCode != http.StatusOK {
		t.Fatalf("second request: expected 200, got %d", w2.Result().StatusCode)
	}

	hits := hitCount.Load()
	if hits != 1 {
		t.Errorf("expected 1 server hit (second from cache), got %d", hits)
	}
}

func TestGetStats_XrayUnavailable(t *testing.T) {
	// Create a handler pointing to a non-existent server
	handler := NewMetricsHandler("http://127.0.0.1:59999", 5*time.Second)

	req := httptest.NewRequest("GET", "/api/metrics/stats", nil)
	w := httptest.NewRecorder()
	handler.GetStats(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if body["available"] != false {
		t.Errorf("expected available=false, got %v", body["available"])
	}
	if body["error"] == nil {
		t.Error("expected error message")
	}
}

func TestGetObservatory_Success(t *testing.T) {
	server := mockXrayMetricsServer(xrayFullResponse)
	defer server.Close()

	handler := NewMetricsHandlerHTTPOnly(server.URL, 5*time.Second)

	req := httptest.NewRequest("GET", "/api/metrics/observatory", nil)
	w := httptest.NewRecorder()
	handler.GetObservatory(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if body["available"] != true {
		t.Errorf("expected available=true, got %v", body["available"])
	}
	if body["results"] == nil {
		t.Error("expected observatory results")
	}

	// Check observatory data structure
	results := body["results"].(map[string]interface{})
	if _, ok := results["proxy-DE-1"]; !ok {
		t.Error("expected proxy-DE-1 in observatory results")
	}
}

func TestGetObservatory_XrayUnavailable(t *testing.T) {
	handler := NewMetricsHandler("http://127.0.0.1:59998", 5*time.Second)

	req := httptest.NewRequest("GET", "/api/metrics/observatory", nil)
	w := httptest.NewRecorder()
	handler.GetObservatory(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", resp.StatusCode)
	}
}

// xrayExpvarStringResponse mimics real expvar where stats/observatory are JSON strings.
const xrayExpvarStringResponse = `{
	"stats": "{\"inbound\":{\"tproxy\":{\"downlink\":100,\"uplink\":50}},\"outbound\":{\"proxy-1\":{\"downlink\":999,\"uplink\":111}}}",
	"observatory": "{\"proxy-1\":{\"alive\":true,\"delay\":120}}"
}`

func TestGetStats_ExpvarStringFormat(t *testing.T) {
	server := mockXrayMetricsServer(xrayExpvarStringResponse)
	defer server.Close()

	handler := NewMetricsHandlerHTTPOnly(server.URL, 5*time.Second)

	req := httptest.NewRequest("GET", "/api/metrics/stats", nil)
	w := httptest.NewRecorder()
	handler.GetStats(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)

	if body["available"] != true {
		t.Errorf("expected available=true")
	}
	outbound, _ := body["outbound"].(map[string]interface{})
	if len(outbound) == 0 {
		t.Error("expected outbound data from string format")
	}
}

// xrayNullStatsResponse mimics Xray where stats feature is loaded
// (key exists) but no counters registered (value is JSON null).
// This happens when policy.system stats flags are not applied.
const xrayNullStatsResponse = `{
	"cmdline": ["/usr/bin/xray"],
	"memstats": {"Alloc": 123456},
	"stats": null,
	"observatory": "{\"proxy-1\":{\"alive\":true,\"delay\":120}}"
}`

func TestGetStats_NullStats(t *testing.T) {
	server := mockXrayMetricsServer(xrayNullStatsResponse)
	defer server.Close()

	handler := NewMetricsHandlerHTTPOnly(server.URL, 5*time.Second)

	req := httptest.NewRequest("GET", "/api/metrics/stats", nil)
	w := httptest.NewRecorder()
	handler.GetStats(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if body["available"] != true {
		t.Errorf("expected available=true, got %v", body["available"])
	}
	// When stats is null (no counters), should return empty maps, not nil
	if body["inbound"] == nil {
		t.Error("expected non-nil inbound (empty map), got nil")
	}
	if body["outbound"] == nil {
		t.Error("expected non-nil outbound (empty map), got nil")
	}
	// Empty maps should have zero entries
	inbound, _ := body["inbound"].(map[string]interface{})
	if len(inbound) != 0 {
		t.Errorf("expected empty inbound map, got %d entries", len(inbound))
	}
	outbound, _ := body["outbound"].(map[string]interface{})
	if len(outbound) != 0 {
		t.Errorf("expected empty outbound map, got %d entries", len(outbound))
	}
	// Debug field should explain the null stats situation
	if body["debug"] == nil {
		t.Error("expected debug field explaining null stats")
	}
}

func TestGetStats_NoStatsKey(t *testing.T) {
	// Xray response without stats key at all (stats feature not loaded)
	server := mockXrayMetricsServer(`{"cmdline":[],"memstats":{}}`)
	defer server.Close()

	handler := NewMetricsHandlerHTTPOnly(server.URL, 5*time.Second)

	req := httptest.NewRequest("GET", "/api/metrics/stats", nil)
	w := httptest.NewRecorder()
	handler.GetStats(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if body["available"] != true {
		t.Errorf("expected available=true, got %v", body["available"])
	}
	if body["inbound"] != nil {
		t.Errorf("expected nil inbound (no stats key), got %v", body["inbound"])
	}
	if body["debug"] == nil {
		t.Error("expected debug field when stats key missing")
	}
}

func TestGetObservatory_ExpvarStringFormat(t *testing.T) {
	server := mockXrayMetricsServer(xrayExpvarStringResponse)
	defer server.Close()

	handler := NewMetricsHandlerHTTPOnly(server.URL, 5*time.Second)

	req := httptest.NewRequest("GET", "/api/metrics/observatory", nil)
	w := httptest.NewRecorder()
	handler.GetObservatory(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)

	results, _ := body["results"].(map[string]interface{})
	if len(results) == 0 {
		t.Error("expected observatory results from string format")
	}
}

// ── WebSocket & History tests ──

func TestMetricsHistory_RingBuffer(t *testing.T) {
	handler := NewMetricsHandlerHTTPOnly("http://127.0.0.1:1", 1*time.Second)

	// Fill the ring buffer beyond capacity
	for i := 0; i < metricsHistoryCapacity+10; i++ {
		handler.appendHistory(MetricsSnapshot{
			Timestamp: int64(i),
			Available: true,
		})
	}

	history := handler.getHistory()
	if len(history) != metricsHistoryCapacity {
		t.Fatalf("expected %d entries, got %d", metricsHistoryCapacity, len(history))
	}

	// Should be in chronological order, oldest entries dropped
	// The oldest entry should be i=10 (first 10 were overwritten)
	if history[0].Timestamp != 10 {
		t.Errorf("expected first entry ts=10, got %d", history[0].Timestamp)
	}
	if history[metricsHistoryCapacity-1].Timestamp != int64(metricsHistoryCapacity+10-1) {
		t.Errorf("expected last entry ts=%d, got %d", metricsHistoryCapacity+10-1, history[metricsHistoryCapacity-1].Timestamp)
	}
}

func TestMetricsHistory_Empty(t *testing.T) {
	handler := NewMetricsHandlerHTTPOnly("http://127.0.0.1:1", 1*time.Second)
	history := handler.getHistory()
	if history != nil {
		t.Errorf("expected nil for empty history, got %v", history)
	}
}

func TestMetricsHistory_ChronologicalOrder(t *testing.T) {
	handler := NewMetricsHandlerHTTPOnly("http://127.0.0.1:1", 1*time.Second)

	// Add a few entries
	for i := 0; i < 5; i++ {
		handler.appendHistory(MetricsSnapshot{
			Timestamp: int64(i * 100),
		})
	}

	history := handler.getHistory()
	for i := 1; i < len(history); i++ {
		if history[i].Timestamp <= history[i-1].Timestamp {
			t.Errorf("history not in chronological order at index %d: %d <= %d",
				i, history[i].Timestamp, history[i-1].Timestamp)
		}
	}
}

func TestCollectSnapshot_Live(t *testing.T) {
	server := mockXrayMetricsServer(xrayFullResponse)
	defer server.Close()

	handler := NewMetricsHandlerHTTPOnly(server.URL, 5*time.Second)

	snap := handler.collectSnapshot()
	if !snap.Available {
		t.Fatalf("expected available snapshot, got: %s", snap.Debug)
	}
	if snap.Timestamp == 0 {
		t.Error("expected non-zero timestamp")
	}
	if snap.Inbound == nil {
		t.Error("expected inbound data")
	}
	if snap.Outbound == nil {
		t.Error("expected outbound data")
	}
	if snap.Observable == nil {
		t.Error("expected observatory data")
	}
}

func TestCollectSnapshot_Unavailable(t *testing.T) {
	handler := NewMetricsHandlerHTTPOnly("http://127.0.0.1:59999", 1*time.Second)

	snap := handler.collectSnapshot()
	if snap.Available {
		t.Error("expected unavailable snapshot")
	}
	if snap.Debug == "" {
		t.Error("expected debug message")
	}
}

func TestNewMetricsHandler_Close(t *testing.T) {
	server := mockXrayMetricsServer(xrayFullResponse)
	defer server.Close()

	handler := NewMetricsHandlerWithOrigins(server.URL, 5*time.Second, nil)

	// Give background goroutines time to start
	time.Sleep(100 * time.Millisecond)

	// Close should not panic
	handler.Close()
}

// ── Proxy Names tests ──

func TestGetProxyNames_Empty(t *testing.T) {
	handler := NewMetricsHandlerHTTPOnly("http://127.0.0.1:1", 1*time.Second)

	req := httptest.NewRequest("GET", "/api/metrics/proxy-names", nil)
	w := httptest.NewRecorder()
	handler.GetProxyNames(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(body) != 0 {
		t.Errorf("expected empty map, got %d entries", len(body))
	}
}

func TestGetProxyNames_WithData(t *testing.T) {
	handler := NewMetricsHandlerHTTPOnly("http://127.0.0.1:1", 1*time.Second)

	// Set proxy names
	handler.UpdateProxyNames(map[string]string{
		"proxy-DE-1": "Germany Fast Server",
		"proxy-US-1": "USA Premium Node",
		"direct":     "",
	})

	req := httptest.NewRequest("GET", "/api/metrics/proxy-names", nil)
	w := httptest.NewRecorder()
	handler.GetProxyNames(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if body["proxy-DE-1"] != "Germany Fast Server" {
		t.Errorf("expected 'Germany Fast Server', got %q", body["proxy-DE-1"])
	}
	if body["proxy-US-1"] != "USA Premium Node" {
		t.Errorf("expected 'USA Premium Node', got %q", body["proxy-US-1"])
	}
	// Empty remarks should not be in the map
	if _, ok := body["direct"]; ok {
		t.Error("expected 'direct' (empty remarks) to be excluded from map")
	}
}

func TestGetProxyNames_UpdateOverwrites(t *testing.T) {
	handler := NewMetricsHandlerHTTPOnly("http://127.0.0.1:1", 1*time.Second)

	// First update
	handler.UpdateProxyNames(map[string]string{
		"proxy-DE-1": "Old Name",
	})

	// Second update — should replace entirely
	handler.UpdateProxyNames(map[string]string{
		"proxy-US-1": "USA Node",
	})

	req := httptest.NewRequest("GET", "/api/metrics/proxy-names", nil)
	w := httptest.NewRecorder()
	handler.GetProxyNames(w, req)

	var body map[string]string
	resp := w.Result()
	json.NewDecoder(resp.Body).Decode(&body)

	if _, ok := body["proxy-DE-1"]; ok {
		t.Error("expected old entry to be removed after update")
	}
	if body["proxy-US-1"] != "USA Node" {
		t.Errorf("expected 'USA Node', got %q", body["proxy-US-1"])
	}
}

func TestNewMetricsHandlerWithOrigins(t *testing.T) {
	server := mockXrayMetricsServer(xrayFullResponse)
	defer server.Close()

	handler := NewMetricsHandlerWithOrigins(server.URL, 5*time.Second, []string{"http://localhost:3000"})
	defer handler.Close()

	if !handler.allowedOrigins["http://localhost:3000"] {
		t.Error("expected allowed origin to be set")
	}
}
