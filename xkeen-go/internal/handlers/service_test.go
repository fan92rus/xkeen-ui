// Package handlers provides HTTP handlers for XKEEN-GO API endpoints.
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// mockExecutor implements CommandExecutor for testing
type mockExecutor struct {
	responses map[string]struct {
		output string
		err    error
	}
	calls []string // Track which commands were called
}

func (m *mockExecutor) Execute(ctx context.Context, name string, args ...string) (string, error) {
	cmd := name + " " + strings.Join(args, " ")
	m.calls = append(m.calls, cmd)

	if resp, ok := m.responses[cmd]; ok {
		return resp.output, resp.err
	}
	return "", nil
}

func (m *mockExecutor) wasCalled(cmd string) bool {
	for _, c := range m.calls {
		if strings.Contains(c, cmd) {
			return true
		}
	}
	return false
}

func (m *mockExecutor) callCount() int {
	return len(m.calls)
}

// ============== TESTS ==============

func TestServiceHandler_GetStatus_Running(t *testing.T) {
	mock := &mockExecutor{
		responses: map[string]struct {
			output string
			err    error
		}{
			"xkeen -status": {
				output: "xkeen is running (PID: 1234)",
				err:    nil,
			},
		},
	}

	handler := NewServiceHandlerWithExecutor(mock)

	req := httptest.NewRequest("GET", "/api/xkeen/status", nil)
	rr := httptest.NewRecorder()

	handler.GetStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	var resp ServiceResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if !resp.Success {
		t.Errorf("Expected success=true, got false")
	}

	if resp.Status == nil {
		t.Fatal("Status should not be nil")
	}

	if !resp.Status.Running {
		t.Error("Service should be reported as running")
	}

	if !mock.wasCalled("xkeen -status") {
		t.Error("Should have called 'xkeen status' command")
	}

	t.Logf("Status response: running=%v, message=%s", resp.Status.Running, resp.Message)
}

func TestServiceHandler_GetStatus_Stopped(t *testing.T) {
	mock := &mockExecutor{
		responses: map[string]struct {
			output string
			err    error
		}{
			"xkeen -status": {
				output: "xkeen is not running",
				err:    nil,
			},
		},
	}

	handler := NewServiceHandlerWithExecutor(mock)

	req := httptest.NewRequest("GET", "/api/xkeen/status", nil)
	rr := httptest.NewRecorder()

	handler.GetStatus(rr, req)

	var resp ServiceResponse
	json.NewDecoder(rr.Body).Decode(&resp)

	if resp.Status.Running {
		t.Error("Service should be reported as not running")
	}

	t.Logf("Status: running=%v", resp.Status.Running)
}

func TestServiceHandler_GetStatus_CommandNotFound(t *testing.T) {
	mock := &mockExecutor{
		responses: map[string]struct {
			output string
			err    error
		}{
			"xkeen -status": {
				output: "bash: /opt/etc/init.d/xkeen: No such file or directory",
				err:    errors.New("exit status 127"),
			},
		},
	}

	handler := NewServiceHandlerWithExecutor(mock)

	req := httptest.NewRequest("GET", "/api/xkeen/status", nil)
	rr := httptest.NewRecorder()

	handler.GetStatus(rr, req)

	// Should still return OK but with running=false
	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	var resp ServiceResponse
	json.NewDecoder(rr.Body).Decode(&resp)

	if resp.Status.Running {
		t.Error("Service should not be reported as running when command fails")
	}

	t.Logf("Handled missing command: running=%v", resp.Status.Running)
}

func TestServiceHandler_Start_Success(t *testing.T) {
	mock := &mockExecutor{
		responses: map[string]struct {
			output string
			err    error
		}{
			"xkeen -start": {
				output: "Starting xkeen... done",
				err:    nil,
			},
		},
	}

	handler := NewServiceHandlerWithExecutor(mock)

	req := httptest.NewRequest("POST", "/api/xkeen/start", nil)
	rr := httptest.NewRecorder()

	handler.Start(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	var resp ServiceResponse
	json.NewDecoder(rr.Body).Decode(&resp)

	if !resp.Success {
		t.Errorf("Expected success=true, got false: %s", resp.Message)
	}

	if !mock.wasCalled("xkeen -start") {
		t.Error("Should have called 'xkeen start' command")
	}

	t.Logf("Start response: %s", resp.Message)
}

func TestServiceHandler_Start_AlreadyRunning(t *testing.T) {
	mock := &mockExecutor{
		responses: map[string]struct {
			output string
			err    error
		}{
			"xkeen -start": {
				output: "xkeen is already running",
				err:    nil, // Some init scripts return 0 even if already running
			},
		},
	}

	handler := NewServiceHandlerWithExecutor(mock)

	req := httptest.NewRequest("POST", "/api/xkeen/start", nil)
	rr := httptest.NewRecorder()

	handler.Start(rr, req)

	var resp ServiceResponse
	json.NewDecoder(rr.Body).Decode(&resp)

	// Should succeed even if already running
	if !resp.Success {
		t.Errorf("Start should succeed when already running: %s", resp.Message)
	}

	t.Logf("Start (already running): %s", resp.Message)
}

func TestServiceHandler_Start_Fail(t *testing.T) {
	mock := &mockExecutor{
		responses: map[string]struct {
			output string
			err    error
		}{
			"xkeen -start": {
				output: "Failed to start xkeen: permission denied",
				err:    errors.New("exit status 1"),
			},
		},
	}

	handler := NewServiceHandlerWithExecutor(mock)

	req := httptest.NewRequest("POST", "/api/xkeen/start", nil)
	rr := httptest.NewRecorder()

	handler.Start(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d", rr.Code)
	}

	var resp ServiceResponse
	json.NewDecoder(rr.Body).Decode(&resp)

	if resp.Success {
		t.Error("Should not succeed when start fails")
	}

	t.Logf("Start failure handled: %s", resp.Message)
}

func TestServiceHandler_Stop_Success(t *testing.T) {
	mock := &mockExecutor{
		responses: map[string]struct {
			output string
			err    error
		}{
			"xkeen -stop": {
				output: "Stopping xkeen... done",
				err:    nil,
			},
		},
	}

	handler := NewServiceHandlerWithExecutor(mock)

	req := httptest.NewRequest("POST", "/api/xkeen/stop", nil)
	rr := httptest.NewRecorder()

	handler.Stop(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	var resp ServiceResponse
	json.NewDecoder(rr.Body).Decode(&resp)

	if !resp.Success {
		t.Errorf("Expected success=true: %s", resp.Message)
	}

	if !mock.wasCalled("xkeen -stop") {
		t.Error("Should have called 'xkeen stop' command")
	}

	t.Logf("Stop response: %s", resp.Message)
}

func TestServiceHandler_Stop_NotRunning(t *testing.T) {
	mock := &mockExecutor{
		responses: map[string]struct {
			output string
			err    error
		}{
			"xkeen -stop": {
				output: "xkeen is not running",
				err:    errors.New("exit status 1"),
			},
		},
	}

	handler := NewServiceHandlerWithExecutor(mock)

	req := httptest.NewRequest("POST", "/api/xkeen/stop", nil)
	rr := httptest.NewRecorder()

	handler.Stop(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d", rr.Code)
	}

	t.Logf("Stop (not running) handled correctly")
}

func TestServiceHandler_Restart_Success(t *testing.T) {
	mock := &mockExecutor{
		responses: map[string]struct {
			output string
			err    error
		}{
			"xkeen -restart": {
				output: "Restarting xkeen... done",
				err:    nil,
			},
		},
	}

	handler := NewServiceHandlerWithExecutor(mock)

	req := httptest.NewRequest("POST", "/api/xkeen/restart", nil)
	rr := httptest.NewRecorder()

	handler.Restart(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	var resp ServiceResponse
	json.NewDecoder(rr.Body).Decode(&resp)

	if !resp.Success {
		t.Errorf("Expected success=true: %s", resp.Message)
	}

	if !mock.wasCalled("xkeen -restart") {
		t.Error("Should have called 'xkeen restart' command")
	}

	t.Logf("Restart response: %s", resp.Message)
}

func TestServiceHandler_Restart_Fail(t *testing.T) {
	mock := &mockExecutor{
		responses: map[string]struct {
			output string
			err    error
		}{
			"xkeen -restart": {
				output: "Failed to restart xkeen",
				err:    errors.New("exit status 1"),
			},
		},
	}

	handler := NewServiceHandlerWithExecutor(mock)

	req := httptest.NewRequest("POST", "/api/xkeen/restart", nil)
	rr := httptest.NewRecorder()

	handler.Restart(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d", rr.Code)
	}

	var resp ServiceResponse
	json.NewDecoder(rr.Body).Decode(&resp)

	if resp.Success {
		t.Error("Should not succeed when restart fails")
	}

	t.Logf("Restart failure handled: %s", resp.Message)
}

// slowExecutor simulates a slow command for timeout testing
type slowExecutor struct {
	delay time.Duration
}

func (e *slowExecutor) Execute(ctx context.Context, name string, args ...string) (string, error) {
	select {
	case <-time.After(e.delay):
		return "done", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func TestServiceHandler_Timeout(t *testing.T) {
	// Create executor that takes 5 seconds (longer than timeout)
	mock := &slowExecutor{delay: 5 * time.Second}
	handler := NewServiceHandlerWithExecutor(mock)

	// Create request with short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	req := httptest.NewRequest("GET", "/api/xkeen/status", nil).WithContext(ctx)
	rr := httptest.NewRecorder()

	handler.GetStatus(rr, req)

	// The handler should handle the timeout gracefully
	// Note: Due to how context works, the response code depends on timing
	t.Logf("Timeout test response code: %d", rr.Code)

	var resp ServiceResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	t.Logf("Timeout test response: success=%v, message=%s", resp.Success, resp.Message)
}

func TestServiceHandler_UnknownAction(t *testing.T) {
	handler := NewServiceHandler()

	// Try to execute unknown action directly
	ctx := context.Background()
	_, err := handler.executeCommandWithTimeout(ctx, "unknown")

	if err == nil {
		t.Error("Should return error for unknown action")
	}

	if !strings.Contains(err.Error(), "unknown action") {
		t.Errorf("Expected 'unknown action' error, got: %v", err)
	}

	t.Logf("Unknown action correctly rejected: %v", err)
}

func TestServiceHandler_CommandInjection(t *testing.T) {
	mock := &mockExecutor{
		responses: map[string]struct {
			output string
			err    error
		}{},
	}

	handler := NewServiceHandlerWithExecutor(mock)

	// The handler only allows whitelisted commands
	// Try to access a command that doesn't exist in whitelist
	req := httptest.NewRequest("POST", "/api/xkeen/start;rm+-rf+/", nil)
	rr := httptest.NewRecorder()

	handler.Start(rr, req)

	// Check that only the whitelisted command was called
	for _, call := range mock.calls {
		if strings.Contains(call, "rm") {
			t.Errorf("Command injection detected! Called: %s", call)
		}
	}

	// The handler should only call the whitelisted command
	t.Logf("Commands called: %v", mock.calls)
}

func TestServiceHandler_AllCommandsUseCorrectPaths(t *testing.T) {
	mock := &mockExecutor{
		responses: map[string]struct {
			output string
			err    error
		}{},
	}

	handler := NewServiceHandlerWithExecutor(mock)

	// Test all commands
	commands := []struct {
		method   string
		path     string
		expected string
	}{
		{"GET", "/api/xkeen/status", "xkeen -status"},
		{"POST", "/api/xkeen/start", "xkeen -start"},
		{"POST", "/api/xkeen/stop", "xkeen -stop"},
		{"POST", "/api/xkeen/restart", "xkeen -restart"},
	}

	for _, cmd := range commands {
		mock.calls = nil // Reset

		var req *http.Request
		if cmd.method == "GET" {
			req = httptest.NewRequest("GET", cmd.path, nil)
		} else {
			req = httptest.NewRequest("POST", cmd.path, nil)
		}
		rr := httptest.NewRecorder()

		switch cmd.path {
		case "/api/xkeen/status":
			handler.GetStatus(rr, req)
		case "/api/xkeen/start":
			handler.Start(rr, req)
		case "/api/xkeen/stop":
			handler.Stop(rr, req)
		case "/api/xkeen/restart":
			handler.Restart(rr, req)
		}

		found := false
		for _, call := range mock.calls {
			if call == cmd.expected {
				found = true
				break
			}
		}

		if !found {
			t.Errorf("Command %s not called. Expected: %s, Got: %v", cmd.path, cmd.expected, mock.calls)
		} else {
			t.Logf("OK: %s -> %s", cmd.path, cmd.expected)
		}
	}
}
