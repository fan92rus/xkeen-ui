package handlers

import (
	"context"
	"fmt"
	"log"
	"net"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/fan92rus/xkeen-ui/internal/config"
)

// DNS sync: native Keenetic conditional DNS forwarding (ip name-server <ip> <zone>).

var (
	dnsSyncNDMCFn = func(ctx context.Context, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, "ndmc", args...).CombinedOutput()
	}
	dnsSyncLookPathFn = exec.LookPath
	dnsSyncTimeout    = 8 * time.Second
)

var zoneRe = regexp.MustCompile(`^[\w-]+(\.[\w-]+)*$`)

func ndmc(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), dnsSyncTimeout)
	defer cancel()
	return dnsSyncNDMCFn(ctx, args...)
}

// discoverDNSUpstream returns the first non-loopback default DNS server from
// Keenetic's name-server list (the one with no domain), or "" if none.
func discoverDNSUpstream() string {
	out, err := ndmc("-c", "show ip name-server")
	if err != nil {
		return ""
	}
	var addr string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "address:") {
			addr = strings.TrimSpace(strings.TrimPrefix(line, "address:"))
		} else if strings.HasPrefix(line, "domain:") {
			domain := strings.TrimSpace(strings.TrimPrefix(line, "domain:"))
			if domain == "" && net.ParseIP(addr) != nil && !net.ParseIP(addr).IsLoopback() {
				return addr
			}
			addr = ""
		}
	}
	return ""
}

// SyncDNSForward applies (or removes) native conditional DNS forwarding for
// the configured zones. Only zones previously recorded in ManagedZones are
// removed — entries configured by the user or ISP are never touched.
func SyncDNSForward(cfg *config.Config, configPath string) (string, error) {
	if _, err := dnsSyncLookPathFn("ndmc"); err != nil {
		return "ndmc not available (not running on a Keenetic router)", nil
	}

	// Remove only the zones we previously created (ownership tracking).
	managed := map[string]bool{}
	for _, z := range cfg.AWDNSManagedZones {
		managed[strings.TrimPrefix(strings.TrimSpace(z), ".")] = true
	}

	want := map[string]bool{}
	if cfg.AWDNSForward {
		if cfg.AWDNSForwardAllZones {
			// "All zones" = use the default upstream for everything: ensure
			// it is registered with an empty domain, then no zone rules.
			up := discoverDNSUpstream()
			if up == "" {
				return "", fmt.Errorf("cannot detect default DNS upstream")
			}
			if out, err := ndmc("-c", fmt.Sprintf("ip name-server %s", up)); err != nil {
				return "", fmt.Errorf("ip name-server %s: %v\n%s", up, err, string(out))
			}
		} else {
			for _, z := range cfg.AWDNSZones {
				z = strings.TrimPrefix(strings.TrimSpace(z), ".")
				if z != "" && zoneRe.MatchString(z) {
					want[z] = true
				}
			}
		}
	}

	// Remove stale OWNED zones.
	upstream := discoverDNSUpstream()
	for z := range managed {
		if !want[z] {
			_, _ = ndmc("-c", fmt.Sprintf("no ip name-server %s %s", upstream, z))
		}
	}

	if cfg.AWDNSForward && !cfg.AWDNSForwardAllZones {
		if upstream == "" {
			return "", fmt.Errorf("cannot detect default DNS upstream — set a DNS server in Keenetic first")
		}
		for z := range want {
			// remove-then-add keeps Keenetic from duplicating a rule
			_, _ = ndmc("-c", fmt.Sprintf("no ip name-server %s %s", upstream, z))
			if out, err := ndmc("-c", fmt.Sprintf("ip name-server %s %s", upstream, z)); err != nil {
				return "", fmt.Errorf("ip name-server %s %s: %v\n%s", upstream, z, err, string(out))
			}
		}
	}

	// Persist ownership = current zones (so future runs can clean them up).
	keys := make([]string, 0, len(want))
	for z := range want {
		keys = append(keys, z)
	}
	cfg.AWDNSManagedZones = keys
	if configPath != "" {
		if err := cfg.SaveConfig(configPath); err != nil {
			log.Printf("[awg] warning: could not persist DNS managed zones: %v", err)
		}
	}
	_, _ = ndmc("-c", "system configuration save")

	msg := fmt.Sprintf("DNS zones forwarded to %s: %s", upstream, strings.Join(keys, ", "))
	log.Printf("[awg] %s", msg)
	return msg, nil
}
