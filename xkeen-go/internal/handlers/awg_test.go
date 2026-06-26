package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"github.com/fan92rus/xkeen-ui/internal/subscription"
)

// newTestAWGHandler creates an AWGHandler with a temp store and awgDir.
func newTestAWGHandler(t *testing.T) (*AWGHandler, *mux.Router, string) {
	t.Helper()
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "subscriptions.json")
	awgDir := filepath.Join(tmpDir, "awg")
	os.MkdirAll(awgDir, 0755)

	store, err := subscription.NewStore(storePath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	handler := NewAWGHandler(store, awgDir)
	r := mux.NewRouter()
	RegisterAWGRoutes(r, handler)

	return handler, r, awgDir
}

// ---------- ListInterfaces ----------

func TestAWGListInterfaces_Empty(t *testing.T) {
	_, router, _ := newTestAWGHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/awg/interfaces", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp awgInterfacesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// interfaces must be [] not null — frontend expects an array
	if resp.Interfaces == nil {
		t.Fatal("interfaces is null — frontend will crash on .length")
	}
	if len(resp.Interfaces) != 0 {
		t.Errorf("expected 0 interfaces, got %d", len(resp.Interfaces))
	}
}

func TestAWGListInterfaces_WithConfigs(t *testing.T) {
	handler, router, awgDir := newTestAWGHandler(t)

	// Write two .conf files
	confContent := `[Interface]
PrivateKey = aA1bB2cC3dD4eE5fF6gG7hH8iI9jJ0kK1lL2mM3nN4oO=
Address = 10.0.0.2/32
DNS = 1.1.1.1

[Peer]
PublicKey = pP0oO1iI2uU3yY4tT5rR6eE7wW8qQ9zZ0xX1cC2vV3bB4nN5mM=
AllowedIPs = 0.0.0.0/0
Endpoint = 162.159.192.192:2408
`
	os.WriteFile(filepath.Join(awgDir, "warp1.conf"), []byte(confContent), 0644)
	os.WriteFile(filepath.Join(awgDir, "warp2.conf"), []byte(confContent), 0644)

	// Register configs in store
	handler.store.ScanAWGConfigs(awgDir)

	req := httptest.NewRequest(http.MethodGet, "/awg/interfaces", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp awgInterfacesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(resp.Interfaces) != 2 {
		t.Fatalf("expected 2 interfaces, got %d", len(resp.Interfaces))
	}

	if resp.Interfaces[0].Name != "warp1" {
		t.Errorf("expected name 'warp1', got %q", resp.Interfaces[0].Name)
	}
	if resp.Interfaces[1].Name != "warp2" {
		t.Errorf("expected name 'warp2', got %q", resp.Interfaces[1].Name)
	}

	// Both should be inactive (no real awg tool)
	if resp.Interfaces[0].Active {
		t.Error("warp1 should be inactive")
	}
	if resp.Interfaces[1].Active {
		t.Error("warp2 should be inactive")
	}

	// Mark should be assigned sequentially
	if resp.Interfaces[0].Mark != 100 {
		t.Errorf("warp1 expected mark 100, got %d", resp.Interfaces[0].Mark)
	}
	if resp.Interfaces[1].Mark != 101 {
		t.Errorf("warp2 expected mark 101, got %d", resp.Interfaces[1].Mark)
	}
}

// ---------- validateAWGName ----------

func TestValidateAWGName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"empty", "", false},
		{"simple", "warp", true},
		{"with_hyphen", "my-config", true},
		{"with_underscore", "my_config", true},
		{"alphanumeric", "warp123", true},
		{"with_dots", "config.v1", false},  // dots not allowed by validateAWGName
		{"path_traversal", "../etc", false},
		{"with_slash", "a/b", false},
		{"with_backslash", "a\\b", false},
		{"dot_prefix", ".hidden", false},
		{"dot_dot", "a..b", false},
		{"special_chars", "warp@1", false},
		{"with_space", "my config", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validateAWGName(tt.input)
			if got != tt.want {
				t.Errorf("validateAWGName(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// ---------- UpInterface validation ----------

func TestAWGUpInterface_Validation(t *testing.T) {
	_, router, _ := newTestAWGHandler(t)

	// Empty name should fail
	req := httptest.NewRequest(http.MethodPost, "/awg/up", body(`{"name":""}`))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty name, got %d", w.Code)
	}

	// Path traversal should fail
	req = httptest.NewRequest(http.MethodPost, "/awg/up", body(`{"name":"../etc/passwd"}`))
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for path traversal, got %d", w.Code)
	}
}

func TestAWGDownInterface_Validation(t *testing.T) {
	_, router, _ := newTestAWGHandler(t)

	// Empty name should fail
	req := httptest.NewRequest(http.MethodPost, "/awg/down", body(`{"name":""}`))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty name, got %d", w.Code)
	}
}

// ---------- DeleteConfig ----------

func TestAWGDeleteConfig_NotFound(t *testing.T) {
	_, router, _ := newTestAWGHandler(t)

	req := httptest.NewRequest(http.MethodDelete, "/awg/config/nonexistent", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for nonexistent config, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAWGDeleteConfig_PathTraversal(t *testing.T) {
	// validateAWGName rejects traversal patterns
	tests := []struct {
		name  string
		input string
	}{
		{"contains_slash", "a/b"},
		{"contains_dotdot", ".."},
		{"starts_with_dot", ".hidden"},
		{"empty", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if validateAWGName(tt.input) {
				t.Errorf("validateAWGName(%q) should be false", tt.input)
			}
		})
	}
}

// ---------- Helpers ----------

func body(s string) io.Reader {
	return strings.NewReader(s)
}
