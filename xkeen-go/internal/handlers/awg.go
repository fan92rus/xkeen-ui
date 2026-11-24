package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
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
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to scan AWG dir %s: %v", h.awgDir, err))
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

	// Auto-fix (issue #3): old awg builds shipped for Keenetic reject the I2/I3
	// obfuscation params with an opaque `awg setconf: Line unrecognized: I2=`
	// error, and numeric I1 = 0 breaks client apps ("Invalid Signature format":
	// AWG 2.0 clients expect tag-format signatures). These params are optional
	// junk/signature packets (upstream docs: "I2-I5 can be left empty") - strip
	// them from the config instead of failing, and tell the user what happened.
	var autoFixWarning string
	if conf, perr := subscription.ParseAWGConf(confPath); perr == nil && conf.Interface != nil {
		var unsupported []string
		for _, key := range []string{"I1", "I2", "I3"} {
			if _, ok := conf.Interface.Values[key]; ok {
				unsupported = append(unsupported, key)
			}
		}
		if len(unsupported) > 0 {
			peerMu.Lock()
			params := readAWGParams(conf)
			delete(params, "I1")
			delete(params, "I2")
			delete(params, "I3")
			serr := applyObfuscationToConfig(confPath, params)
			peerMu.Unlock()
			if serr != nil {
				respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to auto-remove unsupported %s params: %v", strings.Join(unsupported, ", "), serr))
				return
			}
			autoFixWarning = fmt.Sprintf(
				"removed unsupported %s line(s) from the config: your awg build rejects them (awg setconf: 'Line unrecognized'). They are optional junk-packet params - the interface works without them. You can also try updating the AWG tools via Settings - AWG Install (newer builds support I1-I5) and/or KeeneticOS 5.1+, but this is not guaranteed to help on older firmware",
				strings.Join(unsupported, ", "))
			log.Printf("[awg] %s: %s", req.Name, autoFixWarning)
		}
	}

	// Steps 1+2: bring the interface up (attach or awg-quick) and apply the
	// role-based post-up state. See awg_lifecycle.go.
	attachMode, err := h.bringUp(req.Name, confPath)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.postUp(req.Name, cfg.Role, cfg.Mark)

	// Verify: poll awg show for up to 3s until the interface appears.
	alive := false
	for i := 0; i < ifacePollAttempts; i++ {
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

	msg := fmt.Sprintf("Interface %q is up (mark %d)", req.Name, cfg.Mark)
	if attachMode {
		msg = fmt.Sprintf("Interface %q is up (OpkgTun attach mode, mark %d)", req.Name, cfg.Mark)
	}
	if autoFixWarning != "" {
		msg += " - warning: " + autoFixWarning
	}
	respondJSON(w, http.StatusOK, &awgActionResponse{
		Success: true,
		Message: msg,
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

	// Resolve role/mark; for an untracked config (store reset/rescan lag)
	// fall back to parsing the file so a server's firewall rules never
	// outlive the interface.
	cfg, ok := h.store.GetAWGConfig(req.Name)
	role := subscription.AWGRoleClient
	mark := 0
	if ok {
		role, mark = cfg.Role, cfg.Mark
	} else if h.isServerConf(filepath.Join(h.awgDir, req.Name+".conf")) {
		role = subscription.AWGRoleServer
	}

	// OpkgTun attach mode: the netdev belongs to NDMS and must not be deleted.
	// Detection: netdev OR running daemon (the Keenetic UI may have deleted
	// the netdev while the daemon lives). Runs even for untracked configs.
	if isOpkgTunOwned(req.Name) {
		if err := h.teardown(req.Name, confPathFor(h.awgDir, req.Name), role, mark, false); err != nil {
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		respondJSON(w, http.StatusOK, &awgActionResponse{
			Success: true,
			Message: fmt.Sprintf("Interface %q is down (OpkgTun attach mode; netdev left to KeeneticOS)", req.Name),
		})
		return
	}

	// Quick mode: remove routing/firewall, then awg-quick down + verify.
	h.preDown(req.Name, role, mark)

	// awg-quick down — must use the full config path, same as UpInterface.
	// 'awg-quick down <name>' looks for the config in its default directory
	// (/opt/etc/amnezia/amneziawg/), but we store configs in h.awgDir.
	confPath := filepath.Join(h.awgDir, req.Name+".conf")
	output, err := awgQuickFn("down", confPath)
	if err != nil {
		log.Printf("[awg] awg-quick down %s: %v\n%s", req.Name, err, string(output))
	}

	// Verify: poll awg show for up to 3s until the interface disappears.
	// This prevents a race where the frontend refreshes the interface list
	// before the kernel has fully torn down the interface.
	gone := false
	for i := 0; i < ifacePollAttempts; i++ {
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
// ---------- Route registration ----------

// RegisterAWGRoutes registers AWG management routes.
func RegisterAWGRoutes(r *mux.Router, handler *AWGHandler) {
	r.HandleFunc("/awg/interfaces", handler.ListInterfaces).Methods("GET")
	r.HandleFunc("/awg/up", handler.UpInterface).Methods("POST")
	r.HandleFunc("/awg/down", handler.DownInterface).Methods("POST")
	r.HandleFunc("/awg/config/{name}", handler.DeleteConfig).Methods("DELETE")
	r.HandleFunc("/awg/upload", handler.UploadConfig).Methods("POST")
	// Server config creation (multi-server support)
	r.HandleFunc("/awg/server/create", handler.CreateServer).Methods("POST")

	// Peer management (server configs only)
	r.HandleFunc("/awg/peers/{name}", handler.ListPeers).Methods("GET")
	r.HandleFunc("/awg/peers/{name}", handler.AddPeer).Methods("POST")
	r.HandleFunc("/awg/peers/{name}", handler.DeletePeer).Methods("DELETE")

	// Peer client config (stored for QR/download)
	r.HandleFunc("/awg/peer-config/{name}", handler.GetPeerConfig).Methods("GET")

	// Firewall restore is NOT registered here: the endpoint is wired in the
	// server package with a loopback-only wrapper so the NDM netfilter.d hook
	// can call it without a session (see server/routes.go).

	// Obfuscation presets (server configs)
	r.HandleFunc("/awg/obfuscation/{name}", handler.GetObfuscation).Methods("GET")
	r.HandleFunc("/awg/obfuscation/{name}", handler.ApplyObfuscation).Methods("POST")
}
