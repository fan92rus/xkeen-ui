package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
)

// --- CommandsHandler construction ---

func TestNewCommandsHandler(t *testing.T) {
	h := NewCommandsHandler()
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
	if h.allowedCommands == nil {
		t.Error("expected allowedCommands to be initialized")
	}
	if len(h.allowedCommands) == 0 {
		t.Error("expected non-empty allowedCommands")
	}
}

// --- GetCommands ---

func TestGetCommands_ReturnsOK(t *testing.T) {
	h := NewCommandsHandler()

	req := httptest.NewRequest("GET", "/api/xkeen/commands", nil)
	rec := httptest.NewRecorder()
	h.GetCommands(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetCommands_ContentTypeJSON(t *testing.T) {
	h := NewCommandsHandler()

	req := httptest.NewRequest("GET", "/api/xkeen/commands", nil)
	rec := httptest.NewRecorder()
	h.GetCommands(rec, req)

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected application/json, got %q", ct)
	}
}

func TestGetCommands_ReturnsAllDefaultCommands(t *testing.T) {
	h := NewCommandsHandler()

	req := httptest.NewRequest("GET", "/api/xkeen/commands", nil)
	rec := httptest.NewRecorder()
	h.GetCommands(rec, req)

	var resp CommandsListResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	expectedCount := len(defaultCommands)
	if len(resp.Commands) != expectedCount {
		t.Errorf("expected %d commands, got %d", expectedCount, len(resp.Commands))
	}
}

func TestGetCommands_EachCommandHasRequiredFields(t *testing.T) {
	h := NewCommandsHandler()

	req := httptest.NewRequest("GET", "/api/xkeen/commands", nil)
	rec := httptest.NewRecorder()
	h.GetCommands(rec, req)

	var resp CommandsListResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	for _, cmd := range resp.Commands {
		if cmd.Cmd == "" {
			t.Error("command has empty Cmd field")
		}
		if cmd.Description == "" {
			t.Errorf("command %q has empty Description", cmd.Cmd)
		}
	}
}

func TestGetCommands_DangerousFlagsPresent(t *testing.T) {
	h := NewCommandsHandler()

	req := httptest.NewRequest("GET", "/api/xkeen/commands", nil)
	rec := httptest.NewRecorder()
	h.GetCommands(rec, req)

	var resp CommandsListResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	dangerousCount := 0
	safeCount := 0
	for _, cmd := range resp.Commands {
		if cmd.Dangerous {
			dangerousCount++
		} else {
			safeCount++
		}
	}

	if dangerousCount == 0 {
		t.Error("expected at least one dangerous command")
	}
	if safeCount == 0 {
		t.Error("expected at least one safe command")
	}
}

func TestGetCommands_SpecificDangerousCommands(t *testing.T) {
	h := NewCommandsHandler()

	req := httptest.NewRequest("GET", "/api/xkeen/commands", nil)
	rec := httptest.NewRecorder()
	h.GetCommands(rec, req)

	var resp CommandsListResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	dangerousCommands := map[string]bool{
		"-i":      true,
		"-io":     true,
		"-remove": true,
		"-dgs":    true,
		"-dgi":    true,
		"-dx":     true,
		"-dm":     true,
		"-dk":     true,
		"-drk":    true,
		"-drx":    true,
		"-drm":    true,
	}

	for _, cmd := range resp.Commands {
		expected, isDangerous := dangerousCommands[cmd.Cmd]
		if isDangerous && expected && !cmd.Dangerous {
			t.Errorf("command %q should be marked dangerous", cmd.Cmd)
		}
	}
}

func TestGetCommands_SpecificSafeCommands(t *testing.T) {
	h := NewCommandsHandler()

	req := httptest.NewRequest("GET", "/api/xkeen/commands", nil)
	rec := httptest.NewRecorder()
	h.GetCommands(rec, req)

	var resp CommandsListResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	safeCommands := []string{"-uk", "-ug", "-ux", "-status", "-start", "-stop", "-restart", "-v", "-about"}

	cmdMap := make(map[string]CommandInfo)
	for _, cmd := range resp.Commands {
		cmdMap[cmd.Cmd] = cmd
	}

	for _, name := range safeCommands {
		cmd, ok := cmdMap[name]
		if !ok {
			t.Errorf("expected safe command %q in response", name)
			continue
		}
		if cmd.Dangerous {
			t.Errorf("command %q should NOT be marked dangerous", name)
		}
	}
}

func TestGetCommands_NoDuplicateCommands(t *testing.T) {
	h := NewCommandsHandler()

	req := httptest.NewRequest("GET", "/api/xkeen/commands", nil)
	rec := httptest.NewRecorder()
	h.GetCommands(rec, req)

	var resp CommandsListResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	seen := make(map[string]bool)
	for _, cmd := range resp.Commands {
		if seen[cmd.Cmd] {
			t.Errorf("duplicate command: %q", cmd.Cmd)
		}
		seen[cmd.Cmd] = true
	}
}

func TestGetCommands_CommandsMatchDefaultConfig(t *testing.T) {
	h := NewCommandsHandler()

	req := httptest.NewRequest("GET", "/api/xkeen/commands", nil)
	rec := httptest.NewRecorder()
	h.GetCommands(rec, req)

	var resp CommandsListResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	for _, cmd := range resp.Commands {
		config, ok := defaultCommands[cmd.Cmd]
		if !ok {
			t.Errorf("returned command %q not in defaultCommands", cmd.Cmd)
			continue
		}
		if cmd.Description != config.Description {
			t.Errorf("command %q: description mismatch", cmd.Cmd)
		}
		if cmd.Dangerous != config.Dangerous {
			t.Errorf("command %q: dangerous flag mismatch (got %v, want %v)", cmd.Cmd, cmd.Dangerous, config.Dangerous)
		}
	}
}

func TestGetCommands_TimeoutNotExposedInAPI(t *testing.T) {
	h := NewCommandsHandler()

	req := httptest.NewRequest("GET", "/api/xkeen/commands", nil)
	rec := httptest.NewRecorder()
	h.GetCommands(rec, req)

	// Verify timeout is NOT in the JSON response
	var resp struct {
		Commands []map[string]interface{} `json:"commands"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	for _, cmd := range resp.Commands {
		if _, hasTimeout := cmd["timeout"]; hasTimeout {
			t.Error("timeout field should not be exposed in API response")
		}
	}
}

// --- RegisterCommandsRoutes ---

func TestRegisterCommandsRoutes(t *testing.T) {
	h := NewCommandsHandler()
	router := mux.NewRouter()
	RegisterCommandsRoutes(router, h)

	req := httptest.NewRequest("GET", "/xkeen/commands", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestRegisterCommandsRoutes_MethodNotAllowed(t *testing.T) {
	h := NewCommandsHandler()
	router := mux.NewRouter()
	RegisterCommandsRoutes(router, h)

	req := httptest.NewRequest("POST", "/xkeen/commands", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code == http.StatusOK {
		t.Error("POST should not be allowed on /xkeen/commands")
	}
}

// --- defaultCommands integrity checks ---

func TestDefaultCommands_AllStartWithDash(t *testing.T) {
	for cmd, config := range defaultCommands {
		if cmd != config.Cmd {
			t.Errorf("map key %q does not match config.Cmd %q", cmd, config.Cmd)
		}
		if len(cmd) == 0 || cmd[0] != '-' {
			t.Errorf("command %q should start with '-'", cmd)
		}
	}
}

func TestDefaultCommands_AllHaveTimeout(t *testing.T) {
	for cmd, config := range defaultCommands {
		if config.Timeout == 0 {
			t.Errorf("command %q has zero timeout", cmd)
		}
	}
}
