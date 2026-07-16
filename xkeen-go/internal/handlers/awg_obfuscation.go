package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand/v2"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/fan92rus/xkeen-ui/internal/subscription"
)

// ---------- Preset definitions ----------

// awgParamKeys is the full set of known AmneziaWG obfuscation parameters.
// Used to detect and strip existing params when applying a new preset.
var awgParamKeys = []string{
	"Jc", "Jmin", "Jmax",
	"S1", "S2", "S3", "S4",
	"H1", "H2", "H3", "H4",
	"I1", "I2", "I3",
}

// awgParamOrder is the preferred write order for params in the config file.
var awgParamOrder = awgParamKeys

// AWGObfuscationPreset defines one obfuscation profile.
type AWGObfuscationPreset struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Warning     string            `json:"warning,omitempty"`
	Params      map[string]string `json:"params"`
	Random      bool              `json:"random,omitempty"` // if true, params generated on apply
}

// getAWGObfuscationPresets returns the obfuscation profiles.
// Order: random (recommended) → max protection → energy-efficient → plain.
//
// AWG parameter cost model:
//   - Jc/Jmin/Jmax: junk packets BEFORE handshake only (every ~2-3 min) → negligible
//   - S1/S2/S3:     stolen bytes from handshake messages → negligible
//   - S4:           stolen bytes from EVERY transport packet → real bandwidth cost
//   - H1-H4:        header obfuscation bits, default 1,2,3,4 — CANNOT be 0
//                    (Keenetic kernel rejects H=0), negligible cost, can randomize ≥1
//   - I1:           init packet magic header → negligible (changes fingerprint)
func getAWGObfuscationPresets() []AWGObfuscationPreset {
	return []AWGObfuscationPreset{
		{
			ID:          "random",
			Name:        "🎲 Random (уникальный)",
			Description: "Уникальные параметры для вашего сервера",
			Warning:     "Генерирует случайный набор — каждый сервер выглядит по-разному",
			Params:      nil, // generated on apply
			Random:      true,
		},
		{
			ID:          "full",
			Name:        "🛡 Full",
			Description: "Jc=8 + stolen packets, максимальная защита",
			Warning:     "Нагрузка только на хендшейк (~каждые 2-3 мин), на трафик не влияет",
			Params: map[string]string{
				"Jc": "8", "Jmin": "50", "Jmax": "100",
				"S1": "30", "S2": "20", "S3": "0", "S4": "0",
				"H1": "1", "H2": "2", "H3": "3", "H4": "4",
				"I1": "0",
			},
		},
		{
			ID:          "light",
			Name:        "⚡ Light",
			Description: "Jc=4, стандартный AWG (как WARP)",
			Warning:     "",
			Params: map[string]string{
				"Jc": "4", "Jmin": "40", "Jmax": "70",
				"S1": "0", "S2": "0", "S3": "0", "S4": "0",
				"H1": "1", "H2": "2", "H3": "3", "H4": "4",
				"I1": "0",
			},
		},
		{
			ID:          "minimal",
			Name:        "🌿 Minimal",
			Description: "Jc=2, минимальная обфускация",
			Warning:     "",
			Params: map[string]string{
				"Jc": "2", "Jmin": "20", "Jmax": "40",
				"S1": "0", "S2": "0", "S3": "0", "S4": "0",
				"H1": "1", "H2": "2", "H3": "3", "H4": "4",
				"I1": "0",
			},
		},
		{
			ID:          "plain",
			Name:        "○ Plain WG",
			Description: "Без AWG-обфускации",
			Warning:     "НЕ работает для WARP! Только для своего сервера",
			Params:      map[string]string{},
		},
	}
}

// generateRandomObfuscationParams creates a unique set of AWG parameters.
// Each server gets different values, preventing DPI from creating a universal
// fingerprint for all xkeen-ui deployments.
//
// H1-H4 must be >= 1 (Keenetic kernel rejects H=0, handshake fails).
// Default is 1,2,3,4; we randomize in 1-100 range for per-server uniqueness.
//nolint:gosec // non-crypto randomness for AWG DPI obfuscation (safe by design)
func generateRandomObfuscationParams() map[string]string {
	jmin := 30 + rand.IntN(20)        // 30-49
	jmax := jmin + 20 + rand.IntN(20) // 50-88 (always > jmin)
	return map[string]string{
		"Jc":   strconv.Itoa(4 + rand.IntN(8)),   // 4-11
		"Jmin": strconv.Itoa(jmin),
		"Jmax": strconv.Itoa(jmax),
		"S1":   strconv.Itoa(rand.IntN(24)),      // 0-23
		"S2":   strconv.Itoa(rand.IntN(24)),      // 0-23
		"S3":   "0",
		"S4":   "0",
		"H1":   strconv.Itoa(1 + rand.IntN(100)), // 1-100 (0 = handshake rejected)
		"H2":   strconv.Itoa(1 + rand.IntN(100)), // 1-100
		"H3":   strconv.Itoa(1 + rand.IntN(100)), // 1-100
		"H4":   strconv.Itoa(1 + rand.IntN(100)), // 1-100
		"I1":   "0",
	}
}

// ---------- Config modification ----------

// isAWGParamLine checks if a config line sets an AWG obfuscation parameter.
func isAWGParamLine(line string) bool {
	idx := strings.Index(line, "=")
	if idx < 0 {
		return false
	}
	key := strings.TrimSpace(line[:idx])
	for _, k := range awgParamKeys {
		if key == k {
			return true
		}
	}
	return false
}

// applyObfuscationToConfig replaces all AWG obfuscation parameters in the
// [Interface] section of the given config file with the provided params.
// Existing params are stripped first, then new ones are inserted before the
// first [Peer] section (or at end of [Interface]).
func applyObfuscationToConfig(confPath string, params map[string]string) error {
	content, err := os.ReadFile(confPath)
	if err != nil {
		return err
	}

	lines := strings.Split(string(content), "\n")
	var result = make([]string, 0, len(lines))
	inInterface := false
	inserted := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Detect section boundaries
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			// Leaving [Interface] — insert AWG params before next section
			if inInterface && !inserted {
				for _, key := range awgParamOrder {
					if val, ok := params[key]; ok {
						result = append(result, fmt.Sprintf("%s = %s", key, val))
					}
				}
				inserted = true
			}
			inInterface = trimmed == "[Interface]"
		}

		// Skip existing AWG params in [Interface] (they'll be replaced)
		if inInterface && isAWGParamLine(trimmed) {
			continue
		}

		result = append(result, line)
	}

	// File ends while still in [Interface] (no [Peer] after)
	if inInterface && !inserted && len(params) > 0 {
		for _, key := range awgParamOrder {
			if val, ok := params[key]; ok {
				result = append(result, fmt.Sprintf("%s = %s", key, val))
			}
		}
	}

	output := strings.Join(result, "\n")
	return os.WriteFile(confPath, []byte(output), 0o600)
}

// readAWGParams extracts all AWG obfuscation parameters present in the
// [Interface] section of a parsed config. Returns an empty map when none are
// set (plain WireGuard) or when the config has no [Interface] section.
func readAWGParams(conf *subscription.AWGConf) map[string]string {
	params := make(map[string]string)
	if conf == nil || conf.Interface == nil {
		return params
	}
	for _, key := range awgParamKeys {
		if val, exists := conf.Interface.Values[key]; exists {
			params[key] = val
		}
	}
	return params
}

// rewriteEndpointPort updates the port of every `Endpoint = host:port` line in
// a config file, preserving the host. Handles IPv6 bracketed hosts
// (`[::1]:443`) by splitting on the LAST colon. No-op when port <= 0.
func rewriteEndpointPort(clientPath string, port int) error {
	if port <= 0 {
		return nil
	}
	data, err := os.ReadFile(clientPath)
	if err != nil {
		return err
	}
	portStr := strconv.Itoa(port)
	lines := strings.Split(string(data), "\n")
	changed := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		idx := strings.Index(trimmed, "=")
		if idx < 0 || strings.TrimSpace(trimmed[:idx]) != "Endpoint" {
			continue
		}
		val := strings.TrimSpace(trimmed[idx+1:])
		c := strings.LastIndex(val, ":")
		if c < 0 {
			continue // malformed endpoint — leave untouched
		}
		lines[i] = "Endpoint = " + val[:c] + ":" + portStr
		changed = true
	}
	if !changed {
		return nil
	}
	return os.WriteFile(clientPath, []byte(strings.Join(lines, "\n")), 0o600)
}

// detectObfuscationPreset compares current config params to known presets.
// Returns preset ID ("full", "light", "minimal", "plain") or "custom".
func detectObfuscationPreset(confPath string) (string, error) {
	conf, err := subscription.ParseAWGConf(confPath)
	if err != nil {
		return "", err
	}

	current := readAWGParams(conf)

	if len(current) == 0 {
		return "plain", nil
	}

	for _, preset := range getAWGObfuscationPresets() {
		if preset.Random {
			continue // random preset has no fixed params to match
		}
		if awgParamsEqual(current, preset.Params) {
			return preset.ID, nil
		}
	}

	return "custom", nil
}

func awgParamsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
}

// updateClientConfigsObfuscation updates the [Interface] AWG params in all
// stored client configs for the given server.
func (h *AWGHandler) updateClientConfigsObfuscation(server string, params map[string]string) {
	clientsDir := filepath.Join(h.awgDir, "clients")
	entries, err := os.ReadDir(clientsDir)
	if err != nil {
		return // dir doesn't exist or unreadable — skip
	}

	prefix := server + "-"
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), prefix) || !strings.HasSuffix(entry.Name(), ".conf") {
			continue
		}
		clientPath := filepath.Join(clientsDir, entry.Name())
		if err := applyObfuscationToConfig(clientPath, params); err != nil {
			log.Printf("[awg] failed to update client config %s: %v", entry.Name(), err)
		}
	}
}

// ---------- Interface restart ----------

// restartInterface does a full down + up cycle (required for AWG param changes,
// since syncconf cannot change transport-level obfuscation params).
func (h *AWGHandler) restartInterface(name string, role subscription.AWGRole, mark int) error {
	confPath := filepath.Join(h.awgDir, name+".conf")

	// Pre-down: remove firewall / routing
	if role == subscription.AWGRoleServer {
		params := h.getServerFirewallParams(name)
		RemoveFullTunnelFirewall(params)
	} else {
		h.removeClientFwmarkRouting(name, mark)
	}

	// Down
	if output, err := exec.Command("awg-quick", "down", confPath).CombinedOutput(); err != nil {
		log.Printf("[awg] restart: awg-quick down failed: %v\n%s", err, output)
		// Continue anyway — interface might already be down
	}

	time.Sleep(500 * time.Millisecond)

	// Up
	if output, err := exec.Command("awg-quick", "up", confPath).CombinedOutput(); err != nil {
		return fmt.Errorf("awg-quick up failed: %w\n%s", err, output)
	}

	// Post-up: re-apply firewall / routing
	if role == subscription.AWGRoleServer {
		params := h.getServerFirewallParams(name)
		ApplyFullTunnelFirewall(params)
	} else {
		h.applyClientFwmarkRouting(name, mark)
	}

	return nil
}

// ---------- HTTP handlers ----------

// GetObfuscation returns the current preset and available presets.
// GET /api/awg/obfuscation/{name}
func (h *AWGHandler) GetObfuscation(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	name := vars["name"]
	if !validateAWGName(name) {
		respondError(w, http.StatusBadRequest, "invalid config name")
		return
	}

	confPath := filepath.Join(h.awgDir, name+".conf")
	current, err := detectObfuscationPreset(confPath)
	if err != nil {
		current = "unknown"
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"current": current,
		"presets": getAWGObfuscationPresets(),
	})
}

// ApplyObfuscation applies an obfuscation preset to a config.
// POST /api/awg/obfuscation/{name}  body: {"preset": "full"}
func (h *AWGHandler) ApplyObfuscation(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	name := vars["name"]
	if !validateAWGName(name) {
		respondError(w, http.StatusBadRequest, "invalid config name")
		return
	}

	var req struct {
		Preset string `json:"preset"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Find preset
	var preset AWGObfuscationPreset
	found := false
	for _, p := range getAWGObfuscationPresets() {
		if p.ID == req.Preset {
			preset = p
			found = true
			break
		}
	}
	if !found {
		respondError(w, http.StatusBadRequest, "unknown preset: "+req.Preset)
		return
	}

	// Resolve params (generate if random preset)
	params := preset.Params
	if preset.Random {
		params = generateRandomObfuscationParams()
	}

	confPath := filepath.Join(h.awgDir, name+".conf")
	if _, err := os.Stat(confPath); err != nil {
		respondError(w, http.StatusNotFound, "config not found: "+name)
		return
	}

	// 1. Apply to server config
	if err := applyObfuscationToConfig(confPath, params); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to update config: %v", err))
		return
	}

	// 2. Update all stored client configs
	h.updateClientConfigsObfuscation(name, params)

	// 3. If interface is running, restart it (AWG params need full restart)
	wasRunning := false
	restarted := false
	var restartErr error
	if active := h.getActiveInterfaces(); active != nil {
		if _, isUp := active[name]; isUp {
			wasRunning = true
			cfg, _ := h.store.GetAWGConfig(name)
			var role subscription.AWGRole
			var mark int
			if cfg != nil {
				role = cfg.Role
				mark = cfg.Mark
			}
			restartErr = h.restartInterface(name, role, mark)
			restarted = restartErr == nil
		}
	}

	log.Printf("[awg] applied obfuscation preset '%s' to %s (wasRunning=%v restarted=%v)",
		preset.ID, name, wasRunning, restarted)

	resp := map[string]interface{}{
		"success":     true,
		"preset":      preset.ID,
		"was_running": wasRunning,
		"restarted":   restarted,
		"params":      params,
	}
	if restartErr != nil {
		resp["warning"] = "config updated but interface restart failed: " + restartErr.Error()
	}
	respondJSON(w, http.StatusOK, resp)
}
