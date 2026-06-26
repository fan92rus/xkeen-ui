// Package handlers provides HTTP handlers for XKEEN-UI API endpoints.
package handlers

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

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
	registry        *CommandRegistry // shared with CommandsHandler
	allowedOrigins  map[string]bool
	upgrader        websocket.Upgrader
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
// Goroutines are tracked via WaitGroup and interrupted via context cancellation
// to prevent goroutine leaks after command completion or shutdown.
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

	var wg sync.WaitGroup

	// Read from PTY and send to WebSocket
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[interactive] WebSocket write panic recovered: %v", r)
			}
		}()
		buf := make([]byte, 1024)
		for {
			select {
			case <-ctx.Done():
				return
			default:
				// Use read deadline so we periodically check ctx cancellation
				_ = ptmx.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
				n, err := ptmx.Read(buf)
				if n > 0 {
					_ = conn.WriteJSON(ServerMessage{
						Type: "output",
						Text: string(buf[:n]),
					})
				}
				if err != nil {
					if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
						continue // deadline timeout, re-check ctx
					}
					return // EOF or real error
				}
			}
		}
	}()

	// Read WebSocket messages and write to PTY
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[interactive] WebSocket read panic recovered: %v", r)
			}
		}()
		for {
			select {
			case <-ctx.Done():
				return
			default:
				// Use read deadline so we periodically check ctx cancellation
				_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
				var msg ClientMessage
				if err := conn.ReadJSON(&msg); err != nil {
					if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
						continue // deadline timeout, re-check ctx
					}
					return // connection closed or real error
				}
				switch msg.Type {
				case "input":
					_, _ = ptmx.Write([]byte(msg.Text))
				case "signal":
					if cmd.Process != nil {
						_ = cmd.Process.Signal(syscall.SIGTERM)
					}
					return
				}
			}
		}
	}()

	// Wait for command to complete
	err = cmd.Wait()

	// Cancel context to signal goroutines to stop
	cancel()

	// Wait for goroutines with timeout
	wgDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(wgDone)
	}()

	select {
	case <-wgDone:
		// Goroutines finished cleanly
	case <-time.After(3 * time.Second):
		log.Printf("Interactive: timeout waiting for goroutines to finish for command '%s'", config.Cmd)
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

	// Send completion message (goroutines are done, no race on conn.WriteJSON)
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
