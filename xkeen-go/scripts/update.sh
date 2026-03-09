#!/bin/sh
# XKEEN-UI Atomic Update Script
# This script is called by the running process before it exits.
# It waits for the old process to terminate, then atomically replaces the binary.
# Compatible with busybox (no fractional sleep).

OLD_PID=$1
NEW_BINARY="/tmp/xkeen-ui-keenetic-arm64.new"
TARGET_BINARY="/opt/bin/xkeen-ui-keenetic-arm64"
INIT_SCRIPT="/opt/etc/init.d/xkeen-ui"
LOGFILE="/opt/var/log/xkeen-ui.log"

log() {
    echo "$(date '+%Y-%m-%d %H:%M:%S') [update] $1" >> "$LOGFILE"
    echo "$1"
}

# Wait for old process to terminate
log "Waiting for process $OLD_PID to terminate..."
WAIT_COUNT=0
while kill -0 "$OLD_PID" 2>/dev/null; do
    sleep 1
    WAIT_COUNT=$((WAIT_COUNT + 1))
    # Timeout after 30 seconds
    if [ $WAIT_COUNT -gt 30 ]; then
        log "ERROR: Timeout waiting for process $OLD_PID to terminate"
        rm -f "$NEW_BINARY"
        exit 1
    fi
done
log "Process $OLD_PID terminated"

# Verify new binary exists
if [ ! -f "$NEW_BINARY" ]; then
    log "ERROR: New binary not found at $NEW_BINARY"
    exit 1
fi

# Replace binary atomically
log "Replacing binary..."
if ! mv "$NEW_BINARY" "$TARGET_BINARY"; then
    log "ERROR: Failed to replace binary"
    rm -f "$NEW_BINARY"
    exit 1
fi

# Ensure correct permissions
chmod 755 "$TARGET_BINARY"
log "Binary replaced successfully"

# Clean up any leftover temp files
rm -f "$NEW_BINARY"

# Run install to update init script and other system files
log "Updating system files..."
"$TARGET_BINARY" install >/dev/null 2>&1

# Start the new version
log "Starting service..."
"$INIT_SCRIPT" start
log "Update complete"
