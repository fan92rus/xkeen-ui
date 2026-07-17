// Package handlers — AmneziaWG installation handler.
//
// Embeds install-awg.sh and executes it on the router via sh.
// stdout lines are streamed to the frontend as SSE events for progress display.
package handlers

import (
	"encoding/json"
	_ "embed"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gorilla/mux"
)

//go:embed install-awg.sh
var installAWGScript string

//go:embed install-awg-uninstall.sh
var installAWGUninstallScript string

//go:embed install-awg-init.sh
var installAWGInitScript string

// AWG init script paths.  The script must have an S-prefix (S90awg) so that
// Entware's rc.unslung picks it up at boot — without it, WARP and other AWG
// client interfaces never auto-start after a reboot.
const (
	awgInitScriptPath    = "/opt/etc/init.d/S90awg"
	awgInitScriptPathOld = "/opt/init.d/awg" // legacy path without S-prefix
)

// InstallHandler handles AWG installation.
type InstallHandler struct {
	mu         sync.Mutex
	installing bool
}

// NewInstallHandler creates a new InstallHandler.
func NewInstallHandler() *InstallHandler {
	return &InstallHandler{}
}

// AWGStatusResponse is the response for AWG status check.
type AWGStatusResponse struct {
	Installed     bool   `json:"installed"`
	HasInitScript bool   `json:"has_init_script"`
	GoVersion     string `json:"amneziawg_go_version,omitempty"`
	ToolsVersion  string `json:"amneziawg_tools_version,omitempty"`
	Interfaces    string `json:"interfaces,omitempty"`
	Error         string `json:"error,omitempty"`
}

// Status checks if AWG is installed.
// GET /api/install/awg/status
func (h *InstallHandler) Status(w http.ResponseWriter, _ *http.Request) {
	resp := AWGStatusResponse{}

	// Check if awg binary exists
	if _, err := os.Stat("/opt/bin/awg"); err == nil {
		resp.Installed = true
		// Try to get versions from opkg
		data, err := exec.Command("opkg", "list-installed").Output()
		if err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				parts := strings.Fields(line)
				if len(parts) >= 3 {
					switch parts[0] {
					case "amneziawg-go":
						resp.GoVersion = parts[2]
					case "amneziawg-tools":
						resp.ToolsVersion = parts[2]
					}
				}
			}
		}
	}

	// Check init script (new S90awg or legacy awg without prefix)
	_, err := os.Stat(awgInitScriptPath)
	if err != nil {
		_, err = os.Stat(awgInitScriptPathOld)
	}
	resp.HasInitScript = err == nil

	// Check interfaces
	initScript := awgInitScriptPath
	if _, statErr := os.Stat(initScript); statErr != nil {
		initScript = awgInitScriptPathOld
	}
	if data, err := exec.Command(initScript, "status").Output(); err == nil {
		resp.Interfaces = strings.TrimSpace(string(data))
	}

	respondJSON(w, http.StatusOK, resp)
}

// InitScriptResponse is the response for init script operations.
type InitScriptResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// SetupInitScript creates or updates the AWG init script.
// POST /api/install/awg/init
func (h *InstallHandler) SetupInitScript(w http.ResponseWriter, _ *http.Request) {
	targetPath := awgInitScriptPath

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o750); err != nil {
		respondJSON(w, http.StatusInternalServerError, InitScriptResponse{
			Success: false,
			Message: fmt.Sprintf("failed to create init.d dir: %v", err),
		})
		return
	}

	//nolint:gosec // AWG init script needs execute
	if err := os.WriteFile(targetPath, []byte(installAWGInitScript), 0o755); err != nil {
		respondJSON(w, http.StatusInternalServerError, InitScriptResponse{
			Success: false,
			Message: fmt.Sprintf("failed to write init script: %v", err),
		})
		return
	}

	// Remove legacy non-S-prefixed script so rc.unslung doesn't get
	// confused and there's a single source of truth.
	if err := os.Remove(awgInitScriptPathOld); err != nil && !os.IsNotExist(err) {
		log.Printf("[install] Warning: failed to remove legacy %s: %v", awgInitScriptPathOld, err)
	}

	log.Printf("[install] AWG init script written to %s", targetPath)
	respondJSON(w, http.StatusOK, InitScriptResponse{
		Success: true,
		Message: "init script updated (auto-starts at boot as S90awg)",
	})
}

// AWGInstallProgress is sent as SSE events during installation.
type AWGInstallProgress struct {
	Percent int    `json:"percent"`
	Status  string `json:"status"`
	Error   string `json:"error,omitempty"`
}

// RegisterInstallRoutes registers AWG installation routes.
func RegisterInstallRoutes(apiRouter *mux.Router, h *InstallHandler) {
	sub := apiRouter.PathPrefix("/install").Subrouter()
	sub.HandleFunc("/awg/status", h.Status).Methods("GET")
	sub.HandleFunc("/awg/install", h.Install).Methods("POST")
	sub.HandleFunc("/awg/uninstall", h.Uninstall).Methods("POST")
	sub.HandleFunc("/awg/init", h.SetupInitScript).Methods("POST")
}

// Install downloads and installs amneziawg-go and amneziawg-tools.
// POST /api/install/awg/install
func (h *InstallHandler) Install(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	if h.installing {
		h.mu.Unlock()
		respondError(w, http.StatusConflict, "installation already in progress")
		return
	}
	h.installing = true
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		h.installing = false
		h.mu.Unlock()
	}()

	// SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	sendEvent := func(event string, data interface{}) {
		jsonData, _ := json.Marshal(data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, string(jsonData))
		flusher.Flush()
	}

	// Write install script to temp
	installScriptPath := "/tmp/install-awg.sh"
	//nolint:gosec // AWG install script needs execute
	if err := os.WriteFile(installScriptPath, []byte(installAWGScript), 0o755); err != nil {
		sendEvent("error", AWGInstallProgress{Percent: 0, Status: "failed",
			Error: fmt.Sprintf("cannot write script: %v", err)})
		return
	}
	defer func() { _ = os.Remove(installScriptPath) }()

	sendEvent("progress", AWGInstallProgress{Percent: 5, Status: "starting installation..."})

	// Execute install script, pipe stdout line by line
	cmd := exec.CommandContext(r.Context(), "sh", installScriptPath)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		sendEvent("error", AWGInstallProgress{Percent: 0, Status: "failed",
			Error: fmt.Sprintf("pipe error: %v", err)})
		return
	}

	if err := cmd.Start(); err != nil {
		sendEvent("error", AWGInstallProgress{Percent: 0, Status: "failed",
			Error: fmt.Sprintf("cannot start install: %v", err)})
		return
	}

	// SSE events: DETECT→10 LISTING→20 FOUND→25 DOWNLOAD→45 INSTALLING→70 OK DONE→100
	progressMap := map[string]int{
		"DETECT":     10,
		"LISTING":    20,
		"FOUND":      25,
		"DOWNLOAD":   45,
		"INSTALLING": 70,
	}

	buf := make([]byte, 4096)
	remaining := ""
	lastProgress := 5

	for {
		n, err := stdout.Read(buf)
		if n > 0 {
			remaining += string(buf[:n])
			for {
				idx := strings.IndexByte(remaining, '\n')
				if idx < 0 {
					break
				}
				line := strings.TrimRight(remaining[:idx], "\r\n")
				remaining = remaining[idx+1:]

				if line == "" {
					continue
				}

				// Check for DONE
				if line == "DONE" {
					sendEvent("progress", AWGInstallProgress{Percent: 100, Status: "installation complete"})
					goto waitExit
				}

				// Check for ERROR:message
				if strings.HasPrefix(line, "ERROR:") {
					msg := strings.TrimPrefix(line, "ERROR:")
					sendEvent("error", AWGInstallProgress{Percent: lastProgress, Status: "failed", Error: msg})
					goto waitExit
				}

				// Check for progress prefixes
				colonIdx := strings.IndexByte(line, ':')
				if colonIdx > 0 {
					prefix := line[:colonIdx]
					val := line[colonIdx+1:]

					if pct, ok := progressMap[prefix]; ok {
						lastProgress = pct
						sendEvent("progress", AWGInstallProgress{Percent: pct, Status: val})
					} else if prefix == "OK" {
						sendEvent("progress", AWGInstallProgress{Percent: lastProgress + 5, Status: val})
					}
				}
			}
		}
		if err != nil {
			break
		}
	}

waitExit:
	if err := cmd.Wait(); err != nil {
		log.Printf("[install] command wait: %v", err)
	}

	if cmd.ProcessState.ExitCode() == 0 {
		// Auto-create init script after successful install
		targetPath := awgInitScriptPath
		_ = os.MkdirAll(filepath.Dir(targetPath), 0o750)
		if werr := os.WriteFile(targetPath, []byte(installAWGInitScript), 0o755); werr == nil { //nolint:gosec // AWG init script needs execute
			_ = os.Remove(awgInitScriptPathOld) // clean up legacy
			log.Printf("[install] AWG init script auto-created at %s", targetPath)
		} else {
			log.Printf("[install] Warning: failed to create init script: %v", werr)
		}
	}

	log.Printf("[install] AmneziaWG installation finished (exit code: %d)", cmd.ProcessState.ExitCode())
}

// Uninstall removes amneziawg-go and amneziawg-tools.
// POST /api/install/awg/uninstall
func (h *InstallHandler) Uninstall(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	if h.installing {
		h.mu.Unlock()
		respondError(w, http.StatusConflict, "operation already in progress")
		return
	}
	h.installing = true
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		h.installing = false
		h.mu.Unlock()
	}()

	// SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	sendEvent := func(event string, data interface{}) {
		jsonData, _ := json.Marshal(data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, string(jsonData))
		flusher.Flush()
	}

	// Write uninstall script to temp
	scriptPath := "/tmp/uninstall-awg.sh"
	//nolint:gosec // AWG uninstall script needs execute
	if err := os.WriteFile(scriptPath, []byte(installAWGUninstallScript), 0o755); err != nil {
		sendEvent("error", AWGInstallProgress{Percent: 0, Status: "failed",
			Error: fmt.Sprintf("cannot write script: %v", err)})
		return
	}
	defer func() { _ = os.Remove(scriptPath) }()

	sendEvent("progress", AWGInstallProgress{Percent: 5, Status: "starting uninstall..."})

	// Execute uninstall script, pipe stdout line by line
	cmd := exec.CommandContext(r.Context(), "sh", scriptPath)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		sendEvent("error", AWGInstallProgress{Percent: 0, Status: "failed",
			Error: fmt.Sprintf("pipe error: %v", err)})
		return
	}

	if err := cmd.Start(); err != nil {
		sendEvent("error", AWGInstallProgress{Percent: 0, Status: "failed",
			Error: fmt.Sprintf("cannot start uninstall: %v", err)})
		return
	}

	// SSE events: CLEANING→20 REMOVING→50 OK→75 DONE→100
	progressMap := map[string]int{
		"CLEANING":   20,
		"REMOVING":   50,
	}

	buf := make([]byte, 4096)
	remaining := ""
	lastProgress := 5

	for {
		n, err := stdout.Read(buf)
		if n > 0 {
			remaining += string(buf[:n])
			for {
				idx := strings.IndexByte(remaining, '\n')
				if idx < 0 {
					break
				}
				line := strings.TrimRight(remaining[:idx], "\r\n")
				remaining = remaining[idx+1:]

				if line == "" {
					continue
				}

				// Check for DONE
				if line == "DONE" {
					sendEvent("progress", AWGInstallProgress{Percent: 100, Status: "uninstall complete"})
					goto waitUninstallExit
				}

				// Check for ERROR:message
				if strings.HasPrefix(line, "ERROR:") {
					msg := strings.TrimPrefix(line, "ERROR:")
					sendEvent("error", AWGInstallProgress{Percent: lastProgress, Status: "failed", Error: msg})
					goto waitUninstallExit
				}

				// Check for progress prefixes
				colonIdx := strings.IndexByte(line, ':')
				if colonIdx > 0 {
					prefix := line[:colonIdx]
					val := line[colonIdx+1:]

					if pct, ok := progressMap[prefix]; ok {
						lastProgress = pct
						sendEvent("progress", AWGInstallProgress{Percent: pct, Status: val})
					} else if prefix == "OK" {
						sendEvent("progress", AWGInstallProgress{Percent: lastProgress + 5, Status: val})
					} else if prefix == "WARN" {
						sendEvent("progress", AWGInstallProgress{Percent: lastProgress, Status: val})
					}
				}
			}
		}
		if err != nil {
			break
		}
	}

waitUninstallExit:
	if err := cmd.Wait(); err != nil {
		log.Printf("[install] command wait: %v", err)
	}
	log.Printf("[install] AmneziaWG uninstall finished (exit code: %d)", cmd.ProcessState.ExitCode())
}
