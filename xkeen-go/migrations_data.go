package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// migrateAWGInitS90 renames the legacy AWG init script (/opt/etc/init.d/awg,
// no S-prefix) to /opt/etc/init.d/S90awg so Entware's rc.unslung starts it
// at boot. Without the S-prefix, WARP and other AWG client interfaces never
// auto-started after a reboot.
//
// Safe to run multiple times: if S90awg already exists, just removes old awg.
func migrateAWGInitS90() error {
	const (
		oldPath = "/opt/etc/init.d/awg"      // legacy: no S-prefix, never ran at boot
		newPath = "/opt/etc/init.d/S90awg"   // S-prefix: rc.unslung picks it up
		confDir = "/opt/etc/awg"             // AWG config directory
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
