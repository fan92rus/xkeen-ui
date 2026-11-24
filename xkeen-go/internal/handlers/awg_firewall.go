package handlers

import (
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fan92rus/xkeen-ui/internal/subscription"
)

// Full-tunnel firewall preset for AWG server configs.
//
// When a server interface is brought up, these rules allow:
//   - Incoming UDP on the listen port
//   - Forwarding between the AWG interface, LAN bridge, and WAN
//   - NAT masquerading so clients get internet access via the router
//   - A route for the tunnel subnet
//
// Parameters are auto-detected or configured in Settings.

// AWGFirewallParams holds the variables for the full-tunnel preset.
type AWGFirewallParams struct {
	Iface    string // AWG interface name (e.g. "server")
	Port     int    // ListenPort from config
	Subnet   string // tunnel subnet, e.g. "10.8.0.0/24"
	LANIface string // LAN bridge interface (e.g. "br0")
	WANIface string // WAN interface (e.g. "eth3")
}

// detectWANInterface finds the default-route interface.
func detectWANInterface() string {
	out, err := exec.Command("ip", "route", "show", "default").Output()
	if err != nil {
		return "eth3" // Keenetic default fallback
	}
	// Output: "default via 1.2.3.4 dev eth3 ..."
	fields := strings.Fields(string(out))
	for i, f := range fields {
		if f == "dev" && i+1 < len(fields) {
			return fields[i+1]
		}
	}
	return "eth3"
}

// detectLANInterface finds the bridge interface (Keenetic typically uses br0).
func detectLANInterface() string {
	// Try common Keenetic bridge names
	for _, name := range []string{"br0", "bridge", "br-lan"} {
		if err := exec.Command("ip", "link", "show", name).Run(); err == nil {
			return name
		}
	}
	return "br0"
}

// runIPTablesFn runs an iptables command, logging errors but not failing.
// It is a variable so tests can capture calls without executing iptables.
var runIPTablesFn = runIPTables

func runIPTables(args ...string) error {
	cmd := exec.Command("iptables", args...)
	out, err := cmd.CombinedOutput()
	if err != nil && !strings.Contains(string(out), "does not exist") {
		// "Rule ... does not exist" is the expected miss from ensureRule's
		// -C probe — not worth a log line on every firewall rebuild.
		log.Printf("[awg] iptables %s: %v\n%s", strings.Join(args, " "), err, string(out))
	}
	return err
}

// firewallsMu serializes Apply/Remove/Restore: the NDM netfilter hook and
// the UI can trigger restores concurrently, and two
// concurrent -C misses would both insert the same rule (duplicates).
var firewallsMu sync.Mutex

// ensureRule adds an iptables rule only if it is not already present.
// args must start with -I or -A; the check uses -C with the same arguments.
// NDM rebuilds the firewall frequently and RestoreAWGServerFirewalls re-runs
// Apply on every netfilter.d event, so Apply must stay idempotent.
func ensureRule(args ...string) {
	if len(args) < 2 {
		return
	}
	// Build the -C check. For table rules ("-t nat -A POSTROUTING ...") it
	// must become "-t nat -C POSTROUTING ..."; otherwise "-I X ..." -> "-C X ...".
	var check []string
	if args[0] == "-t" && len(args) >= 4 {
		check = append(check, "-t", args[1], "-C")
		check = append(check, args[3:]...)
	} else {
		check = append(check, "-C")
		check = append(check, args[1:]...)
	}
	if runIPTablesFn(check...) == nil {
		return // already present
	}
	_ = runIPTablesFn(args...)
}

// fullTunnelRules builds the complete rule set for the full-tunnel preset
// from normalized params. Apply and Remove both derive from THIS table, so
// the two can never drift apart (a missed mirror edit was a real bug class).
func fullTunnelRules(p AWGFirewallParams) [][]string {
	port := fmt.Sprintf("%d", p.Port)
	rules := [][]string{
		// INPUT: allow incoming UDP on the listen port
		{"-I", "INPUT", "-p", "udp", "--dport", port, "-j", "ACCEPT"},
		// INPUT: tunnel clients can reach the router itself — ICMP (ping the
		// tunnel IP) and DNS (use the router as DNS, resolve local names).
		// Keenetic's INPUT policy is DROP: without these, VPN clients look
		// like they have "no LAN access".
		{"-I", "INPUT", "-i", p.Iface, "-p", "icmp", "-j", "ACCEPT"},
		{"-I", "INPUT", "-i", p.Iface, "-p", "udp", "--dport", "53", "-j", "ACCEPT"},
		{"-I", "INPUT", "-i", p.Iface, "-p", "tcp", "--dport", "53", "-j", "ACCEPT"},
		// INPUT: tunnel clients can reach the router's web UI and SSH
		// (Keenetic admin panel 80/443 and dropbear sshd 22).
		{"-I", "INPUT", "-i", p.Iface, "-p", "tcp", "--dport", "22", "-j", "ACCEPT"},
		{"-I", "INPUT", "-i", p.Iface, "-p", "tcp", "--dport", "80", "-j", "ACCEPT"},
		{"-I", "INPUT", "-i", p.Iface, "-p", "tcp", "--dport", "443", "-j", "ACCEPT"},
		// FORWARD: AWG interface ↔ LAN
		{"-I", "FORWARD", "-i", p.Iface, "-o", p.LANIface, "-j", "ACCEPT"},
		{"-I", "FORWARD", "-i", p.LANIface, "-o", p.Iface, "-j", "ACCEPT"},
		// FORWARD: AWG interface ↔ WAN (internet)
		{"-I", "FORWARD", "-i", p.Iface, "-o", p.WANIface, "-j", "ACCEPT"},
		{"-I", "FORWARD", "-i", p.WANIface, "-o", p.Iface, "-j", "ACCEPT"},
		// NAT: masquerade tunnel traffic going out
		{"-t", "nat", "-A", "POSTROUTING", "-s", p.Subnet, "-j", "MASQUERADE"},
	}
	return rules
}

// normalizeFirewallParams fills defaults (LAN/WAN detection, listen port).
func normalizeFirewallParams(p AWGFirewallParams) AWGFirewallParams {
	if p.LANIface == "" {
		p.LANIface = detectLANInterface()
	}
	if p.WANIface == "" {
		p.WANIface = detectWANInterface()
	}
	if p.Port == 0 {
		p.Port = defaultListenPort
	}
	return p
}

// deriveDelete maps an add rule (leading -I/-A, optionally "-t <table>") to
// its exact -D counterpart used by Remove.
func deriveDelete(rule []string) []string {
	out := make([]string, len(rule))
	copy(out, rule)
	for i, tok := range out {
		if tok == "-I" || tok == "-A" {
			out[i] = "-D"
			break
		}
	}
	return out
}

// ApplyFullTunnelFirewall applies the full-tunnel preset rules for a server interface.
func ApplyFullTunnelFirewall(p AWGFirewallParams) {
	firewallsMu.Lock()
	defer firewallsMu.Unlock()
	if p.Iface == "" || p.Subnet == "" {
		log.Printf("[awg] skip firewall: missing iface or subnet")
		return
	}
	p = normalizeFirewallParams(p)

	log.Printf("[awg] applying full-tunnel firewall for %s (port=%d subnet=%s lan=%s wan=%s)",
		p.Iface, p.Port, p.Subnet, p.LANIface, p.WANIface)

	for _, rule := range fullTunnelRules(p) {
		ensureRule(rule...)
	}

	// Route: tunnel subnet via this interface (best-effort, may exist).
	//nolint:gosec // AWG config validated in handler
	if err := exec.Command("ip", "route", "add", p.Subnet, "dev", p.Iface).Run(); err != nil {
		log.Printf("[awg] ip route add %s dev %s: %v (may already exist)", p.Subnet, p.Iface, err)
	}
}

// RemoveFullTunnelFirewall removes the full-tunnel preset rules (reverse
// order, best-effort — rules may not exist).
func RemoveFullTunnelFirewall(p AWGFirewallParams) {
	firewallsMu.Lock()
	defer firewallsMu.Unlock()
	if p.Iface == "" {
		return
	}
	p = normalizeFirewallParams(p)
	log.Printf("[awg] removing full-tunnel firewall for %s", p.Iface)

	//nolint:gosec // subnet/iface from validated config
	_ = exec.Command("ip", "route", "del", p.Subnet, "dev", p.Iface).Run()

	rules := fullTunnelRules(p)
	for i := len(rules) - 1; i >= 0; i-- {
		_ = runIPTablesFn(deriveDelete(rules[i])...)
	}
}

// RestoreAWGServerFirewalls re-applies firewall rules for all active server configs.
// Called by the NDM netfilter hook (via the loopback restore endpoint) and the UI.
func (h *AWGHandler) RestoreAWGServerFirewalls() {
	configs, err := h.store.ScanAWGConfigs(h.awgDir)
	if err != nil {
		log.Printf("[awg] restore firewalls: scan failed: %v", err)
		return
	}

	active := h.getActiveInterfaces()
	for _, c := range configs {
		if c.Role != "server" {
			continue
		}
		if _, isActive := active[c.Name]; !isActive {
			continue
		}
		// Re-apply firewall for this active server
		params := h.getServerFirewallParams(c.Name)
		ApplyFullTunnelFirewall(params)
		log.Printf("[awg] restored firewall for %s", c.Name)
	}
}

// restoreInFlight collapses concurrent restore calls (NDM hook, UI):
// the rule pass is idempotent and cheap to skip when one is already running.
var restoreInFlight atomic.Bool

// RestoreFirewall is an HTTP handler that re-applies firewall rules for all active servers.
// POST /api/awg/restore-firewall — used by the NDM netfilter hook and cron.
func (h *AWGHandler) RestoreFirewall(w http.ResponseWriter, _ *http.Request) {
	if !restoreInFlight.CompareAndSwap(false, true) {
		respondJSON(w, http.StatusAccepted, map[string]interface{}{
			"success": true,
			"message": "restore already in progress",
		})
		return
	}
	defer restoreInFlight.Store(false)
	h.RestoreAWGServerFirewalls()
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "firewall rules restored for active server configs",
	})
}

// isServerConf reports whether the config behind confPath is a server:
// tracked role wins; otherwise parse the file (a listen port implies server).
func (h *AWGHandler) isServerConf(confPath string) bool {
	name := strings.TrimSuffix(filepath.Base(confPath), ".conf")
	if cfg, ok := h.store.GetAWGConfig(name); ok {
		return cfg.Role == subscription.AWGRoleServer
	}
	if conf, err := subscription.ParseAWGConf(confPath); err == nil && conf.GetListenPort() > 0 {
		return true
	}
	return false
}

// getServerFirewallParams builds the firewall params for a server config.
func (h *AWGHandler) getServerFirewallParams(name string) AWGFirewallParams {
	confPath := filepath.Join(h.awgDir, name+".conf")
	conf, err := subscription.ParseAWGConf(confPath)
	if err != nil {
		log.Printf("[awg] getServerFirewallParams: parse %s failed: %v", confPath, err)
	}

	var port int
	var subnet string
	if conf != nil {
		port = conf.GetListenPort()
		subnet = conf.GetTunnelSubnet()
	}
	if subnet == "" {
		subnet = defaultTunnelSubnet // safe default
	}

	return AWGFirewallParams{
		Iface:    name,
		Port:     port,
		Subnet:   subnet,
		LANIface: h.cfg.AWGLanIface,
		WANIface: h.cfg.AWGWanIface,
	}
}

// applyClientFwmarkRouting sets up ip rule + ip route for fwmark-based routing.
// This allows Xray to send traffic with a specific mark through the AWG interface.
func (h *AWGHandler) applyClientFwmarkRouting(iface string, mark int) {
	if mark == 0 {
		// mark 0 means "untracked/default" (e.g. an attach-mode config with
		// no mark): installing fwmark-0 rules or touching the main routing
		// table would be wrong — nothing to apply.
		return
	}
	priority := fwmarkPriorityBase + mark

	// Add routing rule if not already present
	checkOut, _ := ipRunFn("rule", "show")
	if !strings.Contains(string(checkOut), fmt.Sprintf("fwmark %d", mark)) {
		if out, err := ipRunFn("rule", "add", "fwmark", fmt.Sprintf("%d", mark),
			"table", fmt.Sprintf("%d", mark), "priority", fmt.Sprintf("%d", priority)); err != nil {
			log.Printf("[awg] warning: ip rule add failed: %v\n%s", err, string(out))
		}
	}

	// Add routing table entry if not already present
	routeOut, _ := ipRunFn("route", "show", "table", fmt.Sprintf("%d", mark))
	if !strings.Contains(string(routeOut), "default") {
		if out, err := ipRunFn("route", "add", "default", "dev", iface,
			"table", fmt.Sprintf("%d", mark)); err != nil {
			log.Printf("[awg] warning: ip route add failed: %v\n%s", err, string(out))
		}
	}
}

// removeClientFwmarkRouting tears down fwmark routing for a client interface.
func (h *AWGHandler) removeClientFwmarkRouting(iface string, mark int) {
	if mark == 0 {
		// Never delete from the main routing table (table 0) or touch
		// fwmark-0 rules: mark 0 is the untracked-default, not a real mark.
		return
	}
	_, _ = ipRunFn("route", "del", "default", "dev", iface, "table", fmt.Sprintf("%d", mark))
	_, _ = ipRunFn("rule", "del", "fwmark", fmt.Sprintf("%d", mark))
}

// Shared constants for the AWG subsystem.
const (
	// maxAWGUploadBytes caps uploaded .conf files (an [Interface]+[Peer]
	// config is a few KB; 64KB is generous).
	maxAWGUploadBytes = 64 << 10
	// ifacePollAttempts × ifacePollInterval = the "within 3s" liveness
	// window used by Up/Down verification loops.
	ifacePollAttempts   = 6
	ifacePollInterval   = 500 * time.Millisecond
	defaultListenPort   = 443
	defaultTunnelSubnet = "10.8.0.0/24"
	// fwmarkPriorityBase: ip rule priorities for AWG marks start above
	// system rules (NDMS uses low priorities).
	fwmarkPriorityBase = 1000
)

// Injection seams for tests (same style as opkg*Fn in awg_opkgtun.go).
var (
	// awgShowFn runs `awg show` and returns its output.
	awgShowFn = func() ([]byte, error) { return exec.Command("awg", "show").Output() }
	// awgQuickFn runs `awg-quick <action> <confPath>`.
	awgQuickFn = func(action, confPath string) ([]byte, error) {
		return exec.Command("awg-quick", action, confPath).CombinedOutput()
	}
	// ipRunFn runs an `ip <args...>` command.
	ipRunFn = func(args ...string) ([]byte, error) {
		return exec.Command("ip", args...).CombinedOutput()
	}
)

// detectAWGEndpoint returns the WAN IP used for client endpoints, or "" if it
// cannot be determined. Mirrors AWGHandler.detectEndpoint (package-level so
// the Settings handler can call it without an AWGHandler).
func detectAWGEndpoint() string {
	out, err := exec.Command("ip", "route", "get", "8.8.8.8").Output()
	if err != nil {
		return ""
	}
	re := regexp.MustCompile(`src\s+(\d+\.\d+\.\d+\.\d+)`)
	matches := re.FindStringSubmatch(string(out))
	if len(matches) >= 2 {
		return matches[1]
	}
	return ""
}
