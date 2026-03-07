// Package handlers provides HTTP handlers for XKEEN-GO API endpoints.
package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
)

// Timeout constants for XKeen CLI commands.
const (
	// CommandStatusTimeout is the timeout for status check operations.
	CommandStatusTimeout = 10 * time.Second
	// CommandStartStopTimeout is the timeout for start/stop operations.
	CommandStartStopTimeout = 30 * time.Second
	// CommandRestartTimeout is the timeout for restart operations.
	CommandRestartTimeout = 45 * time.Second
	// CommandBackupTimeout is the timeout for backup operations.
	CommandBackupTimeout = 60 * time.Second
	// CommandUpdateTimeout is the timeout for update operations.
	CommandUpdateTimeout = 120 * time.Second
)

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
				Text: fmt.Sprintf("Command timed out"),
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

// CommandConfig holds configuration for a whitelisted command.
type CommandConfig struct {
	Cmd         string        // The command flag (e.g., "start", "kb")
	Description string        // Human-readable description
	Dangerous   bool          // Whether this command is dangerous/destructive
	Timeout     time.Duration // Timeout for this command
}

// defaultCommands is the default set of whitelisted XKeen commands.
var defaultCommands = map[string]CommandConfig{
	// Proxy client management
	"start": {
		Cmd:         "start",
		Description: "Start XKeen proxy client",
		Dangerous:   false,
		Timeout:     CommandStartStopTimeout,
	},
	"stop": {
		Cmd:         "stop",
		Description: "Stop XKeen proxy client",
		Dangerous:   false,
		Timeout:     CommandStartStopTimeout,
	},
	"restart": {
		Cmd:         "restart",
		Description: "Restart XKeen proxy client",
		Dangerous:   false,
		Timeout:     CommandRestartTimeout,
	},
	"status": {
		Cmd:         "status",
		Description: "Check XKeen proxy client status",
		Dangerous:   false,
		Timeout:     CommandStatusTimeout,
	},
	// Backup operations
	"kb": {
		Cmd:         "kb",
		Description: "Create backup of XKeen configuration",
		Dangerous:   false,
		Timeout:     CommandBackupTimeout,
	},
	"kbr": {
		Cmd:         "kbr",
		Description: "Create backup and reset configuration (DANGEROUS)",
		Dangerous:   true,
		Timeout:     CommandBackupTimeout,
	},
	// Update operations
	"uk": {
		Cmd:         "uk",
		Description: "Update XKEEN",
		Dangerous:   false,
		Timeout:     CommandUpdateTimeout,
	},
	"ug": {
		Cmd:         "ug",
		Description: "Update geodata",
		Dangerous:   false,
		Timeout:     CommandUpdateTimeout,
	},
	"ux": {
		Cmd:         "ux",
		Description: "Update Xray core",
		Dangerous:   false,
		Timeout:     CommandUpdateTimeout,
	},
	"um": {
		Cmd:         "um",
		Description: "Update all modules",
		Dangerous:   false,
		Timeout:     CommandUpdateTimeout,
	},
}

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

// CommandsHandler handles XKeen CLI command execution.
type CommandsHandler struct {
	mu              sync.RWMutex
	allowedCommands map[string]CommandConfig
	executor        CommandExecutor   // Keep for backward compatibility
	streamExecutor  StreamExecutor    // For streaming execution
}

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

// CommandRequest represents a request to execute a command.
type CommandRequest struct {
	Command string `json:"command"`
}

// CommandResponse represents the response from a command execution.
type CommandResponse struct {
	Success   bool   `json:"success"`
	Message   string `json:"message"`
	Output    string `json:"output,omitempty"`
	Dangerous bool   `json:"dangerous,omitempty"`
}

// CommandInfo represents information about an available command.
type CommandInfo struct {
	Cmd         string `json:"cmd"`
	Description string `json:"description"`
	Dangerous   bool   `json:"dangerous"`
}

// CommandsListResponse represents the response listing available commands.
type CommandsListResponse struct {
	Commands []CommandInfo `json:"commands"`
}

// splitCommand safely splits a command string into parts.
func splitCommand(cmd string) []string {
	return strings.Fields(cmd)
}

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

// GetCommands returns the list of available commands.
// GET /api/xkeen/commands
func (h *CommandsHandler) GetCommands(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	commands := make([]CommandInfo, 0, len(h.allowedCommands))

	for _, config := range h.allowedCommands {
		commands = append(commands, CommandInfo{
			Cmd:         config.Cmd,
			Description: config.Description,
			Dangerous:   config.Dangerous,
		})
	}

	h.respondJSON(w, http.StatusOK, CommandsListResponse{
		Commands: commands,
	})
}

// RegisterCommandsRoutes registers command-related routes.
func RegisterCommandsRoutes(r *mux.Router, handler *CommandsHandler) {
	r.HandleFunc("/xkeen/command", handler.ExecuteCommand).Methods("POST")
	r.HandleFunc("/xkeen/commands", handler.GetCommands).Methods("GET")
}

// respondJSON writes a JSON response.
func (h *CommandsHandler) respondJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("Error encoding JSON response: %v", err)
	}
}
