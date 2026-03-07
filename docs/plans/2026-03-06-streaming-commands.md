# Streaming Commands Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement NDJSON streaming for command execution to show real-time output in the UI.

**Architecture:** Replace synchronous command execution with streaming response. Backend uses `exec.Cmd` with `StdoutPipe`/`StderrPipe` to read output line-by-line and flush each NDJSON message immediately. Frontend uses `fetch` with `ReadableStream` to display output in real-time.

**Tech Stack:** Go (net/http, os/exec), JavaScript (fetch API, ReadableStream), NDJSON format

---

## Task 1: Increase HTTP Server WriteTimeout

**Files:**
- Modify: `xkeen-go/internal/server/server.go:132`

**Step 1: Update WriteTimeout**

Change line 132 from `15 * time.Second` to `300 * time.Second`:

```go
// Create HTTP server
s.http = &http.Server{
    Addr:         fmt.Sprintf(":%d", cfg.Port),
    Handler:      router,
    ReadTimeout:  15 * time.Second,
    WriteTimeout: 300 * time.Second, // Increased for long-running commands
    IdleTimeout:  60 * time.Second,
}
```

**Step 2: Run tests to verify nothing broke**

Run: `cd xkeen-go && go test ./internal/server/... -v`
Expected: All tests pass

**Step 3: Commit**

```bash
git add xkeen-go/internal/server/server.go
git commit -m "feat(server): increase WriteTimeout to 300s for long-running commands"
```

---

## Task 2: Add Streaming Executor Interface

**Files:**
- Modify: `xkeen-go/internal/handlers/commands.go`

**Step 1: Add StreamExecutor interface and types**

Add after the `CommandExecutor` interface (around line 32):

```go
// StreamMessage represents a single message in the NDJSON stream.
type StreamMessage struct {
	Type     string `json:"type"`               // "output", "error", or "complete"
	Text     string `json:"text,omitempty"`     // For output/error types
	Success  bool   `json:"success,omitempty"`  // For complete type
	ExitCode int    `json:"exitCode,omitempty"` // For complete type
}

// StreamWriter is used to send streaming messages.
type StreamWriter interface {
	WriteMessage(msg StreamMessage) error
}

// StreamExecutor executes commands and streams output.
type StreamExecutor interface {
	ExecuteStream(ctx context.Context, sw StreamWriter, name string, args ...string) error
}
```

**Step 2: Add realStreamExecutor implementation**

Add after `realExecutor` struct (around line 55):

```go
// realStreamExecutor implements StreamExecutor using actual exec.Command.
type realStreamExecutor struct{}

func (e *realStreamExecutor) ExecuteStream(ctx context.Context, sw StreamWriter, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start command: %w", err)
	}

	// Read stdout and stderr concurrently
	var wg sync.WaitGroup
	wg.Add(2)

	// Read stdout line by line
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			sw.WriteMessage(StreamMessage{
				Type: "output",
				Text: scanner.Text(),
			})
		}
	}()

	// Read stderr line by line
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			sw.WriteMessage(StreamMessage{
				Type: "error",
				Text: scanner.Text(),
			})
		}
	}()

	wg.Wait()

	// Wait for command to complete
	err = cmd.Wait()

	// Check for context cancellation
	if ctx.Err() != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			sw.WriteMessage(StreamMessage{
				Type: "error",
				Text: fmt.Sprintf("Command timed out after %v", ctx.Deadline()),
			})
		}
		sw.WriteMessage(StreamMessage{
			Type:     "complete",
			Success:  false,
			ExitCode: -1,
		})
		return ctx.Err()
	}

	// Get exit code
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	sw.WriteMessage(StreamMessage{
		Type:     "complete",
		Success:  exitCode == 0,
		ExitCode: exitCode,
	})

	return nil
}
```

**Step 3: Add bufio import**

Add `"bufio"` to imports:

```go
import (
	"bufio"
	"context"
	"encoding/json"
	// ... rest of imports
)
```

**Step 4: Verify code compiles**

Run: `cd xkeen-go && go build ./...`
Expected: No errors

**Step 5: Commit**

```bash
git add xkeen-go/internal/handlers/commands.go
git commit -m "feat(handlers): add StreamExecutor interface for streaming command output"
```

---

## Task 3: Implement HTTP Response StreamWriter

**Files:**
- Modify: `xkeen-go/internal/handlers/commands.go`

**Step 1: Add httpResponseWriter struct**

Add before `CommandsHandler` struct (around line 107):

```go
// httpResponseWriter implements StreamWriter for HTTP responses.
type httpResponseWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

func newHTTPResponseWriter(w http.ResponseWriter) *httpResponseWriter {
	return &httpResponseWriter{
		w:       w,
		flusher: w.(http.Flusher),
	}
}

func (h *httpResponseWriter) WriteMessage(msg StreamMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	h.w.Write(data)
	h.w.Write([]byte("\n"))
	h.flusher.Flush()
	return nil
}
```

**Step 2: Verify code compiles**

Run: `cd xkeen-go && go build ./...`
Expected: No errors

**Step 3: Commit**

```bash
git add xkeen-go/internal/handlers/commands.go
git commit -m "feat(handlers): add httpResponseWriter for NDJSON streaming"
```

---

## Task 4: Update CommandsHandler to Use Streaming

**Files:**
- Modify: `xkeen-go/internal/handlers/commands.go`

**Step 1: Add streamExecutor to CommandsHandler**

Modify `CommandsHandler` struct:

```go
// CommandsHandler handles XKeen CLI command execution.
type CommandsHandler struct {
	mu              sync.RWMutex
	allowedCommands map[string]CommandConfig
	executor        CommandExecutor    // Keep for backward compatibility
	streamExecutor  StreamExecutor     // For streaming execution
}
```

**Step 2: Update constructors**

```go
// NewCommandsHandler creates a new CommandsHandler with default settings.
func NewCommandsHandler() *CommandsHandler {
	return &CommandsHandler{
		allowedCommands: defaultCommands,
		executor:        &realExecutor{},
		streamExecutor:  &realStreamExecutor{},
	}
}

// NewCommandsHandlerWithExecutor creates a CommandsHandler with a custom executor (for testing).
func NewCommandsHandlerWithExecutor(executor CommandExecutor) *CommandsHandler {
	return &CommandsHandler{
		allowedCommands: defaultCommands,
		executor:        executor,
		streamExecutor:  &realStreamExecutor{},
	}
}

// NewCommandsHandlerWithStreamExecutor creates a CommandsHandler with custom executors (for testing).
func NewCommandsHandlerWithStreamExecutor(executor CommandExecutor, streamExecutor StreamExecutor) *CommandsHandler {
	return &CommandsHandler{
		allowedCommands: defaultCommands,
		executor:        executor,
		streamExecutor:  streamExecutor,
	}
}
```

**Step 3: Rewrite ExecuteCommand for streaming**

Replace the entire `ExecuteCommand` method:

```go
// ExecuteCommand executes a whitelisted XKeen command with streaming output.
// POST /api/xkeen/command
func (h *CommandsHandler) ExecuteCommand(w http.ResponseWriter, r *http.Request) {
	var req CommandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondJSON(w, http.StatusBadRequest, CommandResponse{
			Success: false,
			Message: "Invalid JSON request",
		})
		return
	}

	// Validate command is not empty
	if req.Command == "" {
		h.respondJSON(w, http.StatusBadRequest, CommandResponse{
			Success: false,
			Message: "Command is required",
		})
		return
	}

	// Look up command in whitelist
	config, exists := h.allowedCommands[req.Command]
	if !exists {
		h.respondJSON(w, http.StatusBadRequest, CommandResponse{
			Success: false,
			Message: fmt.Sprintf("Unknown command: %s", req.Command),
		})
		return
	}

	// Build the command to execute (xkeen -<flag> format)
	cmdStr := fmt.Sprintf("xkeen -%s", config.Cmd)
	parts := splitCommand(cmdStr)
	if len(parts) == 0 {
		h.respondJSON(w, http.StatusInternalServerError, CommandResponse{
			Success: false,
			Message: "Failed to parse command",
		})
		return
	}

	// Set headers for NDJSON streaming
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Transfer-Encoding", "chunked")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	// Create context with timeout
	ctx, cancel := context.WithTimeout(r.Context(), config.Timeout)
	defer cancel()

	// Create streaming response writer
	sw := newHTTPResponseWriter(w)

	// Execute with streaming
	err := h.streamExecutor.ExecuteStream(ctx, sw, parts[0], parts[1:]...)

	// Log for debugging
	log.Printf("Command '%s' executed: err=%v", req.Command, err)
}
```

**Step 4: Verify code compiles**

Run: `cd xkeen-go && go build ./...`
Expected: No errors

**Step 5: Run existing tests (some may fail due to new response format)**

Run: `cd xkeen-go && go test ./internal/handlers/... -run TestCommands -v`
Expected: Some tests may fail because they expect JSON response, not NDJSON

**Step 6: Commit**

```bash
git add xkeen-go/internal/handlers/commands.go
git commit -m "feat(handlers): implement streaming command execution with NDJSON"
```

---

## Task 5: Update Tests for Streaming Format

**Files:**
- Modify: `xkeen-go/internal/handlers/commands_test.go`

**Step 1: Add mockStreamExecutor**

Add after `slowExecutor` definition in `service_test.go` or at the end of `commands_test.go`:

```go
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
```

**Step 2: Add helper to parse NDJSON response**

```go
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
```

**Step 3: Add test for streaming execution**

```go
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
		t.Errorf("Expected 4 messages, got %d", len(messages))
	}

	// Check output messages
	if messages[0].Type != "output" || messages[0].Text != "Line 1" {
		t.Errorf("Expected first message to be output 'Line 1', got %+v", messages[0])
	}

	// Check complete message
	if messages[3].Type != "complete" {
		t.Errorf("Expected last message type 'complete', got %s", messages[3].Type)
	}
	if !messages[3].Success {
		t.Error("Expected success=true in complete message")
	}

	t.Logf("Streaming response: %d messages", len(messages))
}
```

**Step 4: Add test for streaming with errors**

```go
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
```

**Step 5: Run new tests**

Run: `cd xkeen-go && go test ./internal/handlers/... -run "TestCommandsHandler_ExecuteCommand_Streaming" -v`
Expected: Both tests pass

**Step 6: Commit**

```bash
git add xkeen-go/internal/handlers/commands_test.go
git commit -m "test(handlers): add streaming command execution tests"
```

---

## Task 6: Update Frontend for Streaming

**Files:**
- Modify: `xkeen-go/web/static/js/components/commands.js`

**Step 1: Rewrite doExecute method**

Replace the `doExecute` method (lines 91-127):

```javascript
async doExecute(command) {
    this.executingCommand = command
    this.modalError = ''
    this.modalOutput = ''
    this.modalCommand = command
    this.showModal = true  // Open modal immediately
    this.commandComplete = false  // Track completion

    try {
        const res = await fetch('/api/xkeen/command', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'X-CSRF-Token': this.$store.app.csrfToken
            },
            body: JSON.stringify({ command: command })
        })

        if (!res.ok) {
            // Handle non-streaming error response
            const data = await res.json().catch(() => ({}))
            this.modalError = data.message || 'Command execution failed'
            return
        }

        // Stream the response
        await this.readStream(res.body)

    } catch (err) {
        this.modalError = 'Failed to execute command: ' + err.message
    } finally {
        this.executingCommand = ''
        this.commandComplete = true
    }
},
```

**Step 2: Add readStream method**

Add after `doExecute`:

```javascript
async readStream(body) {
    const reader = body.getReader()
    const decoder = new TextDecoder()
    let buffer = ''

    try {
        while (true) {
            const { done, value } = await reader.read()
            if (done) break

            buffer += decoder.decode(value, { stream: true })
            const lines = buffer.split('\n')
            buffer = lines.pop()  // Keep incomplete line in buffer

            for (const line of lines) {
                if (line.trim()) {
                    try {
                        const msg = JSON.parse(line)
                        this.handleStreamMessage(msg)
                    } catch (e) {
                        console.warn('Failed to parse stream line:', line)
                    }
                }
            }
        }

        // Process any remaining buffer
        if (buffer.trim()) {
            try {
                const msg = JSON.parse(buffer)
                this.handleStreamMessage(msg)
            } catch (e) {
                console.warn('Failed to parse final buffer:', buffer)
            }
        }
    } finally {
        reader.releaseLock()
    }
},
```

**Step 3: Add handleStreamMessage method**

Add after `readStream`:

```javascript
handleStreamMessage(msg) {
    if (msg.type === 'output') {
        this.modalOutput += msg.text + '\n'
        this.scrollToBottom()
    } else if (msg.type === 'error') {
        this.modalError += (this.modalError ? '\n' : '') + msg.text
        this.scrollToBottom()
    } else if (msg.type === 'complete') {
        this.commandComplete = true
        if (!msg.success && !this.modalError) {
            this.modalError = `Command failed with exit code ${msg.exitCode}`
        }
    }
},
```

**Step 4: Add scrollToBottom method**

Add after `handleStreamMessage`:

```javascript
scrollToBottom() {
    // Use nextTick to ensure DOM is updated
    this.$nextTick(() => {
        const outputEl = document.getElementById('modal-output')
        if (outputEl) {
            outputEl.scrollTop = outputEl.scrollHeight
        }
    })
},
```

**Step 5: Add commandComplete to state**

Add to the return object at the top:

```javascript
function commandsComponent() {
    return {
        // State
        executingCommand: '',
        commandComplete: false,  // <-- Add this

        // Modal state
        showModal: false,
        // ... rest of state
```

**Step 6: Update closeModal to handle stream cancellation**

```javascript
closeModal() {
    this.showModal = false
    this.modalOutput = ''
    this.modalCommand = ''
    this.modalError = ''
    this.commandComplete = false
},
```

**Step 7: Verify JavaScript syntax**

No direct way to test, but check for obvious errors.

**Step 8: Commit**

```bash
git add xkeen-go/web/static/js/components/commands.js
git commit -m "feat(web): implement streaming command output with NDJSON"
```

---

## Task 7: Update Modal HTML for Auto-Scroll

**Files:**
- Modify: `xkeen-go/web/index.html`

**Step 1: Find modal output element and add id**

Find the modal output element (search for `modalOutput` or the output display area) and ensure it has `id="modal-output"` for the auto-scroll function.

Example:
```html
<pre id="modal-output" class="..." x-text="modalOutput"></pre>
```

**Step 2: Commit**

```bash
git add xkeen-go/web/index.html
git commit -m "feat(web): add id to modal output for auto-scroll"
```

---

## Task 8: Integration Testing

**Step 1: Build the application**

Run: `cd xkeen-go && go build -o build/xkeen-go .`

**Step 2: Run the server locally**

Run: `cd xkeen-go && ./build/xkeen-go` (or `make run`)

**Step 3: Test in browser**

1. Open the web UI
2. Go to Commands tab
3. Click on a command (e.g., "status")
4. Verify:
   - Modal opens immediately
   - Output appears line by line (if command produces multiple lines)
   - "Executing..." indicator disappears when complete

**Step 4: Test long-running command**

If you have xkeen installed, test with a longer command like `uk` or `ux`.

---

## Task 9: Final Commit and Cleanup

**Step 1: Run all tests**

Run: `cd xkeen-go && go test ./... -v`

**Step 2: Final commit (if any changes)**

```bash
git status
git add -A
git commit -m "feat: complete streaming commands implementation"
```

---

## Summary

| Task | Description | Files |
|------|-------------|-------|
| 1 | Increase WriteTimeout | server.go |
| 2 | Add StreamExecutor interface | commands.go |
| 3 | Add httpResponseWriter | commands.go |
| 4 | Update ExecuteCommand for streaming | commands.go |
| 5 | Update tests | commands_test.go |
| 6 | Update frontend streaming | commands.js |
| 7 | Add modal output id | index.html |
| 8 | Integration testing | - |
| 9 | Final cleanup | - |
