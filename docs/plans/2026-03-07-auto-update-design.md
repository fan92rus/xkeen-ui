# Auto-Update Feature Design

## Overview

Function for automatic application updates from GitHub releases with a button in settings.

**Repository:** `https://github.com/fan92rus/xkeen-go-ui`
**Platform:** arm64 only

## Requirements

- Check latest release from GitHub
- Compare versions (current vs latest)
- Show update notification if new version available
- One-click update with progress display
- Automatic service restart after update

## Architecture

### 1. Version Package

New package `internal/version/version.go`:

```go
package version

var (
    Version   = "dev"
    BuildDate = "unknown"
    GitCommit = "unknown"
)

func SetVersion(v, bd, gc string) {
    Version = v
    BuildDate = bd
    GitCommit = gc
}

func GetVersion() string {
    return Version
}
```

`main.go` calls `version.SetVersion()` on startup to pass ldflags values.

### 2. Backend: UpdateHandler

**File:** `internal/handlers/update.go`

#### Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/update/check` | Check GitHub for latest release |
| POST | `/api/update/start` | Start update, returns SSE stream |

#### GET /api/update/check Response

```json
{
  "current_version": "v1.0.0",
  "latest_version": "v1.2.3",
  "update_available": true,
  "release_url": "https://github.com/fan92rus/xkeen-go-ui/releases/tag/v1.2.3",
  "release_notes": "Changelog..."
}
```

#### POST /api/update/start SSE Events

```
event: progress
data: {"percent": 10, "status": "downloading"}

event: progress
data: {"percent": 80, "status": "verifying"}

event: progress
data: {"percent": 90, "status": "replacing"}

event: progress
data: {"percent": 100, "status": "restarting"}

event: complete
data: {"success": true, "message": "Update complete, service restarting..."}

event: error
data: {"error": "Download failed: 404"}
```

### 3. Update Process

1. **Download:** `GET https://github.com/fan92rus/xkeen-go-ui/releases/latest/download/xkeen-go-keenetic-arm64`
2. **Save:** To temp file `/tmp/xkeen-go-keenetic-arm64.new`
3. **Set permissions:** `chmod 0755` on downloaded file
4. **Verify:** Check file is executable, valid size
5. **Replace:**
   - Stop service: `/opt/etc/init.d/xkeen-go stop`
   - Replace binary: `mv /tmp/xkeen-go-keenetic-arm64.new /opt/bin/xkeen-go-keenetic-arm64`
   - Start service: `/opt/etc/init.d/xkeen-go start`
6. **Result:** Send SSE `complete` or `error`

### 4. Frontend: Settings Tab UI

New section "Updates" in Settings tab:

- Display current version
- "Check for Updates" button
- Show "Update available" notification with latest version
- "Update Now" button (visible only when update available)
- Progress bar during update with status text

### 5. Store Changes (store.js)

New state:
```javascript
currentVersion: 'unknown',
updateInfo: {
    update_available: false,
    current_version: '',
    latest_version: '',
    release_notes: ''
},
updateChecking: false,
updating: false,
updateProgress: 0,
updateStatus: '',
```

New actions:
- `checkUpdate()` - call GET /api/update/check
- `startUpdate()` - call POST /api/update/start, listen to SSE

### 6. Security

- Endpoints protected with Auth + CSRF middleware
- Download only from whitelisted domain: `github.com/fan92rus/xkeen-go-ui`
- Content-Type and file size validation
- File permissions set to 0755 before replacement

## Files to Create/Modify

### New Files
- `internal/version/version.go` - version package
- `internal/handlers/update.go` - update handler
- `web/static/js/services/update.js` - update API service

### Modified Files
- `main.go` - import version package, call SetVersion()
- `internal/server/server.go` - register update routes
- `web/static/js/store.js` - add update state and actions
- `web/static/js/app.js` - init update state
- `web/index.html` - add Updates section in Settings tab
- `web/static/css/style.css` - update UI styles

## GitHub Release Format

- Binary name: `xkeen-go-keenetic-arm64`
- Tag format: `v1.2.3` (semantic versioning)
- Version injected via ldflags during build

## Error Handling

- Network errors during check/download
- Invalid response from GitHub API
- File system errors (permissions, disk space)
- Service stop/start failures
- All errors reported via SSE with descriptive messages
