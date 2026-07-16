package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"github.com/fan92rus/xkeen-ui/internal/config"
	"github.com/fan92rus/xkeen-ui/internal/subscription"
)

// AWGHandler handles AWG interface management.
type AWGHandler struct {
	store  *subscription.Store
	awgDir string
	cfg    *config.Config // for lan/wan interface settings
}

// NewAWGHandler creates a new AWGHandler.
func NewAWGHandler(store *subscription.Store, awgDir string, cfg *config.Config) *AWGHandler {
	return &AWGHandler{
		store:  store,
		awgDir: awgDir,
		cfg:    cfg,
	}
}

// ---------- API types ----------

type awgInterface struct {
	Name     string `json:"name"`
	ConfPath string `json:"conf_path"`
	Mark     int    `json:"mark"`
	Role     string `json:"role"` // "client" or "server"
	Active   bool   `json:"active"`
	Address  string `json:"address,omitempty"`
}

type awgInterfacesResponse struct {
	Interfaces []awgInterface `json:"interfaces"`
}

type awgActionRequest struct {
	Name string `json:"name"`
}

type awgActionResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

// ---------- Handlers ----------

// ListInterfaces returns all AWG interfaces and their status.
// GET /api/awg/interfaces
func (h *AWGHandler) ListInterfaces(w http.ResponseWriter, _ *http.Request) {
	// Scan for .conf files
	configs, err := h.store.ScanAWGConfigs(h.awgDir)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to scan AWG dir: %v", err))
		return
	}

	// Get active interfaces from awg show
	activeIfaces := h.getActiveInterfaces()

	result := make([]awgInterface, 0, len(configs))
	for _, c := range configs {
		confPath := filepath.Join(h.awgDir, c.Name+".conf")
		_, active := activeIfaces[c.Name]
		iface := awgInterface{
			Name:     c.Name,
			ConfPath: confPath,
			Mark:     c.Mark,
			Role:     string(c.Role),
			Active:   active,
		}
		if active {
			iface.Address = activeIfaces[c.Name]
		}
		result = append(result, iface)
	}

	respondJSON(w, http.StatusOK, &awgInterfacesResponse{Interfaces: result})
}

// UpInterface brings up an AWG interface.
// POST /api/awg/up
func (h *AWGHandler) UpInterface(w http.ResponseWriter, r *http.Request) {
	var req awgActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" || !validateAWGName(req.Name) {
		respondError(w, http.StatusBadRequest, "invalid config name")
		return
	}

	cfg, ok := h.store.GetAWGConfig(req.Name)
	if !ok {
		respondError(w, http.StatusNotFound, fmt.Sprintf("AWG config %q not found", req.Name))
		return
	}

	confPath := filepath.Join(h.awgDir, req.Name+".conf")
	if _, err := os.Stat(confPath); os.IsNotExist(err) {
		respondError(w, http.StatusNotFound, fmt.Sprintf("config file %s not found", confPath))
		return
	}

	// Step 1: awg-quick up
	cmd := exec.Command("awg-quick", "up", confPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			respondError(w, http.StatusInternalServerError, "awg-quick binary not found — install AWG tools via Settings → AWG Install first")
		} else {
			respondError(w, http.StatusInternalServerError, fmt.Sprintf("awg-quick up failed: %v\n%s", err, string(output)))
		}
		return
	}
	log.Printf("[awg] awg-quick up %s: %s", req.Name, strings.TrimSpace(string(output)))

	// Step 2: role-based post-up hooks
	if cfg.Role == subscription.AWGRoleServer {
		// Server: apply full-tunnel firewall preset (iptables + route)
		params := h.getServerFirewallParams(req.Name)
		ApplyFullTunnelFirewall(params)
	} else {
		// Client: ip rule + ip route for fwmark routing (Xray integration)
		h.applyClientFwmarkRouting(req.Name, cfg.Mark)
	}

	// Verify: poll awg show for up to 3s until the interface appears.
	alive := false
	for i := 0; i < 6; i++ {
		active := h.getActiveInterfaces()
		if addr, ok := active[req.Name]; ok && addr != "" {
			alive = true
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	if !alive {
		log.Printf("[awg] awg-quick up %s — interface did not appear within 3s, but may still come up", req.Name)
		// Don't fail the request — the command returned 0, just log a warning
	} else {
		log.Printf("[awg] awg-quick up %s — interface confirmed up", req.Name)
	}

	respondJSON(w, http.StatusOK, &awgActionResponse{
		Success: true,
		Message: fmt.Sprintf("Interface %q is up (mark %d)", req.Name, cfg.Mark),
	})
}

// DownInterface brings down an AWG interface.
// POST /api/awg/down
func (h *AWGHandler) DownInterface(w http.ResponseWriter, r *http.Request) {
	var req awgActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" || !validateAWGName(req.Name) {
		respondError(w, http.StatusBadRequest, "invalid config name")
		return
	}

	// Role-based pre-down hooks: remove routing/firewall BEFORE awg-quick down
	cfg, ok := h.store.GetAWGConfig(req.Name)
	if ok {
		if cfg.Role == subscription.AWGRoleServer {
			// Server: remove full-tunnel firewall
			params := h.getServerFirewallParams(req.Name)
			RemoveFullTunnelFirewall(params)
		} else {
			// Client: remove fwmark routing
			h.removeClientFwmarkRouting(req.Name, cfg.Mark)
		}
	}

	// awg-quick down — must use the full config path, same as UpInterface.
	// 'awg-quick down <name>' looks for the config in its default directory
	// (/opt/etc/amnezia/amneziawg/), but we store configs in h.awgDir.
	confPath := filepath.Join(h.awgDir, req.Name+".conf")
	cmd := exec.Command("awg-quick", "down", confPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("[awg] awg-quick down %s: %v\n%s", req.Name, err, string(output))
	}

	// Verify: poll awg show for up to 3s until the interface disappears.
	// This prevents a race where the frontend refreshes the interface list
	// before the kernel has fully torn down the interface.
	gone := false
	for i := 0; i < 6; i++ {
		active := h.getActiveInterfaces()
		if _, stillUp := active[req.Name]; !stillUp {
			gone = true
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	if !gone {
		// awg-quick down failed or the interface is stubborn — report error
		msg := fmt.Sprintf("failed to bring interface %q down after 3s", req.Name)
		if err != nil {
			msg = fmt.Sprintf("awg-quick down %q failed: %v\n%s", req.Name, err, strings.TrimSpace(string(output)))
		}
		respondError(w, http.StatusInternalServerError, msg)
		return
	}

	// If the first attempt failed but the interface eventually went down,
	// that's still a success — log it.
	if err != nil {
		log.Printf("[awg] awg-quick down %s eventually succeeded (initial: %v)", req.Name, err)
	} else {
		log.Printf("[awg] awg-quick down %s: %s", req.Name, strings.TrimSpace(string(output)))
	}

	respondJSON(w, http.StatusOK, &awgActionResponse{
		Success: true,
		Message: fmt.Sprintf("Interface %q is down", req.Name),
	})
}

// DeleteConfig removes an AWG config file, brings down the interface,
// cleans up routing rules, and frees the mark.
// DELETE /api/awg/config/{name}
func (h *AWGHandler) DeleteConfig(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	if name == "" || !validateAWGName(name) {
		respondError(w, http.StatusBadRequest, "invalid config name")
		return
	}

	confPath := filepath.Join(h.awgDir, name+".conf")

	// Prevent path traversal
	absPath, _ := filepath.Abs(confPath)
	absDir, _ := filepath.Abs(h.awgDir)
	if !strings.HasPrefix(absPath, absDir) {
		respondError(w, http.StatusForbidden, "invalid config name")
		return
	}

	if _, err := os.Stat(confPath); os.IsNotExist(err) {
		respondError(w, http.StatusNotFound, fmt.Sprintf("config %s not found", name))
		return
	}

	// Bring down interface (if active) and clean up routing
	var messages []string

	// Get mark before removing
	if mark, ok := h.store.RemoveAWGConfig(name); ok {
		// Remove routing rule + route
		_ = exec.Command("ip", "route", "del", "default", "dev", name,
			"table", fmt.Sprintf("%d", mark)).Run()
		_ = exec.Command("ip", "rule", "del", "fwmark", fmt.Sprintf("%d", mark)).Run() //nolint:gosec // mark from validated AWG config
		messages = append(messages, fmt.Sprintf("freed mark %d", mark))
	}

	// Bring down interface — use full config path, same as DownInterface.
	// 'awg-quick down <name>' searches the default directory, not h.awgDir.
	_ = exec.Command("awg-quick", "down", confPath).Run()
	messages = append(messages, "interface down")

	// Remove config file
	if err := os.Remove(confPath); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to remove config: %v", err))
		return
	}
	messages = append(messages, "config removed")

	// Remove AWG proxies from proxy pool
	allProxies := h.store.GetProxies()
	filtered := make([]*subscription.ProxyEntry, 0, len(allProxies))
	for _, p := range allProxies {
		if p.SubscriptionID != subscription.ReservedAWGSubscriptionID || p.Remarks != name {
			filtered = append(filtered, p)
		}
	}
	h.store.SetProxies(filtered)

	log.Printf("[awg] deleted config %s: %s", name, strings.Join(messages, ", "))

	respondJSON(w, http.StatusOK, &awgActionResponse{
		Success: true,
		Message: fmt.Sprintf("Config %q deleted: %s", name, strings.Join(messages, "; ")),
	})
}

// UploadConfig uploads a new AWG .conf file.
// POST /api/awg/upload
func (h *AWGHandler) UploadConfig(w http.ResponseWriter, r *http.Request) {
	// Parse multipart form (max 64KB)
	if err := r.ParseMultipartForm(64 << 10); err != nil {
		respondError(w, http.StatusBadRequest, fmt.Sprintf("failed to parse form: %v", err))
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		respondError(w, http.StatusBadRequest, "file is required")
		return
	}
	defer file.Close()

	name := header.Filename
	if !strings.HasSuffix(name, ".conf") {
		name += ".conf"
	}
	// Sanitize name: only allow alphanumeric, dash, underscore
	name = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			return r
		}
		return -1
	}, name)

	if name == "" || name == ".conf" {
		respondError(w, http.StatusBadRequest, "invalid filename")
		return
	}

	destPath := filepath.Join(h.awgDir, name)
	if _, err := os.Stat(destPath); err == nil {
		respondError(w, http.StatusConflict, fmt.Sprintf("file %s already exists", name))
		return
	}

	// Ensure directory exists
	if err := os.MkdirAll(h.awgDir, 0o750); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create AWG dir: %v", err))
		return
	}

	// Read file content
	buf := make([]byte, 64<<10)
	n, err := file.Read(buf)
	if err != nil && err.Error() != "EOF" {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to read upload: %v", err))
		return
	}
	content := buf[:n]

	// Basic validation: must contain [Interface] and [Peer]
	if !strings.Contains(string(content), "[Interface]") || !strings.Contains(string(content), "[Peer]") {
		respondError(w, http.StatusBadRequest, "invalid AWG config: must contain [Interface] and [Peer] sections")
		return
	}

	if err := os.WriteFile(destPath, content, 0o600); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to write config: %v", err))
		return
	}

	log.Printf("[awg] uploaded config: %s", name)

	// Auto-scan to add to tracked configs
	configs, _ := h.store.ScanAWGConfigs(h.awgDir)

	// Determine role for response
	role := "client"
	confName := strings.TrimSuffix(name, ".conf")
	for _, c := range configs {
		if c.Name == confName {
			role = string(c.Role)
			break
		}
	}

	respondJSON(w, http.StatusCreated, map[string]interface{}{
		"success": true,
		"name":    confName,
		"role":    role,
		"message": fmt.Sprintf("Config %q uploaded (%s)", confName, role),
	})
}

// ---------- Helpers ----------

// getActiveInterfaces returns a map of active AWG interface names to their addresses.
func (h *AWGHandler) getActiveInterfaces() map[string]string {
	result := make(map[string]string)

	cmd := exec.Command("awg", "show")
	output, err := cmd.Output()
	if err != nil {
		// awg not installed or not available — log once and return empty
		if errors.Is(err, exec.ErrNotFound) {
			log.Println("[awg] awg binary not found, skipping interface detection")
		} else {
			log.Printf("[awg] awg show failed: %v", err)
		}
		return result
	}

	// Parse `awg show` output. Format (example):
	//   interface: warp
	//     public key: ...
	//     private key: ...
	//     listening port: 51820
	//   peer: ...
	//     endpoint: ...
	//     allowed ips: ...
	//   ...
	lines := strings.Split(string(output), "\n")
	var currentIface string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "interface:") {
			currentIface = strings.TrimSpace(strings.TrimPrefix(line, "interface:"))
			result[currentIface] = ""
		} else if strings.HasPrefix(line, "address:") && currentIface != "" {
			addr := strings.TrimSpace(strings.TrimPrefix(line, "address:"))
			if currentIface != "" {
				result[currentIface] = addr
			}
		}
	}

	return result
}

// validateAWGName checks that a config name contains only safe characters.
func validateAWGName(name string) bool {
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, "\\") ||
		strings.Contains(name, "..") || strings.HasPrefix(name, ".") {
		return false
	}
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') &&
			(r < '0' || r > '9') && r != '-' && r != '_' {
			return false
		}
	}
	return true
}

// ---------- Route registration ----------

// RegisterAWGRoutes registers AWG management routes.
func RegisterAWGRoutes(r *mux.Router, handler *AWGHandler) {
	r.HandleFunc("/awg/interfaces", handler.ListInterfaces).Methods("GET")
	r.HandleFunc("/awg/up", handler.UpInterface).Methods("POST")
	r.HandleFunc("/awg/down", handler.DownInterface).Methods("POST")
	r.HandleFunc("/awg/config/{name}", handler.DeleteConfig).Methods("DELETE")
	r.HandleFunc("/awg/upload", handler.UploadConfig).Methods("POST")

	// Peer management (server configs only)
	r.HandleFunc("/awg/peers/{name}", handler.ListPeers).Methods("GET")
	r.HandleFunc("/awg/peers/{name}", handler.AddPeer).Methods("POST")
	r.HandleFunc("/awg/peers/{name}", handler.DeletePeer).Methods("DELETE")

	// Peer client config (stored for QR/download)
	r.HandleFunc("/awg/peer-config/{name}", handler.GetPeerConfig).Methods("GET")

	// Firewall restore (watchdog)
	r.HandleFunc("/awg/restore-firewall", handler.RestoreFirewall).Methods("POST")

	// Obfuscation presets (server configs)
	r.HandleFunc("/awg/obfuscation/{name}", handler.GetObfuscation).Methods("GET")
	r.HandleFunc("/awg/obfuscation/{name}", handler.ApplyObfuscation).Methods("POST")
}
