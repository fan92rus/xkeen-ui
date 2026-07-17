#!/bin/sh
# install-awg-uninstall.sh — Remove AmneziaWG (amneziawg-go + amneziawg-tools)
#
# Called by xkeen-ui uninstall handler. Output protocol:
#   CLEANING:message   — stopping interfaces etc.
#   REMOVING:package   — removing via opkg
#   ERROR:message      — fatal error (exit 1)
#   DONE               — success (exit 0)

AWG_DIR="${AWG_DIR:-/opt/etc/awg}"
PREFIX="01__Entware_AWG-Go_Remove"

# ── Bring down all AWG interfaces ──
if command -v awg-quick >/dev/null 2>&1; then
  echo "CLEANING:stopping AWG interfaces..."

  # Bring down each conf to clean up routing rules
  if [ -d "$AWG_DIR" ]; then
    for conf in "$AWG_DIR"/*.conf; do
      [ -f "$conf" ] || continue
      name=$(basename "$conf" .conf)
      echo "CLEANING:bringing down $name..."
      awg-quick down "$conf" >/dev/null 2>&1 || true
    done
  fi
fi

# ── Remove init script (both S90awg and legacy awg) ──
if [ -f /opt/etc/init.d/S90awg ] || [ -f /opt/etc/init.d/awg ]; then
  echo "CLEANING:removing init script..."
  rm -f /opt/etc/init.d/S90awg /opt/etc/init.d/awg
fi

# ── Remove via opkg ──
for pkg in amneziawg-tools amneziawg-go; do
  if opkg list-installed 2>/dev/null | grep -q "^${pkg} "; then
    echo "REMOVING:${pkg}"
    opkg remove --force-depends "${pkg}" 2>&1 || echo "WARN:${pkg} remove returned non-zero"
    echo "OK:${pkg}"
  else
    echo "OK:${pkg} (not installed)"
  fi
done

# ── Remove leftover files ──
echo "CLEANING:removing binaries..."
rm -f /opt/bin/awg /opt/bin/awg-quick 2>/dev/null

echo "DONE"
