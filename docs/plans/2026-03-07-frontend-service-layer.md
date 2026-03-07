# Frontend Service Layer Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Refactor frontend to Service Layer pattern for improved maintainability.

**Architecture:** Extract API calls into services/, components into components/, keep store.js for state only. ES modules, no build step.

**Tech Stack:** Alpine.js 3.14, CodeMirror 6, ES modules, native fetch/WebSocket

---

## Task 1: Create services/api.js

**Files:**
- Create: `xkeen-go/web/static/js/services/api.js`

**Step 1: Create services directory and api.js**

```javascript
// services/api.js - Base HTTP client with CSRF and error handling

const API_BASE = '';

export async function request(path, options = {}) {
    const csrfToken = document.cookie.match(/csrf_token=([^;]+)/)?.[1] || '';

    const res = await fetch(API_BASE + path, {
        ...options,
        headers: {
            'Content-Type': 'application/json',
            'X-CSRF-Token': csrfToken,
            ...options.headers
        }
    });

    if (!res.ok) {
        const error = await res.json().catch(() => ({}));
        throw new ApiError(res.status, error.message || error.error || 'Request failed', error);
    }

    return res;
}

export async function get(path) {
    const res = await request(path);
    return res.json();
}

export async function post(path, data) {
    const res = await request(path, {
        method: 'POST',
        body: JSON.stringify(data)
    });
    return res.json();
}

export async function postStream(path, data, onMessage) {
    const res = await request(path, {
        method: 'POST',
        body: JSON.stringify(data)
    });

    const reader = res.body.getReader();
    const decoder = new TextDecoder();
    let buffer = '';

    try {
        while (true) {
            const { done, value } = await reader.read();
            if (done) break;

            buffer += decoder.decode(value, { stream: true });
            const lines = buffer.split('\n');
            buffer = lines.pop();

            for (const line of lines) {
                if (line.trim()) {
                    try {
                        onMessage(JSON.parse(line));
                    } catch (e) {
                        console.warn('Failed to parse stream line:', line);
                    }
                }
            }
        }

        if (buffer.trim()) {
            try {
                onMessage(JSON.parse(buffer));
            } catch (e) {
                console.warn('Failed to parse final buffer:', buffer);
            }
        }
    } finally {
        reader.releaseLock();
    }
}

export class ApiError extends Error {
    constructor(status, message, data) {
        super(message);
        this.name = 'ApiError';
        this.status = status;
        this.data = data;
    }
}
```

**Step 2: Verify file created**

Run: `ls -la xkeen-go/web/static/js/services/`
Expected: Directory with api.js

**Step 3: Commit**

```bash
git add xkeen-go/web/static/js/services/api.js
git commit -m "feat(web): add base API client with CSRF and streaming support"
```

---

## Task 2: Create services/config.js

**Files:**
- Create: `xkeen-go/web/static/js/services/config.js`

**Step 1: Create config service**

```javascript
// services/config.js - Config file operations

import { get, post } from './api.js';

export async function listFiles() {
    const data = await get('/api/config/files');
    return data.files || [];
}

export async function getFile(path) {
    return get(`/api/config/file?path=${encodeURIComponent(path)}`);
}

export async function saveFile(path, content) {
    return post('/api/config/file', { path, content });
}
```

**Step 2: Commit**

```bash
git add xkeen-go/web/static/js/services/config.js
git commit -m "feat(web): add config service for file operations"
```

---

## Task 3: Create services/xkeen.js

**Files:**
- Create: `xkeen-go/web/static/js/services/xkeen.js`

**Step 1: Create xkeen service**

```javascript
// services/xkeen.js - XKeen service control

import { get, post } from './api.js';

export async function getStatus() {
    const data = await get('/api/xkeen/status');
    if (data.status && data.status.running !== undefined) {
        return data.status.running ? 'running' : 'stopped';
    }
    return 'unknown';
}

export async function start() {
    return post('/api/xkeen/start', {});
}

export async function stop() {
    return post('/api/xkeen/stop', {});
}

export async function restart() {
    return post('/api/xkeen/restart', {});
}

export async function getSettings() {
    return get('/api/xray/settings');
}

export async function setLogLevel(level) {
    return post('/api/xray/settings/log-level', { log_level: level });
}
```

**Step 2: Commit**

```bash
git add xkeen-go/web/static/js/services/xkeen.js
git commit -m "feat(web): add xkeen service for service control"
```

---

## Task 4: Create services/logs.js

**Files:**
- Create: `xkeen-go/web/static/js/services/logs.js`

**Step 1: Create logs service**

```javascript
// services/logs.js - Logs fetching and WebSocket streaming

import { get } from './api.js';

export async function fetchLogs(path, lines = 100) {
    const data = await get(`/api/logs/xray?path=${encodeURIComponent(path)}&lines=${lines}`);
    return data.entries || [];
}

export function createLogStream(onMessage, onError) {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = `${protocol}//${window.location.host}/ws/logs`;

    const ws = new WebSocket(wsUrl);

    ws.onmessage = (event) => {
        try {
            const data = JSON.parse(event.data);
            if (data.type !== 'ping') {
                onMessage(data);
            }
        } catch (e) {
            console.warn('Failed to parse WebSocket message:', e);
        }
    };

    ws.onerror = () => {
        onError?.();
    };

    return {
        close: () => ws.close(),
        isOpen: () => ws.readyState === WebSocket.OPEN
    };
}
```

**Step 2: Commit**

```bash
git add xkeen-go/web/static/js/services/logs.js
git commit -m "feat(web): add logs service with WebSocket streaming"
```

---

## Task 5: Refactor store.js

**Files:**
- Modify: `xkeen-go/web/static/js/store.js`

**Step 1: Update store.js to use services**

Replace entire file with:

```javascript
// store.js - Global Alpine store for application state

import * as configService from './services/config.js';
import * as xkeenService from './services/xkeen.js';
import * as logsService from './services/logs.js';

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
        isValidJson: true,

        // Logs state
        logFilter: 'all',
        logSearch: '',
        logFile: '/opt/var/log/xray/access.log',

        // Xray settings
        xraySettings: {
            logLevel: 'none',
            logLevels: ['debug', 'info', 'warning', 'error', 'none'],
            accessLog: '',
            errorLog: ''
        },

        // Modal state
        modal: {
            show: false,
            command: '',
            output: '',
            error: ''
        },

        // Confirm dialog state
        confirm: {
            show: false,
            command: '',
            description: '',
            onConfirm: null
        },

        // Computed: filtered logs
        get filteredLogs() {
            let logs = this.logs;

            if (this.logFilter !== 'all') {
                logs = logs.filter(log => log.level === this.logFilter);
            }

            if (this.logSearch) {
                const term = this.logSearch.toLowerCase();
                logs = logs.filter(log =>
                    log.message.toLowerCase().includes(term)
                );
            }

            return logs;
        },

        // Toast
        showToast(message, type = '') {
            this.toast = { message, type, show: true };
            setTimeout(() => {
                this.toast.show = false;
            }, 3000);
        },

        // Config actions
        async loadFiles() {
            try {
                this.files = await configService.listFiles();
            } catch (err) {
                this.showToast('Failed to load files', 'error');
            }
        },

        async loadFile(path) {
            try {
                const data = await configService.getFile(path);
                if (data.path) {
                    this.currentFile = {
                        path: data.path,
                        content: data.content,
                        valid: data.valid
                    };
                    this.isValidJson = data.valid;
                }
            } catch (err) {
                this.showToast('Failed to load file', 'error');
            }
        },

        async saveFile(content) {
            if (!this.currentFile) {
                this.showToast('No file selected', 'error');
                return false;
            }

            try {
                await configService.saveFile(this.currentFile.path, content);
                this.showToast('Saved successfully', 'success');
                return true;
            } catch (err) {
                this.showToast(err.message || 'Save failed', 'error');
                return false;
            }
        },

        // XKeen actions
        async fetchServiceStatus() {
            try {
                this.serviceStatus = await xkeenService.getStatus();
            } catch (err) {
                this.serviceStatus = 'unknown';
            }
        },

        async startService() {
            try {
                await xkeenService.start();
                this.showToast('Service started', 'success');
                this.fetchServiceStatus();
            } catch (err) {
                this.showToast('Failed to start service', 'error');
            }
        },

        async stopService() {
            try {
                await xkeenService.stop();
                this.showToast('Service stopped', 'success');
                this.fetchServiceStatus();
            } catch (err) {
                this.showToast('Failed to stop service', 'error');
            }
        },

        async restartService() {
            try {
                await xkeenService.restart();
                this.showToast('Xkeen restarting...', 'success');
            } catch (err) {
                this.showToast('Restart failed', 'error');
            }
        },

        async loadXraySettings() {
            try {
                const data = await xkeenService.getSettings();
                if (data.log_level !== undefined) {
                    this.xraySettings.logLevel = data.log_level;
                    this.xraySettings.logLevels = data.log_levels || this.xraySettings.logLevels;
                    this.xraySettings.accessLog = data.access_log || '';
                    this.xraySettings.errorLog = data.error_log || '';
                }
            } catch (err) {
                this.showToast('Failed to load Xray settings', 'error');
            }
        },

        async updateLogLevel() {
            try {
                const result = await xkeenService.setLogLevel(this.xraySettings.logLevel);
                this.showToast(result.message || 'Log level updated', 'success');
            } catch (err) {
                this.showToast(err.message || 'Failed to update log level', 'error');
                this.loadXraySettings();
            }
        },

        // Logs actions
        async loadLogs() {
            try {
                this.logs = await logsService.fetchLogs(this.logFile, 100);
            } catch (err) {
                this.showToast('Failed to load logs', 'error');
            }
        },

        clearLogs() {
            this.logs = [];
        },

        // Auth
        async logout() {
            try {
                await fetch('/api/auth/logout', { method: 'POST' });
            } catch (err) {
                // Ignore logout errors
            }
            window.location.href = '/login';
        },

        // Modal actions
        closeModal() {
            this.modal.show = false;
            this.modal.output = '';
            this.modal.command = '';
            this.modal.error = '';
        },

        async copyModalOutput() {
            try {
                await navigator.clipboard.writeText(this.modal.output);
                this.showToast('Output copied to clipboard', 'success');
            } catch (err) {
                this.showToast('Failed to copy to clipboard', 'error');
            }
        },

        cancelConfirm() {
            this.confirm.show = false;
            this.confirm.command = '';
            this.confirm.description = '';
            this.confirm.onConfirm = null;
        },

        executeConfirm() {
            if (this.confirm.onConfirm) {
                this.confirm.show = false;
                this.confirm.onConfirm();
                this.confirm.onConfirm = null;
                this.confirm.command = '';
                this.confirm.description = '';
            }
        },

        // Init
        init() {
            this.loadFiles();
            this.loadXraySettings();
        }
    });
});
```

**Step 2: Commit**

```bash
git add xkeen-go/web/static/js/store.js
git commit -m "refactor(web): update store to use services layer"
```

---

## Task 6: Create components/editor.js

**Files:**
- Create: `xkeen-go/web/static/js/components/editor.js`

**Step 1: Create editor component**

```javascript
// components/editor.js - CodeMirror editor component

import { EditorView, basicSetup } from 'codemirror';
import { json } from '@codemirror/lang-json';
import { oneDark } from '@codemirror/theme-one-dark';

document.addEventListener('alpine:init', () => {
    Alpine.data('editor', function() {
        return {
            instance: null,
            ready: false,
            pendingFile: null,

            init() {
                this.initEditor();

                // Watch for file changes from store
                this.$watch('$store.app.currentFile', (file) => {
                    if (!file) return;
                    if (this.ready) {
                        this.loadContent(file);
                    } else {
                        this.pendingFile = file;
                    }
                });

                // Listen for save keyboard shortcut
                this._saveHandler = () => this.save();
                window.addEventListener('editor:save', this._saveHandler);
            },

            destroy() {
                if (this._saveHandler) {
                    window.removeEventListener('editor:save', this._saveHandler);
                }
                if (this.instance) {
                    this.instance.destroy();
                }
            },

            async initEditor() {
                this.instance = new EditorView({
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
                });
                this.ready = true;

                // Load pending file if any
                if (this.pendingFile) {
                    this.loadContent(this.pendingFile);
                    this.pendingFile = null;
                }
            },

            loadContent(file) {
                if (!this.instance) return;
                this.instance.dispatch({
                    changes: {
                        from: 0,
                        to: this.instance.state.doc.length,
                        insert: file.content
                    }
                });
                this.$store.app.isValidJson = file.valid;
            },

            getContent() {
                return this.instance ? this.instance.state.doc.toString() : '';
            },

            async save() {
                const content = this.getContent();
                await this.$store.app.saveFile(content);
            }
        };
    });
});
```

**Step 2: Commit**

```bash
git add xkeen-go/web/static/js/components/editor.js
git commit -m "refactor(web): extract editor component to separate file"
```

---

## Task 7: Create components/logs.js

**Files:**
- Create: `xkeen-go/web/static/js/components/logs.js`

**Step 1: Create logs component**

```javascript
// components/logs.js - Logs viewer with WebSocket streaming

import { createLogStream } from '../services/logs.js';

document.addEventListener('alpine:init', () => {
    Alpine.data('logs', function() {
        return {
            stream: null,

            init() {
                this.$watch('$store.app.activeTab', (tab) => {
                    if (tab === 'logs') {
                        this.connect();
                    } else {
                        this.disconnect();
                    }
                });

                if (this.$store.app.activeTab === 'logs') {
                    this.connect();
                }
            },

            destroy() {
                this.disconnect();
            },

            connect() {
                this.$store.app.loadLogs();

                if (this.stream && this.stream.isOpen()) {
                    return;
                }

                this.stream = createLogStream(
                    (msg) => {
                        this.$store.app.logs.push(msg);

                        if (this.$store.app.logs.length > 500) {
                            this.$store.app.logs = this.$store.app.logs.slice(-500);
                        }
                    },
                    () => {
                        this.$store.app.showToast('Log stream error', 'error');
                    }
                );
            },

            disconnect() {
                if (this.stream) {
                    this.stream.close();
                    this.stream = null;
                }
            }
        };
    });
});
```

**Step 2: Commit**

```bash
git add xkeen-go/web/static/js/components/logs.js
git commit -m "refactor(web): extract logs component to separate file"
```

---

## Task 8: Create components/service.js

**Files:**
- Create: `xkeen-go/web/static/js/components/service.js`

**Step 1: Create service component**

```javascript
// components/service.js - Service status and control buttons

document.addEventListener('alpine:init', () => {
    Alpine.data('service', function() {
        return {
            interval: null,

            init() {
                this.$store.app.fetchServiceStatus();
                this.startPolling();
            },

            destroy() {
                this.stopPolling();
            },

            startPolling() {
                this.interval = setInterval(() => {
                    this.$store.app.fetchServiceStatus();
                }, 5000);
            },

            stopPolling() {
                if (this.interval) {
                    clearInterval(this.interval);
                    this.interval = null;
                }
            },

            start() {
                this.$store.app.startService();
            },

            stop() {
                this.$store.app.stopService();
            }
        };
    });
});
```

**Step 2: Commit**

```bash
git add xkeen-go/web/static/js/components/service.js
git commit -m "refactor(web): extract service component to separate file"
```

---

## Task 9: Update components/commands.js

**Files:**
- Modify: `xkeen-go/web/static/js/components/commands.js`

**Step 1: Update commands component to use api.js**

Replace entire file with:

```javascript
// components/commands.js - Commands tab with categorized XKeen commands

import { postStream } from '../services/api.js';

function commandsComponent() {
    return {
        // State
        executingCommand: '',
        commandComplete: false,

        // Categories with commands and descriptions
        categories: [
            {
                name: 'Управление прокси-клиентом',
                commands: [
                    { name: 'start', description: 'Запуск XKeen' },
                    { name: 'stop', description: 'Остановка XKeen' },
                    { name: 'restart', description: 'Перезапуск XKeen' },
                    { name: 'status', description: 'Статус XKeen' }
                ]
            },
            {
                name: 'Резервная копия XKeen',
                commands: [
                    { name: 'kb', description: 'Создать резервную копию' },
                    { name: 'kbr', description: 'Восстановить из резервной копии' }
                ]
            },
            {
                name: 'Обновление компонентов',
                commands: [
                    { name: 'uk', description: 'Обновить XKeen' },
                    { name: 'ug', description: 'Обновить GeoIP/GeoSite' },
                    { name: 'ux', description: 'Обновить Xray' },
                    { name: 'um', description: 'Обновить модули' }
                ]
            }
        ],

        // Dangerous commands that require confirmation
        dangerousCommands: ['stop', 'restart', 'uk', 'ug', 'ux', 'um'],

        executeCommand(command) {
            if (this.isDangerous(command)) {
                const cmdInfo = this.getCommandInfo(command);
                this.$store.app.confirm.description = cmdInfo?.description || `Execute ${command} command`;
                this.$store.app.confirm.onConfirm = () => this.doExecute(command);
                this.$store.app.confirm.show = true;
            } else {
                this.doExecute(command);
            }
        },

        getCommandInfo(name) {
            for (const cat of this.categories) {
                const cmd = cat.commands.find(c => c.name === name);
                if (cmd) return cmd;
            }
            return null;
        },

        isDangerous(command) {
            return this.dangerousCommands.includes(command);
        },

        async doExecute(command) {
            this.executingCommand = command;
            this.$store.app.modal.error = '';
            this.$store.app.modal.output = '';
            this.$store.app.modal.command = command;
            this.$store.app.modal.show = true;
            this.commandComplete = false;

            try {
                await postStream('/api/xkeen/command', { command: command }, (msg) => {
                    this.handleStreamMessage(msg);
                });
            } catch (err) {
                this.$store.app.modal.error = 'Failed to execute command: ' + err.message;
            } finally {
                this.executingCommand = '';
                this.commandComplete = true;
            }
        },

        handleStreamMessage(msg) {
            if (msg.type === 'output') {
                this.$store.app.modal.output += msg.text + '\n';
                this.scrollToBottom();
            } else if (msg.type === 'error') {
                this.$store.app.modal.error += (this.$store.app.modal.error ? '\n' : '') + msg.text;
                this.scrollToBottom();
            } else if (msg.type === 'complete') {
                this.commandComplete = true;
                if (!msg.success && !this.$store.app.modal.error) {
                    this.$store.app.modal.error = `Command failed with exit code ${msg.exitCode}`;
                }
            }
        },

        scrollToBottom() {
            this.$nextTick(() => {
                const outputEl = document.getElementById('modal-output');
                if (outputEl) {
                    outputEl.scrollTop = outputEl.scrollHeight;
                }
            });
        },

        isLoading(command) {
            return this.executingCommand === command;
        }
    };
}

// Register with Alpine.js when available
document.addEventListener('alpine:init', () => {
    Alpine.data('commands', commandsComponent);
});
```

**Step 2: Commit**

```bash
git add xkeen-go/web/static/js/components/commands.js
git commit -m "refactor(web): update commands component to use api service"
```

---

## Task 10: Simplify app.js

**Files:**
- Modify: `xkeen-go/web/static/js/app.js`

**Step 1: Simplify app.js to keyboard shortcuts only**

Replace entire file with:

```javascript
// app.js - Keyboard shortcuts only
// Components are loaded via separate script tags

document.addEventListener('keydown', (e) => {
    if ((e.ctrlKey || e.metaKey) && e.key === 's') {
        e.preventDefault();
        window.dispatchEvent(new CustomEvent('editor:save'));
    }
});
```

**Step 2: Commit**

```bash
git add xkeen-go/web/static/js/app.js
git commit -m "refactor(web): simplify app.js to keyboard shortcuts only"
```

---

## Task 11: Update index.html

**Files:**
- Modify: `xkeen-go/web/index.html`

**Step 1: Update script loading to ES modules**

Find the script section at the bottom and replace with:

```html
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

    <!-- Load in order: store → components → Alpine -->
    <script type="module" src="/static/js/store.js"></script>
    <script type="module" src="/static/js/app.js"></script>
    <script type="module" src="/static/js/components/editor.js"></script>
    <script type="module" src="/static/js/components/logs.js"></script>
    <script type="module" src="/static/js/components/service.js"></script>
    <script type="module" src="/static/js/components/commands.js"></script>
    <script src="https://cdn.jsdelivr.net/npm/alpinejs@3.14.3/dist/cdn.min.js" integrity="sha384-iZD2X8o1Zdq0HR5H/7oa8W30WS4No+zWCKUPD7fHRay9I1Gf+C4F8sVmw7zec1wW" crossorigin="anonymous"></script>
</body>
</html>
```

**Step 2: Commit**

```bash
git add xkeen-go/web/index.html
git commit -m "refactor(web): update script loading to ES modules"
```

---

## Task 12: Verify and clean up

**Step 1: Verify file structure**

Run: `find xkeen-go/web/static/js -type f -name "*.js" | sort`
Expected:
```
xkeen-go/web/static/js/app.js
xkeen-go/web/static/js/components/commands.js
xkeen-go/web/static/js/components/editor.js
xkeen-go/web/static/js/components/logs.js
xkeen-go/web/static/js/components/service.js
xkeen-go/web/static/js/services/api.js
xkeen-go/web/static/js/services/config.js
xkeen-go/web/static/js/services/logs.js
xkeen-go/web/static/js/services/xkeen.js
xkeen-go/web/static/js/store.js
```

**Step 2: Build and test locally**

Run: `cd xkeen-go && make build && ./build/xkeen-go-*.exe`
Expected: Server starts without errors

**Step 3: Manual browser test**

1. Open http://localhost:8089
2. Login
3. Test each tab: Editor, Logs, Settings, Commands
4. Verify file loading, saving, command execution

**Step 4: Final commit**

```bash
git add -A
git commit -m "refactor(web): complete service layer refactoring

- Extract API calls to services/ directory
- Extract components to components/ directory
- Update store to use services
- Convert to ES modules
- Clean up app.js to keyboard shortcuts only"
```

---

## Summary

| Task | Description | Files |
|------|-------------|-------|
| 1 | Create api.js | services/api.js |
| 2 | Create config.js | services/config.js |
| 3 | Create xkeen.js | services/xkeen.js |
| 4 | Create logs.js | services/logs.js |
| 5 | Refactor store.js | store.js |
| 6 | Create editor.js | components/editor.js |
| 7 | Create logs.js | components/logs.js |
| 8 | Create service.js | components/service.js |
| 9 | Update commands.js | components/commands.js |
| 10 | Simplify app.js | app.js |
| 11 | Update index.html | index.html |
| 12 | Verify and clean up | - |
