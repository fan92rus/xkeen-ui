# Log Rotation for Xray Logs

**Status:** TODO
**Priority:** Medium
**Created:** 2026-03-05

## Problem

Xray writes logs to `/opt/var/log/xray/access.log` and `/opt/var/log/xray/error.log` without any size limit. On embedded routers (Keenetic) with limited storage, log files can grow indefinitely and fill up the disk.

## Requirements

### Core Features
- [ ] Maximum log file size limit (configurable, default 3 MB)
- [ ] Keep N rotated files (configurable, default 3)
- [ ] Automatic rotation when size limit is reached
- [ ] Manual "Clear logs" button in UI

### Configuration
Add to xkeen-go config:
```json
{
  "log_rotation": {
    "enabled": true,
    "max_size_mb": 3,
    "max_files": 3,
    "compress": false
  }
}
```

### UI Changes
- Add "Clear logs" button in Logs tab
- Add rotation settings in Settings tab
- Show current log file sizes

### Backend Changes
- Background goroutine to check log sizes periodically
- Rotation function: rename `access.log` -> `access.log.1`, delete old files
- API endpoint: `POST /api/logs/clear`
- API endpoint: `GET /api/logs/status` (returns file sizes)

## Implementation Notes

### Rotation Strategy
```
access.log -> access.log.1 -> access.log.2 -> access.log.3 (delete)
error.log  -> error.log.1  -> error.log.2  -> error.log.3  (delete)
```

### Considerations
- Use `gzip` compression for rotated files (optional, saves space but uses CPU)
- Handle Xray still writing to file during rotation (copy-truncate or signal-based)
- Check interval: every 5 minutes in background goroutine
- Thread-safe file operations

## Related Files
- `internal/handlers/logs.go` - add rotation logic
- `internal/config/config.go` - add rotation config
- `web/index.html` - add UI controls
- `web/static/js/store.js` - add clear/status methods

## References
- logrotate(8) - standard Linux log rotation
- lumberjack - Go log rotation library
