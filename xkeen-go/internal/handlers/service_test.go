package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mockCommandExecutor implements CommandExecutor for testing.
type mockCommandExecutor struct {
	results map[string]mockResult
	calls   []mockCall
}

type mockResult struct {
	output string
	err    error
}

type mockCall struct {
	name string
	args []string
}

func newMockCmdExecutor() *mockCommandExecutor {
	return &mockCommandExecutor{
		results: make(map[string]mockResult),
	}
}

func (m *mockCommandExecutor) Execute(ctx context.Context, name string, args ...string) (string, error) {
	key := name + " " + strings.Join(args, " ")
	m.calls = append(m.calls, mockCall{name: name, args: args})
	if r, ok := m.results[key]; ok {
		return r.output, r.err
	}
	// Default: try by command name
	if r, ok := m.results[name]; ok {
		return r.output, r.err
	}
	return "", errors.New("command not configured")
}

func (m *mockCommandExecutor) setResult(cmd string, output string, err error) {
	m.results[cmd] = mockResult{output: output, err: err}
}

// --- GetStatus Tests ---

func TestGetStatus_Running(t *testing.T) {
	exec := newMockCmdExecutor()
	exec.setResult("xkeen -status", "Xray is running (PID: 12345)", nil)
	handler := NewServiceHandlerWithExecutor(exec)

	req := httptest.NewRequest("GET", "/api/xkeen/status", nil)
	rec := httptest.NewRecorder()
	handler.GetStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp ServiceResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if !resp.Success {
		t.Error("Expected success=true")
	}
	if resp.Status == nil {
		t.Fatal("Expected status in response")
	}
	if !resp.Status.Running {
		t.Error("Expected running=true")
	}
}

func TestGetStatus_NotRunning(t *testing.T) {
	exec := newMockCmdExecutor()
	exec.setResult("xkeen -status", "Xray is not running", nil)
	handler := NewServiceHandlerWithExecutor(exec)

	req := httptest.NewRequest("GET", "/api/xkeen/status", nil)
	rec := httptest.NewRecorder()
	handler.GetStatus(rec, req)

	var resp ServiceResponse
	json.NewDecoder(rec.Body).Decode(&resp)

	if resp.Status == nil {
		t.Fatal("Expected status in response")
	}
	if resp.Status.Running {
		t.Error("Expected running=false")
	}
}

func TestGetStatus_RussianOutput(t *testing.T) {
	exec := newMockCmdExecutor()
	exec.setResult("xkeen -status", "xray запущен (PID: 99999)", nil)
	handler := NewServiceHandlerWithExecutor(exec)

	req := httptest.NewRequest("GET", "/api/xkeen/status", nil)
	rec := httptest.NewRecorder()
	handler.GetStatus(rec, req)

	var resp ServiceResponse
	json.NewDecoder(rec.Body).Decode(&resp)

	if !resp.Status.Running {
		t.Error("Expected running=true for Russian output")
	}
}

func TestGetStatus_RussianNotRunning(t *testing.T) {
	exec := newMockCmdExecutor()
	exec.setResult("xkeen -status", "xray не запущен", nil)
	handler := NewServiceHandlerWithExecutor(exec)

	req := httptest.NewRequest("GET", "/api/xkeen/status", nil)
	rec := httptest.NewRecorder()
	handler.GetStatus(rec, req)

	var resp ServiceResponse
	json.NewDecoder(rec.Body).Decode(&resp)

	if resp.Status.Running {
		t.Error("Expected running=false for Russian 'not running' output")
	}
}

func TestGetStatus_CommandError(t *testing.T) {
	exec := newMockCmdExecutor()
	exec.setResult("xkeen -status", "", errors.New("command failed"))
	handler := NewServiceHandlerWithExecutor(exec)

	req := httptest.NewRequest("GET", "/api/xkeen/status", nil)
	rec := httptest.NewRecorder()
	handler.GetStatus(rec, req)

	var resp ServiceResponse
	json.NewDecoder(rec.Body).Decode(&resp)

	if resp.Status == nil {
		t.Fatal("Expected status even on command error")
	}
	if resp.Status.Running {
		t.Error("Should not report running on command error")
	}
}

func TestGetStatus_Timeout(t *testing.T) {
	// GetStatus creates its own context.WithTimeout from r.Context().
	// It checks ctx.Err() after the command executes.
	// Since the mock executor returns immediately and never actually
	// causes a context deadline, the handler won't detect a timeout.
	// Instead, test that command errors are handled gracefully.
	exec := newMockCmdExecutor()
	exec.setResult("xkeen -status", "some error output", fmt.Errorf("command failed: timeout"))
	handler := NewServiceHandlerWithExecutor(exec)

	req := httptest.NewRequest("GET", "/api/xkeen/status", nil)
	rec := httptest.NewRecorder()
	handler.GetStatus(rec, req)

	// Should return 200 with status (not running due to error)
	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rec.Code)
	}

	var resp ServiceResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Status == nil {
		t.Fatal("Expected status in response")
	}
	if resp.Status.Running {
		t.Error("Should not be running on error")
	}
}

// --- Start/Stop/Restart Tests ---

func TestStart_ReturnsImmediately(t *testing.T) {
	exec := newMockCmdExecutor()
	exec.setResult("xkeen -start", "Xray started successfully", nil)
	handler := NewServiceHandlerWithExecutor(exec)

	req := httptest.NewRequest("POST", "/api/xkeen/start", nil)
	rec := httptest.NewRecorder()
	handler.Start(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rec.Code)
	}

	var resp ServiceResponse
	json.NewDecoder(rec.Body).Decode(&resp)

	if !resp.Success {
		t.Error("Expected success=true")
	}
	if !strings.Contains(resp.Message, "initiated") {
		t.Errorf("Expected 'initiated' in message, got %q", resp.Message)
	}
}

func TestStop_ReturnsImmediately(t *testing.T) {
	exec := newMockCmdExecutor()
	exec.setResult("xkeen -stop", "Xray stopped successfully", nil)
	handler := NewServiceHandlerWithExecutor(exec)

	req := httptest.NewRequest("POST", "/api/xkeen/stop", nil)
	rec := httptest.NewRecorder()
	handler.Stop(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rec.Code)
	}

	var resp ServiceResponse
	json.NewDecoder(rec.Body).Decode(&resp)

	if !resp.Success {
		t.Error("Expected success=true")
	}
}

func TestRestart_ReturnsImmediately(t *testing.T) {
	exec := newMockCmdExecutor()
	exec.setResult("xkeen -restart", "Xray restarted successfully", nil)
	handler := NewServiceHandlerWithExecutor(exec)

	req := httptest.NewRequest("POST", "/api/xkeen/restart", nil)
	rec := httptest.NewRecorder()
	handler.Restart(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rec.Code)
	}

	var resp ServiceResponse
	json.NewDecoder(rec.Body).Decode(&resp)

	if !resp.Success {
		t.Error("Expected success=true")
	}
}

// --- executeCommandWithTimeout Tests ---

func TestExecuteCommandWithTimeout_UnknownAction(t *testing.T) {
	exec := newMockCmdExecutor()
	handler := NewServiceHandlerWithExecutor(exec)

	ctx := context.Background()
	_, err := handler.executeCommandWithTimeout(ctx, "unknown")

	if err == nil {
		t.Error("Expected error for unknown action")
	}
	if !strings.Contains(err.Error(), "unknown action") {
		t.Errorf("Expected 'unknown action' error, got %v", err)
	}
}

func TestExecuteCommandWithTimeout_ValidAction(t *testing.T) {
	exec := newMockCmdExecutor()
	exec.setResult("xkeen -status", "OK", nil)
	handler := NewServiceHandlerWithExecutor(exec)

	ctx := context.Background()
	output, err := handler.executeCommandWithTimeout(ctx, "status")

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if output != "OK" {
		t.Errorf("Expected 'OK', got %q", output)
	}

	// Verify correct command was called
	if len(exec.calls) != 1 {
		t.Fatalf("Expected 1 call, got %d", len(exec.calls))
	}
	if exec.calls[0].name != "xkeen" {
		t.Errorf("Expected command 'xkeen', got %q", exec.calls[0].name)
	}
	if len(exec.calls[0].args) != 1 || exec.calls[0].args[0] != "-status" {
		t.Errorf("Expected args ['-status'], got %v", exec.calls[0].args)
	}
}

// --- TriggerStatusCheck Tests ---

func TestTriggerStatusCheck_NonBlocking(t *testing.T) {
	handler := NewServiceHandler()

	// Should not block even if nobody is listening
	handler.TriggerStatusCheck()
	handler.TriggerStatusCheck()
	handler.TriggerStatusCheck() // channel is size 1, so extras are dropped
}

// --- Allowed commands whitelist ---

func TestAllowedCommands_ContainsAllActions(t *testing.T) {
	handler := NewServiceHandler()

	expected := []string{"start", "stop", "restart", "status"}
	for _, action := range expected {
		if _, ok := handler.allowedCommands[action]; !ok {
			t.Errorf("Expected action %q in allowedCommands", action)
		}
	}
}

func TestAllowedCommands_NoShellInjection(t *testing.T) {
	handler := NewServiceHandler()

	// Verify commands don't use shell metacharacters
	for action, cmd := range handler.allowedCommands {
		if strings.Contains(cmd, ";") || strings.Contains(cmd, "|") || strings.Contains(cmd, "&") {
			t.Errorf("Action %q command %q contains shell metacharacters", action, cmd)
		}
	}
}

// --- Active (running) pattern detection ---

func TestGetStatus_ActiveRunningPattern(t *testing.T) {
	exec := newMockCmdExecutor()
	exec.setResult("xkeen -status", "active (running)", nil)
	handler := NewServiceHandlerWithExecutor(exec)

	req := httptest.NewRequest("GET", "/api/xkeen/status", nil)
	rec := httptest.NewRecorder()
	handler.GetStatus(rec, req)

	var resp ServiceResponse
	json.NewDecoder(rec.Body).Decode(&resp)

	if !resp.Status.Running {
		t.Error("Expected running=true for 'active (running)' output")
	}
}

func TestGetStatus_RunningWithPIDPattern(t *testing.T) {
	exec := newMockCmdExecutor()
	exec.setResult("xkeen -status", "running (PID: 42)", nil)
	handler := NewServiceHandlerWithExecutor(exec)

	req := httptest.NewRequest("GET", "/api/xkeen/status", nil)
	rec := httptest.NewRecorder()
	handler.GetStatus(rec, req)

	var resp ServiceResponse
	json.NewDecoder(rec.Body).Decode(&resp)

	if !resp.Status.Running {
		t.Error("Expected running=true for 'running (PID: ...)' output")
	}
}

// --- Content-Type check ---

func TestGetStatus_ContentTypeJSON(t *testing.T) {
	exec := newMockCmdExecutor()
	exec.setResult("xkeen -status", "Xray is running", nil)
	handler := NewServiceHandlerWithExecutor(exec)

	req := httptest.NewRequest("GET", "/api/xkeen/status", nil)
	rec := httptest.NewRecorder()
	handler.GetStatus(rec, req)

	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Expected application/json content type, got %q", ct)
	}
}
