#!/usr/bin/env bash
# dev.sh — Run xkeen-go locally with fake Xray data for frontend development.
#
# Starts 3 processes:
#   1. Fake Xray metrics server on :11111
#   2. Go backend on :8089 (proxies to fake Xray)
#   3. Vite dev server on :5173 (hot-reload, proxies API/WS to backend)
#
# Usage: bash test/cmd/dev.sh
# Open: http://localhost:5173

set -e
cd "$(git rev-parse --show-toplevel 2>/dev/null || echo .)"

# Create temp dirs
mkdir -p /tmp/xkeen-dev/xkeen-ui/backups

echo "==> Starting fake Xray on :11111 ..."
go run ./test/cmd/fakexray &
FAKE_PID=$!
sleep 0.5

echo "==> Starting Go backend on :8089 ..."
go run . -config test/cmd/devconfig.json &
BACKEND_PID=$!

echo "==> Starting Vite dev server on :5173 ..."
cd web && npx vite --host &
VITE_PID=$!

cleanup() {
    echo ""
    echo "==> Stopping..."
    kill $FAKE_PID $BACKEND_PID $VITE_PID 2>/dev/null
    wait 2>/dev/null
}
trap cleanup EXIT INT TERM

echo ""
echo "==> Ready! Open http://localhost:5173"
echo "    Login: admin / admin"
echo "    Press Ctrl+C to stop"
echo ""
wait
