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

// Command timeout constants for additional operations.
const (
	// CommandInstallTimeout is the timeout for installation operations.
	CommandInstallTimeout = 300 * time.Second
	// CommandInfoTimeout is the timeout for information commands.
	CommandInfoTimeout = 10 * time.Second
	// CommandPortTimeout is the timeout for port management operations.
	CommandPortTimeout = 15 * time.Second
	// CommandModuleTimeout is the timeout for module operations.
	CommandModuleTimeout = 60 * time.Second
)

// defaultCommands is the default set of whitelisted XKeen commands.
var defaultCommands = map[string]CommandConfig{
	// === Installation ===
	"-i": {
		Cmd:         "-i",
		Description: "Установка XKeen + Xray + GeoFile + Mihomo",
		Dangerous:   true,
		Timeout:     CommandInstallTimeout,
	},
	"-io": {
		Cmd:         "-io",
		Description: "OffLine установка XKeen",
		Dangerous:   true,
		Timeout:     CommandInstallTimeout,
	},

	// === Update ===
	"-uk": {
		Cmd:         "-uk",
		Description: "Обновить XKeen",
		Dangerous:   false,
		Timeout:     CommandUpdateTimeout,
	},
	"-ug": {
		Cmd:         "-ug",
		Description: "Обновить GeoFile (GeoIP/GeoSite)",
		Dangerous:   false,
		Timeout:     CommandUpdateTimeout,
	},
	"-ux": {
		Cmd:         "-ux",
		Description: "Обновить Xray (повышение/понижение версии)",
		Dangerous:   false,
		Timeout:     CommandUpdateTimeout,
	},
	"-um": {
		Cmd:         "-um",
		Description: "Обновить Mihomo (повышение/понижение версии)",
		Dangerous:   false,
		Timeout:     CommandUpdateTimeout,
	},
	"-ugc": {
		Cmd:         "-ugc",
		Description: "Включить/изменить задачу автообновления GeoFile",
		Dangerous:   false,
		Timeout:     CommandStatusTimeout,
	},

	// === System Registration ===
	"-rrk": {
		Cmd:         "-rrk",
		Description: "Регистрация XKeen в системе",
		Dangerous:   false,
		Timeout:     CommandStatusTimeout,
	},
	"-rrx": {
		Cmd:         "-rrx",
		Description: "Регистрация Xray в системе",
		Dangerous:   false,
		Timeout:     CommandStatusTimeout,
	},
	"-rrm": {
		Cmd:         "-rrm",
		Description: "Регистрация Mihomo в системе",
		Dangerous:   false,
		Timeout:     CommandStatusTimeout,
	},
	"-ri": {
		Cmd:         "-ri",
		Description: "Автозапуск XKeen средствами init.d",
		Dangerous:   false,
		Timeout:     CommandStatusTimeout,
	},

	// === Removal | Components ===
	"-remove": {
		Cmd:         "-remove",
		Description: "Полная деинсталляция XKeen",
		Dangerous:   true,
		Timeout:     CommandBackupTimeout,
	},
	"-dgs": {
		Cmd:         "-dgs",
		Description: "Удалить GeoSite",
		Dangerous:   true,
		Timeout:     CommandStatusTimeout,
	},
	"-dgi": {
		Cmd:         "-dgi",
		Description: "Удалить GeoIP",
		Dangerous:   true,
		Timeout:     CommandStatusTimeout,
	},
	"-dx": {
		Cmd:         "-dx",
		Description: "Удалить Xray",
		Dangerous:   true,
		Timeout:     CommandStatusTimeout,
	},
	"-dm": {
		Cmd:         "-dm",
		Description: "Удалить Mihomo",
		Dangerous:   true,
		Timeout:     CommandStatusTimeout,
	},
	"-dk": {
		Cmd:         "-dk",
		Description: "Удалить XKeen",
		Dangerous:   true,
		Timeout:     CommandStatusTimeout,
	},

	// === Removal | Auto-update Tasks ===
	"-dgc": {
		Cmd:         "-dgc",
		Description: "Удалить задачу автообновления GeoFile",
		Dangerous:   false,
		Timeout:     CommandStatusTimeout,
	},

	// === Removal | System Registration ===
	"-drk": {
		Cmd:         "-drk",
		Description: "Удалить регистрацию XKeen",
		Dangerous:   true,
		Timeout:     CommandStatusTimeout,
	},
	"-drx": {
		Cmd:         "-drx",
		Description: "Удалить регистрацию Xray",
		Dangerous:   true,
		Timeout:     CommandStatusTimeout,
	},
	"-drm": {
		Cmd:         "-drm",
		Description: "Удалить регистрацию Mihomo",
		Dangerous:   true,
		Timeout:     CommandStatusTimeout,
	},

	// === Ports | Proxy ===
	"-ap": {
		Cmd:         "-ap",
		Description: "Добавить порт прокси-клиента",
		Dangerous:   false,
		Timeout:     CommandPortTimeout,
	},
	"-dp": {
		Cmd:         "-dp",
		Description: "Удалить порт прокси-клиента",
		Dangerous:   false,
		Timeout:     CommandPortTimeout,
	},
	"-cp": {
		Cmd:         "-cp",
		Description: "Показать порты прокси-клиента",
		Dangerous:   false,
		Timeout:     CommandStatusTimeout,
	},

	// === Ports | Excluded ===
	"-ape": {
		Cmd:         "-ape",
		Description: "Добавить исключённый порт",
		Dangerous:   false,
		Timeout:     CommandPortTimeout,
	},
	"-dpe": {
		Cmd:         "-dpe",
		Description: "Удалить исключённый порт",
		Dangerous:   false,
		Timeout:     CommandPortTimeout,
	},
	"-cpe": {
		Cmd:         "-cpe",
		Description: "Показать исключённые порты",
		Dangerous:   false,
		Timeout:     CommandStatusTimeout,
	},

	// === Reinstallation ===
	"-k": {
		Cmd:         "-k",
		Description: "Переустановить XKeen",
		Dangerous:   false,
		Timeout:     CommandUpdateTimeout,
	},
	"-g": {
		Cmd:         "-g",
		Description: "Переустановить GeoFile",
		Dangerous:   false,
		Timeout:     CommandUpdateTimeout,
	},

	// === Backup | XKeen ===
	"-kb": {
		Cmd:         "-kb",
		Description: "Создать резервную копию XKeen",
		Dangerous:   false,
		Timeout:     CommandBackupTimeout,
	},
	"-kbr": {
		Cmd:         "-kbr",
		Description: "Восстановить резервную копию XKeen",
		Dangerous:   false,
		Timeout:     CommandBackupTimeout,
	},

	// === Backup | Xray ===
	"-cb": {
		Cmd:         "-cb",
		Description: "Создать резервную копию конфигурации Xray",
		Dangerous:   false,
		Timeout:     CommandBackupTimeout,
	},
	"-cbr": {
		Cmd:         "-cbr",
		Description: "Восстановить конфигурацию Xray",
		Dangerous:   false,
		Timeout:     CommandBackupTimeout,
	},

	// === Backup | Mihomo ===
	"-mb": {
		Cmd:         "-mb",
		Description: "Создать резервную копию конфигурации Mihomo",
		Dangerous:   false,
		Timeout:     CommandBackupTimeout,
	},
	"-mbr": {
		Cmd:         "-mbr",
		Description: "Восстановить конфигурацию Mihomo",
		Dangerous:   false,
		Timeout:     CommandBackupTimeout,
	},

	// === Proxy Client Management ===
	"-start": {
		Cmd:         "-start",
		Description: "Запуск прокси-клиента",
		Dangerous:   false,
		Timeout:     CommandStartStopTimeout,
	},
	"-stop": {
		Cmd:         "-stop",
		Description: "Остановка прокси-клиента",
		Dangerous:   false,
		Timeout:     CommandStartStopTimeout,
	},
	"-restart": {
		Cmd:         "-restart",
		Description: "Перезапуск прокси-клиента",
		Dangerous:   false,
		Timeout:     CommandRestartTimeout,
	},
	"-status": {
		Cmd:         "-status",
		Description: "Статус работы прокси-клиента",
		Dangerous:   false,
		Timeout:     CommandStatusTimeout,
	},
	"-tpx": {
		Cmd:         "-tpx",
		Description: "Порты, шлюз и протокол прокси-клиента",
		Dangerous:   false,
		Timeout:     CommandStatusTimeout,
	},
	"-auto": {
		Cmd:         "-auto",
		Description: "Включить/отключить автозапуск прокси-клиента",
		Dangerous:   false,
		Timeout:     CommandStatusTimeout,
	},
	"-d": {
		Cmd:         "-d",
		Description: "Установить задержку автозапуска",
		Dangerous:   false,
		Timeout:     CommandStatusTimeout,
	},
	"-fd": {
		Cmd:         "-fd",
		Description: "Контроль файловых дескрипторов прокси-клиента",
		Dangerous:   false,
		Timeout:     CommandStatusTimeout,
	},
	"-diag": {
		Cmd:         "-diag",
		Description: "Выполнить диагностику",
		Dangerous:   false,
		Timeout:     CommandBackupTimeout,
	},
	"-channel": {
		Cmd:         "-channel",
		Description: "Переключить канал обновлений (Stable/Dev)",
		Dangerous:   false,
		Timeout:     CommandStatusTimeout,
	},
	"-xray": {
		Cmd:         "-xray",
		Description: "Переключить XKeen на ядро Xray",
		Dangerous:   false,
		Timeout:     CommandStartStopTimeout,
	},
	"-mihomo": {
		Cmd:         "-mihomo",
		Description: "Переключить XKeen на ядро Mihomo",
		Dangerous:   false,
		Timeout:     CommandStartStopTimeout,
	},
	"-ipv6": {
		Cmd:         "-ipv6",
		Description: "Включить/отключить IPv6 в KeeneticOS",
		Dangerous:   false,
		Timeout:     CommandStatusTimeout,
	},
	"-dns": {
		Cmd:         "-dns",
		Description: "Включить/отключить перенаправление DNS",
		Dangerous:   false,
		Timeout:     CommandStatusTimeout,
	},

	// === Module Management ===
	"-modules": {
		Cmd:         "-modules",
		Description: "Перенос модулей в пользовательскую директорию",
		Dangerous:   false,
		Timeout:     CommandModuleTimeout,
	},
	"-delmodules": {
		Cmd:         "-delmodules",
		Description: "Удаление модулей из пользовательской директории",
		Dangerous:   false,
		Timeout:     CommandModuleTimeout,
	},

	// === Information ===
	"-about": {
		Cmd:         "-about",
		Description: "О программе XKeen",
		Dangerous:   false,
		Timeout:     CommandInfoTimeout,
	},
	"-ad": {
		Cmd:         "-ad",
		Description: "Поддержать разработчиков",
		Dangerous:   false,
		Timeout:     CommandInfoTimeout,
	},
	"-af": {
		Cmd:         "-af",
		Description: "Обратная связь",
		Dangerous:   false,
		Timeout:     CommandInfoTimeout,
	},
	"-v": {
		Cmd:         "-v",
		Description: "Версия XKeen",
		Dangerous:   false,
		Timeout:     CommandInfoTimeout,
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
