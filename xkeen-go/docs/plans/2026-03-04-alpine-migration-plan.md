# Alpine.js Migration Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Migrate XKEEN-GO frontend from imperative JavaScript to Alpine.js with reactive state management.

**Architecture:** Alpine store as single source of truth, components encapsulate CodeMirror/WebSocket/polling, HTML uses Alpine directives for declarative UI.

**Tech Stack:** Alpine.js 3.x (CDN), CodeMirror 6 (esm.sh), ES modules

---

## Phase 1: Setup

### Task 1.1: Add Alpine.js CDN and x-cloak CSS

**Files:**
- Modify: `web/index.html:7-8` (after style.css link)
- Modify: `web/static/css/style.css` (append)

**Step 1: Add Alpine.js script to index.html**

Insert after the style.css link:

```html
<script defer src="https://cdn.jsdelivr.net/npm/alpinejs@3.x.x/dist/cdn.min.js"></script>
```

**Step 2: Add x-cloak CSS to style.css**

Append to end of file:

```css
/* Alpine.js cloaking */
[x-cloak] {
    display: none !important;
}

/* Service status indicator */
.service-indicator {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-right: 16px;
}

.status-running {
    color: var(--success);
}

.status-stopped {
    color: var(--danger);
}

.status-unknown {
    color: var(--text-secondary);
}

/* Logs filters */
.logs-filters {
    display: flex;
    gap: 10px;
    align-items: center;
}

.logs-filters input[type="text"] {
    padding: 8px 12px;
    background: var(--bg-tertiary);
    border: 1px solid var(--border);
    border-radius: 4px;
    color: var(--text-primary);
    flex: 1;
    max-width: 200px;
}

.logs-filters select {
    padding: 8px 12px;
    background: var(--bg-tertiary);
    border: 1px solid var(--border);
    border-radius: 4px;
    color: var(--text-primary);
}
```

**Step 3: Verify page loads**

Run: Open `web/index.html` in browser or run `make run`
Expected: Page loads without errors, Alpine available globally

**Step 4: Commit**

```bash
git add web/index.html web/static/css/style.css
git commit -m "feat(web): add Alpine.js CDN and x-cloak CSS

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 1.2: Create file structure

**Files:**
- Create: `web/static/js/store.js`
- Create: `web/static/js/utils.js`
- Create: `web/static/js/components/editor.js`
- Create: `web/static/js/components/logs.js`
- Create: `web/static/js/components/service.js`

**Step 1: Create store.js**

```javascript
// store.js - Global Alpine store for application state

document.addEventListener('alpine:init', () => {
    Alpine.store('app', {
        // UI state
        activeTab: 'editor',
        toast: { message: '', type: '', show: false },
        loading: false,

        // Data
        files: [],
        currentFile: null,
        logs: [],
        serviceStatus: 'unknown',
        settings: {},

        // CSRF token from cookie
        get csrfToken() {
            return document.cookie.match(/csrf_token=([^;]+)/)?.[1] || ''
        },

        // Actions
        async loadFiles() {
            try {
                const res = await fetch('/api/config/files', {
                    headers: { 'X-CSRF-Token': this.csrfToken }
                })
                const data = await res.json()
                this.files = data.files || []
            } catch (err) {
                this.showToast('Failed to load files', 'error')
            }
        },

        async loadFile(path) {
            try {
                const res = await fetch(`/api/config/file?path=${encodeURIComponent(path)}`, {
                    headers: { 'X-CSRF-Token': this.csrfToken }
                })
                const data = await res.json()
                this.currentFile = {
                    path: data.path,
                    content: data.content,
                    valid: data.valid
                }
            } catch (err) {
                this.showToast('Failed to load file', 'error')
            }
        },

        async saveFile(content) {
            if (!this.currentFile) {
                this.showToast('No file selected', 'error')
                return false
            }

            try {
                const res = await fetch('/api/config/file', {
                    method: 'POST',
                    headers: {
                        'Content-Type': 'application/json',
                        'X-CSRF-Token': this.csrfToken
                    },
                    body: JSON.stringify({
                        path: this.currentFile.path,
                        content: content
                    })
                })

                if (res.ok) {
                    this.showToast('Saved successfully', 'success')
                    return true
                } else {
                    const data = await res.json()
                    this.showToast(data.error || 'Save failed', 'error')
                    return false
                }
            } catch (err) {
                this.showToast('Save failed', 'error')
                return false
            }
        },

        async restartService() {
            try {
                const res = await fetch('/api/xkeen/restart', {
                    method: 'POST',
                    headers: { 'X-CSRF-Token': this.csrfToken }
                })

                if (res.ok) {
                    this.showToast('Xkeen restarting...', 'success')
                } else {
                    this.showToast('Restart failed', 'error')
                }
            } catch (err) {
                this.showToast('Restart failed', 'error')
            }
        },

        async logout() {
            await fetch('/api/auth/logout', { method: 'POST' })
            window.location.href = '/login'
        },

        showToast(message, type = '') {
            this.toast = { message, type, show: true }
            setTimeout(() => {
                this.toast.show = false
            }, 3000)
        },

        init() {
            this.loadFiles()
        }
    })
})
```

**Step 2: Create utils.js**

```javascript
// utils.js - Helper functions

/**
 * Escape HTML special characters to prevent XSS
 */
export function escapeHtml(text) {
    const div = document.createElement('div')
    div.textContent = text
    return div.innerHTML
}

/**
 * Fetch wrapper with CSRF token and error handling
 */
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
            const data = await res.json().catch(() => ({}))
            throw new Error(data.error || `HTTP ${res.status}`)
        }

        return await res.json()
    } catch (err) {
        console.error('API Error:', err)
        throw err
    }
}
```

**Step 3: Create components/editor.js**

```javascript
// components/editor.js - CodeMirror editor component

import { EditorView, basicSetup } from 'codemirror'
import { json } from '@codemirror/lang-json'
import { oneDark } from '@codemirror/theme-one-dark'

export function editorComponent() {
    return {
        editorInstance: null,
        isValid: true,

        init() {
            this.editorInstance = new EditorView({
                doc: '// Select a file to edit',
                extensions: [
                    basicSetup,
                    json(),
                    oneDark,
                    EditorView.lineWrapping,
                    EditorView.theme({
                        '&': { height: '100%' },
                        '.cm-scroller': { overflow: 'auto' }
                    })
                ],
                parent: this.$refs.editor
            })

            // Watch for file changes from store
            this.$watch('$store.app.currentFile', (file) => {
                if (file) {
                    this.loadContent(file)
                }
            })
        },

        loadContent(file) {
            this.editorInstance.dispatch({
                changes: {
                    from: 0,
                    to: this.editorInstance.state.doc.length,
                    insert: file.content
                }
            })
            this.isValid = file.valid
        },

        getContent() {
            return this.editorInstance.state.doc.toString()
        },

        async save() {
            const content = this.getContent()
            await this.$store.app.saveFile(content)
        }
    }
}
```

**Step 4: Create components/logs.js**

```javascript
// components/logs.js - Logs viewer with WebSocket streaming

export function logsComponent() {
    return {
        ws: null,
        filter: 'all',
        search: '',
        selectedFile: '/opt/var/log/xray/access.log',

        get filteredLogs() {
            let logs = this.$store.app.logs

            if (this.filter !== 'all') {
                logs = logs.filter(log => log.level === this.filter)
            }

            if (this.search) {
                const term = this.search.toLowerCase()
                logs = logs.filter(log =>
                    log.message.toLowerCase().includes(term)
                )
            }

            return logs
        },

        init() {
            // Connect when tab becomes active
            this.$watch('$store.app.activeTab', (tab) => {
                if (tab === 'logs') {
                    this.loadLogs()
                    this.connectWebSocket()
                } else {
                    this.disconnectWebSocket()
                }
            })
        },

        async loadLogs() {
            try {
                const res = await fetch(
                    `/api/logs/xray?path=${encodeURIComponent(this.selectedFile)}&lines=100`,
                    { headers: { 'X-CSRF-Token': this.$store.app.csrfToken } }
                )
                const data = await res.json()
                this.$store.app.logs = data.entries || []
            } catch (err) {
                this.$store.app.showToast('Failed to load logs', 'error')
            }
        },

        connectWebSocket() {
            if (this.ws && this.ws.readyState === WebSocket.OPEN) {
                return
            }

            const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
            const wsUrl = `${protocol}//${window.location.host}/ws/logs`

            this.ws = new WebSocket(wsUrl)

            this.ws.onmessage = (event) => {
                const data = JSON.parse(event.data)
                if (data.type === 'ping') return

                this.$store.app.logs.push(data)

                // Limit to 500 entries
                if (this.$store.app.logs.length > 500) {
                    this.$store.app.logs = this.$store.app.logs.slice(-500)
                }
            }

            this.ws.onerror = () => {
                this.$store.app.showToast('Log stream error', 'error')
            }

            this.ws.onclose = () => {
                // Reconnect after 5 seconds if still on logs tab
                if (this.$store.app.activeTab === 'logs') {
                    setTimeout(() => this.connectWebSocket(), 5000)
                }
            }
        },

        disconnectWebSocket() {
            if (this.ws) {
                this.ws.close()
                this.ws = null
            }
        },

        clearLogs() {
            this.$store.app.logs = []
        }
    }
}
```

**Step 5: Create components/service.js**

```javascript
// components/service.js - Service status polling and control

export function serviceComponent() {
    return {
        pollingInterval: null,

        init() {
            this.fetchStatus()
            this.startPolling()
        },

        destroy() {
            this.stopPolling()
        },

        startPolling() {
            this.pollingInterval = setInterval(() => {
                this.fetchStatus()
            }, 5000)
        },

        stopPolling() {
            if (this.pollingInterval) {
                clearInterval(this.pollingInterval)
                this.pollingInterval = null
            }
        },

        async fetchStatus() {
            try {
                const res = await fetch('/api/xkeen/status')
                const data = await res.json()

                if (data.status && data.status.running !== undefined) {
                    this.$store.app.serviceStatus = data.status.running ? 'running' : 'stopped'
                } else {
                    this.$store.app.serviceStatus = 'unknown'
                }
            } catch (err) {
                this.$store.app.serviceStatus = 'unknown'
            }
        },

        async start() {
            try {
                const res = await fetch('/api/xkeen/start', {
                    method: 'POST',
                    headers: { 'X-CSRF-Token': this.$store.app.csrfToken }
                })
                if (res.ok) {
                    this.$store.app.showToast('Service started', 'success')
                    this.fetchStatus()
                }
            } catch (err) {
                this.$store.app.showToast('Failed to start service', 'error')
            }
        },

        async stop() {
            try {
                const res = await fetch('/api/xkeen/stop', {
                    method: 'POST',
                    headers: { 'X-CSRF-Token': this.$store.app.csrfToken }
                })
                if (res.ok) {
                    this.$store.app.showToast('Service stopped', 'success')
                    this.fetchStatus()
                }
            } catch (err) {
                this.$store.app.showToast('Failed to stop service', 'error')
            }
        },

        async restart() {
            try {
                const res = await fetch('/api/xkeen/restart', {
                    method: 'POST',
                    headers: { 'X-CSRF-Token': this.$store.app.csrfToken }
                })
                if (res.ok) {
                    this.$store.app.showToast('Service restarting...', 'success')
                    setTimeout(() => this.fetchStatus(), 2000)
                }
            } catch (err) {
                this.$store.app.showToast('Failed to restart service', 'error')
            }
        }
    }
}
```

**Step 6: Verify files created**

Run: `ls -la web/static/js/`
Expected: store.js, utils.js, components/ directory with 3 files

**Step 7: Commit**

```bash
git add web/static/js/
git commit -m "feat(web): add Alpine store and component structure

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 1.3: Update app.js entry point

**Files:**
- Modify: `web/static/js/app.js` (rewrite)

**Step 1: Rewrite app.js**

```javascript
// app.js - Main entry point for Alpine.js application

import { editorComponent } from './components/editor.js'
import { logsComponent } from './components/logs.js'
import { serviceComponent } from './components/service.js'

// Register Alpine components after Alpine loads
document.addEventListener('alpine:init', () => {
    Alpine.data('editor', editorComponent)
    Alpine.data('logs', logsComponent)
    Alpine.data('service', serviceComponent)
})

// Keyboard shortcuts
document.addEventListener('keydown', (e) => {
    if ((e.ctrlKey || e.metaKey) && e.key === 's') {
        e.preventDefault()
        // Dispatch custom event that editor component can listen to
        window.dispatchEvent(new CustomEvent('editor:save'))
    }
})
```

**Step 2: Commit**

```bash
git add web/static/js/app.js
git commit -m "refactor(web): rewrite app.js as Alpine entry point

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Phase 2: Migrate HTML to Alpine

### Task 2.1: Update index.html with Alpine directives

**Files:**
- Modify: `web/index.html` (rewrite body)

**Step 1: Rewrite index.html body**

```html
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>XKEEN-GO Config Editor</title>
    <link rel="stylesheet" href="/static/css/style.css">
    <script defer src="https://cdn.jsdelivr.net/npm/alpinejs@3.x.x/dist/cdn.min.js"></script>
</head>
<body x-data x-init="$store.app.init()">
    <div class="app">
        <!-- Header -->
        <header class="header">
            <h1>XKEEN Config Editor</h1>
            <div class="actions">
                <!-- Service Status Indicator -->
                <div x-data="service" class="service-indicator">
                    <span :class="'status-' + $store.app.serviceStatus"
                          x-text="$store.app.serviceStatus"></span>
                </div>
                <button @click="$store.app.saveFile($dispatch('editor:get-content'))"
                        class="btn btn-primary">Save</button>
                <button @click="$store.app.restartService()"
                        class="btn btn-danger">Restart Xkeen</button>
                <button @click="$store.app.logout()"
                        class="btn">Logout</button>
            </div>
        </header>

        <!-- Tabs -->
        <nav class="tabs">
            <button class="tab"
                    :class="{ 'active': $store.app.activeTab === 'editor' }"
                    @click="$store.app.activeTab = 'editor'">Editor</button>
            <button class="tab"
                    :class="{ 'active': $store.app.activeTab === 'logs' }"
                    @click="$store.app.activeTab = 'logs'">Logs</button>
        </nav>

        <!-- Main Content -->
        <main class="main">
            <!-- Editor Tab -->
            <section x-show="$store.app.activeTab === 'editor'"
                     x-data="editor"
                     x-cloak
                     class="tab-content"
                     :class="{ 'active': $store.app.activeTab === 'editor' }">
                <aside class="sidebar">
                    <h2>Config Files</h2>
                    <ul class="file-list">
                        <template x-for="file in $store.app.files" :key="file.path">
                            <li :class="{ 'active': $store.app.currentFile?.path === file.path }"
                                @click="$store.app.loadFile(file.path)"
                                x-text="file.name"></li>
                        </template>
                    </ul>
                </aside>

                <section class="editor-container">
                    <div class="editor-header">
                        <span x-text="$store.app.currentFile?.path || 'No file selected'"></span>
                        <span :class="isValid ? '' : 'error'"
                              x-text="isValid ? 'Valid JSON' : 'Invalid JSON'"></span>
                    </div>
                    <div x-ref="editor" id="editor"></div>
                </section>
            </section>

            <!-- Logs Tab -->
            <section x-show="$store.app.activeTab === 'logs'"
                     x-data="logs"
                     x-cloak
                     class="tab-content"
                     :class="{ 'active': $store.app.activeTab === 'logs' }"
                     id="logsTab">
                <div class="logs-header">
                    <select x-model="selectedFile" @change="loadLogs()">
                        <option value="/opt/var/log/xray/access.log">Access Log</option>
                        <option value="/opt/var/log/xray/error.log">Error Log</option>
                    </select>

                    <div class="logs-filters">
                        <input type="text"
                               x-model="search"
                               placeholder="Search logs...">
                        <select x-model="filter">
                            <option value="all">All levels</option>
                            <option value="error">Errors</option>
                            <option value="warn">Warnings</option>
                            <option value="info">Info</option>
                        </select>
                    </div>

                    <button @click="clearLogs()" class="btn">Clear</button>
                    <button @click="loadLogs()" class="btn">Refresh</button>
                </div>

                <div class="logs-container">
                    <template x-for="(log, index) in filteredLogs" :key="index">
                        <div :class="'log-entry log-' + log.level">
                            <span class="log-time" x-text="log.timestamp"></span>
                            <span class="log-level" x-text="log.level.toUpperCase()"></span>
                            <span class="log-message" x-text="log.message"></span>
                        </div>
                    </template>
                </div>
            </section>
        </main>

        <!-- Toast -->
        <div x-show="$store.app.toast.show"
             x-transition
             :class="'toast ' + ($store.app.toast.type || '')"
             x-text="$store.app.toast.message"></div>
    </div>

    <!-- CodeMirror importmap -->
    <script type="importmap">
    {
        "imports": {
            "codemirror": "https://esm.sh/codemirror@6.0.1",
            "@codemirror/lang-json": "https://esm.sh/@codemirror/lang-json@6.0.1",
            "@codemirror/theme-one-dark": "https://esm.sh/@codemirror/theme-one-dark@6.1.2"
        }
    }
    </script>

    <!-- Load store first, then app -->
    <script src="/static/js/store.js"></script>
    <script type="module" src="/static/js/app.js"></script>
</body>
</html>
```

**Step 2: Test the application**

Run: `make run` then open http://localhost:8089
Expected:
- Page loads without JS errors
- Tabs switch correctly
- File list populates
- Clicking file loads into editor
- Save button works
- Logs tab shows with filters
- Service status shows in header

**Step 3: Commit**

```bash
git add web/index.html
git commit -m "feat(web): migrate HTML to Alpine.js directives

- Tabs use x-show and :class binding
- File list uses x-for template
- Editor uses x-data component
- Logs use x-for with filtering
- Toast uses x-show and x-transition
- Service status indicator in header

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Phase 3: Fix Save functionality

### Task 3.1: Wire up save button with editor content

**Files:**
- Modify: `web/static/js/components/editor.js`
- Modify: `web/index.html` (save button)

**Step 1: Update editor.js to listen for save events**

Replace the `save()` method and add event listener in `init()`:

```javascript
init() {
    this.editorInstance = new EditorView({
        doc: '// Select a file to edit',
        extensions: [
            basicSetup,
            json(),
            oneDark,
            EditorView.lineWrapping,
            EditorView.theme({
                '&': { height: '100%' },
                '.cm-scroller': { overflow: 'auto' }
            })
        ],
        parent: this.$refs.editor
    })

    // Watch for file changes from store
    this.$watch('$store.app.currentFile', (file) => {
        if (file) {
            this.loadContent(file)
        }
    })

    // Listen for save keyboard shortcut
    window.addEventListener('editor:save', () => this.save())
},

// ... rest unchanged ...

async save() {
    const content = this.getContent()
    await this.$store.app.saveFile(content)
}
```

**Step 2: Update save button in index.html to dispatch event**

Change the save button to:

```html
<button @click="window.dispatchEvent(new CustomEvent('editor:save'))"
        class="btn btn-primary">Save</button>
```

**Step 3: Test save functionality**

Run: `make run`
Test:
1. Select a file
2. Edit content
3. Click Save button
4. Verify toast shows "Saved successfully"
5. Try Ctrl+S keyboard shortcut

**Step 4: Commit**

```bash
git add web/static/js/components/editor.js web/index.html
git commit -m "fix(web): wire up save button with editor content

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Phase 4: Add Service Control Buttons

### Task 4.1: Add Start/Stop buttons to header

**Files:**
- Modify: `web/index.html` (header actions)

**Step 1: Update header in index.html**

Replace the service indicator div with:

```html
<!-- Service Status & Controls -->
<div x-data="service" class="service-controls">
    <span :class="'status-' + $store.app.serviceStatus"
          x-text="$store.app.serviceStatus"></span>
    <button @click="start()"
            :disabled="$store.app.serviceStatus === 'running'"
            class="btn btn-sm">Start</button>
    <button @click="stop()"
            :disabled="$store.app.serviceStatus === 'stopped'"
            class="btn btn-sm">Stop</button>
</div>
```

**Step 2: Add CSS for service controls**

Append to `web/static/css/style.css`:

```css
/* Service controls */
.service-controls {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-right: 16px;
}

.btn-sm {
    padding: 4px 10px;
    font-size: 0.8rem;
}

.btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
}
```

**Step 3: Test service controls**

Run: `make run`
Test:
1. Service status shows in header
2. Start/Stop buttons disabled appropriately
3. Clicking buttons calls API

**Step 4: Commit**

```bash
git add web/index.html web/static/css/style.css
git commit -m "feat(web): add service start/stop controls to header

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Phase 5: Cleanup and Final Testing

### Task 5.1: Remove old code and verify

**Files:**
- None (verification only)

**Step 1: Verify all functionality**

Test checklist:
- [ ] Page loads without JS console errors
- [ ] Tab switching works (Editor/Logs)
- [ ] File list populates from API
- [ ] Clicking file loads content into editor
- [ ] CodeMirror syntax highlighting works
- [ ] Save button works
- [ ] Ctrl+S keyboard shortcut works
- [ ] Logs tab shows log entries
- [ ] WebSocket streams new logs
- [ ] Log filtering works (level + search)
- [ ] Service status shows in header
- [ ] Service polling updates status
- [ ] Start/Stop/Restart buttons work
- [ ] Toast notifications show
- [ ] Logout redirects to login

**Step 2: Final commit**

```bash
git add -A
git commit -m "feat(web): complete Alpine.js migration

Migrated from imperative XkeenEditor class to reactive Alpine.js:
- Alpine store for global state management
- Components: editor, logs, service
- Declarative HTML with x-* directives
- Added service status indicator and controls
- Added log filtering (level + search)

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Summary

| Phase | Description | Files Changed |
|-------|-------------|---------------|
| 1.1 | Add Alpine.js CDN + CSS | index.html, style.css |
| 1.2 | Create file structure | store.js, utils.js, components/*.js |
| 1.3 | Update app.js entry point | app.js |
| 2.1 | Migrate HTML to Alpine | index.html |
| 3.1 | Fix save functionality | editor.js, index.html |
| 4.1 | Add service controls | index.html, style.css |
| 5.1 | Final testing | - |

**Total: 8 files created/modified**
