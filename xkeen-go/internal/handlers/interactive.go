// Package handlers provides HTTP handlers for XKEEN-UI API endpoints.
package handlers

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
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
	mu              sync.RWMutex
	allowedCommands map[string]CommandConfig // imported from commands.go
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

// executeInteractive runs the command with PTY and handles stdin/stdout.
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

	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)

	// Start command with PTY
	ptmx, err := pty.Start(cmd)
	if err != nil {
		h.sendError(conn, "Failed to start PTY: "+err.Error())
		return
	}
	defer func() { _ = ptmx.Close() }()

	// Set initial PTY size (reasonable default for web UI)
	_ = pty.Setsize(ptmx, &pty.Winsize{
		Cols: 120,
		Rows: 40,
	})

	// Read from PTY and send to WebSocket
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				// Send output immediately, even partial lines
				if writeErr := conn.WriteJSON(ServerMessage{
					Type: "output",
					Text: string(buf[:n]),
				}); writeErr != nil {
					log.Printf("WebSocket write error: %v", writeErr)
					return
				}
			}
			if err != nil {
				if err != io.EOF {
					log.Printf("PTY read error: %v", err)
				}
				return
			}
		}
	}()

	// Read WebSocket messages and write to PTY
	go func() {
		for {
			var msg ClientMessage
			if err := conn.ReadJSON(&msg); err != nil {
				return // Connection closed or error
			}
			if msg.Type == "input" {
				_, _ = ptmx.Write([]byte(msg.Text))
			} else if msg.Type == "signal" {
				// Handle signal (e.g., terminate)
				if cmd.Process != nil {
					_ = cmd.Process.Signal(syscall.SIGTERM)
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
	_ = conn.WriteJSON(ServerMessage{
		Type:     "complete",
		Success:  exitCode == 0,
		ExitCode: exitCode,
	})

	log.Printf("Interactive command '%s' completed with exit code %d", config.Cmd, exitCode)
}

// RegisterInteractiveWSRoute registers the WebSocket route for interactive commands.
func RegisterInteractiveWSRoute(r *mux.Router, handler *InteractiveHandler, authMiddleware func(http.Handler) http.Handler) {
	wsRouter := r.PathPrefix("/ws").Subrouter()
	wsRouter.Use(authMiddleware)
	wsRouter.Handle("/xkeen/interactive", handler).Methods("GET")
}
