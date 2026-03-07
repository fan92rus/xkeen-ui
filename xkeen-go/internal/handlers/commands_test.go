// Package handlers provides HTTP handlers for XKEEN-GO API endpoints.
package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ============== TESTS ==============

func TestCommandsHandler_ExecuteCommand_Status(t *testing.T) {
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

	handler := NewCommandsHandlerWithExecutor(mock)

	// Create request with command
	reqBody := CommandRequest{Command: "status"}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/api/xkeen/command", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ExecuteCommand(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	var resp CommandResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if !resp.Success {
		t.Errorf("Expected success=true, got false: %s", resp.Message)
	}

	if !mock.wasCalled("xkeen -status") {
		t.Error("Should have called 'xkeen -status' command")
	}

	t.Logf("Status response: success=%v, output=%s", resp.Success, resp.Output)
}

func TestCommandsHandler_ExecuteCommand_UnknownCommand(t *testing.T) {
	mock := &mockExecutor{
		responses: map[string]struct {
			output string
			err    error
		}{},
	}

	handler := NewCommandsHandlerWithExecutor(mock)

	// Try unknown command
	reqBody := CommandRequest{Command: "rm -rf /"}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/api/xkeen/command", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ExecuteCommand(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", rr.Code)
	}

	var resp CommandResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp.Success {
		t.Error("Should not succeed for unknown command")
	}

	if !strings.Contains(strings.ToLower(resp.Message), "unknown command") {
		t.Errorf("Expected 'unknown command' error, got: %s", resp.Message)
	}

	// Ensure no command was executed
	if mock.callCount() > 0 {
		t.Errorf("No command should have been executed, got: %v", mock.calls)
	}

	t.Logf("Unknown command correctly rejected: %s", resp.Message)
}

func TestCommandsHandler_ExecuteCommand_Start(t *testing.T) {
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

	handler := NewCommandsHandlerWithExecutor(mock)

	reqBody := CommandRequest{Command: "start"}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/api/xkeen/command", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ExecuteCommand(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	var resp CommandResponse
	json.NewDecoder(rr.Body).Decode(&resp)

	if !resp.Success {
		t.Errorf("Expected success=true: %s", resp.Message)
	}

	if !mock.wasCalled("xkeen -start") {
		t.Error("Should have called 'xkeen -start' command")
	}

	t.Logf("Start response: %s", resp.Message)
}

func TestCommandsHandler_ExecuteCommand_Backup(t *testing.T) {
	mock := &mockExecutor{
		responses: map[string]struct {
			output string
			err    error
		}{
			"xkeen -kb": {
				output: "Backup created successfully",
				err:    nil,
			},
		},
	}

	handler := NewCommandsHandlerWithExecutor(mock)

	reqBody := CommandRequest{Command: "kb"}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/api/xkeen/command", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ExecuteCommand(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	var resp CommandResponse
	json.NewDecoder(rr.Body).Decode(&resp)

	if !resp.Success {
		t.Errorf("Expected success=true: %s", resp.Message)
	}

	if resp.Dangerous {
		t.Error("kb command should not be marked as dangerous")
	}

	t.Logf("Backup response: %s, dangerous=%v", resp.Message, resp.Dangerous)
}

func TestCommandsHandler_ExecuteCommand_DangerousBackup(t *testing.T) {
	mock := &mockExecutor{
		responses: map[string]struct {
			output string
			err    error
		}{
			"xkeen -kbr": {
				output: "Backup with reset completed",
				err:    nil,
			},
		},
	}

	handler := NewCommandsHandlerWithExecutor(mock)

	reqBody := CommandRequest{Command: "kbr"}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/api/xkeen/command", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ExecuteCommand(rr, req)

	var resp CommandResponse
	json.NewDecoder(rr.Body).Decode(&resp)

	// kbr should be marked as dangerous
	if !resp.Dangerous {
		t.Error("kbr command should be marked as dangerous")
	}

	t.Logf("Dangerous backup response: dangerous=%v", resp.Dangerous)
}

func TestCommandsHandler_ExecuteCommand_Update(t *testing.T) {
	mock := &mockExecutor{
		responses: map[string]struct {
			output string
			err    error
		}{
			"xkeen -uk": {
				output: "XKEEN updated successfully",
				err:    nil,
			},
		},
	}

	handler := NewCommandsHandlerWithExecutor(mock)

	reqBody := CommandRequest{Command: "uk"}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/api/xkeen/command", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ExecuteCommand(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	var resp CommandResponse
	json.NewDecoder(rr.Body).Decode(&resp)

	if !resp.Success {
		t.Errorf("Expected success=true: %s", resp.Message)
	}

	if !mock.wasCalled("xkeen -uk") {
		t.Error("Should have called 'xkeen -uk' command")
	}

	t.Logf("Update response: %s", resp.Message)
}

func TestCommandsHandler_ExecuteCommand_Timeout(t *testing.T) {
	// Create executor that takes longer than the timeout
	mock := &slowExecutor{delay: 5 * time.Second}
	handler := NewCommandsHandlerWithExecutor(mock)

	reqBody := CommandRequest{Command: "status"}
	body, _ := json.Marshal(reqBody)

	// Create request with short context timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	req := httptest.NewRequest("POST", "/api/xkeen/command", bytes.NewReader(body)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ExecuteCommand(rr, req)

	// Should handle timeout gracefully
	t.Logf("Timeout test response code: %d", rr.Code)

	var resp CommandResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	t.Logf("Timeout test response: success=%v, message=%s", resp.Success, resp.Message)
}

func TestCommandsHandler_GetCommands(t *testing.T) {
	handler := NewCommandsHandler()

	req := httptest.NewRequest("GET", "/api/xkeen/commands", nil)
	rr := httptest.NewRecorder()

	handler.GetCommands(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	var resp CommandsListResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Check that we have the expected commands
	expectedCommands := []string{"start", "stop", "restart", "status", "kb", "kbr", "uk", "ug", "ux", "um"}
	foundCommands := make(map[string]bool)

	for _, cmd := range resp.Commands {
		foundCommands[cmd.Cmd] = true
	}

	for _, expected := range expectedCommands {
		if !foundCommands[expected] {
			t.Errorf("Missing expected command: %s", expected)
		}
	}

	// Verify kbr is marked as dangerous
	for _, cmd := range resp.Commands {
		if cmd.Cmd == "kbr" && !cmd.Dangerous {
			t.Error("kbr command should be marked as dangerous")
		}
	}

	t.Logf("Found %d commands", len(resp.Commands))
}

func TestCommandsHandler_CommandInjection(t *testing.T) {
	mock := &mockExecutor{
		responses: map[string]struct {
			output string
			err    error
		}{},
	}

	handler := NewCommandsHandlerWithExecutor(mock)

	// Try various injection attempts
	injectionAttempts := []string{
		"start;rm -rf /",
		"status && cat /etc/passwd",
		"stop|bash",
		"restart`whoami`",
		"status$(id)",
	}

	for _, attempt := range injectionAttempts {
		mock.calls = nil // Reset

		reqBody := CommandRequest{Command: attempt}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest("POST", "/api/xkeen/command", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		handler.ExecuteCommand(rr, req)

		// Should reject the command
		if rr.Code != http.StatusBadRequest {
			t.Errorf("Injection attempt '%s' should be rejected with 400, got %d", attempt, rr.Code)
		}

		// Ensure no command was executed
		if mock.callCount() > 0 {
			t.Errorf("Injection attempt '%s' executed commands: %v", attempt, mock.calls)
		}
	}

	t.Logf("All injection attempts were blocked")
}

func TestCommandsHandler_AllCommandsUseCorrectFormat(t *testing.T) {
	mock := &mockExecutor{
		responses: map[string]struct {
			output string
			err    error
		}{},
	}

	handler := NewCommandsHandlerWithExecutor(mock)

	// Test all commands to ensure they use correct format
	testCases := []struct {
		command  string
		expected string
	}{
		{"start", "xkeen -start"},
		{"stop", "xkeen -stop"},
		{"restart", "xkeen -restart"},
		{"status", "xkeen -status"},
		{"kb", "xkeen -kb"},
		{"kbr", "xkeen -kbr"},
		{"uk", "xkeen -uk"},
		{"ug", "xkeen -ug"},
		{"ux", "xkeen -ux"},
		{"um", "xkeen -um"},
	}

	for _, tc := range testCases {
		mock.calls = nil // Reset

		reqBody := CommandRequest{Command: tc.command}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest("POST", "/api/xkeen/command", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		handler.ExecuteCommand(rr, req)

		found := false
		for _, call := range mock.calls {
			if call == tc.expected {
				found = true
				break
			}
		}

		if !found {
			t.Errorf("Command '%s' not called correctly. Expected: %s, Got: %v", tc.command, tc.expected, mock.calls)
		} else {
			t.Logf("OK: %s -> %s", tc.command, tc.expected)
		}
	}
}

func TestCommandsHandler_EmptyCommand(t *testing.T) {
	handler := NewCommandsHandler()

	reqBody := CommandRequest{Command: ""}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/api/xkeen/command", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ExecuteCommand(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400 for empty command, got %d", rr.Code)
	}
}

func TestCommandsHandler_InvalidJSON(t *testing.T) {
	handler := NewCommandsHandler()

	req := httptest.NewRequest("POST", "/api/xkeen/command", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ExecuteCommand(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400 for invalid JSON, got %d", rr.Code)
	}
}

// ============== STREAMING TESTS ==============

// mockStreamExecutor implements StreamExecutor for testing.
type mockStreamExecutor struct {
	lines    []string
	errLines []string
	err      error
	exitCode int
}

func (m *mockStreamExecutor) ExecuteStream(ctx context.Context, sw StreamWriter, name string, args ...string) error {
	// Send stdout lines
	for _, line := range m.lines {
		sw.WriteMessage(StreamMessage{Type: "output", Text: line})
	}
	// Send stderr lines
	for _, line := range m.errLines {
		sw.WriteMessage(StreamMessage{Type: "error", Text: line})
	}
	// Send complete
	sw.WriteMessage(StreamMessage{Type: "complete", Success: m.err == nil && m.exitCode == 0, ExitCode: m.exitCode})
	return m.err
}

// parseNDJSONResponse parses NDJSON response into messages.
func parseNDJSONResponse(body string) ([]StreamMessage, error) {
	var messages []StreamMessage
	lines := strings.Split(strings.TrimSpace(body), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		var msg StreamMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			return nil, err
		}
		messages = append(messages, msg)
	}
	return messages, nil
}

func TestCommandsHandler_ExecuteCommand_Streaming(t *testing.T) {
	mock := &mockStreamExecutor{
		lines:    []string{"Line 1", "Line 2", "Done"},
		exitCode: 0,
	}

	handler := NewCommandsHandlerWithStreamExecutor(&mockExecutor{}, mock)

	reqBody := CommandRequest{Command: "status"}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/api/xkeen/command", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ExecuteCommand(rr, req)

	// Check content type
	if ct := rr.Header().Get("Content-Type"); ct != "application/x-ndjson" {
		t.Errorf("Expected Content-Type application/x-ndjson, got %s", ct)
	}

	// Parse NDJSON response
	messages, err := parseNDJSONResponse(rr.Body.String())
	if err != nil {
		t.Fatalf("Failed to parse NDJSON: %v", err)
	}

	// Should have 4 messages: 3 output + 1 complete
	if len(messages) != 4 {
		t.Errorf("Expected 4 messages, got %d: %+v", len(messages), messages)
	}

	// Check output messages
	if messages[0].Type != "output" || messages[0].Text != "Line 1" {
		t.Errorf("Expected first message to be output 'Line 1', got %+v", messages[0])
	}

	// Check complete message
	lastMsg := messages[len(messages)-1]
	if lastMsg.Type != "complete" {
		t.Errorf("Expected last message type 'complete', got %s", lastMsg.Type)
	}
	if !lastMsg.Success {
		t.Error("Expected success=true in complete message")
	}

	t.Logf("Streaming response: %d messages", len(messages))
}

func TestCommandsHandler_ExecuteCommand_StreamingWithErrors(t *testing.T) {
	mock := &mockStreamExecutor{
		lines:    []string{"Starting..."},
		errLines: []string{"Warning: something went wrong"},
		exitCode: 1,
	}

	handler := NewCommandsHandlerWithStreamExecutor(&mockExecutor{}, mock)

	reqBody := CommandRequest{Command: "start"}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/api/xkeen/command", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ExecuteCommand(rr, req)

	messages, err := parseNDJSONResponse(rr.Body.String())
	if err != nil {
		t.Fatalf("Failed to parse NDJSON: %v", err)
	}

	// Find error and complete messages
	var hasError, hasComplete bool
	for _, msg := range messages {
		if msg.Type == "error" && msg.Text == "Warning: something went wrong" {
			hasError = true
		}
		if msg.Type == "complete" {
			hasComplete = true
			if msg.Success {
				t.Error("Expected success=false for non-zero exit code")
			}
			if msg.ExitCode != 1 {
				t.Errorf("Expected exitCode=1, got %d", msg.ExitCode)
			}
		}
	}

	if !hasError {
		t.Error("Expected error message in stream")
	}
	if !hasComplete {
		t.Error("Expected complete message in stream")
	}

	t.Logf("Error streaming response OK")
}
