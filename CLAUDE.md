# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

XKEEN-GO is a single-binary Go web application providing a modern web UI for managing XKeen configurations on Keenetic routers. It features real-time log viewing via WebSocket, JSONC configuration editing, and XKeen service control.

**Target platforms:** Linux amd64, arm64, mipsle (Keenetic routers with Entware)

## Build Commands

All commands should be run from the `xkeen-go/` directory:

```bash
# Development
make build          # Build for current OS
make run            # Run locally (uses /opt/etc/xkeen-go/config.json by default)
make deps           # Download dependencies

# Testing
make test           # Run all tests
make test-unit      # Run unit tests only (./internal/utils/... ./internal/services/...)
make test-integration  # Run integration tests (./internal/handlers/...)
make coverage       # Generate coverage report (coverage.html)
make bench          # Run benchmarks

# Code quality
make lint           # Run golangci-lint (requires golangci-lint installed)
make fmt            # Format code
make vet            # Run go vet

# Production builds
make build-all      # Build for all platforms (linux-amd64, arm64, mipsle)
make keenetic       # Build for all Keenetic routers
make keenetic-arm64 # Build for Keenetic ARM64 (KN-1010, KN-1810, etc.)
make keenetic-dist  # Build + compress with UPX for distribution
```

## Manual Build with Compression

If `make` is not available, build manually:

```bash
# Build for Keenetic ARM64
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "-s -w" -o build/xkeen-go-keenetic-arm64 .

# Compress with UPX (requires UPX installed)
upx --best --lzma build/xkeen-go-keenetic-arm64
```

**Binary sizes:**
- Uncompressed: ~6.4 MB
- Compressed with UPX: ~1.9 MB (29% of original)

## Running a Single Test

```bash
# Run specific test
go test -v -run TestFunctionName ./internal/utils/...

# Run tests in specific file
go test -v ./internal/handlers/... -run TestServiceHandler
```

## Architecture

```
xkeen-go/
├── main.go                 # Entry point, install/uninstall commands, server startup
├── static.go               # Embedded web files (go:embed web)
├── internal/
│   ├── config/             # Configuration loading and validation
│   ├── handlers/           # HTTP handlers for API endpoints
│   │   ├── config.go       # Config file operations (list, read, save with backup)
│   │   ├── logs.go         # Log reading and WebSocket streaming
│   │   └── service.go      # XKeen service control (start/stop/restart/status)
│   ├── server/
│   │   ├── server.go       # HTTP server setup, routing, session management
│   │   └── middleware.go   # Auth, CSRF, rate limiting, logging, security headers
│   ├── utils/
│   │   ├── jsonc.go        # JSONC parser (JSON with comments)
│   │   └── path.go         # Path validation against allowed_roots
│   └── testutil/           # Mocks for testing (auth, exec, fs)
├── test/
│   ├── unit/               # Unit tests
│   └── e2e/                # End-to-end tests
└── web/                    # Frontend (embedded into binary)
    ├── index.html          # Main SPA
    ├── login.html          # Login page
    └── static/             # CSS, JS
```

## Key Architecture Patterns

### Middleware Chain
Requests flow through: `LoggingMiddleware` → `SecurityHeadersMiddleware` → `AuthMiddleware` → `CSRFMiddleware`

Protected routes require:
1. Valid session cookie
2. Valid CSRF token in `X-CSRF-Token` header for POST/PUT/DELETE/PATCH

### Session Management
- In-memory sessions with configurable timeout (default 24h)
- Session tokens and CSRF tokens generated with `crypto/rand`
- Sessions cleaned up every 10 minutes

### Path Validation
All file operations use `utils.PathValidator` to ensure paths are within `allowed_roots` from config. This prevents path traversal attacks.

### Service Control
ServiceHandler uses a whitelist of allowed commands (`/opt/etc/init.d/xkeen {start|stop|restart|status}`) with timeouts:
- Status: 10s
- Start/Stop: 30s
- Restart: 45s

### WebSocket for Logs
LogsHandler provides real-time log streaming via WebSocket at `/ws/logs`. Authentication is done via session cookie (WebSocket cannot send custom headers for CSRF).

## Configuration

Config file: `/opt/etc/xkeen-go/config.json` (can override with `-config` flag)

Key settings:
- `port`: HTTP listen port (default 8089)
- `xray_config_dir`: Directory for Xray configs
- `allowed_roots`: Whitelisted directories for file operations
- `auth.password_hash`: bcrypt hash (cost 12) of admin password
- `auth.session_timeout`: Session timeout in hours
- `auth.max_login_attempts`: Failed attempts before lockout
- `auth.lockout_duration`: Lockout duration in minutes

## Security Features

- **bcrypt** password hashing (cost 12)
- **CSRF** protection with constant-time token comparison
- **Rate limiting** per IP for login attempts
- **Path traversal** protection via allowed_roots whitelist
- **Security headers**: X-Frame-Options, CSP, X-Content-Type-Options, etc.

## Testing Approach

- Unit tests use mocks from `internal/testutil/`
- `CommandExecutor` interface allows mocking service commands
- `PathValidator` can be tested with custom allowed_roots
- Integration tests cover handler endpoints with mock auth

## Installation on Keenetic

The binary includes install/uninstall commands:
```bash
./xkeen-go-keenetic-arm64 install    # Install to /opt/etc/xkeen-go/
./xkeen-go-keenetic-arm64 uninstall  # Remove from system
./xkeen-go-keenetic-arm64 version    # Show version info
```

Install creates:
- Binary at `/opt/bin/xkeen-go-keenetic-arm64`
- Config at `/opt/etc/xkeen-go/config.json`
- Init script at `/opt/etc/init.d/xkeen-go`
