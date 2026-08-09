// Package handlers provides HTTP handlers for XKEEN-UI API endpoints.
package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"github.com/fan92rus/xkeen-ui/internal/utils"
)

// ManagedUDPRuleName is the name marker of the UDP routing rule that the
// proxy-ports editor keeps in sync inside 05_routing.json. Xray routing rules
// may carry an arbitrary "name" field.
const ManagedUDPRuleName = "xkeen-ui: UDP proxy ports"

const (
	proxyPortsFile   = "port_proxying.lst"
	excludePortsFile = "port_exclude.lst"
	routingFileName  = "05_routing.json"
)

// ProxyPortsHandler manages the XKeen proxy port lists (port_proxying.lst /
// port_exclude.lst) and keeps the UDP routing rule of 05_routing.json in sync,
// so proxied UDP ports (games, Discord voice, …) are routed through the
// balancer instead of the "udp -> direct" catch-all.
type ProxyPortsHandler struct {
	xkeenConfigDir string
	xrayConfigDir  string
	backupDir      string
	executor       CommandExecutor
}

// NewProxyPortsHandler creates a ProxyPortsHandler. If executor is nil a real
// executor (exec.CommandContext) is used.
func NewProxyPortsHandler(xkeenConfigDir, xrayConfigDir, backupDir string, executor CommandExecutor) *ProxyPortsHandler {
	if executor == nil {
		executor = &realExecutor{}
	}
	return &ProxyPortsHandler{
		xkeenConfigDir: xkeenConfigDir,
		xrayConfigDir:  xrayConfigDir,
		backupDir:      backupDir,
		executor:       executor,
	}
}

// ProxyPortsResponse is the payload of GET /api/settings/proxy-ports.
type ProxyPortsResponse struct {
	Ok       bool   `json:"ok"`
	Mode     string `json:"mode"`      // "proxying" | "exclude" | "none"
	Ports    string `json:"ports"`     // cleaned comma-separated list of the active .lst
	UDPPorts string `json:"udp_ports"` // ports routed via the balancer for UDP (managed routing rule)
	Message  string `json:"message,omitempty"`
}

// UpdateProxyPortsRequest is the payload of PUT /api/settings/proxy-ports.
type UpdateProxyPortsRequest struct {
	Mode     string `json:"mode"`      // "proxying" | "exclude" | "none"
	Ports    string `json:"ports"`     // comma-separated ports/ranges, e.g. "80,443,50000:51000"
	UDPPorts string `json:"udp_ports"` // subset of ports additionally routed via balancer for UDP
}

// portListPath returns the absolute path of a .lst file inside the XKeen dir.
func (h *ProxyPortsHandler) portListPath(name string) string {
	return filepath.Join(h.xkeenConfigDir, name)
}

// GetProxyPorts returns the current proxy port list state.
// GET /api/settings/proxy-ports
func (h *ProxyPortsHandler) GetProxyPorts(w http.ResponseWriter, _ *http.Request) {
	mode, ports := h.readActivePorts()
	udpPorts, _ := h.managedUDPRulePorts()
	respondJSON(w, http.StatusOK, ProxyPortsResponse{
		Ok:       true,
		Mode:     mode,
		Ports:    ports,
		UDPPorts: udpPorts,
	})
}

// UpdateProxyPorts validates, writes and applies the proxy port lists, and
// keeps the managed UDP routing rule in 05_routing.json in sync. Lists are
// written directly in xkeen's .lst format (one port/range per line, ':' range
// separator — the format xkeen's read_ports_file accepts) and applied with
// `xkeen -restart`. Before the restart the merged routing config is
// pre-flight validated with `xray -test -confdir` and rolled back on failure,
// so a bad rule can never take down a running proxy.
// PUT /api/settings/proxy-ports
func (h *ProxyPortsHandler) UpdateProxyPorts(w http.ResponseWriter, r *http.Request) {
	var req UpdateProxyPortsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Mode = strings.TrimSpace(req.Mode)
	switch req.Mode {
	case "proxying", "exclude", "none":
	default:
		respondError(w, http.StatusBadRequest, fmt.Sprintf("invalid mode %q (valid: proxying, exclude, none)", req.Mode))
		return
	}

	ports, err := ValidatePortList(req.Ports)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid ports: "+err.Error())
		return
	}
	udpPorts, err := ValidatePortList(req.UDPPorts)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid udp_ports: "+err.Error())
		return
	}

	switch req.Mode {
	case "proxying":
		if udpPorts != "" && !portListSubset(udpPorts, ports) {
			respondError(w, http.StatusBadRequest, "udp_ports must be a subset of ports in proxying mode")
			return
		}
	case "exclude":
		if udpPorts != "" {
			respondError(w, http.StatusBadRequest, "udp_ports is not applicable in exclude mode (excluded ports bypass Xray entirely)")
			return
		}
	case "none":
		if ports != "" || udpPorts != "" {
			respondError(w, http.StatusBadRequest, "ports must be empty in 'none' mode (proxy all ports)")
			return
		}
	}

	// Current list state (mirrors `xkeen -cp` / `xkeen -cpe`).
	curProxying := readPortListFile(h.portListPath(proxyPortsFile))
	curExclude := readPortListFile(h.portListPath(excludePortsFile))

	proxyingContent, excludeContent := "", ""
	switch req.Mode {
	case "proxying":
		proxyingContent = ports
	case "exclude":
		excludeContent = ports
	}

	// Write the .lst files in xkeen's format (one port/range per line).
	// An empty file == xkeen proxies all ports. Unchanged lists are skipped.
	timestamp := time.Now().Format("20060102-150405")
	listsChanged := false
	for _, l := range []struct {
		name, content, current string
	}{
		{proxyPortsFile, proxyingContent, curProxying},
		{excludePortsFile, excludeContent, curExclude},
	} {
		if l.content == l.current {
			continue
		}
		if _, err := createBackupCore(h.portListPath(l.name), h.backupDir, timestamp); err != nil {
			log.Printf("[proxy-ports] backup failed for %s: %v", l.name, err)
		}
		if err := writePortListFile(h.portListPath(l.name), l.content); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to write "+l.name+": "+err.Error())
			return
		}
		listsChanged = true
	}

	// Keep the managed UDP routing rule in sync BEFORE anything restarts.
	// updateRoutingRule is idempotent and also repairs the legacy array form;
	// it returns a non-empty backup path only when the file actually changed.
	backupPath, err := h.updateRoutingRule(udpPorts)
	if err != nil {
		// Routing file missing/unreadable: apply the lists anyway.
		log.Printf("[proxy-ports] routing update failed (continuing with lists only): %v", err)
	}

	// Pre-flight: never restart with a routing config Xray rejects (this is
	// what used to stop the VPN). On failure everything is rolled back.
	if backupPath != "" {
		if err := h.validateRoutingConfig(); err != nil {
			h.restoreRoutingFile(backupPath)
			h.restoreListFiles(timestamp)
			log.Printf("[proxy-ports] routing validation failed, %s and lists rolled back: %v", routingFileName, err)
			respondError(w, http.StatusInternalServerError,
				"Xray rejected the updated routing config; changes were rolled back: "+err.Error())
			return
		}
	}

	// Apply: `xkeen -restart` re-applies iptables (port lists) and restarts
	// Xray (routing rule). Skipped when nothing actually changed.
	if listsChanged || backupPath != "" {
		ctx, cancel := context.WithTimeout(r.Context(), RestartTimeout)
		defer cancel()
		if _, err := h.executor.Execute(ctx, "xkeen", "-restart"); err != nil {
			respondJSON(w, http.StatusInternalServerError, ProxyPortsResponse{
				Ok:       false,
				Mode:     req.Mode,
				Ports:    ports,
				UDPPorts: udpPorts,
				Message:  "config saved, but applying (xkeen -restart) failed: " + err.Error(),
			})
			return
		}
	}

	// Report the actual applied state.
	mode, appliedPorts := h.readActivePorts()
	appliedUDP, _ := h.managedUDPRulePorts()
	respondJSON(w, http.StatusOK, ProxyPortsResponse{
		Ok:       true,
		Mode:     mode,
		Ports:    appliedPorts,
		UDPPorts: appliedUDP,
	})
}

// writePortListFile writes ports (canonical comma-joined ":" form) into a
// .lst file in xkeen's format: one port or range per line. xkeen's
// read_ports_file accepts only `^[0-9]+(:[0-9]+)?$` per line, so a
// comma-joined single line would be dropped entirely. Empty input produces
// an empty file (xkeen then proxies all ports).
func writePortListFile(path, ports string) error {
	var b strings.Builder
	for _, t := range strings.Split(ports, ",") {
		if t = strings.TrimSpace(t); t != "" {
			b.WriteString(t)
			b.WriteByte('\n')
		}
	}
	return os.WriteFile(path, []byte(b.String()), 0o600)
}

// restoreListFiles restores both .lst files from backups created with the
// given timestamp (used to roll back a failed apply).
func (h *ProxyPortsHandler) restoreListFiles(timestamp string) {
	for _, name := range []string{proxyPortsFile, excludePortsFile} {
		src := filepath.Join(h.backupDir, name+"."+timestamp+".bak")
		data, err := os.ReadFile(src)
		if err != nil {
			continue // no backup was created for this file
		}
		if err := os.WriteFile(h.portListPath(name), data, 0o600); err != nil {
			log.Printf("[proxy-ports] failed to restore %s: %v", name, err)
		}
	}
}

// validateRoutingConfig runs `xray -test -confdir` against the Xray config
// directory so a bad routing rule is caught before any restart. It is a no-op
// (nil) when the xray binary is not available. XRAY_LOCATION_ASSET is passed
// when a geo data directory is found (xkeen stores .dat files in
// <confdir>/../dat and starts xray with that variable set — without it a bare
// `xray -test` fails on geosite/geoip lookups for unrelated, pre-existing
// rules). A failure mentioning geo asset files is treated as an environment
// problem and also skipped: the managed rule never references geo files, so
// such an error cannot be caused by it.
func (h *ProxyPortsHandler) validateRoutingConfig() error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	var out string
	var err error
	if ee, ok := h.executor.(EnvCommandExecutor); ok {
		var env []string
		if dir := xrayAssetDir(h.xrayConfigDir); dir != "" {
			env = append(env, "XRAY_LOCATION_ASSET="+dir)
		}
		out, err = ee.ExecuteWithEnv(ctx, env, "xray", "-test", "-confdir", h.xrayConfigDir)
	} else {
		out, err = h.executor.Execute(ctx, "xray", "-test", "-confdir", h.xrayConfigDir)
	}
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			log.Printf("[proxy-ports] xray binary not found, skipping routing pre-flight check")
			return nil
		}
		if strings.Contains(out, "geosite") || strings.Contains(out, "geoip") {
			log.Printf("[proxy-ports] xray -test failed on geo asset files (missing XRAY_LOCATION_ASSET?), skipping pre-flight check: %s", strings.TrimSpace(out))
			return nil
		}
		return fmt.Errorf("xray -test -confdir: %s", strings.TrimSpace(out))
	}
	return nil
}

// xrayAssetDir returns a candidate XRAY_LOCATION_ASSET directory (a folder
// containing geoip/geosite .dat files) or "" when none is found.
func xrayAssetDir(configDir string) string {
	candidates := []string{
		os.Getenv("XRAY_LOCATION_ASSET"),
		filepath.Join(filepath.Dir(configDir), "dat"),
		filepath.Join(configDir, "dat"),
	}
	seen := map[string]bool{}
	for _, c := range candidates {
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		if fi, err := os.Stat(c); err != nil || !fi.IsDir() {
			continue
		}
		entries, err := os.ReadDir(c)
		if err != nil {
			continue
		}
		for _, e := range entries {
			n := strings.ToLower(e.Name())
			if (strings.HasPrefix(n, "geosite") || strings.HasPrefix(n, "geoip")) && strings.HasSuffix(n, ".dat") {
				return c
			}
		}
	}
	return ""
}

// restoreRoutingFile restores 05_routing.json from a backup created right
// before it was rewritten (used when Xray rejects the updated config).
func (h *ProxyPortsHandler) restoreRoutingFile(backupPath string) {
	data, err := os.ReadFile(backupPath)
	if err != nil {
		log.Printf("[proxy-ports] failed to read routing backup %s: %v", backupPath, err)
		return
	}
	if err := os.WriteFile(filepath.Join(h.xrayConfigDir, routingFileName), data, 0o600); err != nil {
		log.Printf("[proxy-ports] failed to restore routing backup: %v", err)
	}
}

// readActivePorts returns the mode and cleaned port list of the active .lst.
// proxying takes priority over exclude (xkeen's own precedence).
func (h *ProxyPortsHandler) readActivePorts() (mode, ports string) {
	proxying := readPortListFile(h.portListPath(proxyPortsFile))
	excluding := readPortListFile(h.portListPath(excludePortsFile))
	switch {
	case proxying != "":
		return "proxying", proxying
	case excluding != "":
		return "exclude", excluding
	default:
		return "none", ""
	}
}

// readPortListFile returns the cleaned, comma-joined content of a .lst file,
// mirroring xkeen's read_ports_from_file: comments (#), whitespace and empty
// lines are stripped. Returns "" if the file is missing or empty.
func readPortListFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var lines []string
	for _, ln := range strings.Split(string(data), "\n") {
		ln = strings.TrimSpace(ln)
		if i := strings.Index(ln, "#"); i >= 0 {
			ln = strings.TrimSpace(ln[:i])
		}
		if ln != "" {
			lines = append(lines, ln)
		}
	}
	return strings.Join(lines, ",")
}

// managedUDPRulePorts extracts the port list of the managed UDP routing rule
// and normalizes it to the canonical ":"-separated form used by the editor
// (ValidatePortList also converts Xray's "-" range separator to ":").
func (h *ProxyPortsHandler) managedUDPRulePorts() (string, bool) {
	doc, ok := h.readRoutingDoc()
	if !ok {
		return "", false
	}
	for _, r := range routingRules(doc) {
		name, _ := r["name"].(string)
		if name != ManagedUDPRuleName {
			continue
		}
		var list string
		switch p := r["port"].(type) {
		case string:
			list = p
		case []interface{}:
			// Tolerate the legacy array form written before the Xray-syntax fix.
			var out []string
			for _, v := range p {
				out = append(out, fmt.Sprintf("%v", v))
			}
			list = strings.Join(out, ",")
		case float64: // plain JSON number, e.g. "port": 443
			list = strconv.FormatInt(int64(p), 10)
		default:
			return "", true
		}
		normalized, err := ValidatePortList(list)
		if err != nil {
			return "", true
		}
		return normalized, true
	}
	return "", false
}

// readRoutingDoc reads and parses 05_routing.json (JSONC-aware).
func (h *ProxyPortsHandler) readRoutingDoc() (map[string]interface{}, bool) {
	data, err := os.ReadFile(filepath.Join(h.xrayConfigDir, routingFileName))
	if err != nil {
		return nil, false
	}
	jsonData, err := utils.JSONCtoJSON(data)
	if err != nil {
		log.Printf("[proxy-ports] failed to parse %s: %v", routingFileName, err)
		return nil, false
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(jsonData, &doc); err != nil {
		log.Printf("[proxy-ports] failed to unmarshal %s: %v", routingFileName, err)
		return nil, false
	}
	return doc, true
}

// routingRules returns the routing.rules array of a parsed document.
func routingRules(doc map[string]interface{}) []map[string]interface{} {
	routing, _ := doc["routing"].(map[string]interface{})
	rulesRaw, _ := routing["rules"].([]interface{})
	var rules []map[string]interface{}
	for _, r := range rulesRaw {
		if rm, ok := r.(map[string]interface{}); ok {
			rules = append(rules, rm)
		}
	}
	return rules
}

// updateRoutingRule upserts the managed UDP rule in 05_routing.json so that
// proxied UDP ports go through the balancer instead of the "udp -> direct"
// catch-all. An empty udpPorts removes the managed rule.
//
// Returns the backup path when the file was actually rewritten (or "" when
// the content did not change or no backup was created), and an error when the
// routing file is missing/unreadable and udpPorts is non-empty.
func (h *ProxyPortsHandler) updateRoutingRule(udpPorts string) (string, error) {
	doc, ok := h.readRoutingDoc()
	if !ok {
		if udpPorts != "" {
			return "", fmt.Errorf("routing file %s not found; UDP ports not applied", routingFileName)
		}
		return "", nil
	}

	routing, _ := doc["routing"].(map[string]interface{})
	if routing == nil {
		routing = map[string]interface{}{}
		doc["routing"] = routing
	}
	rulesRaw, _ := routing["rules"].([]interface{})
	rules := make([]interface{}, 0, len(rulesRaw))
	rules = append(rules, rulesRaw...)

	// Drop the existing managed rule (if any).
	for i := 0; i < len(rules); i++ {
		if rm, ok := rules[i].(map[string]interface{}); ok {
			if name, _ := rm["name"].(string); name == ManagedUDPRuleName {
				rules = append(rules[:i], rules[i+1:]...)
				break
			}
		}
	}

	if udpPorts != "" {
		rule := buildUDPRule(udpPorts, rules)
		insertIdx := findUDPDirectRuleIndex(rules)
		if insertIdx < 0 {
			insertIdx = len(rules)
		}
		rules = append(rules, nil)
		copy(rules[insertIdx+1:], rules[insertIdx:])
		rules[insertIdx] = rule
	}

	routing["rules"] = rules

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal routing: %w", err)
	}
	out = append(out, '\n')

	routingPath := filepath.Join(h.xrayConfigDir, routingFileName)
	if existing, err := os.ReadFile(routingPath); err == nil && bytes.Equal(existing, out) {
		return "", nil // no change
	}

	backupPath, err := createBackupCore(routingPath, h.backupDir, time.Now().Format("20060102-150405"))
	if err != nil {
		log.Printf("[proxy-ports] routing backup failed: %v", err)
	}
	if err := os.WriteFile(routingPath, out, 0o600); err != nil {
		return backupPath, fmt.Errorf("failed to write routing: %w", err)
	}
	return backupPath, nil
}

// xrayPortList converts a canonical ":"-separated port list (as produced by
// ValidatePortList) into the string form Xray expects in routing-rule "port"
// fields: comma-separated ports and ranges joined with "-" (e.g.
// "80,443,15000-52500"). Xray's PortList parser only accepts a plain number
// or such a string — arrays are rejected with "invalid port".
func xrayPortList(ports string) string {
	var out []string
	for _, t := range strings.Split(ports, ",") {
		t = strings.TrimSpace(t)
		if t != "" {
			out = append(out, strings.ReplaceAll(t, ":", "-"))
		}
	}
	return strings.Join(out, ",")
}

// buildUDPRule builds the managed UDP routing rule, reusing the balancer tag
// of the existing config (first rule carrying a balancerTag).
func buildUDPRule(udpPorts string, rules []interface{}) map[string]interface{} {
	balancerTag := "default-balancer"
	for _, r := range rules {
		if rm, ok := r.(map[string]interface{}); ok {
			if bt, ok := rm["balancerTag"].(string); ok && bt != "" {
				balancerTag = bt
				break
			}
		}
	}
	return map[string]interface{}{
		"type":        "field",
		"name":        ManagedUDPRuleName,
		"network":     "udp",
		"port":        xrayPortList(udpPorts),
		"balancerTag": balancerTag,
	}
}

// findUDPDirectRuleIndex returns the index of the first rule that catches UDP
// and sends it direct (the "udp -> direct" catch-all). The managed UDP rule
// must be inserted before it to take precedence. Returns -1 when absent.
func findUDPDirectRuleIndex(rules []interface{}) int {
	for i, r := range rules {
		rm, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		network, _ := rm["network"].(string)
		outbound, _ := rm["outboundTag"].(string)
		if strings.Contains(network, "udp") && outbound == "direct" {
			return i
		}
	}
	return -1
}

// ValidatePortList validates and normalizes a comma-separated list of ports
// and ranges ("80,443,50000:51000"). Both ":" and "-" are accepted as range
// separators (xkeen converts "-" to ":"). Returns the cleaned, de-duplicated,
// start-sorted list or an error on invalid input.
func ValidatePortList(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if len(raw) > 512 {
		return "", fmt.Errorf("list is too long (max 512 chars)")
	}

	seen := map[string]bool{}
	var out []string
	for _, tok := range strings.Split(raw, ",") {
		tok = strings.TrimSpace(strings.ReplaceAll(tok, "-", ":"))
		if tok == "" {
			continue
		}
		if !validPortToken(tok) {
			return "", fmt.Errorf("invalid port or range %q (use N or A:B, 0-65535)", tok)
		}
		if !seen[tok] {
			seen[tok] = true
			out = append(out, tok)
		}
	}

	sort.Slice(out, func(i, j int) bool {
		return portTokenStart(out[i]) < portTokenStart(out[j])
	})
	return strings.Join(out, ","), nil
}

// validPortToken reports whether tok is a single port (0-65535) or a range
// "A:B" with A <= B within 0-65535.
func validPortToken(tok string) bool {
	if n, err := strconv.Atoi(tok); err == nil {
		return n >= 0 && n <= 65535
	}
	parts := strings.SplitN(tok, ":", 2)
	if len(parts) != 2 {
		return false
	}
	lo, err1 := strconv.Atoi(parts[0])
	hi, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return false
	}
	return lo >= 0 && hi >= 0 && lo <= 65535 && hi <= 65535 && lo <= hi
}

// portTokenStart returns the numeric start of a port token (for sorting).
func portTokenStart(tok string) int {
	if n, err := strconv.Atoi(tok); err == nil {
		return n
	}
	lo, _ := strconv.Atoi(strings.SplitN(tok, ":", 2)[0])
	return lo
}

// portListSubset reports whether every token of sub is present in full.
func portListSubset(sub, full string) bool {
	fullSet := map[string]bool{}
	for _, t := range strings.Split(full, ",") {
		if t != "" {
			fullSet[t] = true
		}
	}
	for _, t := range strings.Split(sub, ",") {
		if t != "" && !fullSet[t] {
			return false
		}
	}
	return true
}

// RegisterProxyPortsRoutes registers the proxy-ports routes.
func RegisterProxyPortsRoutes(r *mux.Router, h *ProxyPortsHandler) {
	r.HandleFunc("/settings/proxy-ports", h.GetProxyPorts).Methods("GET")
	r.HandleFunc("/settings/proxy-ports", h.UpdateProxyPorts).Methods("PUT")
}
