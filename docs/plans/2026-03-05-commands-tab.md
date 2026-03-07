# Commands Tab Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a "Commands" tab to XKEEN-GO web UI for executing XKeen CLI commands (start/stop/restart/status, backup, updates).

**Architecture:** New `CommandsHandler` in Go backend with whitelisted commands, new Alpine.js component for frontend, modal dialog for output display, confirmation for dangerous commands.

**Tech Stack:** Go 1.21+, gorilla/mux, Alpine.js, existing patterns from ServiceHandler

---

## Task 1: Backend - Commands Handler

**Files:**
- Create: `xkeen-go/internal/handlers/commands.go`
- Test: `xkeen-go/internal/handlers/commands_test.go`

### Step 1.1: Write the failing test for command execution

```go
// xkeen-go/internal/handlers/commands_test.go
package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Mock executor for testing
type mockCommandExecutor struct {
	output string
	err    error
}

func (m *mockCommandExecutor) Execute(ctx context.Context, name string, args ...string) (string, error) {
	return m.output, m.err
}

func TestCommandsHandler_ExecuteCommand(t *testing.T) {
	tests := []struct {
		name           string
		command        string
		mockOutput     string
		mockErr        error
		expectedStatus int
		expectedInBody string
	}{
		{
			name:           "status command success",
			command:        "status",
			mockOutput:     "XKeen is running (PID: 1234)",
			expectedStatus: http.StatusOK,
			expectedInBody: `"success":true`,
		},
		{
			name:           "unknown command fails",
			command:        "unknown",
			expectedStatus: http.StatusBadRequest,
			expectedInBody: `"success":false`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockCommandExecutor{output: tt.mockOutput, err: tt.mockErr}
			handler := NewCommandsHandlerWithExecutor(mock)

			req := httptest.NewRequest("POST", "/api/xkeen/command",
				strings.NewReader(`{"command":"`+tt.command+`"}`))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			handler.ExecuteCommand(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.expectedStatus)
			}
			if !strings.Contains(rec.Body.String(), tt.expectedInBody) {
				t.Errorf("body = %s, want to contain %s", rec.Body.String(), tt.expectedInBody)
			}
		})
	}
}
```

### Step 1.2: Run test to verify it fails

Run: `cd xkeen-go && go test -v ./internal/handlers/... -run TestCommandsHandler`
Expected: FAIL - undefined: NewCommandsHandlerWithExecutor

### Step 1.3: Write minimal implementation

```go
// xkeen-go/internal/handlers/commands.go
package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// CommandConfig holds configuration for a whitelisted command.
type CommandConfig struct {
	Cmd         string
	Description string
	Dangerous   bool
	Timeout     time.Duration
}

// CommandsHandler handles XKeen CLI command execution.
type CommandsHandler struct {
	mu              sync.RWMutex
	allowedCommands map[string]CommandConfig
	executor        CommandExecutor
}

// NewCommandsHandler creates a new CommandsHandler.
func NewCommandsHandler() *CommandsHandler {
	return &CommandsHandler{
		allowedCommands: map[string]CommandConfig{
			// Proxy client management
			"start":   {Cmd: "xkeen -start", Description: "Запуск прокси-клиента", Dangerous: false, Timeout: 30 * time.Second},
			"stop":    {Cmd: "xkeen -stop", Description: "Остановка прокси-клиента", Dangerous: false, Timeout: 30 * time.Second},
			"restart": {Cmd: "xkeen -restart", Description: "Перезапуск прокси-клиента", Dangerous: false, Timeout: 45 * time.Second},
			"status":  {Cmd: "xkeen -status", Description: "Статус прокси-клиента", Dangerous: false, Timeout: 10 * time.Second},

			// Backup
			"kb":  {Cmd: "xkeen -kb", Description: "Создать резервную копию XKeen", Dangerous: false, Timeout: 60 * time.Second},
			"kbr": {Cmd: "xkeen -kbr", Description: "Восстановить из резервной копии", Dangerous: true, Timeout: 60 * time.Second},

			// Updates
			"uk": {Cmd: "xkeen -uk", Description: "Обновить XKeen", Dangerous: false, Timeout: 120 * time.Second},
			"ug": {Cmd: "xkeen -ug", Description: "Обновить GeoFile", Dangerous: false, Timeout: 60 * time.Second},
			"ux": {Cmd: "xkeen -ux", Description: "Обновить Xray", Dangerous: false, Timeout: 120 * time.Second},
			"um": {Cmd: "xkeen -um", Description: "Обновить Mihomo", Dangerous: false, Timeout: 120 * time.Second},
		},
		executor: &realExecutor{},
	}
}

// NewCommandsHandlerWithExecutor creates a CommandsHandler with custom executor for testing.
func NewCommandsHandlerWithExecutor(executor CommandExecutor) *CommandsHandler {
	h := NewCommandsHandler()
	h.executor = executor
	return h
}

// CommandRequest is the request body for command execution.
type CommandRequest struct {
	Command string `json:"command"`
}

// CommandResponse is the response for command execution.
type CommandResponse struct {
	Success     bool   `json:"success"`
	Output      string `json:"output,omitempty"`
	Command     string `json:"command,omitempty"`
	Description string `json:"description,omitempty"`
	Dangerous   bool   `json:"dangerous,omitempty"`
	Error       string `json:"error,omitempty"`
}

// ExecuteCommand executes a whitelisted XKeen command.
// POST /api/xkeen/command
func (h *CommandsHandler) ExecuteCommand(w http.ResponseWriter, r *http.Request) {
	var req CommandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondJSON(w, http.StatusBadRequest, CommandResponse{
			Success: false,
			Error:   "Invalid request body",
		})
		return
	}

	cmdConfig, exists := h.allowedCommands[req.Command]
	if !exists {
		h.respondJSON(w, http.StatusBadRequest, CommandResponse{
			Success: false,
			Error:   fmt.Sprintf("Unknown command: %s", req.Command),
		})
		return
	}

	// Execute with timeout
	ctx, cancel := context.WithTimeout(r.Context(), cmdConfig.Timeout)
	defer cancel()

	parts := splitCommand(cmdConfig.Cmd)
	output, err := h.executor.Execute(ctx, parts[0], parts[1:]...)

	if err != nil {
		log.Printf("Command %s failed: %v, output: %s", req.Command, err, output)
		h.respondJSON(w, http.StatusInternalServerError, CommandResponse{
			Success: false,
			Output:  output,
			Command: req.Command,
			Error:   err.Error(),
		})
		return
	}

	log.Printf("Command %s completed: %s", req.Command, output)
	h.respondJSON(w, http.StatusOK, CommandResponse{
		Success:     true,
		Output:      output,
		Command:     req.Command,
		Description: cmdConfig.Description,
		Dangerous:   cmdConfig.Dangerous,
	})
}

// GetCommands returns list of available commands.
// GET /api/xkeen/commands
func (h *CommandsHandler) GetCommands(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	commands := make([]map[string]interface{}, 0, len(h.allowedCommands))
	for name, config := range h.allowedCommands {
		commands = append(commands, map[string]interface{}{
			"name":        name,
			"description": config.Description,
			"dangerous":   config.Dangerous,
		})
	}

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"commands": commands,
	})
}

// RegisterCommandsRoutes registers command-related routes.
func RegisterCommandsRoutes(r *mux.Router, handler *CommandsHandler) {
	r.HandleFunc("/xkeen/command", handler.ExecuteCommand).Methods("POST")
	r.HandleFunc("/xkeen/commands", handler.GetCommands).Methods("GET")
}

// respondJSON writes JSON response.
func (h *CommandsHandler) respondJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("Error encoding JSON response: %v", err)
	}
}

// splitCommand safely splits command string into parts.
func splitCommand(cmd string) []string {
	parts := make([]string, 0)
	for _, p := range splitOnSpaces(cmd) {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

func splitOnSpaces(s string) []string {
	var result []string
	var current strings.Builder
	inQuote := false

	for _, r := range s {
		switch r {
		case '"':
			inQuote = !inQuote
		case ' ':
			if !inQuote {
				result = append(result, current.String())
				current.Reset()
			} else {
				current.WriteRune(r)
			}
		default:
			current.WriteRune(r)
		}
	}
	result = append(result, current.String())
	return result
}
```

### Step 1.4: Run test to verify it passes

Run: `cd xkeen-go && go test -v ./internal/handlers/... -run TestCommandsHandler`
Expected: PASS

### Step 1.5: Commit

```bash
cd xkeen-go && git add internal/handlers/commands.go internal/handlers/commands_test.go
git commit -m "$(cat <<'EOF'
feat(handlers): add CommandsHandler for XKeen CLI commands

Add new handler for executing whitelisted XKeen commands:
- Proxy client management (start/stop/restart/status)
- Backup operations (kb/kbr)
- Updates (uk/ug/ux/um)

Includes timeout handling and dangerous command flag.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: Backend - Register Routes

**Files:**
- Modify: `xkeen-go/internal/server/server.go:40-42, 221-225`

### Step 2.1: Add handler field to Server struct

```go
// In Server struct, add after line 41 (settingsHandler):
	commandsHandler  *handlers.CommandsHandler
```

### Step 2.2: Initialize handler in NewServer

```go
// In NewServer, add after line 103 (settingsHandler init):
	s.commandsHandler = handlers.NewCommandsHandler()
```

### Step 2.3: Register routes in setupRoutes

```go
// In setupRoutes, add after line 225 (RegisterSettingsRoutes):
	handlers.RegisterCommandsRoutes(apiRouter, s.commandsHandler)
```

### Step 2.4: Run tests to verify nothing broke

Run: `cd xkeen-go && go test -v ./...`
Expected: PASS

### Step 2.5: Commit

```bash
cd xkeen-go && git add internal/server/server.go
git commit -m "$(cat <<'EOF'
feat(server): register CommandsHandler routes

Add /api/xkeen/command and /api/xkeen/commands endpoints.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Frontend - Alpine Component

**Files:**
- Create: `xkeen-go/web/static/js/components/commands.js`

### Step 3.1: Create commands component

```javascript
// xkeen-go/web/static/js/components/commands.js
document.addEventListener('alpine:init', () => {
    Alpine.data('commands', () => ({
        commands: [],
        loading: false,
        executingCommand: null,

        // Modal state
        showModal: false,
        modalOutput: '',
        modalCommand: '',
        modalError: false,

        // Confirmation dialog
        showConfirm: false,
        confirmCommand: null,
        confirmDescription: '',

        // Categories for UI grouping
        categories: [
            {
                name: 'Управление прокси-клиентом',
                commands: ['start', 'stop', 'restart', 'status']
            },
            {
                name: 'Резервная копия XKeen',
                commands: ['kb', 'kbr']
            },
            {
                name: 'Обновление компонентов',
                commands: ['uk', 'ug', 'ux', 'um']
            }
        ],

        init() {
            this.loadCommands()
        },

        async loadCommands() {
            try {
                const res = await fetch('/api/xkeen/commands', {
                    headers: { 'X-CSRF-Token': this.$store.app.csrfToken }
                })
                const data = await res.json()
                if (data.success) {
                    this.commands = data.commands
                }
            } catch (err) {
                this.$store.app.showToast('Failed to load commands', 'error')
            }
        },

        getCommandInfo(name) {
            return this.commands.find(c => c.name === name) || { name, description: name, dangerous: false }
        },

        async executeCommand(command) {
            const cmdInfo = this.getCommandInfo(command)

            // Show confirmation for dangerous commands
            if (cmdInfo.dangerous) {
                this.confirmCommand = command
                this.confirmDescription = cmdInfo.description
                this.showConfirm = true
                return
            }

            await this.doExecute(command)
        },

        async confirmExecute() {
            this.showConfirm = false
            if (this.confirmCommand) {
                await this.doExecute(this.confirmCommand)
                this.confirmCommand = null
            }
        },

        cancelConfirm() {
            this.showConfirm = false
            this.confirmCommand = null
        },

        async doExecute(command) {
            this.loading = true
            this.executingCommand = command

            try {
                const res = await fetch('/api/xkeen/command', {
                    method: 'POST',
                    headers: {
                        'Content-Type': 'application/json',
                        'X-CSRF-Token': this.$store.app.csrfToken
                    },
                    body: JSON.stringify({ command })
                })

                const data = await res.json()

                this.modalCommand = command
                this.modalOutput = data.output || data.error || 'No output'
                this.modalError = !data.success
                this.showModal = true

                if (data.success) {
                    // Refresh service status if it was a control command
                    if (['start', 'stop', 'restart'].includes(command)) {
                        setTimeout(() => this.$store.app.loadServiceStatus?.(), 1000)
                    }
                }
            } catch (err) {
                this.modalCommand = command
                this.modalOutput = 'Request failed: ' + err.message
                this.modalError = true
                this.showModal = true
            } finally {
                this.loading = false
                this.executingCommand = null
            }
        },

        closeModal() {
            this.showModal = false
        },

        copyOutput() {
            navigator.clipboard.writeText(this.modalOutput)
                .then(() => this.$store.app.showToast('Скопировано', 'success'))
                .catch(() => this.$store.app.showToast('Ошибка копирования', 'error'))
        },

        isLoading(command) {
            return this.loading && this.executingCommand === command
        }
    }))
})
```

### Step 3.2: Commit

```bash
cd xkeen-go && git add web/static/js/components/commands.js
git commit -m "$(cat <<'EOF'
feat(web): add Alpine component for commands tab

Add commands.js with:
- Command categories grouping
- Execute command with loading state
- Modal for output display
- Confirmation for dangerous commands
- Copy to clipboard functionality

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: Frontend - HTML Tab

**Files:**
- Modify: `xkeen-go/web/index.html:36-46, 168-169`
- Modify: `xkeen-go/web/static/js/app.js` (add commands.js import)

### Step 4.1: Add Commands tab button

In `index.html`, after line 45 (Settings tab button), add:

```html
            <button class="tab"
                    :class="{ 'active': $store.app.activeTab === 'commands' }"
                    @click="$store.app.activeTab = 'commands'">Commands</button>
```

### Step 4.2: Add Commands tab content

In `index.html`, after line 168 (end of Settings section), add:

```html

            <!-- Commands Tab -->
            <section x-show="$store.app.activeTab === 'commands'"
                     x-data="commands"
                     x-cloak
                     class="tab-content"
                     :class="{ 'active': $store.app.activeTab === 'commands' }">
                <div class="commands-container">
                    <template x-for="category in categories" :key="category.name">
                        <div class="commands-section">
                            <h3 x-text="category.name"
                                @click="$el.nextElementSibling.classList.toggle('collapsed')"
                                class="section-header"></h3>
                            <div class="commands-grid">
                                <template x-for="cmdName in category.commands" :key="cmdName">
                                    <button class="btn cmd-btn"
                                            :class="{ 'btn-danger': getCommandInfo(cmdName).dangerous, 'loading': isLoading(cmdName) }"
                                            @click="executeCommand(cmdName)"
                                            :disabled="loading">
                                        <span x-text="getCommandInfo(cmdName).description || cmdName"></span>
                                        <span x-show="getCommandInfo(cmdName).dangerous" class="danger-badge">!</span>
                                    </button>
                                </template>
                            </div>
                        </div>
                    </template>
                </div>

                <!-- Output Modal -->
                <div class="modal-overlay" x-show="showModal" x-transition @click.self="closeModal">
                    <div class="modal">
                        <div class="modal-header">
                            <h3 x-text="'Результат: ' + modalCommand"></h3>
                            <button class="modal-close" @click="closeModal">&times;</button>
                        </div>
                        <div class="modal-body">
                            <pre :class="{ 'error': modalError }" x-text="modalOutput"></pre>
                        </div>
                        <div class="modal-footer">
                            <button class="btn" @click="copyOutput">Копировать</button>
                            <button class="btn btn-primary" @click="closeModal">OK</button>
                        </div>
                    </div>
                </div>

                <!-- Confirmation Dialog -->
                <div class="modal-overlay" x-show="showConfirm" x-transition>
                    <div class="modal modal-confirm">
                        <div class="modal-header">
                            <h3>Подтверждение</h3>
                        </div>
                        <div class="modal-body">
                            <p>Вы уверены, что хотите выполнить:</p>
                            <p><strong x-text="confirmDescription"></strong>?</p>
                            <p class="warning">Это действие может быть опасным.</p>
                        </div>
                        <div class="modal-footer">
                            <button class="btn" @click="cancelConfirm">Отмена</button>
                            <button class="btn btn-danger" @click="confirmExecute">Выполнить</button>
                        </div>
                    </div>
                </div>
            </section>
```

### Step 4.3: Add commands.js script tag

In `index.html`, after line 191 (app.js script), add:

```html
    <script src="/static/js/components/commands.js"></script>
```

### Step 4.4: Commit

```bash
cd xkeen-go && git add web/index.html
git commit -m "$(cat <<'EOF'
feat(web): add Commands tab to UI

Add new Commands tab with:
- Collapsible command categories
- Modal for command output
- Confirmation dialog for dangerous commands

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: Frontend - Styles

**Files:**
- Modify: `xkeen-go/web/static/css/style.css`

### Step 5.1: Add command styles

Add at end of `style.css`:

```css
/* Commands Tab Styles */
.commands-container {
    padding: 1rem;
    max-width: 800px;
    margin: 0 auto;
}

.commands-section {
    margin-bottom: 1.5rem;
    background: var(--bg-secondary, #1e1e1e);
    border-radius: 8px;
    overflow: hidden;
}

.section-header {
    padding: 0.75rem 1rem;
    margin: 0;
    background: var(--bg-tertiary, #2d2d2d);
    cursor: pointer;
    user-select: none;
    font-size: 1rem;
    font-weight: 600;
}

.section-header:hover {
    background: var(--bg-hover, #363636);
}

.commands-grid {
    display: flex;
    flex-wrap: wrap;
    gap: 0.5rem;
    padding: 1rem;
}

.commands-grid.collapsed {
    display: none;
}

.cmd-btn {
    min-width: 140px;
    position: relative;
}

.cmd-btn.loading {
    opacity: 0.7;
    pointer-events: none;
}

.cmd-btn.loading::after {
    content: '';
    position: absolute;
    width: 16px;
    height: 16px;
    border: 2px solid transparent;
    border-top-color: currentColor;
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
    margin-left: 8px;
}

.danger-badge {
    background: #e74c3c;
    color: white;
    font-size: 10px;
    padding: 2px 6px;
    border-radius: 4px;
    margin-left: 6px;
}

/* Modal Styles */
.modal-overlay {
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    background: rgba(0, 0, 0, 0.7);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 1000;
}

.modal {
    background: var(--bg-primary, #252526);
    border-radius: 8px;
    max-width: 600px;
    width: 90%;
    max-height: 80vh;
    display: flex;
    flex-direction: column;
    box-shadow: 0 4px 20px rgba(0, 0, 0, 0.5);
}

.modal-confirm {
    max-width: 400px;
}

.modal-header {
    padding: 1rem;
    border-bottom: 1px solid var(--border-color, #3c3c3c);
    display: flex;
    justify-content: space-between;
    align-items: center;
}

.modal-header h3 {
    margin: 0;
    font-size: 1.1rem;
}

.modal-close {
    background: none;
    border: none;
    font-size: 1.5rem;
    cursor: pointer;
    color: var(--text-muted, #888);
    padding: 0;
    line-height: 1;
}

.modal-close:hover {
    color: var(--text-primary, #fff);
}

.modal-body {
    padding: 1rem;
    overflow-y: auto;
    flex: 1;
}

.modal-body pre {
    background: var(--bg-secondary, #1e1e1e);
    padding: 1rem;
    border-radius: 4px;
    overflow-x: auto;
    white-space: pre-wrap;
    word-break: break-word;
    margin: 0;
    font-family: 'Consolas', 'Monaco', monospace;
    font-size: 0.9rem;
    color: var(--text-primary, #d4d4d4);
}

.modal-body pre.error {
    color: #e74c3c;
    border-left: 3px solid #e74c3c;
}

.modal-body .warning {
    color: #f39c12;
    font-size: 0.9rem;
}

.modal-footer {
    padding: 1rem;
    border-top: 1px solid var(--border-color, #3c3c3c);
    display: flex;
    justify-content: flex-end;
    gap: 0.5rem;
}

@keyframes spin {
    to { transform: rotate(360deg); }
}

/* Button danger style */
.btn-danger {
    background: #c0392b;
    color: white;
}

.btn-danger:hover {
    background: #e74c3c;
}

/* Dark theme variables (if not already present) */
:root {
    --bg-primary: #252526;
    --bg-secondary: #1e1e1e;
    --bg-tertiary: #2d2d2d;
    --bg-hover: #363636;
    --text-primary: #d4d4d4;
    --text-muted: #888;
    --border-color: #3c3c3c;
}
```

### Step 5.2: Commit

```bash
cd xkeen-go && git add web/static/css/style.css
git commit -m "$(cat <<'EOF'
style(web): add styles for Commands tab

Add CSS for:
- Command categories and grid layout
- Modal dialogs (output and confirmation)
- Loading spinner animation
- Danger button and badge styles

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: Integration Test

**Files:**
- Modify: `xkeen-go/internal/handlers/commands_test.go` (add integration test)

### Step 6.1: Add integration test for routes

```go
// Add to commands_test.go

func TestCommandsHandler_GetCommands(t *testing.T) {
	handler := NewCommandsHandler()

	req := httptest.NewRequest("GET", "/api/xkeen/commands", nil)
	rec := httptest.NewRecorder()

	handler.GetCommands(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !resp["success"].(bool) {
		t.Error("expected success to be true")
	}

	commands := resp["commands"].([]interface{})
	if len(commands) == 0 {
		t.Error("expected some commands to be returned")
	}
}
```

### Step 6.2: Run all tests

Run: `cd xkeen-go && go test -v ./...`
Expected: PASS

### Step 6.3: Build and verify

Run: `cd xkeen-go && go build -o build/xkeen-go .`
Expected: Build success, no errors

### Step 6.4: Commit

```bash
cd xkeen-go && git add internal/handlers/commands_test.go
git commit -m "$(cat <<'EOF'
test(handlers): add GetCommands integration test

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>
EOF
)"
```

---

## Summary

| Task | Description | Files |
|------|-------------|-------|
| 1 | Backend CommandsHandler | `internal/handlers/commands.go`, `commands_test.go` |
| 2 | Register routes | `internal/server/server.go` |
| 3 | Alpine component | `web/static/js/components/commands.js` |
| 4 | HTML tab | `web/index.html` |
| 5 | CSS styles | `web/static/css/style.css` |
| 6 | Integration tests | `commands_test.go` |

**Total commits:** 6
