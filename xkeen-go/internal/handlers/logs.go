// Package handlers provides HTTP handlers for XKEEN-GO API endpoints.
package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"

	"github.com/user/xkeen-ui/internal/utils"
)

// Default log files to watch
var defaultLogFiles = []string{
	"/opt/var/log/xray/access.log",
	"/opt/var/log/xray/error.log",
	"/opt/var/log/mihomo/access.log",
	"/opt/var/log/mihomo/error.log",
}

// LogsHandler handles log-related operations.
type LogsHandler struct {
	validator      *utils.PathValidator
	logFiles       []string
	clients        map[*websocket.Conn]bool
	clientsMu      sync.RWMutex
	broadcast      chan LogMessage
	allowedOrigins map[string]bool
	upgrader       websocket.Upgrader

	// For graceful shutdown
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// LogMessage represents a log entry.
type LogMessage struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Message   string `json:"message"`
	File      string `json:"file"`
}

// LogsConfig configures the logs handler.
type LogsConfig struct {
	AllowedRoots   []string
	LogFiles       []string
	AllowedOrigins []string
}

// NewLogsHandler creates a new LogsHandler.
func NewLogsHandler(cfg LogsConfig) *LogsHandler {
	validator, err := utils.NewPathValidator(cfg.AllowedRoots)
	if err != nil {
		log.Printf("Warning: failed to create path validator: %v", err)
	}

	// Build allowed origins map
	allowedOrigins := make(map[string]bool)
	for _, origin := range cfg.AllowedOrigins {
		allowedOrigins[origin] = true
	}

	// Use default log files if none specified
	logFiles := cfg.LogFiles
	if len(logFiles) == 0 {
		logFiles = defaultLogFiles
	}

	ctx, cancel := context.WithCancel(context.Background())

	h := &LogsHandler{
		validator:      validator,
		logFiles:       logFiles,
		clients:        make(map[*websocket.Conn]bool),
		broadcast:      make(chan LogMessage, 100),
		allowedOrigins: allowedOrigins,
		ctx:            ctx,
		cancel:         cancel,
	}

	// Create upgrader with origin check
	h.upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     h.checkOrigin,
	}

	// Start broadcast goroutine
	h.wg.Add(1)
	go h.runBroadcast()

	// Start tailing log files
	for _, file := range logFiles {
		h.wg.Add(1)
		go h.tailFile(file)
	}

	return h
}

// Close gracefully stops all goroutines.
func (h *LogsHandler) Close() {
	h.cancel()
	h.wg.Wait()
	close(h.broadcast)
}

// checkOrigin validates the origin of WebSocket connections.
func (h *LogsHandler) checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	host := r.Host

	if origin == "" {
		// Allow connections without Origin header (same-origin browser requests)
		return true
	}

	// Check against allowed origins
	if h.allowedOrigins[origin] {
		return true
	}

	// Allow same-origin requests
	if origin == "http://"+host || origin == "https://"+host {
		return true
	}

	log.Printf("WebSocket connection rejected from origin: %s (host: %s)", origin, host)
	return false
}

// runBroadcast handles broadcasting messages to all connected clients.
func (h *LogsHandler) runBroadcast() {
	defer h.wg.Done()

	for msg := range h.broadcast {
		h.clientsMu.RLock()
		// Collect dead clients to remove
		var deadClients []*websocket.Conn
		for client := range h.clients {
			err := client.WriteJSON(msg)
			if err != nil {
				deadClients = append(deadClients, client)
			}
		}
		h.clientsMu.RUnlock()

		// Remove dead clients
		if len(deadClients) > 0 {
			h.clientsMu.Lock()
			for _, client := range deadClients {
				client.Close()
				delete(h.clients, client)
			}
			h.clientsMu.Unlock()
		}
	}
}

// tailFile runs tail -F on a log file and sends new lines to broadcast.
// Uses -F (follow with retry) instead of -f to handle log rotation.
// tail -F will wait for file to appear if it doesn't exist yet.
func (h *LogsHandler) tailFile(path string) {
	defer h.wg.Done()

	log.Printf("Starting tail -F on: %s", path)

	for {
		select {
		case <-h.ctx.Done():
			return
		default:
		}

		// Run tail -F (follow with retry - handles log rotation and missing files)
		cmd := exec.CommandContext(h.ctx, "tail", "-F", path)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			log.Printf("Failed to create pipe for tail: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}

		if err := cmd.Start(); err != nil {
			log.Printf("Failed to start tail: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}

		// Read lines from tail output
		scanner := bufio.NewReader(stdout)
		for {
			select {
			case <-h.ctx.Done():
				cmd.Process.Kill()
				cmd.Wait()
				return
			default:
			}

			line, err := scanner.ReadString('\n')
			if err != nil {
				if err != io.EOF && h.ctx.Err() == nil {
					log.Printf("Tail read error for %s: %v", path, err)
				}
				break
			}

			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			// Parse and send to broadcast channel (non-blocking)
			msg := h.parseLogLine(line, path)
			select {
			case h.broadcast <- msg:
				log.Printf("[LOG] %s: %s", filepath.Base(path), truncate(line, 50))
			default:
				// Channel full, skip message
				log.Printf("Broadcast channel full, dropping message from %s", path)
			}
		}

		// Wait for command to finish and restart
		cmd.Wait()

		if h.ctx.Err() == nil {
			log.Printf("Tail process ended for %s, restarting in 2s...", path)
			time.Sleep(2 * time.Second)
		}
	}
}

// ReadLogs reads recent log entries from a file.
// GET /api/logs/xray?path=/opt/var/log/xray/access.log&lines=100
func (h *LogsHandler) ReadLogs(w http.ResponseWriter, r *http.Request) {
	logPath := r.URL.Query().Get("path")
	if logPath == "" {
		logPath = "/opt/var/log/xray/access.log"
	}

	// Validate path
	if h.validator == nil {
		h.respondError(w, http.StatusInternalServerError, "Path validator not initialized")
		return
	}

	cleanPath, err := h.validator.Validate(logPath)
	if err != nil {
		h.respondError(w, http.StatusForbidden, err.Error())
		return
	}

	lines := 100
	if l := r.URL.Query().Get("lines"); l != "" {
		fmt.Sscanf(l, "%d", &lines)
		if lines > 1000 {
			lines = 1000
		}
		if lines < 1 {
			lines = 100
		}
	}

	// Read file
	entries, err := h.readLastLines(cleanPath, lines)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"path":    cleanPath,
		"entries": entries,
	})
}

// readLastLines reads the last N lines from a file.
func (h *LogsHandler) readLastLines(path string, n int) ([]LogMessage, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var entries []LogMessage
	scanner := bufio.NewScanner(file)

	// Simple implementation: read all and take last n
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	start := 0
	if len(lines) > n {
		start = len(lines) - n
	}

	for i := start; i < len(lines); i++ {
		entries = append(entries, h.parseLogLine(lines[i], path))
	}

	return entries, scanner.Err()
}

// parseLogLine parses a log line into a LogMessage.
func (h *LogsHandler) parseLogLine(line, file string) LogMessage {
	msg := LogMessage{
		Timestamp: time.Now().Format(time.RFC3339),
		Message:   line,
		File:      filepath.Base(file),
	}

	// Detect log level from content
	lineUpper := strings.ToUpper(line)
	switch {
	case strings.Contains(lineUpper, "ERROR") || strings.Contains(lineUpper, "ERR"):
		msg.Level = "error"
	case strings.Contains(lineUpper, "WARN") || strings.Contains(lineUpper, "WARNING"):
		msg.Level = "warn"
	case strings.Contains(lineUpper, "DEBUG"):
		msg.Level = "debug"
	case strings.Contains(lineUpper, "INFO"):
		msg.Level = "info"
	default:
		// Infer from filename
		if strings.Contains(file, "error") {
			msg.Level = "error"
		} else {
			msg.Level = "info"
		}
	}

	return msg
}

// WebSocket handles WebSocket connections for real-time logs.
// GET /ws/logs
func (h *LogsHandler) WebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	h.clientsMu.Lock()
	h.clients[conn] = true
	clientCount := len(h.clients)
	h.clientsMu.Unlock()

	log.Printf("WebSocket client connected. Total: %d", clientCount)

	// Send initial message
	conn.WriteJSON(LogMessage{
		Timestamp: time.Now().Format(time.RFC3339),
		Level:     "info",
		Message:   "Connected to log stream",
	})

	// Keep connection alive with ping
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Read loop (required for proper WebSocket close handling)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	}()

	for {
		select {
		case <-done:
			// Client disconnected
			h.clientsMu.Lock()
			delete(h.clients, conn)
			h.clientsMu.Unlock()
			return
		case <-ticker.C:
			if err := conn.WriteJSON(map[string]string{"type": "ping"}); err != nil {
				h.clientsMu.Lock()
				delete(h.clients, conn)
				h.clientsMu.Unlock()
				return
			}
		case <-h.ctx.Done():
			// Server shutting down
			return
		}
	}
}

// respondJSON writes a JSON response.
func (h *LogsHandler) respondJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}

// respondError writes an error response.
func (h *LogsHandler) respondError(w http.ResponseWriter, statusCode int, message string) {
	h.respondJSON(w, statusCode, ErrorResponse{Error: message})
}

// RegisterLogsRoutes registers logs-related routes (protected API routes).
func RegisterLogsRoutes(r *mux.Router, handler *LogsHandler) {
	r.HandleFunc("/logs/xray", handler.ReadLogs).Methods("GET")
}

// RegisterLogsWSRoute registers WebSocket route separately (requires auth but not CSRF).
func RegisterLogsWSRoute(r *mux.Router, handler *LogsHandler, authMiddleware func(http.Handler) http.Handler) {
	wsRouter := r.PathPrefix("/ws").Subrouter()
	wsRouter.Use(authMiddleware)
	wsRouter.HandleFunc("/logs", handler.WebSocket).Methods("GET")
}

// truncate shortens a string for logging
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
