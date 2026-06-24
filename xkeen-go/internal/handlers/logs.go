// Package handlers provides HTTP handlers for XKEEN-UI API endpoints.
package handlers

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"

	"github.com/fan92rus/xkeen-ui/internal/utils"
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

	// Kill orphaned tail processes from previous runs
	killOrphanedTails(logFiles)

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
	// Note: h.broadcast is NOT closed — goroutines exit via ctx cancellation.
	// The GC reclaims the channel once unreferenced.
}

// checkOrigin validates the origin of WebSocket connections.
func (h *LogsHandler) checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}

	// Check against explicitly allowed origins
	if h.allowedOrigins[origin] {
		return true
	}

	// Allow same-origin requests
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return u.Host == r.Host
}

// runBroadcast handles broadcasting messages to all connected clients.
func (h *LogsHandler) runBroadcast() {
	defer h.wg.Done()

	for {
		select {
		case <-h.ctx.Done():
			// Server shutting down — drain remaining messages best-effort
			for {
				select {
				case msg, ok := <-h.broadcast:
					if !ok {
						return
					}
					h.sendToClients(msg)
				default:
					return
				}
			}
		case msg, ok := <-h.broadcast:
			if !ok {
				return
			}
			h.sendToClients(msg)
		}
	}
}

// sendToClients sends a message to all connected WebSocket clients.
// Sets a write deadline to prevent head-of-line blocking on slow clients.
func (h *LogsHandler) sendToClients(msg LogMessage) {
	h.clientsMu.RLock()
	var deadClients []*websocket.Conn
	for client := range h.clients {
		client.SetWriteDeadline(time.Now().Add(5 * time.Second))
		err := client.WriteJSON(msg)
		if err != nil {
			deadClients = append(deadClients, client)
		}
	}
	h.clientsMu.RUnlock()

	if len(deadClients) > 0 {
		h.clientsMu.Lock()
		for _, client := range deadClients {
			client.Close()
			delete(h.clients, client)
		}
		h.clientsMu.Unlock()
	}
}

// tailFile tails a log file using native Go file reading (no external tail process).
// Handles: missing files (waits for creation), log rotation (reopens), and context cancellation.
// All resources are freed when context is cancelled — no orphan processes.
func (h *LogsHandler) tailFile(path string) {
	defer h.wg.Done()

	log.Printf("[logs] Native tail starting: %s", path)

	var lastSize int64 = -1 // -1 = first read, start from beginning
	var lastInode uint64 = 0

	for {
		select {
		case <-h.ctx.Done():
			return
		default:
		}

		// Wait for file to exist
		info, err := os.Stat(path)
		if err != nil {
			select {
			case <-h.ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
			continue
		}

		// Detect rotation: inode changed (file was replaced)
		currentInode := getFileInode(info)
		if currentInode != 0 {
			if lastInode != 0 && currentInode != lastInode {
				lastSize = -1
				log.Printf("[logs] File rotated, reopening from start: %s", path)
			}
			lastInode = currentInode
		}

		// Open file
		f, err := os.Open(path)
		if err != nil {
			select {
			case <-h.ctx.Done():
				return
			case <-time.After(1 * time.Second):
			}
			continue
		}

		// First read or after rotation: seek to end (stream new lines only)
		// File truncated: read from start
		// Otherwise: seek to last known position
		if lastSize < 0 {
			f.Seek(0, 2)
		} else if info.Size() < lastSize {
			f.Seek(0, 0)
		} else {
			f.Seek(lastSize, 0)
		}

		// Read available new lines
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // 1MB max line
		linesRead := false

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}

			msg := h.parseLogLine(line, path)
			select {
			case h.broadcast <- msg:
			case <-h.ctx.Done():
				f.Close()
				return
			default:
				// Channel full, drop message
			}
			linesRead = true
		}

		// Update last known position
		if pos, err := f.Seek(0, 1); err == nil {
			lastSize = pos
		} else if linesRead {
			lastSize = info.Size()
		}

		f.Close()

		// Poll interval: fast when active, slower when idle
		interval := 500 * time.Millisecond
		if !linesRead {
			interval = 2 * time.Second
		}

		select {
		case <-h.ctx.Done():
			return
		case <-time.After(interval):
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
		respondError(w, http.StatusInternalServerError, "Path validator not initialized")
		return
	}

	cleanPath, err := h.validator.Validate(logPath)
	if err != nil {
		respondError(w, http.StatusForbidden, err.Error())
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
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
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
