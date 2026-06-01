package subscription

import (
	"encoding/json"
	"testing"
)

func TestGenerateMetricsJSON_Disabled(t *testing.T) {
	result := GenerateMetricsJSON(0)
	if result != nil {
		t.Errorf("expected nil for port=0, got: %s", string(result))
	}
}

func TestGenerateMetricsJSON_ZeroPort(t *testing.T) {
	result := GenerateMetricsJSON(0)
	if result != nil {
		t.Errorf("expected nil for zero port, got: %s", string(result))
	}
}

func TestGenerateMetricsJSON_NegativePort(t *testing.T) {
	result := GenerateMetricsJSON(-1)
	if result != nil {
		t.Errorf("expected nil for negative port, got: %s", string(result))
	}
}

func TestGenerateMetricsJSON_Enabled(t *testing.T) {
	port := 11111
	result := GenerateMetricsJSON(port)
	if result == nil {
		t.Fatal("expected non-nil result for port > 0")
	}

	// Validate JSON structure
	var obj map[string]interface{}
	if err := json.Unmarshal(result, &obj); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// Check metrics key exists
	metrics, ok := obj["metrics"]
	if !ok {
		t.Fatal("missing 'metrics' key")
	}

	metricsMap, ok := metrics.(map[string]interface{})
	if !ok {
		t.Fatal("'metrics' is not an object")
	}

	// Check tag
	if metricsMap["tag"] != "Metrics" {
		t.Errorf("expected tag='Metrics', got %v", metricsMap["tag"])
	}

	// Check listen
	expectedListen := "127.0.0.1:11111"
	if metricsMap["listen"] != expectedListen {
		t.Errorf("expected listen=%s, got %v", expectedListen, metricsMap["listen"])
	}
}

func TestGenerateMetricsJSON_DifferentPort(t *testing.T) {
	port := 18888
	result := GenerateMetricsJSON(port)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	var obj map[string]interface{}
	if err := json.Unmarshal(result, &obj); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	metricsMap := obj["metrics"].(map[string]interface{})
	expectedListen := "127.0.0.1:18888"
	if metricsMap["listen"] != expectedListen {
		t.Errorf("expected listen=%s, got %v", expectedListen, metricsMap["listen"])
	}
}
