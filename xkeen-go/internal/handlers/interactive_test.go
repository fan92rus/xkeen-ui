package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

// === NewInteractiveHandler ===

func TestNewInteractiveHandler(t *testing.T) {
	handler := NewInteractiveHandler(nil)
	if handler == nil {
		t.Fatal("Expected non-nil handler")
	}
	if handler.allowedCommands == nil {
		t.Error("Expected allowedCommands to be initialized")
	}
	if len(handler.allowedCommands) == 0 {
		t.Error("Expected non-empty allowedCommands")
	}
	if handler.upgrader.ReadBufferSize != 1024 {
		t.Errorf("Expected ReadBufferSize 1024, got %d", handler.upgrader.ReadBufferSize)
	}
	if handler.upgrader.WriteBufferSize != 1024 {
		t.Errorf("Expected WriteBufferSize 1024, got %d", handler.upgrader.WriteBufferSize)
	}
}

func TestNewInteractiveHandler_WithConfig(t *testing.T) {
	cfg := &InteractiveConfig{
		AllowedOrigins: []string{"http://trusted.example.com", "http://other.example.com"},
	}
	handler := NewInteractiveHandler(cfg)
	if handler == nil {
		t.Fatal("Expected non-nil handler")
	}
	if len(handler.allowedOrigins) != 2 {
		t.Errorf("Expected 2 allowed origins, got %d", len(handler.allowedOrigins))
	}
	if !handler.allowedOrigins["http://trusted.example.com"] {
		t.Error("Expected 'http://trusted.example.com' to be allowed")
	}
	if !handler.allowedOrigins["http://other.example.com"] {
		t.Error("Expected 'http://other.example.com' to be allowed")
	}
}

func TestNewInteractiveHandler_NilConfig(t *testing.T) {
	handler := NewInteractiveHandler(nil)
	if len(handler.allowedOrigins) != 0 {
		t.Errorf("Expected 0 allowed origins with nil config, got %d", len(handler.allowedOrigins))
	}
}

func TestNewInteractiveHandler_EmptyAllowedOrigins(t *testing.T) {
	handler := NewInteractiveHandler(&InteractiveConfig{AllowedOrigins: []string{}})
	if len(handler.allowedOrigins) != 0 {
		t.Errorf("Expected 0 allowed origins with empty config, got %d", len(handler.allowedOrigins))
	}
}

// === Message types ===

func TestInteractiveMessageTypes(t *testing.T) {
	// Test ClientMessage parsing
	clientJSON := `{"type":"start","command":"uk"}`
	var clientMsg ClientMessage
	if err := json.Unmarshal([]byte(clientJSON), &clientMsg); err != nil {
		t.Fatalf("Failed to parse ClientMessage: %v", err)
	}
	if clientMsg.Type != "start" || clientMsg.Command != "uk" {
		t.Errorf("Unexpected ClientMessage values: %+v", clientMsg)
	}

	// Test input message
	inputJSON := `{"type":"input","text":"2.3.1\n"}`
	var inputMsg ClientMessage
	if err := json.Unmarshal([]byte(inputJSON), &inputMsg); err != nil {
		t.Fatalf("Failed to parse input message: %v", err)
	}
	if inputMsg.Type != "input" || inputMsg.Text != "2.3.1\n" {
		t.Errorf("Unexpected input message values: %+v", inputMsg)
	}

	// Test signal message
	signalJSON := `{"type":"signal","signal":"SIGTERM"}`
	var signalMsg ClientMessage
	if err := json.Unmarshal([]byte(signalJSON), &signalMsg); err != nil {
		t.Fatalf("Failed to parse signal message: %v", err)
	}
	if signalMsg.Type != "signal" || signalMsg.Signal != "SIGTERM" {
		t.Errorf("Unexpected signal message values: %+v", signalMsg)
	}

	// Test ServerMessage
	serverMsg := ServerMessage{Type: "output", Text: "hello"}
	data, err := json.Marshal(serverMsg)
	if err != nil {
		t.Fatalf("Failed to marshal ServerMessage: %v", err)
	}
	if string(data) != `{"type":"output","text":"hello"}` {
		t.Errorf("Unexpected ServerMessage JSON: %s", data)
	}
}

func TestServerMessage_Complete(t *testing.T) {
	msg := ServerMessage{Type: "complete", Success: true, ExitCode: 0}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}
	var decoded ServerMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}
	if decoded.Type != "complete" || decoded.Success != true || decoded.ExitCode != 0 {
		t.Errorf("Unexpected values: %+v", decoded)
	}
}

func TestServerMessage_Error(t *testing.T) {
	msg := ServerMessage{Type: "error", Text: "something went wrong"}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}
	var decoded ServerMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}
	if decoded.Type != "error" || decoded.Text != "something went wrong" {
		t.Errorf("Unexpected values: %+v", decoded)
	}
}

func TestClientMessage_AllFields(t *testing.T) {
	raw := `{"type":"start","command":"-k","text":"input","signal":"SIGTERM"}`
	var msg ClientMessage
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}
	if msg.Type != "start" || msg.Command != "-k" || msg.Text != "input" || msg.Signal != "SIGTERM" {
		t.Errorf("Unexpected values: %+v", msg)
	}
}

// === checkOrigin tests for InteractiveHandler ===

func TestInteractiveCheckOrigin_EmptyOrigin(t *testing.T) {
	h := NewInteractiveHandler(nil)

	req := httptest.NewRequest("GET", "/ws/xkeen/interactive", nil)
	req.Header.Del("Origin")

	if h.checkOrigin(req) {
		t.Error("empty origin should be rejected")
	}
}

func TestInteractiveCheckOrigin_SameOrigin(t *testing.T) {
	h := NewInteractiveHandler(nil)

	req := httptest.NewRequest("GET", "/ws/xkeen/interactive", nil)
	req.Header.Set("Origin", "http://localhost:8089")
	req.Host = "localhost:8089"

	if !h.checkOrigin(req) {
		t.Error("same-origin should be allowed")
	}
}

func TestInteractiveCheckOrigin_SameOriginHTTPS(t *testing.T) {
	h := NewInteractiveHandler(nil)

	req := httptest.NewRequest("GET", "/ws/xkeen/interactive", nil)
	req.Header.Set("Origin", "https://router.lan:8089")
	req.Host = "router.lan:8089"

	if !h.checkOrigin(req) {
		t.Error("same-origin HTTPS should be allowed")
	}
}

func TestInteractiveCheckOrigin_RejectedOrigin(t *testing.T) {
	h := NewInteractiveHandler(nil)

	req := httptest.NewRequest("GET", "/ws/xkeen/interactive", nil)
	req.Header.Set("Origin", "http://evil.example.com")
	req.Host = "localhost:8089"

	if h.checkOrigin(req) {
		t.Error("cross-origin should be rejected")
	}
}

func TestInteractiveCheckOrigin_AllowedOrigin(t *testing.T) {
	h := NewInteractiveHandler(&InteractiveConfig{
		AllowedOrigins: []string{"http://trusted.example.com"},
	})

	req := httptest.NewRequest("GET", "/ws/xkeen/interactive", nil)
	req.Header.Set("Origin", "http://trusted.example.com")
	req.Host = "other-host"

	if !h.checkOrigin(req) {
		t.Error("explicitly allowed origin should pass")
	}
}

func TestInteractiveCheckOrigin_MalformedOrigin(t *testing.T) {
	h := NewInteractiveHandler(nil)

	req := httptest.NewRequest("GET", "/ws/xkeen/interactive", nil)
	req.Header.Set("Origin", "://::bad")
	req.Host = "localhost:8089"

	if h.checkOrigin(req) {
		t.Error("malformed origin should be rejected")
	}
}

// === isCommandAllowed ===

func TestIsCommandAllowed_AllowedCommand(t *testing.T) {
	h := NewInteractiveHandler(nil)

	// Test a known default command
	config, ok := h.isCommandAllowed("-k")
	if !ok {
		t.Error("Expected '-k' command to be allowed")
	}
	if config.Cmd != "-k" {
		t.Errorf("Expected Cmd '-k', got %q", config.Cmd)
	}
}

func TestIsCommandAllowed_MultipleAllowedCommands(t *testing.T) {
	h := NewInteractiveHandler(nil)

	allowedCmds := []string{"-k", "-g", "-i", "-start", "-stop", "-restart", "-status"}
	for _, cmd := range allowedCmds {
		_, ok := h.isCommandAllowed(cmd)
		if !ok {
			t.Errorf("Expected %q to be allowed", cmd)
		}
	}
}

func TestIsCommandAllowed_DisallowedCommand(t *testing.T) {
	h := NewInteractiveHandler(nil)

	_, ok := h.isCommandAllowed("rm -rf /")
	if ok {
		t.Error("Expected 'rm -rf /' to be disallowed")
	}
}

func TestIsCommandAllowed_EmptyCommand(t *testing.T) {
	h := NewInteractiveHandler(nil)

	_, ok := h.isCommandAllowed("")
	if ok {
		t.Error("Expected empty command to be disallowed")
	}
}

func TestIsCommandAllowed_CaseSensitive(t *testing.T) {
	h := NewInteractiveHandler(nil)

	// Commands are case-sensitive: "-K" should not match "-k"
	_, ok := h.isCommandAllowed("-K")
	if ok {
		t.Error("Expected '-K' (uppercase) to be disallowed (commands are case-sensitive)")
	}
}

func TestIsCommandAllowed_UnknownCommand(t *testing.T) {
	h := NewInteractiveHandler(nil)

	_, ok := h.isCommandAllowed("unknown-cmd")
	if ok {
		t.Error("Expected unknown command to be disallowed")
	}
}

func TestIsCommandAllowed_ReturnsConfig(t *testing.T) {
	h := NewInteractiveHandler(nil)

	config, ok := h.isCommandAllowed("-k")
	if !ok {
		t.Fatal("Expected '-k' to be allowed")
	}
	if config.Description == "" {
		t.Error("Expected non-empty description")
	}
	if config.Timeout == 0 {
		t.Error("Expected non-zero timeout")
	}
}

func TestIsCommandAllowed_DangerousFlag(t *testing.T) {
	h := NewInteractiveHandler(nil)

	config, ok := h.isCommandAllowed("-remove")
	if !ok {
		t.Fatal("Expected '-remove' to be allowed")
	}
	if !config.Dangerous {
		t.Error("Expected '-remove' to be flagged as dangerous")
	}
}

func TestIsCommandAllowed_NonDangerousFlag(t *testing.T) {
	h := NewInteractiveHandler(nil)

	config, ok := h.isCommandAllowed("-status")
	if !ok {
		t.Fatal("Expected '-status' to be allowed")
	}
	if config.Dangerous {
		t.Error("Expected '-status' to NOT be flagged as dangerous")
	}
}

// === sendError ===

// newTestInteractiveHandler creates a handler with permissive origin check for testing.
func newTestInteractiveHandler() *InteractiveHandler {
	h := NewInteractiveHandler(nil)
	h.upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     func(r *http.Request) bool { return true },
	}
	return h
}

func TestSendError_WritesCorrectJSON(t *testing.T) {
	// Create a test server that upgrades to WebSocket
	handler := newTestInteractiveHandler()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := handler.upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("Upgrade error: %v", err)
			return
		}
		defer conn.Close()
		handler.sendError(conn, "test error message")
	}))
	defer server.Close()

	// Connect as WebSocket client
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to dial WebSocket: %v", err)
	}
	defer conn.Close()

	// Read the error message
	var errorMsg ServerMessage
	if err := conn.ReadJSON(&errorMsg); err != nil {
		t.Fatalf("Failed to read error message: %v", err)
	}
	if errorMsg.Type != "error" {
		t.Errorf("Expected type 'error', got %q", errorMsg.Type)
	}
	if errorMsg.Text != "test error message" {
		t.Errorf("Expected text 'test error message', got %q", errorMsg.Text)
	}

	// Read the complete message that follows
	var completeMsg ServerMessage
	if err := conn.ReadJSON(&completeMsg); err != nil {
		t.Fatalf("Failed to read complete message: %v", err)
	}
	if completeMsg.Type != "complete" {
		t.Errorf("Expected type 'complete', got %q", completeMsg.Type)
	}
	if completeMsg.Success {
		t.Error("Expected success to be false for error")
	}
	if completeMsg.ExitCode != 1 {
		t.Errorf("Expected exit code 1, got %d", completeMsg.ExitCode)
	}
}

func TestSendError_EmptyMessage(t *testing.T) {
	handler := newTestInteractiveHandler()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := handler.upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		handler.sendError(conn, "")
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer conn.Close()

	var msg ServerMessage
	if err := conn.ReadJSON(&msg); err != nil {
		t.Fatalf("Failed to read: %v", err)
	}
	if msg.Type != "error" {
		t.Errorf("Expected type 'error', got %q", msg.Type)
	}
	if msg.Text != "" {
		t.Errorf("Expected empty text, got %q", msg.Text)
	}
}

// === RegisterInteractiveWSRoute ===

func TestRegisterInteractiveWSRoute(t *testing.T) {
	handler := NewInteractiveHandler(nil)

	router := mux.NewRouter()
	RegisterInteractiveWSRoute(router, handler, func(next http.Handler) http.Handler {
		return next // passthrough auth middleware
	})

	// Verify the route exists by attempting a GET request
	req := httptest.NewRequest("GET", "/ws/xkeen/interactive", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	// Should attempt WebSocket upgrade and fail (400 Bad Request from upgrader)
	// because the request is not a WebSocket upgrade
	if rec.Code == http.StatusNotFound {
		t.Error("Route should be registered, got 404")
	}
}

func TestRegisterInteractiveWSRoute_MethodNotAllowed(t *testing.T) {
	handler := NewInteractiveHandler(nil)

	router := mux.NewRouter()
	RegisterInteractiveWSRoute(router, handler, func(next http.Handler) http.Handler {
		return next
	})

	// POST should not match the registered GET route
	req := httptest.NewRequest("POST", "/ws/xkeen/interactive", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code == http.StatusOK {
		t.Error("POST should not be allowed")
	}
}

func TestRegisterInteractiveWSRoute_AuthMiddlewareApplied(t *testing.T) {
	handler := NewInteractiveHandler(nil)
	authCalled := false

	router := mux.NewRouter()
	RegisterInteractiveWSRoute(router, handler, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authCalled = true
			next.ServeHTTP(w, r)
		})
	})

	req := httptest.NewRequest("GET", "/ws/xkeen/interactive", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if !authCalled {
		t.Error("Expected auth middleware to be called")
	}
}

// === ServeHTTP ===

func TestServeHTTP_RejectsNonWebSocket(t *testing.T) {
	handler := NewInteractiveHandler(nil)

	req := httptest.NewRequest("GET", "/ws/xkeen/interactive", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Non-WebSocket requests should get 400 Bad Request from upgrader
	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for non-WebSocket request, got %d", rec.Code)
	}
}

func TestServeHTTP_ReceivesStartMessage_WrongType(t *testing.T) {
	handler := newTestInteractiveHandler()
	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer conn.Close()

	// Send a message with wrong type
	err = conn.WriteJSON(ClientMessage{Type: "input", Text: "hello"})
	if err != nil {
		t.Fatalf("Failed to write: %v", err)
	}

	// Should receive error message
	var errorMsg ServerMessage
	if err := conn.ReadJSON(&errorMsg); err != nil {
		t.Fatalf("Failed to read: %v", err)
	}
	if errorMsg.Type != "error" {
		t.Errorf("Expected error type, got %q", errorMsg.Type)
	}
	if !strings.Contains(errorMsg.Text, "Expected 'start'") {
		t.Errorf("Error should mention 'start', got: %q", errorMsg.Text)
	}
}

func TestServeHTTP_DisallowedCommand(t *testing.T) {
	handler := newTestInteractiveHandler()
	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer conn.Close()

	// Send start message with disallowed command
	err = conn.WriteJSON(ClientMessage{Type: "start", Command: "rm -rf /"})
	if err != nil {
		t.Fatalf("Failed to write: %v", err)
	}

	// Should receive error about unknown command
	var errorMsg ServerMessage
	if err := conn.ReadJSON(&errorMsg); err != nil {
		t.Fatalf("Failed to read: %v", err)
	}
	if errorMsg.Type != "error" {
		t.Errorf("Expected error type, got %q", errorMsg.Type)
	}
	if !strings.Contains(errorMsg.Text, "Unknown command") {
		t.Errorf("Error should mention 'Unknown command', got: %q", errorMsg.Text)
	}
}

func TestServeHTTP_DisconnectBeforeStart(t *testing.T) {
	handler := newTestInteractiveHandler()
	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	// Immediately close without sending any message
	conn.Close()

	// Give server time to process
	time.Sleep(100 * time.Millisecond)
	// Should not panic — this tests the ReadJSON error handling path
}

// === AllowedCommands contents ===

func TestAllowedCommands_ContainsKeyCommands(t *testing.T) {
	handler := NewInteractiveHandler(nil)

	// Verify that key management commands are present
	keyCommands := []string{"-k", "-g", "-start", "-stop", "-restart", "-status"}
	for _, cmd := range keyCommands {
		_, ok := handler.isCommandAllowed(cmd)
		if !ok {
			t.Errorf("Expected key command %q to be allowed", cmd)
		}
	}
}

func TestAllowedCommands_ContainsInstallCommands(t *testing.T) {
	handler := NewInteractiveHandler(nil)

	installCmds := []string{"-i", "-io", "-uk", "-ug", "-ux", "-um", "-ugc"}
	for _, cmd := range installCmds {
		_, ok := handler.isCommandAllowed(cmd)
		if !ok {
			t.Errorf("Expected install command %q to be allowed", cmd)
		}
	}
}

func TestAllowedCommands_ContainsRemoveCommands(t *testing.T) {
	handler := NewInteractiveHandler(nil)

	removeCmds := []string{"-rrk", "-rrx", "-rrm", "-ri", "-remove"}
	for _, cmd := range removeCmds {
		_, ok := handler.isCommandAllowed(cmd)
		if !ok {
			t.Errorf("Expected remove command %q to be allowed", cmd)
		}
	}
}

func TestAllowedCommands_ContainsPortCommands(t *testing.T) {
	handler := NewInteractiveHandler(nil)

	portCmds := []string{"-ap", "-dp", "-cp", "-ape", "-dpe", "-cpe"}
	for _, cmd := range portCmds {
		_, ok := handler.isCommandAllowed(cmd)
		if !ok {
			t.Errorf("Expected port command %q to be allowed", cmd)
		}
	}
}

func TestAllowedCommands_ContainsExcludePortCommands(t *testing.T) {
	handler := NewInteractiveHandler(nil)

	excludeCmds := []string{"-dgs", "-dgi", "-dx", "-dm", "-dk", "-dgc", "-drk", "-drx", "-drm"}
	for _, cmd := range excludeCmds {
		_, ok := handler.isCommandAllowed(cmd)
		if !ok {
			t.Errorf("Expected exclude port command %q to be allowed", cmd)
		}
	}
}

func TestAllowedCommands_DoesNotContainRandomCommands(t *testing.T) {
	handler := NewInteractiveHandler(nil)

	randomCmds := []string{"ls", "cat", "rm", "bash", "sh", "python", "curl", "wget", "chmod"}
	for _, cmd := range randomCmds {
		_, ok := handler.isCommandAllowed(cmd)
		if ok {
			t.Errorf("Expected system command %q to NOT be allowed", cmd)
		}
	}
}

// === InteractiveConfig ===

func TestInteractiveConfig_NilDoesNotPanic(t *testing.T) {
	// Should not panic with nil config
	handler := NewInteractiveHandler(nil)
	if handler == nil {
		t.Error("Expected handler to be created with nil config")
	}
}

func TestInteractiveConfig_MultipleAllowedOrigins(t *testing.T) {
	cfg := &InteractiveConfig{
		AllowedOrigins: []string{
			"http://router.lan",
			"http://192.168.1.1",
			"http://localhost:8089",
		},
	}
	handler := NewInteractiveHandler(cfg)

	for _, origin := range cfg.AllowedOrigins {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("Origin", origin)
		req.Host = "other-host"

		if !handler.checkOrigin(req) {
			t.Errorf("Expected origin %q to be allowed", origin)
		}
	}
}

// === Upgrader configuration ===

func TestInteractiveHandler_UpgraderConfig(t *testing.T) {
	handler := NewInteractiveHandler(nil)

	if handler.upgrader.ReadBufferSize != 1024 {
		t.Errorf("Expected ReadBufferSize 1024, got %d", handler.upgrader.ReadBufferSize)
	}
	if handler.upgrader.WriteBufferSize != 1024 {
		t.Errorf("Expected WriteBufferSize 1024, got %d", handler.upgrader.WriteBufferSize)
	}
}

// === Concurrent isCommandAllowed ===

func TestIsCommandAllowed_Concurrent(t *testing.T) {
	handler := NewInteractiveHandler(nil)

	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				_, ok := handler.isCommandAllowed("-k")
				if !ok {
					t.Error("Expected '-k' to be allowed")
				}
			}
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

// === Goroutine leak regression tests ===

func TestExecuteInteractive_HandlerReturnsWithinTimeout(t *testing.T) {
	// Regression test for goroutine leak: after command completion (possibly with error),
	// the handler should return within a reasonable timeout. The goroutines are tracked
	// via WaitGroup and cancelled via context after cmd.Wait().
	handler := newTestInteractiveHandler()
	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer conn.Close()

	// Send a start command with a whitelisted command.
	// On non-Linux (e.g. Windows dev), PTY will fail and handler returns early.
	// On Linux, the command will attempt to run and also fail (no xkeen binary).
	start := time.Now()
	if err := conn.WriteJSON(ClientMessage{Type: "start", Command: "-status"}); err != nil {
		t.Fatalf("Failed to write start message: %v", err)
	}

	// Read until complete (or error). Timeout ensures no goroutine leak.
	var gotComplete bool
	for {
		var msg ServerMessage
		if err := conn.ReadJSON(&msg); err != nil {
			break // connection closed
		}
		if msg.Type == "complete" || msg.Type == "error" {
			gotComplete = true
			break
		}
	}

	elapsed := time.Since(start)
	if elapsed > 30*time.Second {
		t.Errorf("Handler took too long (%v) — possible goroutine hang/deadlock", elapsed)
	}
	if !gotComplete {
		t.Fatal("Handler did not send complete/error message — possible goroutine leak")
	}
	t.Logf("Handler completed in %v", elapsed)
}

func TestExecuteInteractive_SignalMidCommand(t *testing.T) {
	// Test signal handling doesn't cause hang.
	// On non-Linux (no PTY), the handler returns immediately with error+complete.
	// We just verify the handler terminates cleanly either way.
	handler := newTestInteractiveHandler()
	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer conn.Close()

	// Send start
	if err := conn.WriteJSON(ClientMessage{Type: "start", Command: "-uk"}); err != nil {
		t.Fatalf("Failed to write start: %v", err)
	}

	// Continue reading until we get at least one complete/error message.
	// Then send signal — it may or may not be processed (depends on PTY availability).
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var gotCompletion bool
	for {
		var msg ServerMessage
		if err := conn.ReadJSON(&msg); err != nil {
			break
		}
		if msg.Type == "complete" || msg.Type == "error" {
			gotCompletion = true
		}
	}

	// Send signal after server already finished
	_ = conn.WriteJSON(ClientMessage{Type: "signal", Signal: "SIGTERM"})

	if !gotCompletion {
		t.Fatal("Did not receive completion message — possible goroutine leak")
	}
}
