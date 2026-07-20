package handlers

import (
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"

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

// joinPath joins path elements (wrapper around filepath.Join for convenience).
func joinPath(elem ...string) string {
	return filepath.Join(elem...)
}

// parseAWGConfLocal wraps the subscription parser for handler use.
func parseAWGConfLocal(path string) (*subscription.AWGConf, error) {
	return subscription.ParseAWGConf(path)
}

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
	for _, tok := range strings.Fields(string(out)) {
		// no-op placeholder
		_ = tok
	}
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

// runIPTables runs an iptables command, logging errors but not failing.
func runIPTables(args ...string) error {
	cmd := exec.Command("iptables", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("[awg] iptables %s: %v\n%s", strings.Join(args, " "), err, string(out))
	}
	return err
}

// ApplyFullTunnelFirewall applies the full-tunnel preset rules for a server interface.
func ApplyFullTunnelFirewall(p AWGFirewallParams) {
	if p.Iface == "" || p.Subnet == "" {
		log.Printf("[awg] skip firewall: missing iface or subnet")
		return
	}
	if p.LANIface == "" {
		p.LANIface = detectLANInterface()
	}
	if p.WANIface == "" {
		p.WANIface = detectWANInterface()
	}

	port := p.Port
	if port == 0 {
		port = 443
	}

	log.Printf("[awg] applying full-tunnel firewall for %s (port=%d subnet=%s lan=%s wan=%s)",
		p.Iface, port, p.Subnet, p.LANIface, p.WANIface)

	// INPUT: allow incoming UDP on listen port
	_ = runIPTables("-I", "INPUT", "-p", "udp", "--dport", fmt.Sprintf("%d", port), "-j", "ACCEPT")

	// FORWARD: allow traffic between AWG interface and LAN
	_ = runIPTables("-I", "FORWARD", "-i", p.Iface, "-o", p.LANIface, "-j", "ACCEPT")
	_ = runIPTables("-I", "FORWARD", "-i", p.LANIface, "-o", p.Iface, "-j", "ACCEPT")

	// FORWARD: allow traffic between AWG interface and WAN (internet)
	_ = runIPTables("-I", "FORWARD", "-i", p.Iface, "-o", p.WANIface, "-j", "ACCEPT")
	_ = runIPTables("-I", "FORWARD", "-i", p.WANIface, "-o", p.Iface, "-j", "ACCEPT")

	// NAT: masquerade tunnel traffic going out
	_ = runIPTables("-t", "nat", "-A", "POSTROUTING", "-s", p.Subnet, "-j", "MASQUERADE")

	// Route: tunnel subnet via this interface
	//nolint:gosec // AWG config validated in handler
	if err := exec.Command("ip", "route", "add", p.Subnet, "dev", p.Iface).Run(); err != nil {
		log.Printf("[awg] ip route add %s dev %s: %v (may already exist)", p.Subnet, p.Iface, err)
	}
}

// RemoveFullTunnelFirewall removes the full-tunnel preset rules.
func RemoveFullTunnelFirewall(p AWGFirewallParams) {
	if p.Iface == "" {
		return
	}
	if p.LANIface == "" {
		p.LANIface = detectLANInterface()
	}
	if p.WANIface == "" {
		p.WANIface = detectWANInterface()
	}

	port := p.Port
	if port == 0 {
		port = 443
	}

	log.Printf("[awg] removing full-tunnel firewall for %s", p.Iface)

	// Remove in reverse order. Use -D (delete) and -t nat -D.
	// Best-effort: ignore errors (rules may not exist).

	// Remove route
	_ = exec.Command("ip", "route", "del", p.Subnet, "dev", p.Iface).Run() //nolint:gosec // subnet/iface from validated config

	// NAT
	_ = runIPTables("-t", "nat", "-D", "POSTROUTING", "-s", p.Subnet, "-j", "MASQUERADE")

	// FORWARD WAN
	_ = runIPTables("-D", "FORWARD", "-i", p.WANIface, "-o", p.Iface, "-j", "ACCEPT")
	_ = runIPTables("-D", "FORWARD", "-i", p.Iface, "-o", p.WANIface, "-j", "ACCEPT")

	// FORWARD LAN
	_ = runIPTables("-D", "FORWARD", "-i", p.LANIface, "-o", p.Iface, "-j", "ACCEPT")
	_ = runIPTables("-D", "FORWARD", "-i", p.Iface, "-o", p.LANIface, "-j", "ACCEPT")

	// INPUT
	_ = runIPTables("-D", "INPUT", "-p", "udp", "--dport", fmt.Sprintf("%d", port), "-j", "ACCEPT")
}

// RestoreAWGServerFirewalls re-applies firewall rules for all active server configs.
// Called by the watchdog (init script 'check' / cron).
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

// RestoreFirewall is an HTTP handler that re-applies firewall rules for all active servers.
// POST /api/awg/restore-firewall — used by watchdog/cron.
func (h *AWGHandler) RestoreFirewall(w http.ResponseWriter, _ *http.Request) {
	h.RestoreAWGServerFirewalls()
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "firewall rules restored for active server configs",
	})
}

// getServerFirewallParams builds the firewall params for a server config.
func (h *AWGHandler) getServerFirewallParams(name string) AWGFirewallParams {
	confPath := joinPath(h.awgDir, name+".conf")
	conf, err := parseAWGConfLocal(confPath)
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
		subnet = "10.8.0.0/24" // safe default
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
	priority := 1000 + mark

	// Add routing rule if not already present
	checkOut, _ := exec.Command("ip", "rule", "show").Output()
	if !strings.Contains(string(checkOut), fmt.Sprintf("fwmark %d", mark)) {
		//nolint:gosec // mark/iface from validated AWG config
		ruleCmd := exec.Command("ip", "rule", "add", "fwmark", fmt.Sprintf("%d", mark),
			"table", fmt.Sprintf("%d", mark), "priority", fmt.Sprintf("%d", priority))
		if out, err := ruleCmd.CombinedOutput(); err != nil {
			log.Printf("[awg] warning: ip rule add failed: %v\n%s", err, string(out))
		}
	}

	// Add routing table entry if not already present
	//nolint:gosec // iface/mark from validated AWG config
	routeOut, _ := exec.Command("ip", "route", "show", "table", fmt.Sprintf("%d", mark)).Output()
	if !strings.Contains(string(routeOut), "default") {
		//nolint:gosec // iface/mark from validated AWG config
		addRoute := exec.Command("ip", "route", "add", "default", "dev", iface,
			"table", fmt.Sprintf("%d", mark))
		if out, err := addRoute.CombinedOutput(); err != nil {
			log.Printf("[awg] warning: ip route add failed: %v\n%s", err, string(out))
		}
	}
}

// removeClientFwmarkRouting tears down fwmark routing for a client interface.
func (h *AWGHandler) removeClientFwmarkRouting(iface string, mark int) {
	//nolint:gosec // iface/mark from validated AWG config
	_ = exec.Command("ip", "route", "del", "default", "dev", iface,
		"table", fmt.Sprintf("%d", mark)).Run()
	//nolint:gosec // mark from validated AWG config
	_ = exec.Command("ip", "rule", "del", "fwmark", fmt.Sprintf("%d", mark)).Run()
}
