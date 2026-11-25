package handlers

import (
	"context"
	"fmt"
	"log"
	"net"
	"os/exec"
	"regexp"
	"strings"
	"sync"
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

// cfgWriteMu serializes config.json writes across HTTP handlers and CLI
// (each has its own Config instance; handler-level cfgMu is not enough).
var cfgWriteMu sync.Mutex

// LockConfigWrite serializes config.json writes with the handlers' config
// saves. Other packages (e.g. server auth) must call it before SaveConfig.
func LockConfigWrite() { cfgWriteMu.Lock() }

// UnlockConfigWrite releases the config.json write lock acquired by LockConfigWrite.
func UnlockConfigWrite() { cfgWriteMu.Unlock() }

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
	// The whole reconcile (reads, mutations, config save) must be atomic
	// against other writers of the same *Config in this process.
	cfgWriteMu.Lock()
	defer cfgWriteMu.Unlock()
	if _, err := dnsSyncLookPathFn("ndmc"); err != nil {
		return "ndmc not available (not running on a Keenetic router)", nil
	}

	// Remove only the zones we previously created (ownership tracking).
	managed := map[string]bool{}
	for _, z := range cfg.AWDNSManagedZones {
		managed[strings.TrimPrefix(strings.TrimSpace(z), ".")] = true
	}

	want := map[string]bool{}
	// "All zones" = the existing default (empty-domain) name-server already
	// handles every query; no zone-scoped rules to add in that mode.
	if cfg.AWDNSForward && !cfg.AWDNSForwardAllZones {
		for _, z := range cfg.AWDNSZones {
			z = strings.TrimPrefix(strings.TrimSpace(z), ".")
			if z != "" && zoneRe.MatchString(z) {
				want[z] = true
			}
		}
	}

	// Remove stale OWNED zones.
	upstream := discoverDNSUpstream()
	for z := range managed {
		if !want[z] {
			ip := upstream
			if recorded := cfg.AWDNSManagedZoneIPs[z]; recorded != "" {
				ip = recorded
			}
			if ip != "" {
				_, _ = ndmc("-c", fmt.Sprintf("no ip name-server %s %s", ip, z))
			}
		}
	}

	if cfg.AWDNSForward && !cfg.AWDNSForwardAllZones && upstream == "" {
		// Do not touch ownership or persisted config: without an upstream we
		// cannot manage these rules safely.
		return "", fmt.Errorf("cannot detect default DNS upstream — set a DNS server in Keenetic first")
	}
	if cfg.AWDNSForward && !cfg.AWDNSForwardAllZones {
		for z := range want {
			// remove-then-add keeps Keenetic from duplicating a rule. Use the
			// recorded IP for removal in case the default upstream changed.
			// Always remove before adding — Keenetic may already have a rule
			// from a previous sync (recorded IP or current upstream).
			if oldIP := cfg.AWDNSManagedZoneIPs[z]; oldIP != "" {
				_, _ = ndmc("-c", fmt.Sprintf("no ip name-server %s %s", oldIP, z))
			}
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
	if cfg.AWDNSManagedZoneIPs == nil {
		cfg.AWDNSManagedZoneIPs = map[string]string{}
	}
	for z := range cfg.AWDNSManagedZoneIPs {
		if !want[z] {
			delete(cfg.AWDNSManagedZoneIPs, z)
		}
	}
	for _, z := range keys {
		cfg.AWDNSManagedZoneIPs[z] = upstream
	}
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
