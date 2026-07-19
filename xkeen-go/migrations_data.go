package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/fan92rus/xkeen-ui/internal/handlers"
	"github.com/fan92rus/xkeen-ui/internal/subscription"
)

// migrateAWGInitS90 renames the legacy AWG init script (/opt/etc/init.d/awg,
// no S-prefix) to /opt/etc/init.d/S90awg so Entware's rc.unslung starts it
// at boot. Without the S-prefix, WARP and other AWG client interfaces never
// auto-started after a reboot.
//
// Safe to run multiple times: if S90awg already exists, just removes old awg.
func migrateAWGInitS90() error {
	const (
		oldPath = "/opt/etc/init.d/awg"    // legacy: no S-prefix, never ran at boot
		newPath = "/opt/etc/init.d/S90awg" // S-prefix: rc.unslung picks it up
		confDir = "/opt/etc/awg"           // AWG config directory
	)

	// Case 1: S90awg already exists — just clean up legacy if present.
	if _, err := os.Stat(newPath); err == nil {
		if _, err := os.Stat(oldPath); err == nil {
			if rerr := os.Remove(oldPath); rerr != nil {
				return fmt.Errorf("remove legacy %s: %w", oldPath, rerr)
			}
			fmt.Printf("  Removed legacy %s (S90awg already in place)\n", oldPath)
		}
		return nil
	}

	// Case 2: legacy awg exists → rename to S90awg.
	if _, err := os.Stat(oldPath); err == nil {
		if err := os.Rename(oldPath, newPath); err != nil {
			return fmt.Errorf("rename %s → %s: %w", oldPath, newPath, err)
		}
		fmt.Printf("  Renamed %s → %s (now auto-starts at boot)\n", oldPath, newPath)
		return nil
	}

	// Case 3: neither exists, but AWG configs are present → create S90awg
	// from the embedded template so interfaces auto-start on next boot.
	matches, _ := filepath.Glob(filepath.Join(confDir, "*.conf"))
	if len(matches) > 0 {
		if err := os.WriteFile(newPath, []byte(legacyAWGInitFallback), 0o755); err != nil { //nolint:gosec // init script needs execute permission
			return fmt.Errorf("write %s: %w", newPath, err)
		}
		fmt.Printf("  Created %s (%d AWG config(s) detected)\n", newPath, len(matches))
		return nil
	}

	// Case 4: no AWG configs at all — nothing to do.
	fmt.Printf("  No AWG setup detected, skipping\n")
	return nil
}

// legacyAWGInitFallback is a minimal init script used by migration 001 when
// AWG configs exist but no init script was ever created. It starts/stops all
// .conf files in /opt/etc/awg/. The full-featured template (with firewall
// rules) is installed via the UI's "Setup init script" button.
const legacyAWGInitFallback = `#!/bin/sh
# AWG init script — auto-created by xkeen-ui migration 001
# Starts/stops all AmneziaWG interfaces at boot.
# For full firewall rules, use the UI's "Setup init script" button.

AWG_QUICK=/opt/bin/awg-quick
CONFIG_DIR=/opt/etc/awg

get_confs() {
  ls "$CONFIG_DIR"/*.conf 2>/dev/null | while read -r f; do
    basename "$f" .conf
  done
}

start() {
  for iface in $(get_confs); do
    "$AWG_QUICK" up "$iface" 2>&1
  done
}

stop() {
  for iface in $(get_confs); do
    "$AWG_QUICK" down "$iface" 2>&1
  done
}

case "$1" in
  start) start ;;
  stop) stop ;;
  restart) stop; sleep 1; start ;;
  *) echo "Usage: $0 {start|stop|restart}"; exit 1 ;;
esac
`

// migrateAWGInitUniversal replaces the zoo of AWG init scripts with ONE
// universal S90awg written from the full embedded template.
//
// Before this migration users could have:
//   - S89awg-server (custom): starts ONLY server.conf, ignores WARP clients
//   - S90awg (minimal, from migration 001 or old xkeen-ui): no firewall rules
//
// After this migration there is exactly ONE script: S90awg, written from
// handlers.InstallAWGInitScript which includes:
//   - Correct shebang (#!/bin/sh, not the broken #!/bin/sh /opt/etc/init.d/COMMAND)
//   - Role detection (server vs client via ListenPort/Endpoint)
//   - Firewall rules for server configs (auto-detected port/subnet/interfaces)
//   - 'check' command for cron watchdogs
//
// The old S89awg-server is deleted (not backed up) because the universal
// template is a strict superset of its functionality.
//
// Safe to run multiple times: idempotent.
func migrateAWGInitUniversal() error {
	const (
		s89Path = "/opt/etc/init.d/S89awg-server" // custom: only server.conf
		newPath = "/opt/etc/init.d/S90awg"        // universal: all .conf
		confDir = "/opt/etc/awg"
	)

	// Step 1: Delete custom S89awg-server. The universal S90awg template is
	// a strict superset: it starts server.conf AND all WARP clients, with
	// auto-detected firewall rules instead of hardcoded values.
	if _, err := os.Stat(s89Path); err == nil {
		if err := os.Remove(s89Path); err != nil {
			return fmt.Errorf("remove %s: %w", s89Path, err)
		}
		fmt.Printf("  Removed custom %s (replaced by universal S90awg)\n", s89Path)
	}

	// Step 2: Only write S90awg if there are AWG configs to manage.
	matches, _ := filepath.Glob(filepath.Join(confDir, "*.conf"))
	if len(matches) == 0 {
		fmt.Printf("  No AWG setup detected, skipping\n")
		return nil
	}

	// Step 3: Always overwrite S90awg with the full universal template.
	// This upgrades users who had a minimal script (from migration 001 case 3
	// or old xkeen-ui versions) to the full-featured one with firewall rules.
	if err := os.MkdirAll(filepath.Dir(newPath), 0o750); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(newPath), err)
	}
	if err := os.WriteFile(newPath, []byte(handlers.InstallAWGInitScript), 0o755); err != nil { //nolint:gosec // init script needs execute
		return fmt.Errorf("write %s: %w", newPath, err)
	}
	fmt.Printf("  Wrote universal %s (%d config(s): server + clients)\n", newPath, len(matches))
	return nil
}

// migrateXkeenUISocksInbound ensures the managed SOCKS5 inbound file exists
// in the Xray config directory. This runs on every server startup (as a
// startup migration) so the file is self-healing: if it gets deleted by
// the user, xkeen, or any other process, it is recreated on next startup.
//
// Why this exists: on transparent-proxy Xray configs (which only hook
// iptables PREROUTING for LAN clients, not OUTPUT for router-originated
// traffic), all HTTP requests made by xkeen-ui bypass the VPN entirely.
// A local SOCKS5 inbound lets the fetcher route through the VPN tunnel.
//
// The inbound listens on 127.0.0.1 only (loopback, not exposed) and is
// detected automatically by DetectInboundProxy.
//
// Xray routing rules remain entirely under the user's control — xkeen-ui
// does not modify routing. Existing routing rules (which typically match
// Cloudflare and CDN IPs by ipset) apply to SOCKS5 traffic the same way
// they apply to transparent-proxy traffic.
//
// Idempotent and silent when there's nothing to do:
//   - user has their own SOCKS5/HTTP inbound → skip silently
//   - managed file exists → skip silently
//   - managed file missing → create + restart xray
func migrateXkeenUISocksInbound() error {
	return ensureManagedSocksInbound(installXrayConfigDir)
}

// ensureManagedSocksInbound is the core logic, separated so it can accept
// the actual config directory (useful for testing).
func ensureManagedSocksInbound(xrayConfigDir string) error {
	// Step 1: If the user already has a SOCKS5/HTTP inbound, we don't need
	// our own. DetectInboundProxy checks all inbound files including ours.
	if existing := subscription.DetectInboundProxy(xrayConfigDir); existing != "" {
		return nil // user has their own inbound, nothing to do
	}

	// Step 2: If our managed file already exists, it means DetectInboundProxy
	// failed to parse it (corrupt JSON?) or the inbound in it isn't socks/http.
	// Don't blindly overwrite — the user may have edited it intentionally.
	path := filepath.Join(xrayConfigDir, subscription.ManagedSocksInboundFile)
	if _, err := os.Stat(path); err == nil {
		return nil // file exists, leave it alone
	}

	// Step 3: File is missing — recreate it.
	if err := os.MkdirAll(xrayConfigDir, 0o750); err != nil {
		return fmt.Errorf("mkdir %s: %w", xrayConfigDir, err)
	}
	if err := os.WriteFile(path, []byte(subscription.ManagedSocksInboundConfig()), 0o644); err != nil { //nolint:gosec // config file is not executable
		return fmt.Errorf("write %s: %w", path, err)
	}
	fmt.Printf("[startup] Created %s (SOCKS5 on 127.0.0.1:%d for VPN-routed fetches)\n",
		path, subscription.ManagedSocksPort)

	// Step 4: Restart Xray so the new inbound takes effect.
	if err := restartXrayViaXkeen(); err != nil {
		// Non-fatal: the file is created and will be picked up on next xray restart.
		fmt.Printf("[startup] ⚠ Could not restart xray (will apply on next restart): %v\n", err)
	} else {
		fmt.Printf("[startup] Xray restarted to load the new inbound\n")
	}
	return nil
}

// restartXrayViaXkeen restarts the xray service via the xkeen binary.
// This is the same mechanism used by the service handler.
func restartXrayViaXkeen() error {
	cmd := exec.Command("xkeen", "-restart")
	return cmd.Run()
}
