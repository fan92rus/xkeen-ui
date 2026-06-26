package handlers

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/gorilla/mux"

	"github.com/fan92rus/xkeen-ui/internal/subscription"
)

// ---------- Peer types ----------

// awgPeer represents a single [Peer] section in a server config.
type awgPeer struct {
	PublicKey       string `json:"public_key"`
	AllowedIPs      string `json:"allowed_ips"`
	Label           string `json:"label,omitempty"`            // from comment "# peer: <label>"
	IP              string `json:"ip"`                         // extracted from AllowedIPs (without /32)
	HasClientConfig bool   `json:"has_client_config"`         // stored client .conf available for QR/download
}

// awgServerInfo holds parsed info about a server config.
type awgServerInfo struct {
	Name         string    `json:"name"`
	ListenPort   int       `json:"listen_port"`
	TunnelSubnet string    `json:"tunnel_subnet"`
	PeerCount    int       `json:"peer_count"`
	Peers        []awgPeer `json:"peers"`
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
	var peers []awgPeer
	for _, p := range conf.Peers {
		peer := awgPeer{
			PublicKey:  p.Values["PublicKey"],
			AllowedIPs: p.Values["AllowedIPs"],
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
	clientConf, err := h.generateClientConfig(confPath, privKey, ip)
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

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"success":       true,
		"public_key":    pubKey,
		"client_ip":     ip,
		"client_config": clientConf,
	})
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

	// Also accept key/ip from JSON body (more reliable than query params on DELETE)
	if pubKey == "" && peerIP == "" {
		var body struct {
			Key string `json:"key"`
			IP  string `json:"ip"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
			pubKey = body.Key
			peerIP = body.IP
		}
	}

	if pubKey == "" && peerIP == "" {
		respondError(w, http.StatusBadRequest, "provide key or ip (query param or JSON body)")
		return
	}

	confPath := filepath.Join(h.awgDir, name+".conf")
	if err := removePeerFromFile(confPath, pubKey, peerIP); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to remove peer: %v", err))
		return
	}

	// Also clean up any stored client config
	if peerIP != "" {
		h.removeClientConfig(name, peerIP)
	}

	log.Printf("[awg] removed peer from %s (key=%s ip=%s)", name, maskKey(pubKey), peerIP)
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "peer removed",
	})
}

// ---------- Key generation ----------

// generateAWGKeypair runs `awg genkey` and `awg pubkey` to produce a Curve25519 keypair.
func generateAWGKeypair() (privateKey, publicKey string, err error) {
	// Generate private key
	privOut, err := exec.Command("awg", "genkey").Output()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", "", errors.New("awg binary not found — install AWG tools first")
		}
		return "", "", fmt.Errorf("awg genkey: %w", err)
	}
	privateKey = strings.TrimSpace(string(privOut))

	// Derive public key from private
	pubCmd := exec.Command("awg", "pubkey")
	pubCmd.Stdin = strings.NewReader(privateKey + "\n")
	pubOut, err := pubCmd.Output()
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

// allocatePeerIP finds the next available IP in the tunnel subnet.
// Scans existing peers' AllowedIPs and picks the lowest free address.
func (h *AWGHandler) allocatePeerIP(conf *subscription.AWGConf) (string, error) {
	subnet := conf.GetTunnelSubnet()
	if subnet == "" {
		subnet = "10.8.0.0/24"
	}

	// Extract the base (first 3 octets)
	parts := strings.Split(subnet, ".")
	if len(parts) < 3 {
		return "", errors.New("invalid subnet format")
	}
	base := parts[0] + "." + parts[1] + "." + parts[2]

	// Collect used IPs (skip .0 network, .1 server, .255 broadcast)
	used := make(map[int]bool)
	used[0] = true
	used[1] = true // server
	used[255] = true

	for _, p := range conf.Peers {
		allowed := p.Values["AllowedIPs"]
		if allowed == "" {
			continue
		}
		first := strings.Split(allowed, ",")[0]
		first = strings.TrimSpace(first)
		ipStr := strings.SplitN(first, "/", 2)[0]
		octets := strings.Split(ipStr, ".")
		if len(octets) == 4 {
			last, err := strconv.Atoi(octets[3])
			if err == nil {
				used[last] = true
			}
		}
	}

	// Find lowest free (start from 2)
	for i := 2; i < 255; i++ {
		if !used[i] {
			return fmt.Sprintf("%s.%d", base, i), nil
		}
	}
	return "", errors.New("no free IPs in tunnel subnet (10.8.0.2–10.8.0.254 all used)")
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
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(content)
	return err
}

// removePeerFromFile removes a [Peer] section matching the given public key or IP.
func removePeerFromFile(path, pubKey, peerIP string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	lines := strings.Split(string(content), "\n")
	var result []string
	inPeerSection := false
	skipCurrent := false
	peerMatchKey := strings.TrimSpace(pubKey)
	peerMatchIP := strings.TrimSpace(peerIP)
	if peerMatchIP != "" && !strings.Contains(peerMatchIP, "/") {
		peerMatchIP = peerMatchIP + "/32"
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Track section boundaries
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			// Leaving a peer section
			if inPeerSection && skipCurrent {
				// Already skipped, nothing to do
			}
			inPeerSection = trimmed == "[Peer]"
			skipCurrent = false
		}

		if inPeerSection {
			// Check if this peer matches
			if peerMatchKey != "" && strings.HasPrefix(trimmed, "PublicKey") {
				val := strings.TrimSpace(strings.TrimPrefix(trimmed, "PublicKey"))
				val = strings.TrimPrefix(val, "=")
				val = strings.TrimSpace(val)
				if val == peerMatchKey {
					skipCurrent = true
					continue
				}
			}
			if peerMatchIP != "" && strings.HasPrefix(trimmed, "AllowedIPs") {
				val := strings.TrimSpace(strings.TrimPrefix(trimmed, "AllowedIPs"))
				val = strings.TrimPrefix(val, "=")
				val = strings.TrimSpace(val)
				if strings.Contains(val, peerMatchIP) {
					skipCurrent = true
					continue
				}
			}
			if skipCurrent {
				continue
			}
		}

		result = append(result, line)
	}

	// Clean up trailing empty lines
	output := strings.Join(result, "\n")
	output = strings.TrimRight(output, "\n") + "\n"
	return os.WriteFile(path, []byte(output), 0600)
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
	if err := os.MkdirAll(clientsDir, 0755); err != nil {
		log.Printf("[awg] warning: failed to create clients dir: %v", err)
		return
	}
	if err := os.WriteFile(clientConfPath, []byte(config), 0600); err != nil {
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

// generateClientConfig produces a client .conf from a server config and a new client's keys.
func (h *AWGHandler) generateClientConfig(serverConfPath, clientPrivKey, clientIP string) (string, error) {
	serverConf, err := subscription.ParseAWGConf(serverConfPath)
	if err != nil {
		return "", err
	}
	if serverConf.Interface == nil {
		return "", errors.New("server config has no [Interface] section")
	}

	// Get server's public key from its private key
	serverPriv := serverConf.GetPrivateKey()
	if serverPriv == "" {
		return "", errors.New("server config has no PrivateKey")
	}
	serverPub, err := derivePublicKey(serverPriv)
	if err != nil {
		return "", fmt.Errorf("failed to derive server public key: %w", err)
	}

	// Get listen port and endpoint
	port := serverConf.GetListenPort()
	if port == 0 {
		port = 443
	}

	// Determine endpoint (WAN IP)
	endpoint := h.detectEndpoint()
	if endpoint == "" {
		endpoint = fmt.Sprintf("%%s:%d", port) // placeholder
	} else {
		endpoint = fmt.Sprintf("%s:%d", endpoint, port)
	}

	// Build client config
	var sb strings.Builder
	sb.WriteString("[Interface]\n")
	sb.WriteString(fmt.Sprintf("PrivateKey = %s\n", clientPrivKey))
	sb.WriteString(fmt.Sprintf("Address = %s/32\n", clientIP))
	sb.WriteString("DNS = 1.1.1.1\n")
	sb.WriteString("MTU = 1420\n")

	// Copy AWG obfuscation params from server (Jc, Jmin, Jmax, S1-S4, H1-H4, I1)
	awgParams := []string{"Jc", "Jmin", "Jmax", "S1", "S2", "S3", "S4", "H1", "H2", "H3", "H4", "I1"}
	for _, param := range awgParams {
		if val, ok := serverConf.Interface.Values[param]; ok {
			sb.WriteString(fmt.Sprintf("%s = %s\n", param, val))
		}
	}

	sb.WriteString("\n[Peer]\n")
	sb.WriteString(fmt.Sprintf("PublicKey = %s\n", serverPub))
	sb.WriteString("AllowedIPs = 0.0.0.0/0, ::/0\n")
	sb.WriteString(fmt.Sprintf("Endpoint = %s\n", endpoint))
	sb.WriteString("PersistentKeepalive = 25\n")

	return sb.String(), nil
}

// derivePublicKey computes the public key from a private key via `awg pubkey`.
func derivePublicKey(privateKey string) (string, error) {
	cmd := exec.Command("awg", "pubkey")
	cmd.Stdin = strings.NewReader(privateKey + "\n")
	out, err := cmd.Output()
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
	out, err := exec.Command("ip", "route", "get", "8.8.8.8").Output()
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

// unused but kept for potential future server config generation
var _ = bufio.NewScanner
