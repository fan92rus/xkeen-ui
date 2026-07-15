#!/bin/sh
# XKEEN-UI Uninstall Script
# Completely removes XKEEN-UI from the system.
# Compatible with busybox (Keenetic/Entware).

set -e

BINARY_DIR="/opt/bin"
INIT_SCRIPT="/opt/etc/init.d/xkeen-ui"
RC_DIR="/opt/etc/init.d/rc.d"
UPDATE_SCRIPT="/opt/etc/xkeen-ui/update.sh"
LOGFILE="/opt/var/log/xkeen-ui.log"
LOG_DIR="/opt/var/log/xkeen-ui"
PIDFILE="/var/run/xkeen-ui.pid"
CONFIG_DIR="/opt/etc/xkeen-ui"

log() { echo "$1"; }
error() { echo "ERROR: $1" >&2; }

# Check root
if [ "$(id -u)" != "0" ]; then
    error "This script must be run as root"
    exit 1
fi

echo "==================================="
echo "  XKEEN-UI Uninstall Script"
echo "==================================="
echo ""

# 1. Stop service via init script
if [ -f "$INIT_SCRIPT" ]; then
    log "Stopping service..."
    "$INIT_SCRIPT" stop 2>/dev/null || true
fi

# 2. Kill any remaining processes
log "Checking for running processes..."
# Exclude the uninstall script itself from the kill loop (its path
# contains "xkeen-ui-uninstall").
for PID in $(ps 2>/dev/null | grep "xkeen-ui" | grep -v grep | grep -v uninstall | awk '{print $1}'); do
    log "Killing process $PID..."
    kill "$PID" 2>/dev/null || true
done
sleep 1
for PID in $(ps 2>/dev/null | grep "xkeen-ui" | grep -v grep | grep -v uninstall | awk '{print $1}'); do
    kill -9 "$PID" 2>/dev/null || true
done

# 3. Remove PID file
rm -f "$PIDFILE"

# 4. Remove cron watchdog
log "Removing cron watchdog..."
rm -f "/opt/etc/cron.d/xkeen-ui-watchdog"
# Restart cron to apply
killall -HUP crond 2>/dev/null || true

# 5. Remove rc.d symlinks
log "Removing rc.d symlinks..."
rm -f "$RC_DIR/S70xkeen-ui" "$RC_DIR/S99xkeen-ui" "$RC_DIR/K01xkeen-ui"

# 5b. Remove autostart symlinks (S70 current, S99 legacy)
rm -f "/opt/etc/init.d/S70xkeen-ui" "/opt/etc/init.d/S99xkeen-ui"

# 5. Remove init script
if [ -f "$INIT_SCRIPT" ]; then
    log "Removing init script..."
    rm -f "$INIT_SCRIPT"
fi

# 6. Remove symlink
rm -f "$BINARY_DIR/xkeen-ui"

# 7. Remove all xkeen-ui binaries (any arch)
log "Removing binaries..."
rm -f "$BINARY_DIR/xkeen-ui-keenetic-"*
rm -f "$BINARY_DIR/xkeen-ui-linux-"*

# 8. Remove logs
log "Removing logs..."
rm -f "$LOGFILE"
rm -rf "$LOG_DIR"

# 9. Remove update script
rm -f "$UPDATE_SCRIPT"

# 10. Config directory — ask before removing
echo ""
printf "Remove config directory %s? [y/N]: " "$CONFIG_DIR"
read -r ANSWER </dev/tty
case "$ANSWER" in
    [Yy]*) rm -rf "$CONFIG_DIR"; echo "Config removed." ;;
    *)     echo "Config preserved: $CONFIG_DIR" ;;
esac

# 11. Check for leftovers
echo ""
LEFTOVERS=$(find /opt -name "*xkeen-ui*" 2>/dev/null)
if [ -z "$LEFTOVERS" ]; then
    echo "Clean — nothing left."
else
    echo "Remaining files:"
    echo "$LEFTOVERS"
fi

echo ""
echo "Done."
