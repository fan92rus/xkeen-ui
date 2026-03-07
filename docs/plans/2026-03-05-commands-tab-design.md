# Design: XKeen Commands Tab

**Date:** 2026-03-05
**Status:** Approved

## Overview

Add a new "Commands" tab to XKEEN-GO web UI for executing XKeen CLI commands without additional arguments.

## Selected Commands

Only commands that don't require additional arguments:

### Proxy Client Management
| Flag | Description | Dangerous |
|------|-------------|-----------|
| `-start` | Start proxy client | No |
| `-stop` | Stop proxy client | No |
| `-restart` | Restart proxy client | No |
| `-status` | Show proxy client status | No |

### XKeen Backup
| Flag | Description | Dangerous |
|------|-------------|-----------|
| `-kb` | Create backup | No |
| `-kbr` | Restore from backup | Yes (overwrites current) |

### Updates
| Flag | Description | Dangerous |
|------|-------------|-----------|
| `-uk` | Update XKeen | No |
| `-ug` | Update GeoFile | No |
| `-ux` | Update Xray | No |
| `-um` | Update Mihomo | No |

## Architecture

### Backend (Go)

**New file:** `internal/handlers/commands.go`

```go
type CommandsHandler struct {
    allowedCommands map[string]CommandConfig
    executor        CommandExecutor
}

type CommandConfig struct {
    Cmd         string
    Description string
    Dangerous   bool
    Timeout     time.Duration
}
```

**Endpoint:** `POST /api/xkeen/command`
- Request: `{"command": "status"}`
- Response: `{"success": true, "output": "...", "command": "status"}`

**Timeouts:**
- Status/Info commands: 10s
- Start/Stop/Restart: 30s
- Backup/Update: 60s

### Frontend (Alpine.js)

**New files:**
- `web/static/js/components/commands.js` - Alpine component
- Update `web/index.html` - Add Commands tab

**UI Components:**
1. Collapsible category groups
2. Buttons for each command
3. Modal dialog for command output (with copy button)
4. Confirmation dialog for dangerous commands

## Security

- All commands go through whitelist
- CSRF token required
- Dangerous commands require confirmation in UI
- No shell execution (direct exec.CommandContext)
- Proper timeout handling

## UI Layout

```
[Editor] [Logs] [Settings] [Commands]

▸ Управление прокси-клиентом
  [Start] [Stop] [Restart] [Status]

▸ Резервная копия XKeen
  [Создать бэкап] [Восстановить]

▸ Обновление компонентов
  [XKeen] [GeoFile] [Xray] [Mihomo]
```

## Future Extensions

- Add more command categories as needed
- Command history
- Real-time output streaming for long operations
