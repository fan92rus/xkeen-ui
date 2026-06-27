# XKEEN-UI

A web interface for managing network service configurations on Keenetic routers with Entware.

> 🇷🇺 [Русская версия](README.md)

<img width="1918" height="886" alt="image" src="https://github.com/user-attachments/assets/26d13089-0a58-4df8-853b-de1aa68168a9" />

## Quick Install

Run a single command on your router:

```bash
curl -Ls https://raw.githubusercontent.com/fan92rus/xkeen-ui/master/xkeen-go/scripts/setup.sh | sh
```

Or via wget:

```bash
wget -qO- https://raw.githubusercontent.com/fan92rus/xkeen-ui/master/xkeen-go/scripts/setup.sh | sh
```

Open the web interface: `http://<router-ip>:8089`

**Default password:** `admin`

> ⚠️ **Important:** The system will prompt you to change the password on first login.

## Requirements

- Keenetic router with [Entware](https://github.com/Entware/Entware/wiki) installed
- XKeen installed
- Architecture: ARM64 (KN-1010, KN-1810, KN-1910, etc.) or MIPS

## Features

### Config Editor

- JSON/YAML syntax highlighting with One Dark theme
- JSONC (JSON with Comments) support
- Automatic backups on save
- Diff view between versions
- Restore from backups

### Service Management

- Start, stop, restart
- Real-time status monitoring
- Switch between Xray and Mihomo kernels

### Log Viewer

- Real-time log streaming via WebSocket
- Filter by level (error, warn, info)
- Search log contents
- Toggle between access.log and error.log

### XKeen Command Console

- Interactive command execution with real-time output
- Input support for interactive commands
- Commands grouped by category
- Warnings for destructive operations
- Command list cached for 5 hours with manual refresh

### AmneziaWG (AWG) Management

- One-click AWG installation (amneziawg-go + amneziawg-tools)
- AWG interface management: start, stop, add, delete configurations
- **Client mode** — tunnel with fwmark routing for Xray/Mihomo integration
- **Server mode** — full-tunnel VPN server with built-in iptables firewall
- Peer management: add, delete, generate client configurations
- QR code for mobile client configs (AmneziaWG app)
- Obfuscation presets: Random, Full, Light, Minimal, Plain WG
- Peer changes applied live via `awg syncconf` — no service interruption
- Built-in watchdog: cron health checks every minute, automatic iptables restoration
- Persistent client config storage in `/opt/etc/awg/clients/`

### Subscriptions

- Subscription-based proxy management
- Built-in AWG subscription (ID `__awg__`) — automatic proxy generation from AWG interfaces
- Filter system for selective proxy usage
- Mihomo config generation from subscriptions with proxy-group strategy mapping
- Optional Xray 05_routing.json to Mihomo rules conversion

### Settings

- Switch operation mode (Xray/Mihomo)
- Change logging level
- Manage admin password
- One-click update check and install via GitHub releases with SSE progress
- AWG install and uninstall
- Conditional tabs: AWG shown only when installed, Metrics shown only when enabled

## Service Management Commands

```bash
xkeen-ui start      # Start
xkeen-ui stop       # Stop
xkeen-ui restart    # Restart
xkeen-ui status     # Status
xkeen-ui log        # Logs
xkeen-ui uninstall  # Uninstall
```

## Configuration

Configuration file: `/opt/etc/xkeen-ui/config.json`

```json
{
    "port": 8089,
    "mode": "xray",
    "xray_config_dir": "/opt/etc/xray/configs",
    "xkeen_binary": "xkeen",
    "mihomo_config_dir": "/opt/etc/mihomo",
    "mihomo_binary": "mihomo",
    "awg_config_dir": "/opt/etc/awg",
    "allowed_roots": [
        "/opt/etc/xray",
        "/opt/etc/xkeen",
        "/opt/etc/mihomo",
        "/opt/etc/awg",
        "/opt/var/log"
    ],
    "log_level": "info",
    "metrics_port": 0,
    "cookie_secure": false,
    "trust_proxy_headers": false,
    "auth": {
        "session_timeout": 24,
        "max_login_attempts": 5,
        "lockout_duration": 5
    }
}
```

### Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `port` | Web interface port | 8089 |
| `mode` | Operation mode: `xray` or `mihomo` | xray |
| `xray_config_dir` | Xray config directory | /opt/etc/xray/configs |
| `xkeen_binary` | Path to xkeen binary | xkeen |
| `mihomo_config_dir` | Mihomo config directory | /opt/etc/mihomo |
| `mihomo_binary` | Path to mihomo binary | mihomo |
| `awg_config_dir` | AWG config directory | /opt/etc/awg |
| `awg_lan_iface` | LAN interface for firewall (empty = auto) | — |
| `awg_wan_iface` | WAN interface for firewall (empty = auto) | — |
| `awg_endpoint` | Endpoint override for clients (empty = auto) | — |
| `allowed_roots` | Allowed directories for file operations | xray, xkeen, mihomo, awg, log |
| `log_level` | Logging level (debug, info, warn, error) | info |
| `metrics_port` | Xray metrics port (0 = disabled) | 0 |
| `cookie_secure` | Set Secure flag on cookies (enable behind HTTPS) | false |
| `trust_proxy_headers` | Trust X-Forwarded-For headers (behind reverse proxy) | false |
| `session_timeout` | Session lifetime in hours | 24 |
| `max_login_attempts` | Login attempts before lockout | 5 |
| `lockout_duration` | Lockout duration in minutes | 5 |

## Security

- **bcrypt** — password hashing with cost 12
- **CSRF** — cross-site request forgery protection
- **Rate limiting** — login attempt throttling by IP
- **Path validation** — path traversal protection via directory whitelist
- **Security headers** — X-Frame-Options, CSP, X-Content-Type-Options

## Uninstall

```bash
xkeen-ui uninstall
```

You will be prompted to keep or remove the configuration directory.

## Build from Source

```bash
git clone https://github.com/fan92rus/xkeen-ui.git
cd xkeen-ui/xkeen-go

make deps                  # Install dependencies
make build                 # Build for current OS
make keenetic-arm64        # Build for Keenetic ARM64

# Optional: UPX compression
upx --best --lzma build/xkeen-ui-keenetic-arm64
```

## Tech Stack

- **Backend:** Go
- **Frontend:** Vue 3, Pinia, CodeMirror 6, Chart.js
- **Protocols:** HTTP, WebSocket, SSE
- **Testing:** Vitest (frontend), Go testing (backend)

## License

MIT License

## Author

[fan92rus](https://github.com/fan92rus)

<img width="1918" height="886" alt="image" src="https://github.com/user-attachments/assets/e97ec103-7231-4b19-a41b-532ea0ee5093" />
<img width="1909" height="882" alt="image" src="https://github.com/user-attachments/assets/4cad8dbc-957d-403e-834e-6cd86424bbc5" />
<img width="1914" height="814" alt="image" src="https://github.com/user-attachments/assets/741b42f6-3b1d-4ff6-af40-e110c31bd14e" />
<img width="1910" height="880" alt="image" src="https://github.com/user-attachments/assets/3c7e25ee-eda8-42bf-927c-bba9c07895d4" />
<img width="1917" height="865" alt="image" src="https://github.com/user-attachments/assets/711231f2-25e1-4b34-b026-36a815c99901" />
