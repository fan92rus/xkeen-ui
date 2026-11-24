package handlers

// Config file management for AWG: delete (with teardown) and upload.

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/fan92rus/xkeen-ui/internal/subscription"

	"github.com/gorilla/mux"
)

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

	// Server detection must read the store BEFORE RemoveAWGConfig below
	// (otherwise the tracked role is always gone and a server without an
	// explicit ListenPort would leak its firewall rules).
	role := subscription.AWGRoleClient
	if h.isServerConf(confPath) {
		role = subscription.AWGRoleServer
	}
	trackedMark := 0
	if c, ok := h.store.GetAWGConfig(name); ok {
		trackedMark = c.Mark
	}

	// Get mark before removing. The routing teardown itself goes through
	// removeClientFwmarkRouting (guarded against mark 0, behind the ipRunFn
	// seam) instead of a duplicated inline ip route/rule del.
	if mark, ok := h.store.RemoveAWGConfig(name); ok {
		h.removeClientFwmarkRouting(name, mark)
		messages = append(messages, fmt.Sprintf("freed mark %d", mark))
	}

	// 'awg-quick down <name>' searches the default directory, not h.awgDir.
	// The lifecycle engine handles the pre-down hooks (firewall/fwmark) and
	// the attach-vs-quick dispatch; tolerant: the interface may already be
	// down. In OpkgTun mode the netdev belongs to NDMS and is never deleted.
	_ = h.teardown(name, confPath, role, trackedMark, true)
	if isOpkgTunOwned(name) {
		// Remove any leftover runtime conf files (current per-run pattern and
		// the legacy fixed name from older versions).
		if matches, _ := filepath.Glob(filepath.Join(os.TempDir(), "xkeen-ui-"+name+"-*.set")); matches != nil {
			for _, m := range matches {
				_ = os.Remove(m)
			}
		}
		_ = os.Remove(filepath.Join(os.TempDir(), "xkeen-ui-"+name+".set"))
	}
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
	if err := r.ParseMultipartForm(maxAWGUploadBytes); err != nil {
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

	// Read file content. io.ReadAll+LimitReader instead of a single Read:
	// Read may legally return fewer bytes than the file has (silent truncation)
	// and n>0 with a non-EOF error.
	content, err := io.ReadAll(io.LimitReader(file, maxAWGUploadBytes+1))
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to read upload %s: %v", header.Filename, err))
		return
	}
	if len(content) > maxAWGUploadBytes {
		respondError(w, http.StatusBadRequest, fmt.Sprintf("config file %s exceeds %d KB", header.Filename, maxAWGUploadBytes>>10))
		return
	}

	// Basic validation: must contain [Interface] and [Peer]
	if !strings.Contains(string(content), "[Interface]") || !strings.Contains(string(content), "[Peer]") {
		respondError(w, http.StatusBadRequest, "invalid AWG config: must contain [Interface] and [Peer] sections")
		return
	}

	if err := os.WriteFile(destPath, content, 0o600); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to write config %s: %v", destPath, err))
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
