package handlers

import (
	"fmt"
	"log"
	"net"
	"os/exec"
	"regexp"
	"strings"

	"github.com/fan92rus/xkeen-ui/internal/config"
)

// DNS sync: native Keenetic conditional forwarding (ip name-server <ip> <zone>).

var (
	dnsSyncNDMCFn = func(args ...string) ([]byte, error) {
		return exec.Command("ndmc", args...).CombinedOutput()
	}
	dnsSyncLookPathFn = exec.LookPath
)

var zoneRe = regexp.MustCompile(`^[\w-]+(\.[\w-]+)*$`)

// discoverDNSUpstream returns the first non-loopback default DNS server from
// Keenetic's name-server list (the one with no domain), or "" if none.
func discoverDNSUpstream() string {
	out, err := dnsSyncNDMCFn("-c", "show ip name-server")
	if err != nil {
		return ""
	}
	var current string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "address:") {
			current = strings.TrimSpace(strings.TrimPrefix(line, "address:"))
		} else if strings.HasPrefix(line, "domain:") {
			domain := strings.TrimSpace(strings.TrimPrefix(line, "domain:"))
			if domain == "" && net.ParseIP(current) != nil && !net.ParseIP(current).IsLoopback() {
				return current
			}
		}
	}
	return ""
}

// SyncDNSForward applies (or removes) native conditional DNS forwarding for
// the configured zones. Returns a human-readable status.
func SyncDNSForward(cfg *config.Config) (string, error) {
	if _, err := dnsSyncLookPathFn("ndmc"); err != nil {
		return "ndmc not available (not running on a Keenetic router)", nil
	}

	// Read currently configured zones so we can remove stale ones.
	existing := map[string]string{} // zone -> upstream IP ("" zone = default)
	out, _ := dnsSyncNDMCFn("-c", "show ip name-server")
	var addr, domain string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "address:") {
			addr = strings.TrimSpace(strings.TrimPrefix(line, "address:"))
		} else if strings.HasPrefix(line, "domain:") {
			domain = strings.TrimSpace(strings.TrimPrefix(line, "domain:"))
			if addr != "" && domain != "" {
				existing[domain] = addr
			}
			addr, domain = "", ""
		}
	}

	// Zones we want; "" means all zones (default server override).
	want := map[string]bool{}
	if cfg.AWDNSForward {
		if !cfg.AWDNSForwardAllZones {
			for _, z := range cfg.AWDNSZones {
				z = strings.TrimPrefix(strings.TrimSpace(z), ".")
				if z != "" && zoneRe.MatchString(z) {
					want[z] = true
				}
			}
		}
	}

	// Remove stale zone-scoped servers (never remove the default server).
	for z := range existing {
		if !want[z] {
			_, _ = dnsSyncNDMCFn("-c", fmt.Sprintf("no ip name-server %s %s", existing[z], z))
		}
	}

	if !cfg.AWDNSForward {
		_, _ = dnsSyncNDMCFn("-c", "system configuration save")
		return "DNS forwarding disabled; stale zone rules removed", nil
	}

	upstream := discoverDNSUpstream()
	if upstream == "" {
		return "", fmt.Errorf("cannot detect default DNS upstream — set a DNS server in Keenetic first")
	}

	for z := range want {
		// Keenetic is idempotent here but may duplicate; remove then add.
		_, _ = dnsSyncNDMCFn("-c", fmt.Sprintf("no ip name-server %s %s", upstream, z))
		if out, err := dnsSyncNDMCFn("-c", fmt.Sprintf("ip name-server %s %s", upstream, z)); err != nil {
			return "", fmt.Errorf("ip name-server %s %s: %v\n%s", upstream, z, err, string(out))
		}
	}
	_, _ = dnsSyncNDMCFn("-c", "system configuration save")
	msg := fmt.Sprintf("DNS zones forwarded to %s: %s", upstream, strings.Join(mapKeys(want), ", "))
	log.Printf("[awg] %s", msg)
	return msg, nil
}

func mapKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
