// Package handlers provides HTTP handlers for XKEEN-GO API endpoints.
package handlers

import (
	"encoding/json"
	"log"
	"net/http"
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

// CommandConfig holds configuration for a whitelisted command.
type CommandConfig struct {
	Cmd         string
	Description string
	Dangerous   bool
	Timeout     time.Duration
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

// CommandsHandler handles XKeen CLI command metadata.
// Command execution is handled by InteractiveHandler via WebSocket.
type CommandsHandler struct {
	mu              sync.RWMutex
	allowedCommands map[string]CommandConfig
}

// NewCommandsHandler creates a new CommandsHandler with default settings.
func NewCommandsHandler() *CommandsHandler {
	return &CommandsHandler{
		allowedCommands: defaultCommands,
	}
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
