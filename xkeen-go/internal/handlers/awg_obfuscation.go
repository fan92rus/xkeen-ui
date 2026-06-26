package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
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
}

// getAWGObfuscationPresets returns the four obfuscation profiles.
// Order: max protection → energy-efficient.
func getAWGObfuscationPresets() []AWGObfuscationPreset {
	return []AWGObfuscationPreset{
		{
			ID:          "full",
			Name:        "Full AWG",
			Description: "Максимальная защита от DPI",
			Warning:     "+400% мусорных пакетов, высокая нагрузка",
			Params: map[string]string{
				"Jc": "4", "Jmin": "40", "Jmax": "70",
				"S1": "0", "S2": "0",
				"H1": "1", "H2": "2", "H3": "3", "H4": "4",
			},
		},
		{
			ID:          "light",
			Name:        "Light AWG",
			Description: "Баланс защиты и нагрузки",
			Warning:     "~двойной трафик",
			Params: map[string]string{
				"Jc": "2", "Jmin": "30", "Jmax": "50",
				"S1": "0", "S2": "0",
				"H1": "1", "H2": "2", "H3": "3", "H4": "4",
			},
		},
		{
			ID:          "minimal",
			Name:        "Minimal AWG",
			Description: "Минимальная обфускация",
			Warning:     "~одинарный трафик, без H1-H4 (без size obfuscation)",
			Params: map[string]string{
				"Jc": "1", "Jmin": "20", "Jmax": "40",
				"S1": "0", "S2": "0",
			},
		},
		{
			ID:          "plain",
			Name:        "Plain WG",
			Description: "Без AWG-обфускации",
			Warning:     "НЕ работает для WARP! Только для своего сервера",
			Params:      map[string]string{},
		},
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
	var result []string
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
	return os.WriteFile(confPath, []byte(output), 0600)
}

// detectObfuscationPreset compares current config params to known presets.
// Returns preset ID ("full", "light", "minimal", "plain") or "custom".
func detectObfuscationPreset(confPath string) (string, error) {
	conf, err := subscription.ParseAWGConf(confPath)
	if err != nil {
		return "", err
	}

	if conf.Interface == nil {
		return "plain", nil
	}

	// Extract current AWG params from [Interface]
	current := make(map[string]string)
	for _, key := range awgParamKeys {
		if val, exists := conf.Interface.Values[key]; exists {
			current[key] = val
		}
	}

	if len(current) == 0 {
		return "plain", nil
	}

	for _, preset := range getAWGObfuscationPresets() {
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
	var preset *AWGObfuscationPreset
	for _, p := range getAWGObfuscationPresets() {
		if p.ID == req.Preset {
			preset = &p
			break
		}
	}
	if preset == nil {
		respondError(w, http.StatusBadRequest, "unknown preset: "+req.Preset)
		return
	}

	confPath := filepath.Join(h.awgDir, name+".conf")
	if _, err := os.Stat(confPath); err != nil {
		respondError(w, http.StatusNotFound, "config not found: "+name)
		return
	}

	// 1. Apply to server config
	if err := applyObfuscationToConfig(confPath, preset.Params); err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to update config: %v", err))
		return
	}

	// 2. Update all stored client configs
	h.updateClientConfigsObfuscation(name, preset.Params)

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
	}
	if restartErr != nil {
		resp["warning"] = "config updated but interface restart failed: " + restartErr.Error()
	}
	respondJSON(w, http.StatusOK, resp)
}
