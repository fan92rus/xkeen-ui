// Package handlers provides HTTP handlers for XKEEN-UI API endpoints.
package handlers

import (
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

// Timeout constants for service operations.
const (
	// StatusTimeout is the timeout for status check operations (quick read).
	StatusTimeout = 10 * time.Second
	// StartStopTimeout is the timeout for start/stop operations.
	StartStopTimeout = 30 * time.Second
	// RestartTimeout is the timeout for restart operations (stop + start).
	RestartTimeout = 45 * time.Second
)

// CommandExecutor defines the interface for executing system commands.
type CommandExecutor interface {
	Execute(ctx context.Context, name string, args ...string) (string, error)
}

// realExecutor implements CommandExecutor using actual exec.CommandContext.
type realExecutor struct{}

func (e *realExecutor) Execute(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.CombinedOutput()

	// Check for context cancellation/timeout first
	if ctx.Err() != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return string(output), fmt.Errorf("command timed out")
		}
		return string(output), fmt.Errorf("command cancelled: %w", ctx.Err())
	}

	if err != nil {
		return string(output), fmt.Errorf("command failed: %w", err)
	}

	return string(output), nil
}

// ServiceHandler handles xkeen service operations.
type ServiceHandler struct {
	mu              sync.RWMutex
	allowedCommands map[string]string
	executor        CommandExecutor
	statusTrigger   chan struct{}
	wg              sync.WaitGroup
	closeOnce       sync.Once
}

// NewServiceHandler creates a new ServiceHandler.
func NewServiceHandler() *ServiceHandler {
	return &ServiceHandler{
		allowedCommands: map[string]string{
			"start":   "xkeen -start",
			"stop":    "xkeen -stop",
			"restart": "xkeen -restart",
			"status":  "xkeen -status",
		},
		executor:      &realExecutor{},
		statusTrigger: make(chan struct{}, 1),
	}
}

// NewServiceHandlerWithExecutor creates a ServiceHandler with a custom executor (for testing).
func NewServiceHandlerWithExecutor(executor CommandExecutor) *ServiceHandler {
	return &ServiceHandler{
		allowedCommands: map[string]string{
			"start":   "xkeen -start",
			"stop":    "xkeen -stop",
			"restart": "xkeen -restart",
			"status":  "xkeen -status",
		},
		executor:      executor,
		statusTrigger: make(chan struct{}, 1),
	}
}

// TriggerStatusCheck signals all SSE clients to receive an immediate status update.
func (h *ServiceHandler) TriggerStatusCheck() {
	select {
	case h.statusTrigger <- struct{}{}:
	default:
		// Channel full, status check already pending
	}
}

// Close waits for all background goroutines (start/stop/restart) to complete.
// Uses a timeout to prevent hanging forever on stuck operations.
func (h *ServiceHandler) Close() {
	h.closeOnce.Do(func() {
		done := make(chan struct{})
		go func() {
			h.wg.Wait()
			close(done)
		}()
		select {
		case <-done:
			// All goroutines completed
		case <-time.After(5 * time.Minute):
			log.Printf("[service] Close timed out waiting for background goroutines")
		}
	})
}

// ServiceStatus represents the current status of xkeen service.
type ServiceStatus struct {
	Running   bool      `json:"running"`
	PID       int       `json:"pid,omitempty"`
	Uptime    string    `json:"uptime,omitempty"`
	LastCheck time.Time `json:"last_check"`
}

// ServiceResponse is the standard response for service operations.
type ServiceResponse struct {
	Success bool           `json:"success"`
	Message string         `json:"message"`
	Status  *ServiceStatus `json:"status,omitempty"`
}

// executeCommandWithTimeout safely executes a whitelisted command with a timeout context.
func (h *ServiceHandler) executeCommandWithTimeout(ctx context.Context, action string) (string, error) {
	cmd, exists := h.allowedCommands[action]
	if !exists {
		return "", fmt.Errorf("unknown action: %s", action)
	}

	// Split command into parts for safe execution (no shell)
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return "", fmt.Errorf("empty command")
	}

	// Execute using the executor interface
	return h.executor.Execute(ctx, parts[0], parts[1:]...)
}

// GetStatus returns the current service status.
// GET /api/xkeen/status
func (h *ServiceHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), StatusTimeout)
	defer cancel()

	output, err := h.executeCommandWithTimeout(ctx, "status")

	// Log for debugging
	log.Printf("Status check: output=%q, err=%v", output, err)

	// Check if service is running - look for positive indicators
	// Support both English and Russian output from xkeen init script
	// IMPORTANT: Check negative patterns first to avoid false positives
	notRunning := strings.Contains(output, "is not running") ||
		strings.Contains(output, "не запущен")

	isRunning := err == nil && !notRunning &&
		(strings.Contains(output, "is running") ||
			strings.Contains(output, "running (PID:") ||
			strings.Contains(output, "active (running)") ||
			strings.Contains(output, "запущен"))

	status := &ServiceStatus{
		LastCheck: time.Now(),
		Running:   isRunning,
	}

	if status.Running {
		status.Uptime = "active"
	}

	// Handle timeout errors specifically
	if err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		respondJSON(w, http.StatusGatewayTimeout, ServiceResponse{
			Success: false,
			Message: fmt.Sprintf("Status check timed out: %s", err),
		})
		return
	}

	respondJSON(w, http.StatusOK, ServiceResponse{
		Success: true,
		Message: output,
		Status:  status,
	})
}

// StatusStream handles SSE connections for real-time status updates.
// GET /api/xkeen/status/stream
func (h *ServiceHandler) StatusStream(w http.ResponseWriter, r *http.Request) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Send initial status immediately
	h.sendStatusEvent(w, flusher)

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := h.sendStatusEvent(w, flusher); err != nil {
				return
			}
		case <-r.Context().Done():
			return
		case <-h.statusTrigger:
			// Instant check triggered by start/stop/restart
			if err := h.sendStatusEvent(w, flusher); err != nil {
				return
			}
		}
	}
}

// sendStatusEvent sends a single status event to the SSE client
func (h *ServiceHandler) sendStatusEvent(w http.ResponseWriter, flusher http.Flusher) error {
	ctx, cancel := context.WithTimeout(context.Background(), StatusTimeout)
	defer cancel()

	output, err := h.executeCommandWithTimeout(ctx, "status")

	notRunning := strings.Contains(output, "is not running") ||
		strings.Contains(output, "не запущен")

	isRunning := err == nil && !notRunning &&
		(strings.Contains(output, "is running") ||
			strings.Contains(output, "running (PID:") ||
			strings.Contains(output, "active (running)") ||
			strings.Contains(output, "запущен"))

	status := ServiceStatus{
		LastCheck: time.Now(),
		Running:   isRunning,
	}
	if isRunning {
		status.Uptime = "active"
	}

	data, _ := json.Marshal(status)
	log.Printf("[SSE] Sending status event: %s", string(data))
	fmt.Fprintf(w, "event: status\ndata: %s\n\n", data)
	flusher.Flush()
	return nil
}

// Start starts the xkeen service.
// POST /api/xkeen/start
// Runs asynchronously to avoid blocking the request.
func (h *ServiceHandler) Start(w http.ResponseWriter, r *http.Request) {
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()

		ctx, cancel := context.WithTimeout(context.Background(), StartStopTimeout)
		defer cancel()

		output, err := h.executeCommandWithTimeout(ctx, "start")
		if err != nil {
			log.Printf("Start failed: %v, output: %s", err, output)
		} else {
			log.Printf("Start completed: %s", output)
		}

		// Wait a moment for service to start, then trigger status check
		time.Sleep(1 * time.Second)
		h.TriggerStatusCheck()
	}()

	respondJSON(w, http.StatusOK, ServiceResponse{
		Success: true,
		Message: "Start initiated",
	})
}

// Stop stops the xkeen service.
// POST /api/xkeen/stop
// Runs asynchronously to avoid blocking the request.
func (h *ServiceHandler) Stop(w http.ResponseWriter, r *http.Request) {
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()

		ctx, cancel := context.WithTimeout(context.Background(), StartStopTimeout)
		defer cancel()

		output, err := h.executeCommandWithTimeout(ctx, "stop")
		if err != nil {
			log.Printf("Stop failed: %v, output: %s", err, output)
		} else {
			log.Printf("Stop completed: %s", output)
		}

		// Wait a moment for service to stop, then trigger status check
		time.Sleep(1 * time.Second)
		h.TriggerStatusCheck()
	}()

	respondJSON(w, http.StatusOK, ServiceResponse{
		Success: true,
		Message: "Stop initiated",
	})
}

// Restart restarts the xkeen service.
// POST /api/xkeen/restart
// Runs restart asynchronously to avoid blocking the request.
func (h *ServiceHandler) Restart(w http.ResponseWriter, r *http.Request) {
	h.wg.Add(1)
	// Run restart in background goroutine
	go func() {
		defer h.wg.Done()

		ctx, cancel := context.WithTimeout(context.Background(), RestartTimeout)
		defer cancel()

		output, err := h.executeCommandWithTimeout(ctx, "restart")
		if err != nil {
			log.Printf("Restart failed: %v, output: %s", err, output)
		} else {
			log.Printf("Restart completed: %s", output)
		}

		// Wait a moment for service to restart, then trigger status check
		time.Sleep(2 * time.Second)
		h.TriggerStatusCheck()
	}()

	// Return immediately
	respondJSON(w, http.StatusOK, ServiceResponse{
		Success: true,
		Message: "Restart initiated",
	})
}

// RegisterServiceRoutes registers service-related routes.
func RegisterServiceRoutes(r *mux.Router, handler *ServiceHandler) {
	r.HandleFunc("/xkeen/status", handler.GetStatus).Methods("GET")
	r.HandleFunc("/xkeen/status/stream", handler.StatusStream).Methods("GET")
	r.HandleFunc("/xkeen/start", handler.Start).Methods("POST")
	r.HandleFunc("/xkeen/stop", handler.Stop).Methods("POST")
	r.HandleFunc("/xkeen/restart", handler.Restart).Methods("POST")
}

