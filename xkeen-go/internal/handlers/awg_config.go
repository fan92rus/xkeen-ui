package handlers

// Config file management for AWG: delete (with teardown) and upload.

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
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

// CreateServer creates a new AWG server config (opkgtunN) from scratch.
// POST /api/awg/server/create  body: {"name","subnet","port","obfuscation","params"}
//
// It generates a fresh keypair, writes the .conf file, and returns the NDMS
// commands needed to create the matching OpkgTun netdev. It does NOT create
// the NDMS interface itself (that is the firmware's job) and does NOT bring
// the interface up — the caller does that after creating the netdev.
func (h *AWGHandler) CreateServer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string            `json:"name"`
		Subnet      string            `json:"subnet"`
		Port        int               `json:"port"`
		Obfuscation string            `json:"obfuscation"`
		Params      map[string]string `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// --- validate name ---------------------------------------------------
	if !validateServerName(req.Name) {
		respondError(w, http.StatusBadRequest, "invalid server name: must match opkgtunN (e.g. opkgtun1)")
		return
	}
	confPath := filepath.Join(h.awgDir, req.Name+".conf")
	if _, err := os.Stat(confPath); err == nil {
		respondError(w, http.StatusConflict, fmt.Sprintf("config %s already exists", req.Name))
		return
	}

	// --- validate subnet -------------------------------------------------
	subnet, err := parseServerSubnet(req.Subnet)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid subnet: "+err.Error())
		return
	}
	if err := h.checkSubnetConflict(subnet, req.Name); err != nil {
		respondError(w, http.StatusConflict, err.Error())
		return
	}

	// --- validate port ---------------------------------------------------
	if req.Port < 1 || req.Port > 65535 {
		respondError(w, http.StatusBadRequest, "port must be 1..65535")
		return
	}
	if err := h.checkPortConflict(req.Port, req.Name); err != nil {
		respondError(w, http.StatusConflict, err.Error())
		return
	}

	// --- obfuscation preset ---------------------------------------------
	params, err := resolveObfuscationParams(req.Obfuscation, req.Params)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	// --- generate keypair -----------------------------------------------
	privKey, _, err := awgGenKeyFn()
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to generate keypair: %v", err))
		return
	}

	// --- write config ----------------------------------------------------
	gateway := subnetNetworkPlusOne(subnet)
	var b strings.Builder
	b.WriteString("[Interface]\n")
	b.WriteString(fmt.Sprintf("PrivateKey = %s\n", privKey))
	b.WriteString(fmt.Sprintf("Address = %s\n", gateway))
	b.WriteString(fmt.Sprintf("ListenPort = %d\n", req.Port))
	b.WriteString("MTU = 1420\n")
	for _, key := range awgParamKeys {
		if v, ok := params[key]; ok && v != "" {
			b.WriteString(fmt.Sprintf("%s = %s\n", key, v))
		}
	}
	b.WriteString("\n")

	if err := os.MkdirAll(h.awgDir, 0o750); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create AWG dir: %v", err))
		return
	}
	if err := os.WriteFile(confPath, []byte(b.String()), 0o600); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to write config %s: %v", confPath, err))
		return
	}

	log.Printf("[awg] created server config %s (subnet %s, port %d)", req.Name, subnet, req.Port)

	// Apply the NDMS interface automatically when ndmc is available (xkeen-ui
	// runs on the router). Every Keenetic CLI command is a separate `ndmc -c`
	// invocation, re-entering the interface context for each subcommand —
	// multiline `ndmc -c` is rejected by NDM and a plain pipe needs a tty.
	ndmsApplied, ndmsErr := applyNDMSInterface(req.Name, subnet)
	msg := fmt.Sprintf("Server config %q created. NDMS interface registered — reboot the router once for its netdev to appear, then press Start.", req.Name)
	if !ndmsApplied {
		msg = fmt.Sprintf("Server config %q created, but NDMS interface could not be created automatically — run the commands below in the Keenetic CLI, then press Start.", req.Name)
	}

	respondJSON(w, http.StatusCreated, map[string]interface{}{
		"success":       true,
		"name":          req.Name,
		"path":          confPath,
		"subnet":        subnet,
		"port":          req.Port,
		"gateway":       gateway,
		"ndms_applied":  ndmsApplied,
		"ndms_error":    ndmsErr,
		"ndms_commands": ndmsCreateCommands(req.Name, subnet),
		"message":       msg,
	})
}

// applyNDMSInterface creates the matching Keenetic OpkgTun interface via ndm
// CLI. Keenetic's `ndmc -c` executes exactly one command; subcommands must be
// prefixed with the interface name in each call. Returns applied=false with
// an error string when ndmc is missing or a command fails.
func applyNDMSInterface(name, subnet string) (applied bool, errMsg string) {
	if _, err := ndmcLookPathFn("ndmc"); err != nil {
		return false, "ndmc not found (xkeen-ui not running on a Keenetic router?)"
	}
	ndmsName := "OpkgTun" + strings.TrimPrefix(name, "opkgtun")
	gw := subnetNetworkPlusOne(subnet)
	mask := "255.255.255.0"
	if _, ipnet, err := net.ParseCIDR(subnet); err == nil {
		m := ipnet.Mask
		mask = fmt.Sprintf("%d.%d.%d.%d", m[0], m[1], m[2], m[3])
	}
	cmds := []string{
		fmt.Sprintf("interface %s", ndmsName),
		fmt.Sprintf("interface %s description AWG-Server-%s", ndmsName, strings.TrimPrefix(name, "opkgtun")),
		fmt.Sprintf("interface %s security-level private", ndmsName),
		fmt.Sprintf("interface %s ip address %s %s", ndmsName, strings.SplitN(gw, "/", 2)[0], mask),
		fmt.Sprintf("interface %s up", ndmsName),
		"system configuration save",
	}
	for _, cmd := range cmds {
		out, err := ndmcRunFn("-c", cmd)
		if err == nil {
			continue
		}
		// Keenetic's ndmc sometimes exits non-zero even when the command took
		// effect. Trust explicit success markers in its output.
		text := strings.ToLower(string(out))
		if strings.Contains(text, "created") || strings.Contains(text, "saved") ||
			strings.Contains(text, "done") || strings.Contains(text, "already") ||
			strings.Contains(text, "saved.") {
			log.Printf("[awg] ndmc %q exited %v but output indicates success", cmd, err)
			continue
		}
		log.Printf("[awg] ndmc %q failed: %v\n%s", cmd, err, string(out))
		return false, fmt.Sprintf("ndmc %q failed: %v", cmd, err)
	}

	// Keenetic 5.x registers OpkgTun interfaces in the NDMS config immediately
	// but does NOT materialize the netdev until a full config rebuild/reboot.
	// Verify the interface is present in the NDMS config (not the netdev) —
	// this is the stable success signal across firmware versions.
	if out, err := ndmcRunFn("-c", "show interface "+ndmsName); err == nil || strings.Contains(string(out), ndmsName) {
		if strings.Contains(string(out), ndmsName) {
			log.Printf("[awg] NDMS interface %s created in system config (netdev appears after reboot)", ndmsName)
			return true, ""
		}
	}
	return false, fmt.Sprintf("interface %s was not found in NDMS config after creation — check the Keenetic CLI", ndmsName)
}

// --- helpers ---------------------------------------------------------------

func validateServerName(name string) bool {
	if !strings.HasPrefix(name, "opkgtun") {
		return false
	}
	suffix := strings.TrimPrefix(name, "opkgtun")
	if suffix == "" {
		return false
	}
	for _, c := range suffix {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func parseServerSubnet(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("subnet is required (e.g. 10.8.1.0/24)")
	}
	ip, ipnet, err := net.ParseCIDR(s)
	if err != nil {
		return "", err
	}
	if !ip.Equal(ipnet.IP) {
		return "", fmt.Errorf("must be a network address (e.g. 10.8.1.0/24, not 10.8.1.5/24)")
	}
	ones, bits := ipnet.Mask.Size()
	if bits != 32 || ones < 16 || ones > 30 {
		return "", fmt.Errorf("only IPv4 /16../30 subnets are supported")
	}
	return ipnet.String(), nil
}

func subnetNetworkPlusOne(cidr string) string {
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return ""
	}
	// gateway is the network address plus one
	gw := make(net.IP, len(ipnet.IP))
	copy(gw, ipnet.IP)
	gw[len(gw)-1] = 1
	ones, _ := ipnet.Mask.Size()
	return fmt.Sprintf("%s/%d", gw.String(), ones)
}

func (h *AWGHandler) checkSubnetConflict(newSubnet, self string) error {
	configs, _ := h.store.ScanAWGConfigs(h.awgDir)
	for _, c := range configs {
		if c.Name == self {
			continue
		}
		conf, err := subscription.ParseAWGConf(filepath.Join(h.awgDir, c.Name+".conf"))
		if err != nil || conf == nil || conf.Interface == nil {
			continue
		}
		addr := conf.Interface.Values["Address"]
		if addr == "" {
			continue
		}
		// parse the existing subnet from the Address (assume /24-class)
		_, existingNet, err := net.ParseCIDR(addr)
		if err != nil {
			// Address may be a single IP like 10.8.0.1/24 — try appending
			continue
		}
		_, newNet, err := net.ParseCIDR(newSubnet)
		if err != nil {
			return err
		}
		if existingNet.Contains(newNet.IP) || newNet.Contains(existingNet.IP) {
			return fmt.Errorf("subnet %s overlaps with %s (%s)", newSubnet, c.Name, addr)
		}
	}
	return nil
}

func (h *AWGHandler) checkPortConflict(port int, self string) error {
	configs, _ := h.store.ScanAWGConfigs(h.awgDir)
	for _, c := range configs {
		if c.Name == self {
			continue
		}
		conf, err := subscription.ParseAWGConf(filepath.Join(h.awgDir, c.Name+".conf"))
		if err != nil || conf == nil {
			continue
		}
		if p := conf.GetListenPort(); p == port {
			return fmt.Errorf("port %d already used by %s", port, c.Name)
		}
	}
	return nil
}

func resolveObfuscationParams(preset string, custom map[string]string) (map[string]string, error) {
	preset = strings.TrimSpace(preset)
	if preset == "" {
		preset = "full"
	}
	if preset == "custom" {
		// validate keys
		out := map[string]string{}
		for k, v := range custom {
			ok := false
			for _, key := range awgParamKeys {
				if k == key {
					ok = true
					break
				}
			}
			if !ok {
				return nil, fmt.Errorf("unknown obfuscation param %q", k)
			}
			out[k] = v
		}
		return out, nil
	}
	if preset == "random" {
		return generateRandomObfuscationParams(), nil
	}
	for _, p := range getAWGObfuscationPresets() {
		if p.ID == preset {
			out := map[string]string{}
			for k, v := range p.Params {
				out[k] = v
			}
			return out, nil
		}
	}
	return nil, fmt.Errorf("unknown obfuscation preset %q", preset)
}

func ndmsCreateCommands(name, subnet string) string {
	// NDMS interface names are CamelCase (OpkgTunN), configs are lowercase
	// (opkgtunN) — convert for the CLI commands.
	ndmsName := "OpkgTun" + strings.TrimPrefix(name, "opkgtun")
	// Derive gateway = network + 1
	gw := subnetNetworkPlusOne(subnet)
	mask := "255.255.255.0"
	if _, ipnet, err := net.ParseCIDR(subnet); err == nil {
		m := ipnet.Mask
		mask = fmt.Sprintf("%d.%d.%d.%d", m[0], m[1], m[2], m[3])
	}
	return fmt.Sprintf(`interface %s
  description AWG-Server-%s
  security-level private
  ip address %s %s
  up
exit
system configuration save`, ndmsName, strings.TrimPrefix(name, "opkgtun"), strings.SplitN(gw, "/", 2)[0], mask)
}

// awgGenKeyFn is the seam for key generation (tests override).
var awgGenKeyFn = generateAWGKeypair

// ndmcRunFn / ndmcLookPathFn are seams for Keenetic CLI calls (tests override).
var (
	ndmcRunFn = func(args ...string) ([]byte, error) {
		return exec.Command("ndmc", args...).CombinedOutput()
	}
	ndmcLookPathFn = exec.LookPath
)
