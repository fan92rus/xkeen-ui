// Package handlers provides HTTP handlers for XKEEN-UI API endpoints.
package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
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

// UpdateProxyPorts validates, writes and applies the proxy port lists.
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

	// Backup existing lists before overwriting.
	timestamp := time.Now().Format("20060102-150405")
	for _, name := range []string{proxyPortsFile, excludePortsFile} {
		path := h.portListPath(name)
		if _, err := createBackupCore(path, h.backupDir, timestamp); err != nil {
			log.Printf("[proxy-ports] backup failed for %s: %v", path, err)
		}
	}

	// Write the .lst files. Empty list file == xkeen proxies all ports.
	proxyingContent, excludeContent := "", ""
	switch req.Mode {
	case "proxying":
		proxyingContent = ports
	case "exclude":
		excludeContent = ports
	}
	if err := os.WriteFile(h.portListPath(proxyPortsFile), []byte(proxyingContent), 0o600); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to write "+proxyPortsFile+": "+err.Error())
		return
	}
	if err := os.WriteFile(h.portListPath(excludePortsFile), []byte(excludeContent), 0o600); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to write "+excludePortsFile+": "+err.Error())
		return
	}

	// Keep the managed UDP routing rule in sync (files are already written;
	// a routing failure is logged but does not roll back the lists).
	if err := h.updateRoutingRule(udpPorts); err != nil {
		log.Printf("[proxy-ports] routing update failed: %v", err)
	}

	// Apply: `xkeen -restart` re-applies iptables (port lists) and restarts
	// Xray (routing rule).
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

	respondJSON(w, http.StatusOK, ProxyPortsResponse{
		Ok:       true,
		Mode:     req.Mode,
		Ports:    ports,
		UDPPorts: udpPorts,
	})
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

// managedUDPRulePorts extracts the port list of the managed UDP routing rule.
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
		portsRaw, ok := r["port"].([]interface{})
		if !ok {
			return "", true
		}
		var out []string
		for _, p := range portsRaw {
			out = append(out, fmt.Sprintf("%v", p))
		}
		return strings.Join(out, ","), true
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
func (h *ProxyPortsHandler) updateRoutingRule(udpPorts string) error {
	doc, ok := h.readRoutingDoc()
	if !ok {
		if udpPorts != "" {
			return fmt.Errorf("routing file %s not found; UDP ports not applied", routingFileName)
		}
		return nil
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

	routingPath := filepath.Join(h.xrayConfigDir, routingFileName)
	if _, err := createBackupCore(routingPath, h.backupDir, time.Now().Format("20060102-150405")); err != nil {
		log.Printf("[proxy-ports] routing backup failed: %v", err)
	}

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal routing: %w", err)
	}
	out = append(out, '\n')
	if err := os.WriteFile(routingPath, out, 0o600); err != nil {
		return fmt.Errorf("failed to write routing: %w", err)
	}
	return nil
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
	var portList []interface{}
	for _, t := range strings.Split(udpPorts, ",") {
		t = strings.TrimSpace(t)
		if t != "" {
			portList = append(portList, t)
		}
	}
	return map[string]interface{}{
		"type":        "field",
		"name":        ManagedUDPRuleName,
		"network":     "udp",
		"port":        portList,
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
