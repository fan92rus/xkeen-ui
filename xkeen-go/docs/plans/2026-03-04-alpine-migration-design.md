# Alpine.js Migration Design

## Overview

Migration of XKEEN-GO frontend from imperative JavaScript to Alpine.js for improved code maintainability and future UI extensibility.

## Goals

1. Simplify codebase — reduce manual DOM manipulation
2. Enable future UI expansion (service status, settings, improved logs)
3. Maintain current functionality without breaking changes
4. Keep binary size increase minimal (Alpine.js ~15KB gzip via CDN)

## Current State

- ~300 lines of JS in `XkeenEditor` class
- CodeMirror 6 via CDN (esm.sh)
- Imperative DOM manipulation (`classList.toggle`, `innerHTML`, `createElement`)
- State scattered across class properties: `currentFile`, `logs`, `logWebSocket`, `csrfToken`

## Target Architecture

### File Structure

```
web/
├── index.html              # SPA with Alpine directives
├── login.html              # Unchanged
└── static/
    ├── css/style.css       # Unchanged
    └── js/
        ├── app.js          # Entry point, Alpine.init()
        ├── store.js        # Alpine.store() — global state
        ├── components/
        │   ├── editor.js   # x-data for CodeMirror
        │   ├── logs.js     # x-data for logs (WebSocket, filters)
        │   └── service.js  # x-data for service status
        └── utils.js        # apiFetch, toast, escapeHtml
```

### Alpine Store (Global State)

```javascript
Alpine.store('app', {
    // UI state
    activeTab: 'editor',
    toast: { message: '', type: '', show: false },
    loading: false,

    // Data
    files: [],
    currentFile: null,
    logs: [],
    serviceStatus: 'unknown',  // 'running' | 'stopped' | 'unknown'
    settings: {},

    // Actions
    async loadFiles() { ... },
    async loadFile(path) { ... },
    async saveFile() { ... },
    async restartService() { ... },
    showToast(message, type) { ... }
})
```

### Components

#### Editor Component

Encapsulates CodeMirror 6. Alpine manages file selection state, CodeMirror handles editing.

```javascript
Alpine.data('editor', () => ({
    editorInstance: null,
    isValid: true,

    init() {
        this.editorInstance = new EditorView({
            doc: '// Select a file to edit',
            extensions: [basicSetup, json(), oneDark, ...],
            parent: this.$refs.editor
        })

        this.$watch('$store.app.currentFile', (file) => {
            if (file) this.loadContent(file)
        })
    },

    loadContent(file) { ... },
    getContent() { ... }
}))
```

#### Logs Component

WebSocket streaming with client-side filtering.

```javascript
Alpine.data('logs', () => ({
    ws: null,
    filter: 'all',
    search: '',
    selectedFile: '/opt/var/log/xray/access.log',

    get filteredLogs() {
        return this.$store.app.logs
            .filter(log => this.filter === 'all' || log.level === this.filter)
            .filter(log => !this.search || log.message.includes(this.search))
    },

    init() { this.connectWebSocket() },
    connectWebSocket() { ... },
    disconnectWebSocket() { ... },
    clearLogs() { ... }
}))
```

#### Service Component

Polling-based status monitoring with control buttons.

```javascript
Alpine.data('service', () => ({
    pollingInterval: null,

    init() {
        this.fetchStatus()
        this.startPolling()
    },

    destroy() {
        clearInterval(this.pollingInterval)
    },

    async fetchStatus() { ... },
    async start() { ... },
    async stop() { ... },
    async restart() { ... }
}))
```

### Data Flow

```
┌─────────────────────────────────────────────────────────────┐
│                     Alpine Store (app)                       │
│  files[]  │  logs[]  │  serviceStatus  │  activeTab, toast  │
│  loadFile │ loadLogs │  fetchStatus    │  saveFile, restart │
└─────────────────────────────────────────────────────────────┘
       ▲            ▲            ▲
       │            │            │
       ▼            ▼            ▼
┌──────────┐  ┌──────────┐  ┌──────────┐
│ editor   │  │ logs     │  │ service  │
│ (CM6)    │  │ (WS)     │  │ (poll)   │
└──────────┘  └──────────┘  └──────────┘
```

- **Store** is the single source of truth
- **Components** watch store changes via `$watch`
- **Components** update store via method calls
- **UI** reacts automatically to store changes

### Error Handling

Centralized API helper with automatic toast notifications:

```javascript
export async function apiFetch(url, options = {}) {
    const csrf = document.cookie.match(/csrf_token=([^;]+)/)?.[1]

    try {
        const res = await fetch(url, {
            ...options,
            headers: {
                'Content-Type': 'application/json',
                'X-CSRF-Token': csrf,
                ...options.headers
            }
        })

        if (!res.ok) {
            const data = await res.json()
            throw new Error(data.error || 'Request failed')
        }

        return await res.json()
    } catch (err) {
        Alpine.store('app').showToast(err.message, 'error')
        throw err
    }
}
```

## Migration Plan

### Phase 1: Setup
- Add Alpine.js via CDN
- Create file structure (store.js, components/, utils.js)
- Add `[x-cloak] { display: none !important; }` CSS

### Phase 2: Store + Tabs
- Create `Alpine.store('app')` with base state
- Migrate tabs logic to `store.activeTab`
- Update HTML with Alpine directives

### Phase 3: File List + Editor
- Migrate `loadFiles`/`loadFile` to store
- Create editor component with CodeMirror
- Update HTML with `x-for` for file list

### Phase 4: Logs + WebSocket
- Migrate logs to `store.logs`
- Create logs component with WebSocket
- Add filters and search

### Phase 5: Service Status (new feature)
- Add `store.serviceStatus`
- Create service component with polling
- Add status UI in header

### Phase 6: Settings (new feature)
- Add `store.settings`
- Create settings component with form
- Add Settings tab

## Testing Checklist

After each phase:
- [ ] Page loads correctly
- [ ] Tab switching works
- [ ] File selection → editing → saving
- [ ] Log viewing
- [ ] Service restart

## Constraints

- Alpine.js via CDN (defer loading)
- CodeMirror 6 via importmap (ES modules)
- No old browser support required
- Size increase acceptable within tens of KB
