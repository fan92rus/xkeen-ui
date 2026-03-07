# Interactive Commands Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add WebSocket-based interactive command execution supporting user input during runtime.

**Architecture:** New WebSocket endpoint `/ws/xkeen/interactive` with bidirectional communication. Backend spawns process with stdin/stdout/stderr pipes, streams output to client, accepts input from client. Frontend uses WebSocket instead of HTTP for command execution.

**Tech Stack:** Go, gorilla/websocket, Alpine.js, native WebSocket API

---

## Task 1: Backend - Interactive Handler Types and Constructor

**Files:**
- Create: `xkeen-go/internal/handlers/interactive.go`

**Step 1: Write the failing test**

Create `xkeen-go/internal/handlers/interactive_test.go`:

```go
package handlers

import (
	"testing"
)

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
}
```

**Step 2: Run test to verify it fails**

Run: `cd xkeen-go && go test -v ./internal/handlers/... -run TestNewInteractiveHandler`
Expected: FAIL with "undefined: NewInteractiveHandler"

**Step 3: Write minimal implementation**

Create `xkeen-go/internal/handlers/interactive.go`:

```go
// Package handlers provides HTTP handlers for XKEEN-GO API endpoints.
package handlers

import (
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// InteractiveHandler handles interactive command execution via WebSocket.
type InteractiveHandler struct {
	mu              sync.RWMutex
	allowedCommands map[string]CommandConfig
	allowedOrigins  map[string]bool
	upgrader        websocket.Upgrader
}

// InteractiveConfig configures the interactive handler.
type InteractiveConfig struct {
	AllowedOrigins []string
}

// NewInteractiveHandler creates a new InteractiveHandler.
func NewInteractiveHandler(cfg *InteractiveConfig) *InteractiveHandler {
	// Build allowed origins map
	allowedOrigins := make(map[string]bool)
	if cfg != nil {
		for _, origin := range cfg.AllowedOrigins {
			allowedOrigins[origin] = true
		}
	}

	h := &InteractiveHandler{
		allowedCommands: defaultCommands,
		allowedOrigins:  allowedOrigins,
	}

	// Create upgrader with origin check
	h.upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     h.checkOrigin,
	}

	return h
}

// checkOrigin validates the origin of WebSocket connections.
func (h *InteractiveHandler) checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	host := r.Host

	if origin == "" {
		return true
	}

	if h.allowedOrigins[origin] {
		return true
	}

	if origin == "http://"+host || origin == "https://"+host {
		return true
	}

	log.Printf("WebSocket connection rejected from origin: %s (host: %s)", origin, host)
	return false
}

// isCommandAllowed checks if a command is in the whitelist.
func (h *InteractiveHandler) isCommandAllowed(cmd string) (CommandConfig, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	config, exists := h.allowedCommands[cmd]
	return config, exists
}
```

**Step 4: Run test to verify it passes**

Run: `cd xkeen-go && go test -v ./internal/handlers/... -run TestNewInteractiveHandler`
Expected: PASS

**Step 5: Commit**

```bash
git add xkeen-go/internal/handlers/interactive.go xkeen-go/internal/handlers/interactive_test.go
git commit -m "feat: add InteractiveHandler types and constructor"
```

---

## Task 2: Backend - WebSocket Message Types

**Files:**
- Modify: `xkeen-go/internal/handlers/interactive.go`

**Step 1: Write the failing test**

Add to `xkeen-go/internal/handlers/interactive_test.go`:

```go
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
```

**Step 2: Run test to verify it fails**

Run: `cd xkeen-go && go test -v ./internal/handlers/... -run TestInteractiveMessageTypes`
Expected: FAIL with "undefined: ClientMessage"

**Step 3: Write minimal implementation**

Add to `xkeen-go/internal/handlers/interactive.go` (after imports, before InteractiveHandler):

```go
// ClientMessage represents a message from the WebSocket client.
type ClientMessage struct {
	Type    string `json:"type"`              // "start", "input", "signal"
	Command string `json:"command,omitempty"` // For "start" type
	Text    string `json:"text,omitempty"`    // For "input" type
	Signal  string `json:"signal,omitempty"`  // For "signal" type (e.g., "SIGTERM")
}

// ServerMessage represents a message to the WebSocket client.
type ServerMessage struct {
	Type     string `json:"type"`               // "output", "error", "complete"
	Text     string `json:"text,omitempty"`     // For output/error types
	Success  bool   `json:"success,omitempty"`  // For complete type
	ExitCode int    `json:"exitCode,omitempty"` // For complete type
}
```

Add import for encoding/json if not present.

**Step 4: Run test to verify it passes**

Run: `cd xkeen-go && go test -v ./internal/handlers/... -run TestInteractiveMessageTypes`
Expected: PASS

**Step 5: Commit**

```bash
git add xkeen-go/internal/handlers/interactive.go xkeen-go/internal/handlers/interactive_test.go
git commit -m "feat: add WebSocket message types for interactive commands"
```

---

## Task 3: Backend - WebSocket ServeHTTP Implementation

**Files:**
- Modify: `xkeen-go/internal/handlers/interactive.go`

**Step 1: Write the failing test**

Add to `xkeen-go/internal/handlers/interactive_test.go`:

```go
import (
	"bufio"
	"context"
	"io"
	"strings"
	"time"
)

// mockInteractiveExecutor captures stdin/stdout for testing
type mockInteractiveExecutor struct {
	stdout string
	stderr string
	err    error
}

func (m *mockInteractiveExecutor) ExecuteInteractive(ctx context.Context, stdin io.Writer, stdout, stderr io.Writer, name string, args ...string) error {
	if m.err != nil {
		return m.err
	}
	stdout.Write([]byte(m.stdout))
	stderr.Write([]byte(m.stderr))
	return nil
}

func TestInteractiveHandlerWebSocketFlow(t *testing.T) {
	// This is an integration test that requires more setup
	// For now, we test the command building logic
	handler := NewInteractiveHandler(nil)

	// Test command building
	config, exists := handler.isCommandAllowed("uk")
	if !exists {
		t.Fatal("Command 'uk' should be allowed")
	}
	if config.Cmd != "uk" {
		t.Errorf("Expected Cmd='uk', got %s", config.Cmd)
	}
}
```

**Step 2: Run test to verify it passes**

Run: `cd xkeen-go && go test -v ./internal/handlers/... -run TestInteractiveHandlerWebSocketFlow`
Expected: PASS (testing existing functionality)

**Step 3: Write the ServeHTTP implementation**

Add to `xkeen-go/internal/handlers/interactive.go`:

```go
import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	// ... existing imports
)

// InteractiveExecutor executes commands with interactive stdin/stdout.
type InteractiveExecutor interface {
	ExecuteInteractive(ctx context.Context, stdin io.Writer, stdout, stderr io.Writer, name string, args ...string) error
}

// realInteractiveExecutor implements InteractiveExecutor.
type realInteractiveExecutor struct{}

func (e *realInteractiveExecutor) ExecuteInteractive(ctx context.Context, stdin io.Writer, stdout, stderr io.Writer, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

// executor is the default executor
var interactiveExecutor InteractiveExecutor = &realInteractiveExecutor{}

// ServeHTTP handles WebSocket connections for interactive command execution.
func (h *InteractiveHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	log.Printf("Interactive WebSocket client connected from %s", r.RemoteAddr)

	// Read the start message
	var startMsg ClientMessage
	if err := conn.ReadJSON(&startMsg); err != nil {
		h.sendError(conn, "Failed to read start message: "+err.Error())
		return
	}

	if startMsg.Type != "start" {
		h.sendError(conn, "Expected 'start' message, got: "+startMsg.Type)
		return
	}

	// Validate command
	config, exists := h.isCommandAllowed(startMsg.Command)
	if !exists {
		h.sendError(conn, fmt.Sprintf("Unknown command: %s", startMsg.Command))
		return
	}

	// Execute the command
	h.executeInteractive(conn, config)
}

// sendError sends an error message to the client.
func (h *InteractiveHandler) sendError(conn *websocket.Conn, text string) {
	conn.WriteJSON(ServerMessage{
		Type: "error",
		Text: text,
	})
	conn.WriteJSON(ServerMessage{
		Type:     "complete",
		Success:  false,
		ExitCode: 1,
	})
}

// executeInteractive runs the command and handles stdin/stdout/stderr.
func (h *InteractiveHandler) executeInteractive(conn *websocket.Conn, config CommandConfig) {
	ctx, cancel := context.WithTimeout(context.Background(), config.Timeout)
	defer cancel()

	// Build command
	cmdStr := fmt.Sprintf("xkeen -%s", config.Cmd)
	parts := strings.Fields(cmdStr)
	if len(parts) == 0 {
		h.sendError(conn, "Failed to parse command")
		return
	}

	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)

	// Create pipes
	stdin, err := cmd.StdinPipe()
	if err != nil {
		h.sendError(conn, "Failed to create stdin pipe: "+err.Error())
		return
	}
	defer stdin.Close()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		h.sendError(conn, "Failed to create stdout pipe: "+err.Error())
		return
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		h.sendError(conn, "Failed to create stderr pipe: "+err.Error())
		return
	}

	// Start command
	if err := cmd.Start(); err != nil {
		h.sendError(conn, "Failed to start command: "+err.Error())
		return
	}

	// Done channel for coordination
	done := make(chan struct{})

	// Read stdout in goroutine
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			conn.WriteJSON(ServerMessage{
				Type: "output",
				Text: scanner.Text(),
			})
		}
	}()

	// Read stderr in goroutine
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			conn.WriteJSON(ServerMessage{
				Type: "error",
				Text: scanner.Text(),
			})
		}
	}()

	// Read WebSocket messages and write to stdin
	go func() {
		defer close(done)
		for {
			var msg ClientMessage
			if err := conn.ReadJSON(&msg); err != nil {
				return // Connection closed or error
			}
			if msg.Type == "input" {
				stdin.Write([]byte(msg.Text))
			} else if msg.Type == "signal" {
				// Handle signal (e.g., terminate)
				if cmd.Process != nil {
					cmd.Process.Kill()
				}
				return
			}
		}
	}()

	// Wait for command to complete
	err = cmd.Wait()

	// Get exit code
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	// Send completion message
	conn.WriteJSON(ServerMessage{
		Type:     "complete",
		Success:  exitCode == 0,
		ExitCode: exitCode,
	})

	log.Printf("Interactive command '%s' completed with exit code %d", config.Cmd, exitCode)
}
```

**Step 4: Run tests to verify they pass**

Run: `cd xkeen-go && go test -v ./internal/handlers/...`
Expected: PASS

**Step 5: Commit**

```bash
git add xkeen-go/internal/handlers/interactive.go xkeen-go/internal/handlers/interactive_test.go
git commit -m "feat: implement WebSocket ServeHTTP for interactive commands"
```

---

## Task 4: Backend - Register WebSocket Route

**Files:**
- Modify: `xkeen-go/internal/handlers/interactive.go` (add register function)
- Modify: `xkeen-go/internal/server/server.go` (add handler and route)

**Step 1: Add register function to interactive.go**

Add to end of `xkeen-go/internal/handlers/interactive.go`:

```go
// RegisterInteractiveWSRoute registers the WebSocket route for interactive commands.
func RegisterInteractiveWSRoute(r *mux.Router, handler *InteractiveHandler, authMiddleware func(http.Handler) http.Handler) {
	wsRouter := r.PathPrefix("/ws").Subrouter()
	wsRouter.Use(authMiddleware)
	wsRouter.Handle("/xkeen/interactive", handler).Methods("GET")
}
```

**Step 2: Update server.go**

In `xkeen-go/internal/server/server.go`, add to Server struct (around line 43):

```go
	interactiveHandler *handlers.InteractiveHandler
```

In `NewServer` function (around line 117, after commandsHandler):

```go
	s.interactiveHandler = handlers.NewInteractiveHandler(&handlers.InteractiveConfig{
		AllowedOrigins: cfg.CORS.AllowedOrigins,
	})
```

In `setupRoutes` function (around line 236, after RegisterLogsWSRoute):

```go
	// Interactive command WebSocket (auth required, no CSRF)
	handlers.RegisterInteractiveWSRoute(s.router, s.interactiveHandler, s.middleware.AuthMiddleware)
```

**Step 3: Run tests and build**

Run: `cd xkeen-go && go build . && go test ./...`
Expected: PASS, build succeeds

**Step 4: Commit**

```bash
git add xkeen-go/internal/handlers/interactive.go xkeen-go/internal/server/server.go
git commit -m "feat: register interactive WebSocket route"
```

---

## Task 5: Frontend - InteractiveSession Service

**Files:**
- Create: `xkeen-go/web/static/js/services/interactive.js`

**Step 1: Create the service**

Create `xkeen-go/web/static/js/services/interactive.js`:

```javascript
// services/interactive.js - WebSocket client for interactive command execution

const WS_BASE = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
const WS_HOST = WS_BASE + '//' + window.location.host;

/**
 * InteractiveSession manages a WebSocket connection for interactive command execution.
 */
export class InteractiveSession {
    /**
     * @param {string} command - Command to execute
     * @param {function} onMessage - Callback for output/error messages: (msg: {type, text}) => void
     * @param {function} onComplete - Callback when command completes: (msg: {success, exitCode}) => void
     * @param {function} onError - Callback for connection errors: (error) => void
     */
    constructor(command, onMessage, onComplete, onError) {
        this.ws = null;
        this.command = command;
        this.onMessage = onMessage;
        this.onComplete = onComplete;
        this.onError = onError;
        this.connected = false;
    }

    /**
     * Connect to WebSocket and start the command.
     */
    connect() {
        this.ws = new WebSocket(`${WS_HOST}/ws/xkeen/interactive`);

        this.ws.onopen = () => {
            console.log('Interactive WebSocket connected');
            this.connected = true;
            // Send start message
            this.ws.send(JSON.stringify({
                type: 'start',
                command: this.command
            }));
        };

        this.ws.onmessage = (event) => {
            try {
                const msg = JSON.parse(event.data);
                this.handleMessage(msg);
            } catch (e) {
                console.warn('Failed to parse WebSocket message:', event.data);
            }
        };

        this.ws.onerror = (error) => {
            console.error('WebSocket error:', error);
            if (this.onError) {
                this.onError(error);
            }
        };

        this.ws.onclose = (event) => {
            console.log('WebSocket closed:', event.code, event.reason);
            this.connected = false;
        };
    }

    /**
     * Send input to the running command.
     * @param {string} text - Text to send (should include \n for Enter)
     */
    send(text) {
        if (this.ws && this.connected) {
            this.ws.send(JSON.stringify({
                type: 'input',
                text: text
            }));
        }
    }

    /**
     * Send a signal to the running command.
     * @param {string} signal - Signal name (e.g., 'SIGTERM')
     */
    sendSignal(signal) {
        if (this.ws && this.connected) {
            this.ws.send(JSON.stringify({
                type: 'signal',
                signal: signal
            }));
        }
    }

    /**
     * Close the WebSocket connection.
     */
    close() {
        if (this.ws) {
            this.ws.close();
            this.ws = null;
            this.connected = false;
        }
    }

    /**
     * Handle incoming messages.
     */
    handleMessage(msg) {
        if (msg.type === 'complete') {
            this.connected = false;
            if (this.onComplete) {
                this.onComplete(msg);
            }
            // Server will close the connection
        } else if (msg.type === 'output' || msg.type === 'error') {
            if (this.onMessage) {
                this.onMessage(msg);
            }
        }
    }
}
```

**Step 2: Verify file syntax**

No automated test for JS, but verify file is created correctly.

**Step 3: Commit**

```bash
git add xkeen-go/web/static/js/services/interactive.js
git commit -m "feat: add InteractiveSession WebSocket client service"
```

---

## Task 6: Frontend - Update Commands Component

**Files:**
- Modify: `xkeen-go/web/static/js/components/commands.js`

**Note:** This updates ALL commands to use WebSocket. Non-interactive commands work the same way but just don't require user input.

**Step 1: Update imports and add interactive execution**

Replace `xkeen-go/web/static/js/components/commands.js`:

```javascript
// components/commands.js - Commands tab with categorized XKeen commands

import { postStream } from '../services/api.js';
import { InteractiveSession } from '../services/interactive.js';
import { AnsiUp } from 'https://esm.sh/ansi_up@6.0.2';

const ansi_up = new AnsiUp();

function commandsComponent() {
    return {
        // State
        executingCommand: '',
        commandComplete: false,
        session: null,  // InteractiveSession instance
        inputValue: '', // User input for interactive commands

        // Categories with commands and descriptions
        categories: [
            {
                name: 'Управление прокси-клиентом',
                commands: [
                    { name: 'start', description: 'Запуск XKeen' },
                    { name: 'stop', description: 'Остановка XKeen' },
                    { name: 'restart', description: 'Перезапуск XKeen' },
                    { name: 'status', description: 'Статус XKeen' }
                ]
            },
            {
                name: 'Резервная копия XKeen',
                commands: [
                    { name: 'kb', description: 'Создать резервную копию' },
                    { name: 'kbr', description: 'Восстановить из резервной копии' }
                ]
            },
            {
                name: 'Обновление компонентов',
                commands: [
                    { name: 'uk', description: 'Обновить XKeen' },
                    { name: 'ug', description: 'Обновить GeoIP/GeoSite' },
                    { name: 'ux', description: 'Обновить Xray' },
                    { name: 'um', description: 'Обновить модули' }
                ]
            }
        ],

        // Commands that use interactive mode (WebSocket)
        interactiveCommands: ['uk', 'ug', 'ux', 'um'],

        // Dangerous commands that require confirmation
        dangerousCommands: ['stop', 'restart', 'uk', 'ug', 'ux', 'um'],

        executeCommand(command) {
            if (this.isDangerous(command)) {
                const cmdInfo = this.getCommandInfo(command);
                this.$store.app.confirm.description = cmdInfo?.description || `Execute ${command} command`;
                this.$store.app.confirm.onConfirm = () => this.doExecute(command);
                this.$store.app.confirm.show = true;
            } else {
                this.doExecute(command);
            }
        },

        getCommandInfo(name) {
            for (const cat of this.categories) {
                const cmd = cat.commands.find(c => c.name === name);
                if (cmd) return cmd;
            }
            return null;
        },

        isDangerous(command) {
            return this.dangerousCommands.includes(command);
        },

        isInteractive(command) {
            return this.interactiveCommands.includes(command);
        },

        async doExecute(command) {
            this.executingCommand = command;
            this.$store.app.modal.error = '';
            this.$store.app.modal.output = '';
            this.$store.app.modal.command = command;
            this.$store.app.modal.show = true;
            this.commandComplete = false;
            this.inputValue = '';

            try {
                if (this.isInteractive(command)) {
                    await this.executeInteractive(command);
                } else {
                    await this.executeStream(command);
                }
            } catch (err) {
                this.$store.app.modal.error = 'Failed to execute command: ' + err.message;
            } finally {
                this.executingCommand = '';
                this.commandComplete = true;
            }
        },

        async executeStream(command) {
            await postStream('/api/xkeen/command', { command: command }, (msg) => {
                this.handleStreamMessage(msg);
            });
        },

        executeInteractive(command) {
            return new Promise((resolve, reject) => {
                this.session = new InteractiveSession(
                    command,
                    (msg) => this.handleStreamMessage(msg),
                    (msg) => {
                        this.session = null;
                        if (!msg.success && !this.$store.app.modal.error) {
                            this.$store.app.modal.error = `Command failed with exit code ${msg.exitCode}`;
                        }
                        resolve();
                    },
                    (error) => {
                        this.session = null;
                        reject(new Error('WebSocket connection error'));
                    }
                );
                this.session.connect();
            });
        },

        handleStreamMessage(msg) {
            if (msg.type === 'output') {
                const html = ansi_up.ansi_to_html(msg.text, { use_classes: false });
                this.$store.app.modal.output += html + '\n';
                this.scrollToBottom();
            } else if (msg.type === 'error') {
                const html = ansi_up.ansi_to_html(msg.text, { use_classes: false });
                this.$store.app.modal.error += (this.$store.app.modal.error ? '\n' : '') + html;
                this.scrollToBottom();
            } else if (msg.type === 'complete') {
                this.commandComplete = true;
                if (!msg.success && !this.$store.app.modal.error) {
                    this.$store.app.modal.error = `Command failed with exit code ${msg.exitCode}`;
                }
            }
        },

        sendInput() {
            if (this.session && this.inputValue) {
                this.session.send(this.inputValue + '\n');
                this.inputValue = '';
            }
        },

        scrollToBottom() {
            this.$nextTick(() => {
                const outputEl = document.getElementById('modal-output');
                if (outputEl) {
                    outputEl.scrollTop = outputEl.scrollHeight;
                }
            });
        },

        isLoading(command) {
            return this.executingCommand === command;
        },

        canSendInput() {
            return this.session && this.session.connected && !this.commandComplete;
        }
    };
}

// Register with Alpine.js when available
document.addEventListener('alpine:init', () => {
    Alpine.data('commands', commandsComponent);
});
```

**Step 2: Commit**

```bash
git add xkeen-go/web/static/js/components/commands.js
git commit -m "feat: update commands component for interactive mode"
```

---

## Task 7: Frontend - Update Modal UI with Input Field

**Files:**
- Modify: `xkeen-go/web/index.html`

**Step 1: Update the Output Modal**

Find the Output Modal (around line 333) and replace it with:

```html
        <!-- Output Modal -->
        <div class="modal-overlay" x-show="$store.app.modal.show" x-transition @click.self="$store.app.closeModal()">
            <div class="modal">
                <div class="modal-header">
                    <h3>Command Output: <span x-text="$store.app.modal.command"></span></h3>
                    <button class="modal-close" @click="$store.app.closeModal()">&times;</button>
                </div>
                <div class="modal-body">
                    <pre x-show="$store.app.modal.error" class="modal-error" x-html="$store.app.modal.error"></pre>
                    <pre id="modal-output" class="modal-output" x-html="$store.app.modal.output"></pre>
                </div>
                <div class="modal-input" x-show="canSendInput()">
                    <input type="text"
                           x-model="inputValue"
                           @keydown.enter="sendInput()"
                           placeholder="Enter input and press Enter..."
                           class="modal-input-field">
                    <button class="btn btn-primary" @click="sendInput()">Send</button>
                </div>
                <div class="modal-footer">
                    <button class="btn" @click="$store.app.copyModalOutput()">Copy Output</button>
                    <button class="btn btn-primary" @click="$store.app.closeModal()">Close</button>
                </div>
            </div>
        </div>
```

**Step 2: Add CSS for input field**

Add to `xkeen-go/web/static/css/style.css` (find modal styles and add):

```css
.modal-input {
    display: flex;
    gap: 8px;
    padding: 12px 16px;
    border-top: 1px solid var(--border-color);
    background: var(--bg-secondary);
}

.modal-input-field {
    flex: 1;
    padding: 8px 12px;
    border: 1px solid var(--border-color);
    border-radius: 4px;
    background: var(--bg-primary);
    color: var(--text-primary);
    font-family: 'Consolas', 'Monaco', monospace;
    font-size: 14px;
}

.modal-input-field:focus {
    outline: none;
    border-color: var(--primary);
}
```

**Step 3: Commit**

```bash
git add xkeen-go/web/index.html xkeen-go/web/static/css/style.css
git commit -m "feat: add input field to command output modal"
```

---

## Task 8: Cleanup - Remove Old Command Execution

**Files:**
- Modify: `xkeen-go/internal/handlers/commands.go` (remove ExecuteCommand, keep GetCommands)
- Modify: `xkeen-go/internal/server/server.go` (remove route registration)
- Modify: `xkeen-go/web/static/js/components/commands.js` (already done in Task 6)

**Step 1: Remove old ExecuteCommand from commands.go**

Remove from `xkeen-go/internal/handlers/commands.go`:
- `CommandRequest` struct
- `CommandResponse` struct
- `ExecuteCommand` function
- `httpResponseWriter` struct and methods
- `splitCommand` function
- `realStreamExecutor` struct and methods
- `StreamExecutor` interface
- `StreamWriter` interface
- `CommandStartStopTimeout`, `CommandRestartTimeout`, `CommandBackupTimeout`, `CommandUpdateTimeout` constants
- `NewCommandsHandlerWithStreamExecutor` function

Keep:
- `CommandConfig` struct
- `defaultCommands` map
- `CommandInfo` struct
- `CommandsListResponse` struct
- `GetCommands` function
- `NewCommandsHandler` function
- `RegisterCommandsRoutes` function (but remove the command execution route)

**Step 2: Update RegisterCommandsRoutes**

Change in `xkeen-go/internal/handlers/commands.go`:

```go
// RegisterCommandsRoutes registers command-related routes.
func RegisterCommandsRoutes(r *mux.Router, handler *CommandsHandler) {
	r.HandleFunc("/xkeen/commands", handler.GetCommands).Methods("GET")
}
```

**Step 3: Run tests and build**

Run: `cd xkeen-go && go build . && go test ./...`
Expected: PASS, build succeeds

**Step 4: Commit**

```bash
git add xkeen-go/internal/handlers/commands.go xkeen-go/internal/server/server.go
git commit -m "refactor: remove old NDJSON command execution, use WebSocket instead"
```

---

## Task 9: Integration Testing

**Step 1: Build and run locally**

Run: `cd xkeen-go && make build && ./xkeen-go`

**Step 2: Manual testing checklist**

- [ ] Open browser to http://localhost:8089
- [ ] Login
- [ ] Go to Commands tab
- [ ] Click "Run" on `status` command (non-interactive)
- [ ] Verify output appears in modal
- [ ] Click "Execute" on `uk` command (interactive)
- [ ] Verify modal shows with input field
- [ ] Verify WebSocket connects (check browser console)
- [ ] Type a version number and press Enter
- [ ] Verify input is sent and output continues

**Step 3: Run all tests**

Run: `cd xkeen-go && make test`
Expected: All tests pass

**Step 4: Final commit**

```bash
git add -A
git commit -m "feat: complete interactive command execution via WebSocket"
```

---

## Summary

| Task | Description | Files |
|------|-------------|-------|
| 1 | Backend types & constructor | `interactive.go`, `interactive_test.go` |
| 2 | WebSocket message types | `interactive.go`, `interactive_test.go` |
| 3 | ServeHTTP implementation | `interactive.go` |
| 4 | Register route | `interactive.go`, `server.go` |
| 5 | Frontend service | `interactive.js` |
| 6 | Commands component | `commands.js` |
| 7 | Modal UI | `index.html`, `style.css` |
| 8 | Cleanup old code | `commands.go` |
| 9 | Integration testing | Manual |
