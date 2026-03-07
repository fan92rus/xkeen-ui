# Frontend Service Layer Refactoring Design

Date: 2026-03-07
Status: Approved

## Goal

Improve frontend maintainability through clean architecture and separation of concerns.

## Approach

Evolutionary refactoring - sequential improvement without changing the tech stack (Alpine.js, no build step).

## Architecture

### Service Layer Pattern

```
web/static/js/
├── services/           # API calls, business logic
│   ├── api.js          # Base fetch wrapper (CSRF, error handling)
│   ├── config.js       # Config file operations
│   ├── xkeen.js        # XKeen service commands
│   └── logs.js         # Logs & WebSocket
├── components/         # Alpine components (UI only)
│   ├── editor.js
│   ├── logs.js
│   ├── service.js
│   └── commands.js
├── store.js            # Alpine.store - state + computed only
└── app.js              # Initialization, keyboard shortcuts
```

### Layer Responsibilities

| Layer | Responsibility |
|-------|---------------|
| `api.js` | HTTP client, CSRF token, error handling, streaming |
| `services/*.js` | Domain-specific API calls |
| `store.js` | Reactive state, computed properties, action dispatch |
| `components/*.js` | UI logic, DOM interactions, Alpine.data |

### Data Flow

```
User Action → Component → Store Action → Service → API → Server
                    ↓
              State Update ← Store ← Service Response
```

## Implementation Details

### api.js - Base HTTP Client

- `request(path, options)` - base fetch with CSRF
- `get(path)` - GET request, returns JSON
- `post(path, data)` - POST request, returns JSON
- `postStream(path, data, onMessage)` - NDJSON streaming
- `ApiError` class with status and data

### services/config.js

- `listFiles()` - GET /api/config/files
- `getFile(path)` - GET /api/config/file?path=
- `saveFile(path, content)` - POST /api/config/file

### services/xkeen.js

- `getStatus()` - GET /api/xkeen/status
- `start()` - POST /api/xkeen/start
- `stop()` - POST /api/xkeen/stop
- `restart()` - POST /api/xkeen/restart
- `getSettings()` - GET /api/xray/settings
- `setLogLevel(level)` - POST /api/xray/settings/log-level

### services/logs.js

- `fetchLogs(path, lines)` - GET /api/logs/xray
- `createLogStream(onMessage, onError)` - WebSocket factory

### store.js Changes

- Import services instead of inline fetch
- Actions delegate to services
- Remove duplicated CSRF code
- Keep reactive state management

### components/ Changes

- Extract from app.js to separate files
- Import services/api where needed
- Thin components - UI logic only

### index.html Changes

- Add `type="module"` to script tags
- Load order: store → components → Alpine

## File Size Estimates

| File | Lines |
|------|-------|
| services/api.js | ~60 |
| services/config.js | ~20 |
| services/xkeen.js | ~35 |
| services/logs.js | ~25 |
| components/editor.js | ~60 |
| components/logs.js | ~40 |
| components/service.js | ~25 |
| components/commands.js | ~100 |
| store.js | ~180 |
| app.js | ~10 |
| **Total** | ~555 |

## Benefits

1. **Testability** - Services can be unit tested in isolation
2. **Maintainability** - Clear separation of concerns
3. **Reusability** - API wrapper used across all services
4. **Consistency** - Single error handling pattern
5. **Readability** - Smaller, focused files

## Risks

- Module loading order must be correct (store before components before Alpine)
- No build step means no tree-shaking (acceptable for this size)
