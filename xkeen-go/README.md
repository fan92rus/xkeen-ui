# XKEEN-UI

Lightweight Web UI for XKeen configuration on Keenetic routers.

## Overview

XKEEN-UI is a single-binary web application designed to provide a modern, secure web interface for managing XKeen configurations on Keenetic routers. Built with Go for minimal resource usage, it offers real-time log viewing, configuration editing with JSONC support, and XKeen service control.

## Features

### Core Functionality

- **Single Binary Deployment** - Approximately 1.5 MB binary with no external dependencies
- **Configuration Editor** - Full JSONC (JSON with Comments) support for editing Xray and Xkeen config files
- **Real-time Log Viewer** - WebSocket-based log streaming for live monitoring
- **XKeen Service Control** - Start, stop, and restart XKeen services directly from the UI
- **File Browser** - Navigate and manage configuration directories

### Security Features

- **Session-based Authentication** - HTTP-only cookies with configurable session timeout
- **CSRF Protection** - All state-changing operations require valid CSRF tokens
- **Path Traversal Protection** - Strict path validation against whitelisted directories
- **Rate Limiting** - Configurable login attempt limiting with IP lockout
- **Password Hashing** - bcrypt with cost factor 12 for secure password storage
- **Security Headers** - X-Frame-Options, CSP, X-Content-Type-Options, and more

### User Interface

- **Dark Theme** - Modern dark interface optimized for low-light environments
- **CodeMirror 6** - Advanced code editor with syntax highlighting
- **Responsive Design** - Works on desktop and mobile devices
- **WebSocket Support** - Real-time updates without polling

## Requirements

### Runtime Requirements

- Keenetic router with Entware installed
- XKeen properly configured
- One of the following architectures:
  - x86_64 (amd64)
  - ARM64 (aarch64)
  - MIPSLE (mipsel)

### Development Requirements

- Go 1.21 or later
- Make (optional, for build automation)

## Installation

### Quick Start

1. Download the latest release for your router architecture from the releases page.

2. Copy the binary to your router:
   ```bash
   scp xkeen-go-linux-mipsle root@router:/opt/etc/xkeen-go/
   ```

3. Create the configuration file:
   ```bash
   ssh root@router
   mkdir -p /opt/etc/xkeen-go
   cat > /opt/etc/xkeen-go/config.json << 'EOF'
   {
     "port": 8089,
     "xray_config_dir": "/opt/etc/xray/configs",
     "xkeen_binary": "xkeen",
     "allowed_roots": [
       "/opt/etc/xray",
       "/opt/etc/xkeen",
       "/opt/etc/mihomo",
       "/opt/var/log"
     ],
     "session_secret": "",
     "log_level": "info",
     "auth": {
       "password_hash": "",
       "session_timeout": 24,
       "max_login_attempts": 5,
       "lockout_duration": 5
     }
   }
   EOF
   ```

4. Generate a password hash:
   ```bash
   # On a system with Go installed:
   go run -e 'package main; import ("fmt"; "golang.org/x/crypto/bcrypt"); func main() { h, _ := bcrypt.GenerateFromPassword([]byte("your-password"), 12); fmt.Println(string(h)) }'
   ```

5. Update the config with your password hash and start the service:
   ```bash
   chmod +x /opt/etc/xkeen-go/xkeen-go
   /opt/etc/xkeen-go/xkeen-go -config /opt/etc/xkeen-go/config.json
   ```

6. Access the web UI at `http://router-ip:8089`

### Auto-start with Init Script

Create an init script for automatic startup:

```bash
cat > /opt/etc/init.d/S99xkeen-go << 'EOF'
#!/bin/sh

DAEMON=/opt/etc/xkeen-go/xkeen-go
CONFIG=/opt/etc/xkeen-go/config.json
PIDFILE=/opt/var/run/xkeen-go.pid

start() {
    if [ -f $PIDFILE ] && kill -0 $(cat $PIDFILE) 2>/dev/null; then
        echo "xkeen-go is already running"
        return 1
    fi
    echo "Starting xkeen-go..."
    start-stop-daemon -S -b -m -p $PIDFILE -x $DAEMON -- -config $CONFIG
}

stop() {
    echo "Stopping xkeen-go..."
    start-stop-daemon -K -p $PIDFILE -x $DAEMON
    rm -f $PIDFILE
}

case "$1" in
    start)   start ;;
    stop)    stop ;;
    restart) stop; sleep 1; start ;;
    status)
        if [ -f $PIDFILE ] && kill -0 $(cat $PIDFILE) 2>/dev/null; then
            echo "xkeen-go is running"
        else
            echo "xkeen-go is not running"
        fi
        ;;
    *)       echo "Usage: $0 {start|stop|restart|status}" ;;
esac
EOF

chmod +x /opt/etc/init.d/S99xkeen-go
/opt/etc/init.d/S99xkeen-go start
```

## Configuration

### Configuration File

The configuration file is located at `/opt/etc/xkeen-go/config.json` by default. You can specify a different path using the `-config` flag.

```json
{
  "port": 8089,
  "xray_config_dir": "/opt/etc/xray/configs",
  "xkeen_binary": "xkeen",
  "allowed_roots": [
    "/opt/etc/xray",
    "/opt/etc/xkeen",
    "/opt/etc/mihomo",
    "/opt/var/log"
  ],
  "session_secret": "",
  "log_level": "info",
  "cors": {
    "enabled": false,
    "allowed_origins": []
  },
  "auth": {
    "password_hash": "$2a$12$...",
    "session_timeout": 24,
    "max_login_attempts": 5,
    "lockout_duration": 5
  }
}
```

### Configuration Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `port` | int | 8089 | HTTP server listen port |
| `xray_config_dir` | string | `/opt/etc/xray/configs` | Directory containing Xray configuration files |
| `xkeen_binary` | string | `xkeen` | Path or name of the xkeen binary |
| `allowed_roots` | []string | See defaults | Whitelisted directories for file operations |
| `session_secret` | string | (empty) | Secret key for session encryption |
| `log_level` | string | `info` | Logging level: debug, info, warn, error |
| `cors.enabled` | bool | false | Enable CORS support |
| `cors.allowed_origins` | []string | [] | List of allowed CORS origins |
| `auth.password_hash` | string | (empty) | bcrypt hash of the password |
| `auth.session_timeout` | int | 24 | Session timeout in hours |
| `auth.max_login_attempts` | int | 5 | Maximum failed login attempts before lockout |
| `auth.lockout_duration` | int | 5 | Lockout duration in minutes |

### Default Allowed Roots

By default, the following directories are accessible:

- `/opt/etc/xray` - Xray configuration files
- `/opt/etc/xkeen` - XKeen configuration files
- `/opt/etc/mihomo` - Mihomo/Mihomo configuration files
- `/opt/var/log` - Log files

### Security Considerations

1. **Password Hash**: Always set a strong password hash before deployment. The application will warn if using default credentials.

2. **Session Secret**: For production, generate a random session secret:
   ```bash
   openssl rand -base64 32
   ```

3. **Allowed Roots**: Only add directories that the web UI needs to access. Never add system directories like `/` or `/etc`.

## API Endpoints

### Authentication

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/auth/login` | Authenticate and create session |
| POST | `/api/auth/logout` | Destroy current session |
| GET | `/api/auth/status` | Check authentication status |
| GET | `/api/auth/csrf` | Get CSRF token for current session |

### Configuration Files

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/config/files` | List config files in directory |
| GET | `/api/config/file` | Read file content |
| POST | `/api/config/file` | Save file content (with backup) |

### XKeen Service Control

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/xkeen/status` | Get service status |
| POST | `/api/xkeen/start` | Start XKeen service |
| POST | `/api/xkeen/stop` | Stop XKeen service |
| POST | `/api/xkeen/restart` | Restart XKeen service |

### Logs

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/logs/xray` | Read log entries |
| GET | `/ws/logs` | WebSocket for real-time log streaming |

### File System

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/fs/list` | List directory contents |
| GET | `/api/fs/read` | Read file content |
| PUT | `/api/fs/write` | Write file content |
| DELETE | `/api/fs/delete` | Delete file |
| POST | `/api/fs/mkdir` | Create directory |

### Health Check

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/health` | Server health status (no auth required) |

### API Examples

#### Login

```bash
curl -X POST http://localhost:8089/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"password":"your-password"}'
```

Response:
```json
{
  "ok": true,
  "csrf_token": "base64-encoded-token"
}
```

#### List Config Files

```bash
curl http://localhost:8089/api/config/files?path=/opt/etc/xkeen \
  -H "Cookie: session=your-session-token"
```

Response:
```json
{
  "path": "/opt/etc/xkeen",
  "files": [
    {
      "name": "config.json",
      "path": "/opt/etc/xkeen/config.json",
      "size": 1024,
      "modified": 1709251200,
      "is_dir": false
    }
  ]
}
```

#### Save Config File

```bash
curl -X POST http://localhost:8089/api/config/file \
  -H "Content-Type: application/json" \
  -H "X-CSRF-Token: your-csrf-token" \
  -H "Cookie: session=your-session-token" \
  -d '{"path":"/opt/etc/xkeen/config.json","content":"{\"key\":\"value\"}"}'
```

## Development

### Project Structure

```
xkeen-go/
├── main.go                    # Application entry point
├── Makefile                   # Build automation
├── go.mod                     # Go module definition
├── go.sum                     # Dependency checksums
├── internal/
│   ├── config/
│   │   └── config.go          # Configuration management
│   ├── handlers/
│   │   ├── config.go          # Config file handlers
│   │   ├── logs.go            # Log handling and WebSocket
│   │   └── service.go         # XKeen service control
│   ├── server/
│   │   ├── server.go          # HTTP server setup
│   │   └── middleware.go      # Authentication, CSRF, rate limiting
│   ├── utils/
│   │   ├── jsonc.go           # JSONC parser
│   │   └── path.go            # Path validation utilities
│   └── testutil/
│       ├── mock_auth.go       # Authentication mocks
│       ├── mock_exec.go       # Command execution mocks
│       └── mock_fs.go         # Filesystem mocks
├── test/
│   ├── unit/                  # Unit tests
│   └── e2e/                   # End-to-end tests
└── web/
    ├── index.html             # Main application page
    ├── login.html             # Login page
    └── static/
        ├── css/
        │   └── style.css      # Application styles
        └── js/
            └── app.js         # Frontend application
```

### Building

```bash
# Build for current platform
make build

# Build for all supported platforms
make build-all

# Create release archives
make release

# Run tests
make test

# Run with coverage
make coverage

# Run linter
make lint

# Format code
make fmt
```

### Testing

**Backend (Go):**

```bash
# All Go tests
make test

# Unit tests only (utils + services + handlers)
make test-unit

# With the race detector (recommended before merging; requires CGo / a C
# compiler — run on Linux or via the CI)
CGO_ENABLED=1 go test -race ./...

# Coverage report (opens coverage.html)
make coverage

# Scan dependencies for known Go vulnerabilities
make vet
govulncheck ./...   # go install golang.org/x/vuln/cmd/govulncheck@latest
```

**Frontend (Vue + Vite):**

```bash
cd web
npm test            # vitest run — all unit + component tests
npm run test:watch  # vitest watch mode
npm run build       # production bundle (also verifies it compiles)
npm audit --omit=dev --audit-level=high   # production-dep vuln scan
```

The frontend has two kinds of tests:

- **Pure-logic unit tests** (`tests/*.test.js`) — fast, run in the Node
  environment. Cover utils like ANSI formatting, JSON escaping, filter
  regexes, metrics rates, backoff, log filtering, and diffing.
- **Component tests** (`tests/*.component.test.js`) — mount real `.vue`
  components via `@vue/test-utils` in a `happy-dom` environment (opt-in per
  file via a `// @vitest-environment happy-dom` docblock) so the pure-logic
  tests stay in the fast Node lane.

End-to-end (browser) tests live in `web/test/` and run via Puppeteer
(`npm run test:e2e`, `npm run test:profiles`); they are excluded from
`vitest`.

### Continuous Integration

The `ci` workflow (`.github/workflows/ci.yml`) runs on every push / PR to
`master` as a non-blocking quality gate:

- **go-checks** job: `go vet`, `go test -race`, `go build`, and
  `govulncheck ./...`.
- **frontend-checks** job: `npm ci`, `npm run build`, `npm test`, and
  `npm audit --omit=dev --audit-level=high`.

The `build.yml` / `build-dev.yml` workflows build release binaries for
Keenetic targets (ARM64, MIPSLE) and are triggered separately.
### Available Make Targets

| Target | Description |
|--------|-------------|
| `build` | Build for current OS |
| `build-linux` | Build for Linux amd64 |
| `build-arm64` | Build for Linux arm64 |
| `build-mipsle` | Build for Linux mipsle (Keenetic) |
| `build-all` | Build for all target platforms |
| `compress` | Compress binaries with UPX |
| `test` | Run all tests |
| `test-unit` | Run unit tests only |
| `test-integration` | Run integration tests |
| `coverage` | Generate coverage report |
| `bench` | Run benchmarks |
| `run` | Run locally |
| `deps` | Download dependencies |
| `clean` | Clean build artifacts |
| `lint` | Run golangci-lint |
| `fmt` | Format code |
| `vet` | Run go vet |
| `install` | Install to /opt/etc/xkeen-go |
| `release` | Create release archives |
| `size` | Show binary sizes |

### Running Locally

```bash
# Download dependencies
make deps

# Run in development mode
make run

# Or with custom config
go run main.go -config /path/to/config.json
```

## Security

### Authentication

- Passwords are hashed using bcrypt with a cost factor of 12
- Sessions are stored in memory with configurable expiration
- HTTP-only cookies prevent JavaScript access to session tokens
- SameSite=Strict cookies prevent CSRF via cross-site requests

### CSRF Protection

- All POST, PUT, DELETE, and PATCH requests require a valid CSRF token
- Tokens are session-specific and rotated on each login
- Constant-time comparison prevents timing attacks

### Path Traversal Protection

The `PathValidator` in `internal/utils/path.go` provides comprehensive protection:

- Validates all paths against whitelisted `allowed_roots`
- Detects and blocks `..` traversal patterns
- Optionally blocks symlinks to prevent escaping allowed directories
- Resolves symlinks before validation when allowed

### Rate Limiting

- Configurable maximum login attempts (default: 5)
- IP-based lockout after exceeding attempts (default: 5 minutes)
- Automatic cleanup of expired lockout entries
- Retry-After header included in lockout responses

### Security Headers

The following headers are set on all responses:

- `X-Frame-Options: DENY` - Prevents clickjacking
- `X-Content-Type-Options: nosniff` - Prevents MIME sniffing
- `X-XSS-Protection: 1; mode=block` - XSS protection
- `Referrer-Policy: strict-origin-when-cross-origin`
- `Content-Security-Policy` - Restricts resource loading

### Best Practices

1. **Change Default Credentials**: Always set a strong password before deployment
2. **Use HTTPS**: Deploy behind a reverse proxy with TLS for production
3. **Restrict Network Access**: Limit access to trusted IPs when possible
4. **Regular Updates**: Keep the application and dependencies updated
5. **Monitor Logs**: Review authentication logs for suspicious activity

## Troubleshooting

### Common Issues

**Cannot start server - port in use**

```bash
# Check what's using the port
netstat -tlnp | grep 8089

# Kill the process or change port in config
```

**Permission denied errors**

```bash
# Ensure binary is executable
chmod +x /opt/etc/xkeen-go/xkeen-go

# Check file ownership
chown -R root:root /opt/etc/xkeen-go
```

**Config files not accessible**

- Verify the directory is in `allowed_roots`
- Check file permissions
- Ensure paths are absolute

**Login fails with valid credentials**

- Check if IP is locked out due to rate limiting
- Verify password hash is correctly formatted
- Check server logs for authentication errors

### Log Locations

- Application logs: stdout/stderr (check with `logread` on Keenetic)
- Xray logs: `/opt/var/log/xray/`
- XKeen logs: `/opt/var/log/xkeen/`

### Debug Mode

Enable debug logging by setting `log_level` to `debug` in the configuration:

```json
{
  "log_level": "debug"
}
```

## Architecture

### System Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                        Browser                               │
│  ┌─────────────────┐  ┌─────────────────┐  ┌──────────────┐│
│  │   Login Page    │  │   Main UI       │  │ Log Viewer   ││
│  │   (login.html)  │  │   (index.html)  │  │ (WebSocket)  ││
│  └────────┬────────┘  └────────┬────────┘  └──────┬───────┘│
└───────────┼────────────────────┼──────────────────┼────────┘
            │ HTTP               │ HTTP             │ WS
            ▼                    ▼                  ▼
┌─────────────────────────────────────────────────────────────┐
│                     HTTP Server (Gorilla Mux)               │
│  ┌─────────────────────────────────────────────────────────┐│
│  │                    Middleware Stack                      ││
│  │  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌────────────┐ ││
│  │  │ Logging  │→│ Security │→│   Auth   │→│    CSRF    │ ││
│  │  │          │ │ Headers  │ │          │ │            │ ││
│  │  └──────────┘ └──────────┘ └──────────┘ └────────────┘ ││
│  └─────────────────────────────────────────────────────────┘│
│  ┌───────────────┐  ┌───────────────┐  ┌────────────────┐  │
│  │ ConfigHandler │  │ ServiceHandler│  │  LogsHandler   │  │
│  │ (config.go)   │  │ (service.go)  │  │  (logs.go)     │  │
│  └───────┬───────┘  └───────┬───────┘  └───────┬────────┘  │
└──────────┼──────────────────┼──────────────────┼───────────┘
           │                  │                  │
           ▼                  ▼                  ▼
┌──────────────────────────────────────────────────────────────┐
│                      File System                             │
│  ┌──────────────┐  ┌──────────────┐  ┌────────────────────┐ │
│  │ /opt/etc/xray│  │/opt/etc/xkeen│  │ /opt/var/log       │ │
│  │ (configs)    │  │ (configs)    │  │ (logs)             │ │
│  └──────────────┘  └──────────────┘  └────────────────────┘ │
└──────────────────────────────────────────────────────────────┘
```

### Request Flow

1. Request received by HTTP server
2. Logging middleware records request start time
3. Security headers middleware adds protective headers
4. Auth middleware validates session cookie (for protected routes)
5. CSRF middleware validates CSRF token (for mutating requests)
6. Route handler processes request
7. Path validator ensures file operations are within allowed roots
8. Response returned through middleware chain

## Contributing

Contributions are welcome. Please follow these guidelines:

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

### Code Style

- Run `make fmt` before committing
- Ensure `make lint` passes
- Add tests for new functionality
- Update documentation as needed

## License

MIT License

Copyright (c) 2024

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.

## Credits

- Based on [Xkeen-UI](https://github.com/umarcheh001/Xkeen-UI)
- Built with [Go](https://golang.org/)
- HTTP routing with [Gorilla Mux](https://github.com/gorilla/mux)
- WebSocket support with [Gorilla WebSocket](https://github.com/gorilla/websocket)
- Password hashing with [bcrypt](https://pkg.go.dev/golang.org/x/crypto/bcrypt)
