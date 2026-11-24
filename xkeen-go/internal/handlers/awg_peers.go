package handlers

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"

	"strings"
	"sync"

	"github.com/gorilla/mux"

	"github.com/fan92rus/xkeen-ui/internal/subscription"
)

// peerMu serializes config-file mutations (AddPeer/DeletePeer/ApplyObfuscation):
// concurrent add/remove otherwise race on parse→allocate→append and produce
// duplicate IPs or lost updates.
var peerMu sync.Mutex

// ---------- Peer types ----------

// awgPeer represents a single [Peer] section in a server config.
type awgPeer struct {
	PublicKey       string `json:"public_key"`
	AllowedIPs      string `json:"allowed_ips"`
	Label           string `json:"label,omitempty"`   // from comment "# peer: <label>"
	IP              string `json:"ip"`                // extracted from AllowedIPs (without /32)
	HasClientConfig bool   `json:"has_client_config"` // stored client .conf available for QR/download
	Index           int    `json:"index"`             // 0-based position among [Peer] sections
}

// ---------- Handlers ----------

// ListPeers returns the peers of a server config.
// GET /api/awg/peers/{name}
func (h *AWGHandler) ListPeers(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	name := vars["name"]
	if !validateAWGName(name) {
		respondError(w, http.StatusBadRequest, "invalid config name")
		return
	}

	confPath := filepath.Join(h.awgDir, name+".conf")
	conf, err := subscription.ParseAWGConf(confPath)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to parse config: %v", err))
		return
	}

	peers := extractPeers(conf)
	// Mark peers that have a stored client config
	for i := range peers {
		if peers[i].IP != "" {
			if _, err := os.Stat(h.clientConfigPath(name, peers[i].IP)); err == nil {
				peers[i].HasClientConfig = true
			}
		}
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"name":          name,
		"listen_port":   conf.GetListenPort(),
		"tunnel_subnet": conf.GetTunnelSubnet(),
		"peers":         peers,
	})
}

// extractPeers converts parsed [Peer] sections into awgPeer objects.
func extractPeers(conf *subscription.AWGConf) []awgPeer {
	peers := make([]awgPeer, 0, len(conf.Peers))
	for idx, p := range conf.Peers {
		peer := awgPeer{
			PublicKey:  p.Values["PublicKey"],
			AllowedIPs: p.Values["AllowedIPs"],
			Index:      idx,
		}
		// Extract label from preceding comment "# peer: <label>"
		if strings.HasPrefix(p.Comment, "peer:") {
			peer.Label = strings.TrimSpace(strings.TrimPrefix(p.Comment, "peer:"))
		}
		// Extract IP from AllowedIPs (e.g. "10.8.0.2/32" → "10.8.0.2")
		if allowed := peer.AllowedIPs; allowed != "" {
			first := strings.Split(allowed, ",")[0]
			first = strings.TrimSpace(first)
			peer.IP = strings.SplitN(first, "/", 2)[0]
		}
		peers = append(peers, peer)
	}
	return peers
}

// AddPeer generates a new keypair, assigns an IP, and appends a [Peer] section.
// POST /api/awg/peers/{name}  body: {"label": "phone-client"}
func (h *AWGHandler) AddPeer(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	name := vars["name"]
	if !validateAWGName(name) {
		respondError(w, http.StatusBadRequest, "invalid config name")
		return
	}

	var req struct {
		Label string `json:"label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// Allow empty body
		req.Label = ""
	}

	confPath := filepath.Join(h.awgDir, name+".conf")

	peerMu.Lock()
	defer peerMu.Unlock()

	conf, err := subscription.ParseAWGConf(confPath)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to parse config: %v", err))
		return
	}

	// Determine role — must be a server
	cfg, _ := h.store.GetAWGConfig(name)
	if cfg != nil && cfg.Role != subscription.AWGRoleServer {
		respondError(w, http.StatusBadRequest, "peer management is only available for server configs")
		return
	}

	// Generate keypair
	privKey, pubKey, err := generateAWGKeypair()
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to generate keys: %v", err))
		return
	}

	// Assign next available IP
	ip, err := h.allocatePeerIP(conf)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to allocate IP: %v", err))
		return
	}

	// Append [Peer] section to config file
	label := strings.TrimSpace(req.Label)
	peerSection := buildPeerSection(pubKey, ip, label)
	if err := appendToFile(confPath, peerSection); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to update config: %v", err))
		return
	}

	log.Printf("[awg] added peer %s (%s) to %s", ip, label, name)

	// Generate client config
	clientConf, warn, err := h.generateClientConfig(confPath, privKey, ip)
	if err != nil {
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"success":       true,
			"public_key":    pubKey,
			"client_ip":     ip,
			"client_config": "", // partial success
			"warning":       fmt.Sprintf("peer added but client config generation failed: %v", err),
		})
		return
	}

	// Persist client config for later QR/download
	h.saveClientConfig(name, ip, clientConf)

	// Sync running interface so new peer can connect immediately
	synced, syncErr := h.syncInterface(name)
	resp := map[string]interface{}{
		"success":       true,
		"public_key":    pubKey,
		"client_ip":     ip,
		"client_config": clientConf,
		"synced":        synced,
	}
	if syncErr != nil {
		resp["warning"] = "peer added but interface sync failed — restart the interface to apply: " + syncErr.Error()
	} else if warn != "" {
		resp["warning"] = warn
	}
	respondJSON(w, http.StatusOK, resp)
}

// GetPeerConfig returns a previously stored client config for QR/download.
// GET /api/awg/peer-config/{name}?ip=<ip>
func (h *AWGHandler) GetPeerConfig(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	name := vars["name"]
	if !validateAWGName(name) {
		respondError(w, http.StatusBadRequest, "invalid config name")
		return
	}
	peerIP := strings.TrimSpace(r.URL.Query().Get("ip"))
	if peerIP == "" {
		respondError(w, http.StatusBadRequest, "provide ?ip=<ip>")
		return
	}

	clientConfPath := h.clientConfigPath(name, peerIP)
	if _, err := os.Stat(clientConfPath); err != nil {
		respondError(w, http.StatusNotFound, "client config not saved (private key was shown only at creation)")
		return
	}

	// Re-sync the stored client config with the CURRENT server config so the
	// displayed/downloaded config always matches the live server. AWG requires
	// exact obfuscation-param + port match for handshake, and stored clients can
	// drift when the server is edited manually after the peer was created.
	// Best-effort: on any failure we fall back to the stored config as-is.
	h.syncClientWithServer(name, clientConfPath)

	data, err := os.ReadFile(clientConfPath)
	if err != nil {
		respondError(w, http.StatusNotFound, "client config not saved (private key was shown only at creation)")
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"success":       true,
		"client_config": string(data),
	})
}

// DeletePeer removes a [Peer] section by public key or IP.
// DELETE /api/awg/peers/{name}?key=<pubkey>  or  ?ip=<ip>
func (h *AWGHandler) DeletePeer(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	name := vars["name"]
	if !validateAWGName(name) {
		respondError(w, http.StatusBadRequest, "invalid config name")
		return
	}

	pubKey := r.URL.Query().Get("key")
	peerIP := r.URL.Query().Get("ip")

	// Also accept key/ip/index from JSON body (more reliable than query params on DELETE)
	peerIndex := -1
	if pubKey == "" && peerIP == "" {
		var body struct {
			Key   string `json:"key"`
			IP    string `json:"ip"`
			Index int    `json:"index"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
			pubKey = body.Key
			peerIP = body.IP
			peerIndex = body.Index
		}
	}

	if pubKey == "" && peerIP == "" && peerIndex < 0 {
		respondError(w, http.StatusBadRequest, "provide key, ip, or index (query param or JSON body)")
		return
	}

	confPath := filepath.Join(h.awgDir, name+".conf")

	peerMu.Lock()
	defer peerMu.Unlock()

	// Resolve the peer's IP up front (when deletion is by key/index) so the
	// stored client config — which contains the private key — is cleaned up too.
	if peerIP == "" {
		if conf, perr := subscription.ParseAWGConf(confPath); perr == nil && conf != nil {
			for i, p := range conf.Peers {
				match := (pubKey != "" && p.Values["PublicKey"] == pubKey) || (pubKey == "" && i == peerIndex)
				if !match {
					continue
				}
				if allowed := p.Values["AllowedIPs"]; allowed != "" {
					first := strings.TrimSpace(strings.Split(allowed, ",")[0])
					peerIP = strings.SplitN(first, "/", 2)[0]
				}
				break
			}
		}
	}

	if err := removePeerFromFile(confPath, pubKey, peerIP, peerIndex); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to remove peer: %v", err))
		return
	}

	// Also clean up any stored client config
	if peerIP != "" {
		h.removeClientConfig(name, peerIP)
	}

	log.Printf("[awg] removed peer from %s (key=%s ip=%s)", name, maskKey(pubKey), peerIP)

	// Sync running interface so deleted peer is disconnected immediately
	synced, syncErr := h.syncInterface(name)
	resp := map[string]interface{}{
		"success": true,
		"message": "peer removed",
		"synced":  synced,
	}
	if syncErr != nil {
		resp["warning"] = "peer removed but interface sync failed — restart the interface to apply: " + syncErr.Error()
	}
	respondJSON(w, http.StatusOK, resp)
}

// ---------- Live interface sync ----------

// syncInterface applies config changes to a running AWG interface without
// dropping existing connections. Uses `awg syncconf` which atomically updates
// the peer list (adds new peers, removes deleted ones) while preserving
// handshakes/endpoints of unchanged peers.
//
// Returns (true, nil) if synced, (false, nil) if interface not running,
// (false, err) if sync was attempted but failed.
func (h *AWGHandler) syncInterface(name string) (bool, error) {
	active := h.getActiveInterfaces()
	if _, isUp := active[name]; !isUp {
		return false, nil // interface not running — nothing to sync
	}

	confPath := filepath.Join(h.awgDir, name+".conf")
	tmpFile := fmt.Sprintf("/tmp/.awg-sync-%s", name)
	defer func() { _ = os.Remove(tmpFile) }()

	// Step 1: awg-quick strip converts awg-quick format → awg format
	stripped, err := peersExecFn("awg-quick", "strip", confPath)
	if err != nil {
		return false, fmt.Errorf("awg-quick strip failed: %w", err)
	}
	if err := os.WriteFile(tmpFile, stripped, 0o600); err != nil {
		return false, fmt.Errorf("write temp config: %w", err)
	}

	// Step 2: awg syncconf applies changes to the running interface
	if output, err := peersExecFn("awg", "syncconf", name, tmpFile); err != nil {
		return false, fmt.Errorf("awg syncconf failed: %w\n%s", err, output)
	}

	log.Printf("[awg] synced running interface %s after config change", name)
	return true, nil
}

// ---------- Key generation ----------

// generateAWGKeypair runs `awg genkey` and `awg pubkey` to produce a Curve25519 keypair.
func generateAWGKeypair() (privateKey, publicKey string, err error) {
	// Generate private key
	privOut, err := peersExecFn("awg", "genkey")
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", "", errors.New("awg binary not found — install AWG tools first")
		}
		return "", "", fmt.Errorf("awg genkey: %w", err)
	}
	privateKey = strings.TrimSpace(string(privOut))

	// Derive public key from private
	pubOut, err := peersExecFnIn("awg", []string{"pubkey"}, privateKey+"\n")
	if err != nil {
		return privateKey, "", fmt.Errorf("awg pubkey: %w", err)
	}
	publicKey = strings.TrimSpace(string(pubOut))
	return privateKey, publicKey, nil
}

// maskKey returns a masked version of a key for logging.
func maskKey(key string) string {
	if len(key) <= 8 {
		return "***"
	}
	return key[:4] + "..." + key[len(key)-4:]
}

// ---------- IP allocation ----------

// tunnelPrefix derives the IPv4 /prefix used for peer addressing:
// 1) the server's own Address in [Interface] (works even for the FIRST peer),
// 2) fallback: derived from existing peers' AllowedIPs,
// 3) final fallback: 10.8.0.0/24.
func tunnelPrefix(conf *subscription.AWGConf) netip.Prefix {
	if conf.Interface != nil {
		for _, addr := range strings.Split(conf.Interface.Values["Address"], ",") {
			addr = strings.TrimSpace(addr)
			if addr == "" {
				continue
			}
			if p, err := netip.ParsePrefix(addr); err == nil && p.Addr().Is4() {
				return netip.PrefixFrom(p.Masked().Addr(), p.Bits())
			}
		}
	}
	if subnet := conf.GetTunnelSubnet(); subnet != "" {
		if p, err := netip.ParsePrefix(subnet); err == nil && p.Addr().Is4() {
			return p
		}
	}
	return netip.MustParsePrefix("10.8.0.0/24")
}

// allocatePeerIP finds the next available IPv4 host in the tunnel subnet.
// Supports prefixes of any length (not just /24). Skips the network address,
// the server's own address, the broadcast address (for prefixes ≤ /30) and
// addresses already assigned to peers.
func (h *AWGHandler) allocatePeerIP(conf *subscription.AWGConf) (string, error) {
	prefix := tunnelPrefix(conf)

	used := make(map[netip.Addr]bool)
	serverAddrSet := false
	if conf.Interface != nil {
		for _, addr := range strings.Split(conf.Interface.Values["Address"], ",") {
			addr = strings.TrimSpace(addr)
			if addr == "" {
				continue
			}
			// Accept both bare IPs and IP/prefix forms.
			if strings.Contains(addr, "/") {
				if p, err := netip.ParsePrefix(addr); err == nil && p.Addr().Is4() {
					used[p.Addr()] = true
					serverAddrSet = true
				}
				continue
			}
			if a, err := netip.ParseAddr(addr); err == nil && a.Is4() {
				used[a] = true // server address(es)
				serverAddrSet = true
			}
		}
	}
	for _, p := range conf.Peers {
		for _, a := range strings.Split(p.Values["AllowedIPs"], ",") {
			parts := strings.SplitN(strings.TrimSpace(a), "/", 2)
			if len(parts) == 0 {
				continue
			}
			if ip, err := netip.ParseAddr(parts[0]); err == nil && ip.Is4() {
				used[ip] = true
			}
		}
	}

	network := prefix.Masked().Addr()
	used[network] = true // network address
	// Convention: the first usable host is the server itself. When no server
	// Address is set explicitly (e.g. default /24 fallback), reserve it anyway.
	if !serverAddrSet && prefix.Bits() < 31 {
		used[network.Next()] = true
	}
	broadcast := network
	if prefix.Bits() < 31 {
		//nolint:gosec // prefix.Bits() ≤ 128, no overflow possible
		hostBits := uint32(32 - prefix.Bits())
		hostMask := uint32(1)<<hostBits - 1
		b := network.As4()
		ipU := binary.BigEndian.Uint32(b[:]) | hostMask
		broadcast = netip.AddrFrom4([4]byte{byte(ipU >> 24), byte(ipU >> 16), byte(ipU >> 8), byte(ipU)})
		used[broadcast] = true
	}

	for cur := network.Next(); netip.PrefixFrom(cur, prefix.Bits()).Contains(cur) && cur != broadcast; cur = cur.Next() {
		if !used[cur] {
			return cur.String(), nil
		}
	}
	return "", fmt.Errorf("no free IPs in tunnel subnet %s", prefix.String())
}

// ---------- Config file manipulation ----------

// buildPeerSection builds a [Peer] section string for appending.
func buildPeerSection(pubKey, ip, label string) string {
	var sb strings.Builder
	sb.WriteString("\n")
	if label != "" {
		sb.WriteString(fmt.Sprintf("# peer: %s\n", label))
	}
	sb.WriteString("[Peer]\n")
	sb.WriteString(fmt.Sprintf("PublicKey = %s\n", pubKey))
	sb.WriteString(fmt.Sprintf("AllowedIPs = %s/32\n", ip))
	return sb.String()
}

// appendToFile appends content to a file.
func appendToFile(path, content string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = f.WriteString(content)
	return err
}

// removePeerFromFile removes the [Peer] section matching the given public key,
// IP, or (fallback) 0-based index. The whole block is dropped: its header, all
// of its key=value lines, and any "# peer: ..." comment lines directly
// preceding the header. Other peers (and the [Interface] section) are left
// verbatim.
func removePeerFromFile(path, pubKey, peerIP string, peerIndex int) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	matchKey := strings.TrimSpace(pubKey)
	matchIP := strings.TrimSpace(peerIP)
	if matchIP != "" && !strings.Contains(matchIP, "/") {
		matchIP += "/32"
	}

	// Resolve WHICH peer (by 0-based index) to remove. Matching by key/IP is
	// done via a structural parse so we know the index up front, rather than
	// deciding mid-section (which previously left an empty [Peer] header behind
	// because the match key sits after the header). Falls back to peerIndex when
	// no key/IP match is found.
	removeIdx := -1
	if matchKey != "" || matchIP != "" {
		if conf, perr := subscription.ParseAWGConf(path); perr == nil && conf != nil {
			for i, p := range conf.Peers {
				if matchKey != "" && p.Values["PublicKey"] == matchKey {
					removeIdx = i
					break
				}
				if matchIP != "" && strings.Contains(p.Values["AllowedIPs"], matchIP) {
					removeIdx = i
					break
				}
			}
		}
		if removeIdx < 0 && peerIndex >= 0 {
			removeIdx = peerIndex
		}
	} else if peerIndex >= 0 {
		removeIdx = peerIndex
	}
	if removeIdx < 0 {
		return nil // nothing matched — leave the file untouched
	}

	// Single forward pass. Comment lines are deferred (buffered) until the next
	// section header decides whether they precede a kept or a removed peer; this
	// is what lets us drop a removed peer's "# peer: <label>" comment along with
	// its header and body.
	lines := strings.Split(string(content), "\n")
	var out = make([]string, 0, len(lines))
	var commentBuf []string
	curPeer := -1
	skipSection := false

	flushComments := func(keep bool) {
		if keep {
			out = append(out, commentBuf...)
		}
		commentBuf = nil
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		isHeader := strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]")

		if isHeader {
			if trimmed == "[Peer]" {
				curPeer++
				if curPeer == removeIdx {
					skipSection = true
					flushComments(false) // drop the removed peer's preceding comment(s)
				} else {
					skipSection = false
					flushComments(true)
				}
			} else {
				// [Interface] or other section — keep it and any deferred comments.
				skipSection = false
				flushComments(true)
			}
			if skipSection {
				continue
			}
			out = append(out, line)
			continue
		}

		if strings.HasPrefix(trimmed, "#") {
			// Defer: this comment belongs to whichever section header follows.
			commentBuf = append(commentBuf, line)
			continue
		}

		if skipSection {
			continue // drop body of the removed peer
		}
		// Non-comment line in a kept section: emit any deferred comments first
		// (they belong to this section), then the line itself.
		flushComments(true)
		out = append(out, line)
	}
	flushComments(true) // preserve trailing comments after the last section

	output := strings.TrimRight(strings.Join(out, "\n"), "\n") + "\n"
	return os.WriteFile(path, []byte(output), 0o600)
}

// clientConfigPath returns the path to a stored client config.
// Stored in <awgDir>/clients/<server>-<ip>.conf
func (h *AWGHandler) clientConfigPath(server, ip string) string {
	safeIP := strings.ReplaceAll(ip, "/", "_")
	return filepath.Join(h.awgDir, "clients", fmt.Sprintf("%s-%s.conf", server, safeIP))
}

// saveClientConfig persists a generated client config for later QR/download.
func (h *AWGHandler) saveClientConfig(server, ip, config string) {
	clientConfPath := h.clientConfigPath(server, ip)
	clientsDir := filepath.Dir(clientConfPath)
	if err := os.MkdirAll(clientsDir, 0o750); err != nil {
		log.Printf("[awg] warning: failed to create clients dir: %v", err)
		return
	}
	if err := os.WriteFile(clientConfPath, []byte(config), 0o600); err != nil {
		log.Printf("[awg] warning: failed to save client config: %v", err)
	}
}

// removeClientConfig deletes a stored client config when a peer is removed.
func (h *AWGHandler) removeClientConfig(server, ip string) {
	clientConfPath := h.clientConfigPath(server, ip)
	if err := os.Remove(clientConfPath); err != nil && !os.IsNotExist(err) {
		log.Printf("[awg] warning: failed to remove client config: %v", err)
	}
}

// ---------- Client config generation ----------

// syncClientWithServer refreshes a stored client config's server-derived
// fields from the CURRENT server.conf, so viewing/downloading an existing
// peer always returns a config consistent with the live server.
//
// Synced fields (all server-owned, file-only — no shell calls):
//   - [Interface] AWG obfuscation params (Jc/Jmin/Jmax/S1-S4/H1-H4/I1)
//   - [Peer] Endpoint port (from server ListenPort); the host part is preserved
//
// Client-owned fields (PrivateKey, Address, AllowedIPs, PublicKey, Keepalive)
// are never touched. Best-effort: any read/parse/write failure leaves the
// stored config unchanged so the operation never breaks config display.
func (h *AWGHandler) syncClientWithServer(serverName, clientConfPath string) {
	serverConfPath := filepath.Join(h.awgDir, serverName+".conf")
	serverConf, err := subscription.ParseAWGConf(serverConfPath)
	if err != nil {
		return // server config unreadable — leave client config untouched
	}
	// 1. AWG obfuscation params. applyObfuscationToConfig strips existing params
	//    and inserts the server's current set; with an empty map it strips all
	//    params (syncing a plain-WG server correctly removes them from clients).
	params := readAWGParams(serverConf)
	// Never propagate I1/I2/I3 to clients: numeric I1 = 0 breaks client apps
	// ("Invalid Signature format"), I2/I3 break Keenetic awg builds (issue #3).
	delete(params, "I1")
	delete(params, "I2")
	delete(params, "I3")
	if err := applyObfuscationToConfig(clientConfPath, params); err != nil {
		log.Printf("[awg] syncClientWithServer: failed to sync AWG params for %s: %v", filepath.Base(clientConfPath), err)
		return // don't proceed to port rewrite on a half-updated file
	}
	// 2. Endpoint port (from server ListenPort). The host is preserved.
	if port := serverConf.GetListenPort(); port > 0 {
		if err := rewriteEndpointPort(clientConfPath, port); err != nil {
			log.Printf("[awg] syncClientWithServer: failed to sync endpoint port for %s: %v", filepath.Base(clientConfPath), err)
		}
	}
}

// generateClientConfig produces a client .conf from a server config and a new
// client's keys. Returns (config, warning, err); warning is non-empty when the
// config was generated but likely needs manual attention (private endpoint,
// ::/0 without server IPv6).
func (h *AWGHandler) generateClientConfig(serverConfPath, clientPrivKey, clientIP string) (clientConf, warning string, err error) {
	serverConf, err := subscription.ParseAWGConf(serverConfPath)
	if err != nil {
		return "", "", err
	}
	if serverConf.Interface == nil {
		return "", "", errors.New("server config has no [Interface] section")
	}

	// Get server's public key from its private key
	serverPriv := serverConf.GetPrivateKey()
	if serverPriv == "" {
		return "", "", errors.New("server config has no PrivateKey")
	}
	serverPub, err := derivePublicKey(serverPriv)
	if err != nil {
		return "", "", fmt.Errorf("failed to derive server public key: %w", err)
	}

	// Get listen port and endpoint
	port := serverConf.GetListenPort()
	if port == 0 {
		port = 443
	}

	// Determine endpoint host: manual override or auto-detect
	var endpointHost string
	if h.cfg.AWGEndpoint != "" {
		endpointHost = h.cfg.AWGEndpoint // manual override (IP or domain)
	} else {
		endpointHost = h.detectEndpoint()
		if endpointHost == "" {
			endpointHost = h.detectEndpointFromIface()
		}
	}
	if endpointHost == "" {
		return "", "", errors.New("cannot detect WAN endpoint — set awg_endpoint in settings or configure client Endpoint manually")
	}
	endpoint := fmt.Sprintf("%s:%d", endpointHost, port)

	warning = endpointPrivateWarning(endpointHost)

	// Client-config knobs (settings-overridable, sensible defaults)
	dns := h.cfg.AWGClientDNS
	if dns == "" {
		dns = "1.1.1.1"
	}
	mtu := h.cfg.AWGClientMTU
	if mtu == 0 {
		mtu = 1420
	}
	allowedIPs := h.cfg.AWGClientAllowedIPs
	if allowedIPs == "" {
		// Default to IPv4-only: the app never sets up IPv6 forwarding/NAT66
		// for the tunnel, so ::/0 routes mobile IPv6 traffic into a black hole
		// (Android prefers IPv6 — the whole connection appears dead). A router
		// can have global IPv6 on other interfaces (WARP, ULA) which does NOT
		// make ::/0 usable for VPN clients. Opt in explicitly via
		// awg_client_allowed_ips if IPv6 is really set up end-to-end.
		allowedIPs = "0.0.0.0/0"
	}

	// Build client config
	var sb strings.Builder
	sb.WriteString("[Interface]\n")
	sb.WriteString(fmt.Sprintf("PrivateKey = %s\n", clientPrivKey))
	sb.WriteString(fmt.Sprintf("Address = %s/32\n", clientIP))
	sb.WriteString(fmt.Sprintf("DNS = %s\n", dns))
	sb.WriteString(fmt.Sprintf("MTU = %d\n", mtu))

	// Copy ALL AWG obfuscation params present in the server [Interface]
	// (awgParamKeys order, incl. I2/I3 — params other than Jc/Jmin/Jmax must
	// match exactly between server and client or the handshake fails).
	for _, param := range awgParamKeys {
		if val, ok := serverConf.Interface.Values[param]; ok {
			sb.WriteString(fmt.Sprintf("%s = %s\n", param, val))
		}
	}

	sb.WriteString("\n[Peer]\n")
	sb.WriteString(fmt.Sprintf("PublicKey = %s\n", serverPub))
	sb.WriteString(fmt.Sprintf("AllowedIPs = %s\n", allowedIPs))
	sb.WriteString(fmt.Sprintf("Endpoint = %s\n", endpoint))
	sb.WriteString("PersistentKeepalive = 25\n")

	return sb.String(), warning, nil
}

// derivePublicKey computes the public key from a private key via `awg pubkey`.
func derivePublicKey(privateKey string) (string, error) {
	out, err := peersExecFnIn("awg", []string{"pubkey"}, privateKey+"\n")
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", errors.New("awg binary not found")
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// detectEndpoint tries to find the WAN IP for the client endpoint.
func (h *AWGHandler) detectEndpoint() string {
	// Try ip route to find default route's source IP
	out, err := peersExecFn("ip", "route", "get", "8.8.8.8")
	if err != nil {
		return ""
	}
	// Output: "8.8.8.8 via ... src 146.120.53.90 ..."
	re := regexp.MustCompile(`src\s+(\d+\.\d+\.\d+\.\d+)`)
	matches := re.FindStringSubmatch(string(out))
	if len(matches) >= 2 {
		return matches[1]
	}
	return ""
}

// detectEndpointFromIface tries to read the WAN IP from the configured WAN interface.
func (h *AWGHandler) detectEndpointFromIface() string {
	wanIface := h.cfg.AWGWanIface
	if wanIface == "" {
		wanIface = "eth3" // common Keenetic WAN interface
	}
	out, err := peersExecFn("ip", "-4", "addr", "show", "dev", wanIface)
	if err != nil {
		return ""
	}
	// Parse: "inet 146.120.53.90/24 brd ..."
	re := regexp.MustCompile(`inet\s+(\d+\.\d+\.\d+\.\d+)`)
	matches := re.FindStringSubmatch(string(out))
	if len(matches) >= 2 {
		return matches[1]
	}
	return ""
}

// endpointPrivateWarning returns a warning when the endpoint host is an IPv4
// address that is not globally routable (RFC1918, CGNAT 100.64/10, loopback,
// link-local). A phone cannot reach such an endpoint from the internet —
// typical cause: the router sits behind ISP NAT. Domains and public IPs are fine.
func endpointPrivateWarning(host string) string {
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	// Strip a trailing :port if the caller passed host:port by mistake.
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil {
		return "" // domain name — cannot judge here
	}
	if v4 := ip.To4(); v4 != nil {
		if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
			return fmt.Sprintf("endpoint %s is a private/CGNAT address — the client will NOT be able to connect from the internet; set awg_endpoint to your public IP/domain or set up port forwarding", host)
		}
		if v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
			return fmt.Sprintf("endpoint %s is a CGNAT (100.64.0.0/10) address — the client will NOT be able to connect; set awg_endpoint to your public IP/domain or set up port forwarding", host)
		}
		return ""
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsPrivate() {
		return fmt.Sprintf("endpoint %s is not globally routable — the client will NOT be able to connect from the internet", host)
	}
	return ""
}

// Injection seams for tests (same style as opkg*Fn / ipRunFn).
var (
	peersExecFn = func(name string, args ...string) ([]byte, error) {
		return exec.Command(name, args...).Output()
	}
	peersExecFnIn = func(name string, args []string, stdin string) ([]byte, error) {
		cmd := exec.Command(name, args...)
		cmd.Stdin = strings.NewReader(stdin)
		return cmd.Output()
	}
)
