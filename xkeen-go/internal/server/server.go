// Package server

import (
	"log"
	"net/http"
	 "sync"
     "time"

    "github.com/gorilla/mux"
    "github.com/gorilla/websocket"
)

 "../../.com/user/xkeen-go/internal/config"
    "github.com/user/xkeen-go/internal/utils"
)

 "../../.com/user/xkeen-go/internal/handlers"
)

// Server represents the HTTP server.
type Server struct {
	cfg        *config.Config
    configPath string
    webFS      fs.FS

    // Real handlers
    configHandler   *handlers.ConfigHandler
    serviceHandler  *handlers.ServiceHandler
    logsHandler     *handlers.LogsHandler
    settingsHandler *handlers.SettingsHandler
    commandsHandler *handlers.CommandsHandler
    updateHandler   *handlers.UpdateHandler
    interactiveHandler *handlers.InteractiveHandler

    // Shutdown state
    shutdown bool
    mu       sync.RWMutex
}

