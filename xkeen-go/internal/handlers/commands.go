// Package handlers provides HTTP handlers for XKEEN-UI API endpoints.
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

// defaultCommands is the default set of whitelisted XKeen commands.
var defaultCommands = map[string]CommandConfig{
	// === Installation ===
	"-i": {
		Cmd:         "-i",
		Description: "Установка XKeen + Xray + GeoFile + Mihomo",
		Dangerous:   true,
		Timeout:     CommandTimeout,
	},
	"-io": {
		Cmd:         "-io",
		Description: "OffLine установка XKeen",
		Dangerous:   true,
		Timeout:     CommandTimeout,
	},

	// === Update ===
	"-uk": {
		Cmd:         "-uk",
		Description: "Обновить XKeen",
		Dangerous:   false,
		Timeout:     CommandTimeout,
	},
	"-ug": {
		Cmd:         "-ug",
		Description: "Обновить GeoFile (GeoIP/GeoSite)",
		Dangerous:   false,
		Timeout:     CommandTimeout,
	},
	"-ux": {
		Cmd:         "-ux",
		Description: "Обновить Xray (повышение/понижение версии)",
		Dangerous:   false,
		Timeout:     CommandTimeout,
	},
	"-um": {
		Cmd:         "-um",
		Description: "Обновить Mihomo (повышение/понижение версии)",
		Dangerous:   false,
		Timeout:     CommandTimeout,
	},
	"-ugc": {
		Cmd:         "-ugc",
		Description: "Включить/изменить задачу автообновления GeoFile",
		Dangerous:   false,
		Timeout:     CommandTimeout,
	},

	// === System Registration ===
	"-rrk": {
		Cmd:         "-rrk",
		Description: "Регистрация XKeen в системе",
		Dangerous:   false,
		Timeout:     CommandTimeout,
	},
	"-rrx": {
		Cmd:         "-rrx",
		Description: "Регистрация Xray в системе",
		Dangerous:   false,
		Timeout:     CommandTimeout,
	},
	"-rrm": {
		Cmd:         "-rrm",
		Description: "Регистрация Mihomo в системе",
		Dangerous:   false,
		Timeout:     CommandTimeout,
	},
	"-ri": {
		Cmd:         "-ri",
		Description: "Автозапуск XKeen средствами init.d",
		Dangerous:   false,
		Timeout:     CommandTimeout,
	},

	// === Removal | Components ===
	"-remove": {
		Cmd:         "-remove",
		Description: "Полная деинсталляция XKeen",
		Dangerous:   true,
		Timeout:     CommandTimeout,
	},
	"-dgs": {
		Cmd:         "-dgs",
		Description: "Удалить GeoSite",
		Dangerous:   true,
		Timeout:     CommandTimeout,
	},
	"-dgi": {
		Cmd:         "-dgi",
		Description: "Удалить GeoIP",
		Dangerous:   true,
		Timeout:     CommandTimeout,
	},
	"-dx": {
		Cmd:         "-dx",
		Description: "Удалить Xray",
		Dangerous:   true,
		Timeout:     CommandTimeout,
	},
	"-dm": {
		Cmd:         "-dm",
		Description: "Удалить Mihomo",
		Dangerous:   true,
		Timeout:     CommandTimeout,
	},
	"-dk": {
		Cmd:         "-dk",
		Description: "Удалить XKeen",
		Dangerous:   true,
		Timeout:     CommandTimeout,
	},

	// === Removal | Auto-update Tasks ===
	"-dgc": {
		Cmd:         "-dgc",
		Description: "Удалить задачу автообновления GeoFile",
		Dangerous:   false,
		Timeout:     CommandTimeout,
	},

	// === Removal | System Registration ===
	"-drk": {
		Cmd:         "-drk",
		Description: "Удалить регистрацию XKeen",
		Dangerous:   true,
		Timeout:     CommandTimeout,
	},
	"-drx": {
		Cmd:         "-drx",
		Description: "Удалить регистрацию Xray",
		Dangerous:   true,
		Timeout:     CommandTimeout,
	},
	"-drm": {
		Cmd:         "-drm",
		Description: "Удалить регистрацию Mihomo",
		Dangerous:   true,
		Timeout:     CommandTimeout,
	},

	// === Ports | Proxy ===
	"-ap": {
		Cmd:         "-ap",
		Description: "Добавить порт прокси-клиента",
		Dangerous:   false,
		Timeout:     CommandTimeout,
	},
	"-dp": {
		Cmd:         "-dp",
		Description: "Удалить порт прокси-клиента",
		Dangerous:   false,
		Timeout:     CommandTimeout,
	},
	"-cp": {
		Cmd:         "-cp",
		Description: "Показать порты прокси-клиента",
		Dangerous:   false,
		Timeout:     CommandTimeout,
	},

	// === Ports | Excluded ===
	"-ape": {
		Cmd:         "-ape",
		Description: "Добавить исключённый порт",
		Dangerous:   false,
		Timeout:     CommandTimeout,
	},
	"-dpe": {
		Cmd:         "-dpe",
		Description: "Удалить исключённый порт",
		Dangerous:   false,
		Timeout:     CommandTimeout,
	},
	"-cpe": {
		Cmd:         "-cpe",
		Description: "Показать исключённые порты",
		Dangerous:   false,
		Timeout:     CommandTimeout,
	},

	// === Reinstallation ===
	"-k": {
		Cmd:         "-k",
		Description: "Переустановить XKeen",
		Dangerous:   false,
		Timeout:     CommandTimeout,
	},
	"-g": {
		Cmd:         "-g",
		Description: "Переустановить GeoFile",
		Dangerous:   false,
		Timeout:     CommandTimeout,
	},

	// === Backup | XKeen ===
	"-kb": {
		Cmd:         "-kb",
		Description: "Создать резервную копию XKeen",
		Dangerous:   false,
		Timeout:     CommandTimeout,
	},
	"-kbr": {
		Cmd:         "-kbr",
		Description: "Восстановить резервную копию XKeen",
		Dangerous:   false,
		Timeout:     CommandTimeout,
	},

	// === Backup | Xray ===
	"-cb": {
		Cmd:         "-cb",
		Description: "Создать резервную копию конфигурации Xray",
		Dangerous:   false,
		Timeout:     CommandTimeout,
	},
	"-cbr": {
		Cmd:         "-cbr",
		Description: "Восстановить конфигурацию Xray",
		Dangerous:   false,
		Timeout:     CommandTimeout,
	},

	// === Backup | Mihomo ===
	"-mb": {
		Cmd:         "-mb",
		Description: "Создать резервную копию конфигурации Mihomo",
		Dangerous:   false,
		Timeout:     CommandTimeout,
	},
	"-mbr": {
		Cmd:         "-mbr",
		Description: "Восстановить конфигурацию Mihomo",
		Dangerous:   false,
		Timeout:     CommandTimeout,
	},

	// === Proxy Client Management ===
	"-start": {
		Cmd:         "-start",
		Description: "Запуск прокси-клиента",
		Dangerous:   false,
		Timeout:     CommandTimeout,
	},
	"-stop": {
		Cmd:         "-stop",
		Description: "Остановка прокси-клиента",
		Dangerous:   false,
		Timeout:     CommandTimeout,
	},
	"-restart": {
		Cmd:         "-restart",
		Description: "Перезапуск прокси-клиента",
		Dangerous:   false,
		Timeout:     CommandTimeout,
	},
	"-status": {
		Cmd:         "-status",
		Description: "Статус работы прокси-клиента",
		Dangerous:   false,
		Timeout:     CommandTimeout,
	},
	"-tpx": {
		Cmd:         "-tpx",
		Description: "Порты, шлюз и протокол прокси-клиента",
		Dangerous:   false,
		Timeout:     CommandTimeout,
	},
	"-auto": {
		Cmd:         "-auto",
		Description: "Включить/отключить автозапуск прокси-клиента",
		Dangerous:   false,
		Timeout:     CommandTimeout,
	},
	"-d": {
		Cmd:         "-d",
		Description: "Установить задержку автозапуска",
		Dangerous:   false,
		Timeout:     CommandTimeout,
	},
	"-fd": {
		Cmd:         "-fd",
		Description: "Контроль файловых дескрипторов прокси-клиента",
		Dangerous:   false,
		Timeout:     CommandTimeout,
	},
	"-diag": {
		Cmd:         "-diag",
		Description: "Выполнить диагностику",
		Dangerous:   false,
		Timeout:     CommandTimeout,
	},
	"-channel": {
		Cmd:         "-channel",
		Description: "Переключить канал обновлений (Stable/Dev)",
		Dangerous:   false,
		Timeout:     CommandTimeout,
	},
	"-xray": {
		Cmd:         "-xray",
		Description: "Переключить XKeen на ядро Xray",
		Dangerous:   false,
		Timeout:     CommandTimeout,
	},
	"-mihomo": {
		Cmd:         "-mihomo",
		Description: "Переключить XKeen на ядро Mihomo",
		Dangerous:   false,
		Timeout:     CommandTimeout,
	},
	"-ipv6": {
		Cmd:         "-ipv6",
		Description: "Включить/отключить IPv6 в KeeneticOS",
		Dangerous:   false,
		Timeout:     CommandTimeout,
	},
	"-dns": {
		Cmd:         "-dns",
		Description: "Включить/отключить перенаправление DNS",
		Dangerous:   false,
		Timeout:     CommandTimeout,
	},

	// === Module Management ===
	"-modules": {
		Cmd:         "-modules",
		Description: "Перенос модулей в пользовательскую директорию",
		Dangerous:   false,
		Timeout:     CommandTimeout,
	},
	"-delmodules": {
		Cmd:         "-delmodules",
		Description: "Удаление модулей из пользовательской директории",
		Dangerous:   false,
		Timeout:     CommandTimeout,
	},

	// === Information ===
	"-about": {
		Cmd:         "-about",
		Description: "О программе XKeen",
		Dangerous:   false,
		Timeout:     CommandTimeout,
	},
	"-ad": {
		Cmd:         "-ad",
		Description: "Поддержать разработчиков",
		Dangerous:   false,
		Timeout:     CommandTimeout,
	},
	"-af": {
		Cmd:         "-af",
		Description: "Обратная связь",
		Dangerous:   false,
		Timeout:     CommandTimeout,
	},
	"-v": {
		Cmd:         "-v",
		Description: "Версия XKeen",
		Dangerous:   false,
		Timeout:     CommandTimeout,
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
