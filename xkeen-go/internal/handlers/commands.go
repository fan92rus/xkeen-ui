// Package handlers provides HTTP handlers for XKEEN-UI API endpoints.
package handlers

import (
	"net/http"
	"time"

	"github.com/gorilla/mux"
)

// Timeout constants for XKeen CLI commands.
const (
	// CommandTimeout is the default timeout for all XKeen commands.
	CommandTimeout = 10 * time.Minute
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

// CommandsHandler handles XKeen CLI command metadata.
// Command execution is handled by InteractiveHandler via WebSocket.
//
// The command set is sourced from a shared *CommandRegistry, which discovers
// commands at runtime from `xkeen -help` (see command_registry.go /
// help_parser.go). There is no hardcoded command list.
type CommandsHandler struct {
	registry *CommandRegistry
}

// NewCommandsHandler creates a new CommandsHandler backed by the given
// command registry. The registry is shared with InteractiveHandler so both
// metadata (GetCommands) and execution (isCommandAllowed) agree.
func NewCommandsHandler(registry *CommandRegistry) *CommandsHandler {
	return &CommandsHandler{registry: registry}
}

// GetCommands returns the list of available commands.
// GET /api/xkeen/commands
func (h *CommandsHandler) GetCommands(w http.ResponseWriter, r *http.Request) {
	all := h.registry.All()

	commands := make([]CommandInfo, 0, len(all))
	for _, config := range all {
		commands = append(commands, CommandInfo{
			Cmd:         config.Cmd,
			Description: config.Description,
			Dangerous:   config.Dangerous,
		})
	}

	respondJSON(w, http.StatusOK, CommandsListResponse{
		Commands: commands,
	})
}

// RegisterCommandsRoutes registers command-related routes.
func RegisterCommandsRoutes(r *mux.Router, handler *CommandsHandler) {
	r.HandleFunc("/xkeen/commands", handler.GetCommands).Methods("GET")
}
