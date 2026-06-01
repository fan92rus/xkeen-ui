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

	handler := NewMetricsHandler(server.URL, 5*time.Second)

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

	handler := NewMetricsHandler(wrapped.URL, 5*time.Second)

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

	handler := NewMetricsHandler(server.URL, 5*time.Second)

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

	handler := NewMetricsHandler(server.URL, 5*time.Second)

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

func TestGetObservatory_ExpvarStringFormat(t *testing.T) {
	server := mockXrayMetricsServer(xrayExpvarStringResponse)
	defer server.Close()

	handler := NewMetricsHandler(server.URL, 5*time.Second)

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
