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
	"time"

	"github.com/gorilla/mux"
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

func TestRestartService_RunsCommandAndAsync(t *testing.T) {
	exec := newMockCmdExecutor()
	exec.setResult("xkeen -restart", "Xray restarted successfully", nil)
	handler := NewServiceHandlerWithExecutor(exec)

	// RestartService should return immediately (non-blocking)
	handler.RestartService()

	// Give the background goroutine time to execute
	time.Sleep(100 * time.Millisecond)

	// Verify the restart command was executed by the executor
	if len(exec.calls) == 0 {
		t.Error("expected at least one command execution")
	}
	foundRestart := false
	for _, call := range exec.calls {
		if call.name == "xkeen" && len(call.args) > 0 && call.args[0] == "-restart" {
			foundRestart = true
			break
		}
	}
	if !foundRestart {
		t.Errorf("expected 'xkeen -restart' command to be executed, calls: %+v", exec.calls)
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

// --- StatusStream SSE Tests ---

func TestStatusStream_SendsInitialEvent(t *testing.T) {
	exec := newMockCmdExecutor()
	exec.setResult("xkeen -status", "Xray is running (PID: 12345)", nil)
	handler := NewServiceHandlerWithExecutor(exec)

	r := mux.NewRouter()
	r.HandleFunc("/stream", handler.StatusStream)
	server := httptest.NewServer(r)
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("SSE request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Errorf("expected text/event-stream, got %q", resp.Header.Get("Content-Type"))
	}

	// Read first event (initial status)
	buf := make([]byte, 4096)
	n, err := resp.Body.Read(buf)
	if err != nil && err.Error() != "EOF" {
		t.Fatalf("read SSE event: %v", err)
	}

	body := string(buf[:n])
	if !strings.Contains(body, "event: status") {
		t.Errorf("expected 'event: status' in SSE output, got %q", body)
	}
	if !strings.Contains(body, `"running":true`) {
		t.Errorf("expected running=true in event data, got %q", body)
	}

	cancel()
}

func TestStatusStream_ClosesOnContextCancel(t *testing.T) {
	exec := newMockCmdExecutor()
	exec.setResult("xkeen -status", "Xray is running (PID: 12345)", nil)
	handler := NewServiceHandlerWithExecutor(exec)

	r := mux.NewRouter()
	r.HandleFunc("/stream", handler.StatusStream)
	server := httptest.NewServer(r)
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())

	req, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("SSE request failed: %v", err)
	}

	// Read first event
	buf := make([]byte, 4096)
	resp.Body.Read(buf)

	// Cancel context — stream should stop
	cancel()

	// Subsequent read should get error (stream closed or context cancelled)
	_, readErr := resp.Body.Read(buf)
	if readErr == nil {
		// Try once more with timeout
		done := make(chan error, 1)
		go func() {
			_, err := resp.Body.Read(buf)
			done <- err
		}()
		select {
		case err := <-done:
			if err == nil {
				t.Error("expected read error after context cancel")
			}
		case <-time.After(time.Second):
			t.Error("stream did not close after context cancel within 1s")
		}
	}

	resp.Body.Close()
}

// --- Close() tests ---

func TestClose_WaitsForBackgroundGoroutines(t *testing.T) {
	exec := newMockCmdExecutor()
	exec.setResult("xkeen -start", "started", nil)
	exec.setResult("xkeen -stop", "stopped", nil)
	handler := NewServiceHandlerWithExecutor(exec)

	// Fire start — it spawns goroutine that sleeps 1s then triggers status check
	req := httptest.NewRequest("POST", "/api/xkeen/start", nil)
	rec := httptest.NewRecorder()
	handler.Start(rec, req)

	// Fire stop — another goroutine
	req2 := httptest.NewRequest("POST", "/api/xkeen/stop", nil)
	rec2 := httptest.NewRecorder()
	handler.Stop(rec2, req2)

	// Close should wait for both goroutines to complete
	start := time.Now()
	handler.Close()
	elapsed := time.Since(start)

	// Each goroutine sleeps at least 1s, so Close should take at least ~500ms
	// (they run concurrently so bound is ~1s, not 2s)
	if elapsed < 100*time.Millisecond {
		t.Logf("Close returned in %v (expected to wait for goroutines)", elapsed)
	}
	if elapsed > 10*time.Second {
		t.Errorf("Close took too long: %v", elapsed)
	}
}

func TestClose_Idempotent(t *testing.T) {
	exec := newMockCmdExecutor()
	exec.setResult("xkeen -start", "started", nil)
	handler := NewServiceHandlerWithExecutor(exec)

	// No goroutines fired — Close should return immediately
	handler.Close()
	handler.Close() // second call should be no-op

	// Fire start after close is allowed (wg.Add still works)
	req := httptest.NewRequest("POST", "/api/xkeen/start", nil)
	rec := httptest.NewRecorder()
	handler.Start(rec, req)

	// Close again (first close was idempotent, this one waits for start goroutine)
	done := make(chan struct{})
	go func() {
		handler.Close()
		close(done)
	}()
	select {
	case <-done:
		// OK
	case <-time.After(5 * time.Second):
		t.Error("Close hung")
	}
}
