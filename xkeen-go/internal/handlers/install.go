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
	"strings"
	"sync"

	"github.com/gorilla/mux"
)

//go:embed install-awg.sh
var installAWGScript string

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
	Installed    bool   `json:"installed"`
	GoVersion    string `json:"amneziawg_go_version,omitempty"`
	ToolsVersion string `json:"amneziawg_tools_version,omitempty"`
	Error        string `json:"error,omitempty"`
}

// Status checks if AWG is installed.
// GET /api/install/awg/status
func (h *InstallHandler) Status(w http.ResponseWriter, r *http.Request) {
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

	respondJSON(w, http.StatusOK, resp)
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

	// Write script to temp
	scriptPath := "/tmp/install-awg.sh"
	if err := os.WriteFile(scriptPath, []byte(installAWGScript), 0755); err != nil {
		sendEvent("error", AWGInstallProgress{Percent: 0, Status: "failed",
			Error: fmt.Sprintf("cannot write script: %v", err)})
		return
	}
	defer os.Remove(scriptPath)

	sendEvent("progress", AWGInstallProgress{Percent: 5, Status: "starting installation..."})

	// Execute script, pipe stdout line by line
	cmd := exec.CommandContext(r.Context(), "sh", scriptPath)
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

	// S Event  DETECT → 10%  LISTING → 20%  DOWNLOAD → 40%  INSTALLING → 70%  OK/DONE/ERROR
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
	cmd.Wait()

	log.Printf("[install] AmneziaWG installation finished (exit code: %d)", cmd.ProcessState.ExitCode())
}
