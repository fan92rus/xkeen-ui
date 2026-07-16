// Package handlers provides HTTP handlers for XKEEN-UI API endpoints.
package handlers

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"sync"
	"syscall"

	"github.com/creack/pty"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

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

// InteractiveHandler handles interactive command execution via WebSocket.
type InteractiveHandler struct {
	registry       *CommandRegistry // shared with CommandsHandler
	allowedOrigins map[string]bool
	upgrader       websocket.Upgrader
}

// InteractiveConfig configures the interactive handler.
type InteractiveConfig struct {
	AllowedOrigins []string
}

// NewInteractiveHandler creates a new InteractiveHandler.
// The command whitelist is sourced from the shared *CommandRegistry (runtime
// `xkeen -help`), so the same registry instance should be shared with
// CommandsHandler.
func NewInteractiveHandler(cfg *InteractiveConfig, registry *CommandRegistry) *InteractiveHandler {
	// Build allowed origins map
	allowedOrigins := make(map[string]bool)
	if cfg != nil {
		for _, origin := range cfg.AllowedOrigins {
			allowedOrigins[origin] = true
		}
	}

	h := &InteractiveHandler{
		registry:       registry,
		allowedOrigins: allowedOrigins,
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
	if origin == "" {
		return false
	}

	if h.allowedOrigins[origin] {
		return true
	}

	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return u.Host == r.Host
}

// isCommandAllowed checks if a command is in the whitelist.
func (h *InteractiveHandler) isCommandAllowed(cmd string) (CommandConfig, bool) {
	return h.registry.Get(cmd)
}

// ServeHTTP handles WebSocket connections for interactive command execution.
func (h *InteractiveHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[interactive] WebSocket handler panic recovered: %v", r)
		}
	}()

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	defer func() { _ = conn.Close() }()

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
	_ = conn.WriteJSON(ServerMessage{
		Type: "error",
		Text: text,
	})
	_ = conn.WriteJSON(ServerMessage{
		Type:     "complete",
		Success:  false,
		ExitCode: 1,
	})
}

// executeInteractive runs the command with PTY and handles stdin/stdout.
// Goroutines use BLOCKING reads and are cleaned up by closing ptmx/conn at
// shutdown (see inline comments). This avoids deadline-polling which caused
// a regression where user input was silently dropped.
func (h *InteractiveHandler) executeInteractive(conn *websocket.Conn, config CommandConfig) {
	ctx, cancel := context.WithTimeout(context.Background(), config.Timeout)
	defer cancel()

	// Build command (config.Cmd already includes the dash prefix, e.g. "-start")
	cmdStr := fmt.Sprintf("xkeen %s", config.Cmd)
	parts := strings.Fields(cmdStr)
	if len(parts) == 0 {
		h.sendError(conn, "Failed to parse command")
		return
	}

	//nolint:gosec // command from validated whitelist (CommandRegistry)
	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)

	// Start command with PTY
	ptmx, err := pty.Start(cmd)
	if err != nil {
		h.sendError(conn, "Failed to start PTY: "+err.Error())
		return
	}

	// Set initial PTY size (reasonable default for web UI)
	_ = pty.Setsize(ptmx, &pty.Winsize{
		Cols: 120,
		Rows: 40,
	})

	// Cleanup strategy: goroutines use BLOCKING reads (no deadlines). They are
	// unblocked at shutdown by closing the resource they read from:
	//   - output goroutine reads from ptmx  → closed after cmd.Wait() → EOF
	//   - input  goroutine reads from conn  → closed after 'complete' sent
	// This avoids the deadline-polling pattern that could cause ReadJSON to
	// return a non-timeout error mid-message (killing the input goroutine and
	// silently dropping all subsequent user input).
	var outputWg, inputWg sync.WaitGroup

	// Read from PTY and send to WebSocket (output goroutine)
	outputWg.Add(1)
	go func() {
		defer outputWg.Done()
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[interactive] output goroutine panic recovered: %v", r)
			}
		}()
		buf := make([]byte, 1024)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				if writeErr := conn.WriteJSON(ServerMessage{
					Type: "output",
					Text: string(buf[:n]),
				}); writeErr != nil {
					return // client gone
				}
			}
			if err != nil {
				return // EOF (ptmx closed) or error
			}
		}
	}()

	// Read WebSocket messages and write to PTY (input goroutine)
	inputWg.Add(1)
	go func() {
		defer inputWg.Done()
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[interactive] input goroutine panic recovered: %v", r)
			}
		}()
		for {
			var msg ClientMessage
			if err := conn.ReadJSON(&msg); err != nil {
				return // connection closed or error
			}
			switch msg.Type {
			case "input":
				if _, werr := ptmx.WriteString(msg.Text); werr != nil {
					log.Printf("[interactive] ptmx.Write failed for input %q: %v", msg.Text, werr)
				} else {
					log.Printf("[interactive] wrote %d bytes to PTY: %q", len(msg.Text), msg.Text)
				}
			case "signal":
				if cmd.Process != nil {
					_ = cmd.Process.Signal(syscall.SIGTERM)
				}
				return
			}
		}
	}()

	// Wait for command to complete
	err = cmd.Wait()
	log.Printf("[interactive] command '%s' finished, cleaning up goroutines", config.Cmd)

	// Command done. Unblock the output goroutine by closing ptmx (its Read
	// returns EOF). We do NOT use defer here so the close happens now.
	_ = ptmx.Close()
	outputWg.Wait()

	// Cancel the timeout context (no-op if not expired).
	cancel()

	// Get exit code
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	// Send completion message. Output goroutine is done (ptmx closed), and we
	// haven't closed conn yet, so this write succeeds. The input goroutine may
	// still be blocked on conn.ReadJSON — that's fine, it's reading, not writing.
	_ = conn.WriteJSON(ServerMessage{
		Type:     "complete",
		Success:  exitCode == 0,
		ExitCode: exitCode,
	})

	// Now close conn to unblock the input goroutine's ReadJSON (returns error).
	_ = conn.Close()
	inputWg.Wait()

	log.Printf("[interactive] command '%s' completed with exit code %d", config.Cmd, exitCode)
}

// RegisterInteractiveWSRoute registers the WebSocket route for interactive commands.
func RegisterInteractiveWSRoute(r *mux.Router, handler *InteractiveHandler, authMiddleware func(http.Handler) http.Handler) {
	wsRouter := r.PathPrefix("/ws").Subrouter()
	wsRouter.Use(authMiddleware)
	wsRouter.Handle("/xkeen/interactive", handler).Methods("GET")
}
