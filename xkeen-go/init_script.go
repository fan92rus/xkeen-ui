// Package main provides the entry point for XKEEN-UI.
package main

import "fmt"

// getInitScript returns the init script template with the given binary name.
func getInitScript(binName string) string {
	return fmt.Sprintf(`#!/bin/sh
### BEGIN INIT INFO
# Provides:          xkeen-ui
# Required-Start:    $network $local_fs
# Required-Stop:     $network
# Default-Start:     2 3 4 5
# Default-Stop:      0 1 6
# Description:       XKEEN-UI Web Interface for XKeen on Keenetic routers
### END INIT INFO
PATH=/opt/bin:/opt/sbin:/bin:/sbin:/usr/bin:/usr/sbin
DAEMON=/opt/bin/%s
CONFIG=/opt/etc/xkeen-ui/config.json
PIDFILE=/var/run/xkeen-ui.pid
LOGFILE=/opt/var/log/xkeen-ui.log
NAME=xkeen-ui
DESC="XKEEN-UI Web Interface"

start() {
    # Wait for /opt filesystem to be ready
    i=0
    while [ "$i" -lt 30 ] && [ ! -d /opt/bin ]; do
        sleep 1
        i=$((i + 1))
    done
    if [ ! -d /opt/bin ]; then
        echo "ERROR: /opt filesystem not ready after 30s"
        return 1
    fi

    # Clean stale PID file
    if [ -f "$PIDFILE" ]; then
        if ! kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then
            rm -f "$PIDFILE"
        fi
    fi

    if [ -f "$PIDFILE" ]; then
        echo "$NAME is already running (PID: $(cat "$PIDFILE"))"
        return 1
    fi

    echo "Starting $DESC..."
    mkdir -p /opt/var/log
    "$DAEMON" -config "$CONFIG" >> "$LOGFILE" 2>&1 &
    echo $! > "$PIDFILE"

    # Brief wait to confirm process didn't crash immediately
    sleep 1
    if ! kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then
        rm -f "$PIDFILE"
        echo "ERROR: $NAME failed to start, check $LOGFILE"
        return 1
    fi

    echo "$NAME started (PID: $(cat "$PIDFILE"))"
    echo "Logs: $LOGFILE"
}

stop() {
    echo "Stopping $DESC..."
    if [ -f "$PIDFILE" ]; then
        pid=$(cat "$PIDFILE")
        if kill -0 "$pid" 2>/dev/null; then
            kill "$pid"
            # Wait up to 5 seconds for graceful shutdown
            i=0
            while kill -0 "$pid" 2>/dev/null && [ "$i" -lt 5 ]; do
                sleep 1
                i=$((i + 1))
            done
            # Force kill if still running
            if kill -0 "$pid" 2>/dev/null; then
                kill -9 "$pid" 2>/dev/null
            fi
        fi
    fi
    rm -f "$PIDFILE"
    echo "$NAME stopped"
}

status() {
    if [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then
        echo "$NAME is running (PID: $(cat "$PIDFILE"))"
    else
        echo "$NAME is not running"
    fi
}

case "$1" in
    start)
        start
        ;;
    stop)
        stop
        ;;
    restart)
        stop
        sleep 1
        start
        ;;
    status)
        status
        ;;
    uninstall)
        echo "Running XKEEN-UI uninstall..."
        "$DAEMON" uninstall
        ;;
    check)
        "$DAEMON" check
        ;;
    *)
        echo "Usage: $0 {start|stop|restart|status|check|uninstall}"
        exit 1
        ;;
esac

exit 0
`, binName)
}
