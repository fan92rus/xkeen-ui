package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gorilla/mux"
)

// testCommandsRegistry builds a registry backed by the real `xkeen -help`
// fixture, mirroring the testHelpRegistry helper in interactive_test.go.
func testCommandsRegistry(t *testing.T) *CommandRegistry {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "xkeen-help.txt"))
	if err != nil {
		t.Fatalf("missing testdata/xkeen-help.txt: %v", err)
	}
	cmds := parseHelp(string(data))
	return newCommandRegistryWithLoader(func() (map[string]CommandConfig, error) {
		return cmds, nil
	})
}

// emptyRegistry builds a registry whose loader returns an empty set, modelling
// the "xkeen not installed / -help failed" case.
func emptyRegistry() *CommandRegistry {
	return newCommandRegistryWithLoader(func() (map[string]CommandConfig, error) {
		return map[string]CommandConfig{}, nil
	})
}

func newTestCommandsHandler(t *testing.T) *CommandsHandler {
	return NewCommandsHandler(testCommandsRegistry(t))
}

// --- CommandsHandler construction ---

func TestNewCommandsHandler(t *testing.T) {
	h := newTestCommandsHandler(t)
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
	if h.registry == nil {
		t.Fatal("expected registry to be set")
	}
	if h.registry.Count() == 0 {
		t.Error("expected non-empty registry (backed by help fixture)")
	}
}

// --- GetCommands ---

func TestGetCommands_ReturnsOK(t *testing.T) {
	h := newTestCommandsHandler(t)

	req := httptest.NewRequest("GET", "/api/xkeen/commands", nil)
	rec := httptest.NewRecorder()
	h.GetCommands(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetCommands_ContentTypeJSON(t *testing.T) {
	h := newTestCommandsHandler(t)

	req := httptest.NewRequest("GET", "/api/xkeen/commands", nil)
	rec := httptest.NewRecorder()
	h.GetCommands(rec, req)

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected application/json, got %q", ct)
	}
}

func TestGetCommands_ReturnsAllParsedCommands(t *testing.T) {
	h := newTestCommandsHandler(t)
	expected := h.registry.Count()

	req := httptest.NewRequest("GET", "/api/xkeen/commands", nil)
	rec := httptest.NewRecorder()
	h.GetCommands(rec, req)

	var resp CommandsListResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Commands) != expected {
		t.Errorf("expected %d commands, got %d", expected, len(resp.Commands))
	}
}

func TestGetCommands_EachCommandHasRequiredFields(t *testing.T) {
	h := newTestCommandsHandler(t)

	req := httptest.NewRequest("GET", "/api/xkeen/commands", nil)
	rec := httptest.NewRecorder()
	h.GetCommands(rec, req)

	var resp CommandsListResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Commands) == 0 {
		t.Fatal("expected commands in response")
	}
	for _, cmd := range resp.Commands {
		if cmd.Cmd == "" {
			t.Error("command has empty Cmd field")
		}
		if cmd.Description == "" {
			t.Errorf("command %q has empty Description", cmd.Cmd)
		}
		if cmd.Category == "" {
			t.Errorf("command %q has empty Category", cmd.Cmd)
		}
	}
}

func TestGetCommands_DangerousFlagsPresent(t *testing.T) {
	h := newTestCommandsHandler(t)

	req := httptest.NewRequest("GET", "/api/xkeen/commands", nil)
	rec := httptest.NewRecorder()
	h.GetCommands(rec, req)

	var resp CommandsListResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	dangerousCount, safeCount := 0, 0
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
	h := newTestCommandsHandler(t)

	req := httptest.NewRequest("GET", "/api/xkeen/commands", nil)
	rec := httptest.NewRecorder()
	h.GetCommands(rec, req)

	var resp CommandsListResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	cmdMap := make(map[string]CommandInfo, len(resp.Commands))
	for _, c := range resp.Commands {
		cmdMap[c.Cmd] = c
	}
	// These are the REAL xkeen dangerous commands (Installation + Removal).
	for _, flag := range []string{"-i", "-io", "-remove", "-dgs", "-dgi", "-dx", "-dm", "-dk"} {
		c, ok := cmdMap[flag]
		if !ok {
			t.Errorf("expected dangerous command %q in response", flag)
			continue
		}
		if !c.Dangerous {
			t.Errorf("command %q should be marked dangerous", flag)
		}
	}
}

func TestGetCommands_SpecificSafeCommands(t *testing.T) {
	h := newTestCommandsHandler(t)

	req := httptest.NewRequest("GET", "/api/xkeen/commands", nil)
	rec := httptest.NewRecorder()
	h.GetCommands(rec, req)

	var resp CommandsListResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	cmdMap := make(map[string]CommandInfo, len(resp.Commands))
	for _, c := range resp.Commands {
		cmdMap[c.Cmd] = c
	}
	for _, flag := range []string{"-uk", "-ug", "-ux", "-status", "-start", "-stop", "-restart", "-v", "-about", "-tp"} {
		c, ok := cmdMap[flag]
		if !ok {
			t.Errorf("expected safe command %q in response", flag)
			continue
		}
		if c.Dangerous {
			t.Errorf("command %q should NOT be marked dangerous", flag)
		}
	}
}

func TestGetCommands_NoDuplicateCommands(t *testing.T) {
	h := newTestCommandsHandler(t)

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

func TestGetCommands_TimeoutNotExposedInAPI(t *testing.T) {
	h := newTestCommandsHandler(t)

	req := httptest.NewRequest("GET", "/api/xkeen/commands", nil)
	rec := httptest.NewRecorder()
	h.GetCommands(rec, req)

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

// When xkeen is unavailable, GetCommands returns an empty list (no fallback).
func TestGetCommands_EmptyWhenXkeenUnavailable(t *testing.T) {
	h := NewCommandsHandler(emptyRegistry())

	req := httptest.NewRequest("GET", "/api/xkeen/commands", nil)
	rec := httptest.NewRecorder()
	h.GetCommands(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 even when empty, got %d", rec.Code)
	}
	var resp CommandsListResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if len(resp.Commands) != 0 {
		t.Errorf("expected 0 commands when xkeen unavailable, got %d", len(resp.Commands))
	}
}

// --- RegisterCommandsRoutes ---

func TestRegisterCommandsRoutes(t *testing.T) {
	h := newTestCommandsHandler(t)
	router := mux.NewRouter()
	RegisterCommandsRoutes(router, h)

	req := httptest.NewRequest("GET", "/xkeen/commands", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

// ---------- RefreshCommands ----------

func TestRefreshCommands_ReturnsOK(t *testing.T) {
	h := newTestCommandsHandler(t)

	req := httptest.NewRequest("POST", "/api/xkeen/commands/refresh", nil)
	rec := httptest.NewRecorder()
	h.RefreshCommands(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRefreshCommands_ReturnsSameCommandsAsGet(t *testing.T) {
	h := newTestCommandsHandler(t)
	expected := h.registry.Count()

	// Refresh
	req := httptest.NewRequest("POST", "/api/xkeen/commands/refresh", nil)
	rec := httptest.NewRecorder()
	h.RefreshCommands(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp CommandsListResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(resp.Commands) != expected {
		t.Errorf("expected %d commands, got %d", expected, len(resp.Commands))
	}

	// Verify each command has required fields (same contract as GetCommands)
	for _, cmd := range resp.Commands {
		if cmd.Cmd == "" {
			t.Error("command has empty Cmd field")
		}
		if cmd.Description == "" {
			t.Errorf("command %q has empty Description", cmd.Cmd)
		}
		if cmd.Category == "" {
			t.Errorf("command %q has empty Category", cmd.Cmd)
		}
	}
}

func TestRefreshCommands_EmptyRegistry(t *testing.T) {
	h := NewCommandsHandler(emptyRegistry())

	req := httptest.NewRequest("POST", "/api/xkeen/commands/refresh", nil)
	rec := httptest.NewRecorder()
	h.RefreshCommands(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp CommandsListResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Commands == nil {
		t.Fatal("commands is null — frontend will crash on .length")
	}
	if len(resp.Commands) != 0 {
		t.Errorf("expected 0 commands, got %d", len(resp.Commands))
	}
}

func TestRefreshCommands_RouteRegistered(t *testing.T) {
	h := newTestCommandsHandler(t)
	router := mux.NewRouter()
	RegisterCommandsRoutes(router, h)

	req := httptest.NewRequest("POST", "/xkeen/commands/refresh", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRegisterCommandsRoutes_MethodNotAllowed(t *testing.T) {
	h := newTestCommandsHandler(t)
	router := mux.NewRouter()
	RegisterCommandsRoutes(router, h)

	req := httptest.NewRequest("POST", "/xkeen/commands", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code == http.StatusOK {
		t.Error("POST should not be allowed on /xkeen/commands")
	}
}
