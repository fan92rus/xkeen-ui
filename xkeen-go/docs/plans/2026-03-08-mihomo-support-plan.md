# Mihomo Config Editor Support - Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add Mihomo YAML config editing alongside Xray JSON configs with mode switching in Settings tab.

**Architecture:** Minimal changes approach - parameterize existing code with mode selection. Backend adds mode endpoints and YAML support. Frontend adds mode switching in Settings and dynamic CodeMirror language mode.

**Tech Stack:** Go backend, Alpine.js frontend, CodeMirror 6 with JSON/YAML language modes.

---

## Task 1: Add CodeMirror YAML Language Vendor Package

**Files:**
- Download: `@codemirror/lang-yaml` npm package
- Create: `xkeen-go/web/static/vendor/@codemirror/lang-yaml/6.0.1/index.js`

**Step 1: Download YAML language package**

Use npm to get the package:
```bash
cd xkeen-go/web/static/vendor
mkdir -p @codemirror/lang-yaml/6.0.1
cd @codemirror/lang-yaml/6.0.1
npm pack @codemirror/lang-yaml@6.0.0
tar -xzf codemirror-lang-yaml-6.0.0.tgz
mv package/dist/index.cjs index.js
rm -rf package codemirror-lang-yaml-6.0.0.tgz
```

If npm not available, download from esm.sh:
```bash
curl -L "https://esm.sh/@codemirror/lang-yaml@6.0.0" -o xkeen-go/web/static/vendor/@codemirror/lang-yaml/6.0.1/index.js
```

**Step 2: Verify file exists**

```bash
ls -la xkeen-go/web/static/vendor/@codemirror/lang-yaml/6.0.1/index.js
```
Expected: File exists with reasonable size (~50KB+)

**Step 3: Commit**

```bash
cd xkeen-go
git add web/static/vendor/@codemirror/lang-yaml/
git commit -m "vendor: add CodeMirror YAML language support

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Task 2: Update Config Struct for Mihomo

**Files:**
- Modify: `xkeen-go/internal/config/config.go:13-37` (Config struct)
- Modify: `xkeen-go/internal/config/config.go:68-93` (DefaultConfig)

**Step 1: Add Mihomo fields to Config struct**

Add after `XkeenBinary` field (line ~21):
```go
	// MihomoConfigDir is the directory containing Mihomo configuration files.
	MihomoConfigDir string `json:"mihomo_config_dir"`

	// MihomoBinary is the path or name of the mihomo binary.
	MihomoBinary string `json:"mihomo_binary"`
```

**Step 2: Add defaults to DefaultConfig**

Add after `XkeenBinary` line in DefaultConfig (line ~72):
```go
		MihomoConfigDir: "/opt/etc/mihomo",
		MihomoBinary:    "mihomo",
```

**Step 3: Run tests to verify**

```bash
cd xkeen-go && go test -v ./internal/config/...
```
Expected: PASS

**Step 4: Commit**

```bash
cd xkeen-go
git add internal/config/config.go
git commit -m "feat(config): add Mihomo config directory and binary fields

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Task 3: Add Mode Handler to ConfigHandler

**Files:**
- Modify: `xkeen-go/internal/handlers/config.go` (add ModeHandler, update ListFiles)

**Step 1: Add mode-related types and fields**

Add to ConfigHandler struct (after line 25):
```go
// ConfigHandler handles config file operations.
type ConfigHandler struct {
	validator    *utils.PathValidator
	backupDir    string
	defaultPath   string
	xrayConfigDir  string
	mihomoConfigDir string
}

// ModeInfo represents mode availability information.
type ModeInfo struct {
	Mode            string `json:"mode"`
	XrayAvailable   bool   `json:"xray_available"`
	MihomoAvailable bool   `json:"mihomo_available"`
}

// ModeRequest is the request body for SetMode.
type ModeRequest struct {
	Mode string `json:"mode"` // "xray" or "mihomo"
}
```

**Step 2: Update NewConfigHandler**

Replace NewConfigHandler function (lines 28-38):
```go
// NewConfigHandler creates a new ConfigHandler.
func NewConfigHandler(allowedRoots []string, backupDir, xrayConfigDir, mihomoConfigDir string) *ConfigHandler {
	validator, err := utils.NewPathValidator(allowedRoots)
	if err != nil {
		log.Printf("Warning: failed to create path validator: %v", err)
	}
	return &ConfigHandler{
		validator:       validator,
		backupDir:       backupDir,
		defaultPath:     xrayConfigDir,
		xrayConfigDir:   xrayConfigDir,
		mihomoConfigDir: mihomoConfigDir,
	}
}
```

**Step 3: Add isYAMLFile helper**

Add after FileInfo struct (around line 48):
```go
// isYAMLFile checks if a file is a YAML file.
func isYAMLFile(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".yml") || strings.HasSuffix(lower, ".yaml")
}

// isJSONFile checks if a file is a JSON/JSONC file.
func isJSONFile(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".json") || strings.HasSuffix(lower, ".jsonc")
}
```

**Step 4: Add GetMode handler**

Add new handler after ListFiles:
```go
// GetMode returns current mode and availability.
// GET /api/config/mode
func (h *ConfigHandler) GetMode(w http.ResponseWriter, r *http.Request) {
	xrayAvailable := h.dirExists(h.xrayConfigDir)
	mihomoAvailable := h.dirExists(h.mihomoConfigDir)

	mode := "xray"
	queryMode := r.URL.Query().Get("current")
	if queryMode == "mihomo" {
		mode = "mihomo"
	}

	h.respondJSON(w, http.StatusOK, ModeInfo{
		Mode:            mode,
		XrayAvailable:   xrayAvailable,
		MihomoAvailable: mihomoAvailable,
	})
}

// dirExists checks if a directory exists.
func (h *ConfigHandler) dirExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}
```

**Step 5: Update ListFiles to accept mode parameter**

Replace ListFiles function (lines 75-132):
```go
// ListFiles returns a list of config files in the specified directory.
// GET /api/config/files?path=/opt/etc/xray/configs&mode=xray
func (h *ConfigHandler) ListFiles(w http.ResponseWriter, r *http.Request) {
	queryPath := r.URL.Query().Get("path")
	mode := r.URL.Query().Get("mode")

	// Determine default path based on mode
	if queryPath == "" {
		if mode == "mihomo" {
			queryPath = h.mihomoConfigDir
		} else {
			queryPath = h.xrayConfigDir
		}
	}

	// Validate path
	cleanPath, err := h.validator.Validate(queryPath)
	if err != nil {
		h.respondError(w, http.StatusForbidden, err.Error())
		return
	}

	// Read directory
	entries, err := os.ReadDir(cleanPath)
	if err != nil {
		if os.IsNotExist(err) {
			h.respondError(w, http.StatusNotFound, "directory not found")
			return
		}
		h.respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to read directory: %v", err))
		return
	}

	// Filter files based on mode
	files := []FileInfo{}
	for _, entry := range entries {
		name := entry.Name()

		// Skip backups directory
		if entry.IsDir() && name == "backups" {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		// For directories, always include
		if entry.IsDir() {
			files = append(files, FileInfo{
				Name:     name,
				Path:     filepath.Join(cleanPath, name),
				Size:     info.Size(),
				Modified: info.ModTime().Unix(),
				IsDir:    true,
			})
			continue
		}

		// For files, filter by extension based on mode
		if mode == "mihomo" {
			if isYAMLFile(name) {
				files = append(files, FileInfo{
					Name:     name,
					Path:     filepath.Join(cleanPath, name),
					Size:     info.Size(),
					Modified: info.ModTime().Unix(),
					IsDir:    false,
				})
			}
		} else {
			// Xray mode - show JSON/JSONC files
			if isJSONFile(name) {
				files = append(files, FileInfo{
					Name:     name,
					Path:     filepath.Join(cleanPath, name),
					Size:     info.Size(),
					Modified: info.ModTime().Unix(),
					IsDir:    false,
				})
			}
		}
	}

	h.respondJSON(w, http.StatusOK, ListFilesResponse{
		Path:  cleanPath,
		Files: files,
	})
}
```

**Step 6: Update ReadFile to skip JSON validation for YAML**

Replace ReadFile function (lines 134-175):
```go
// ReadFile returns the content of a config file.
// GET /api/config/file?path=/opt/etc/xkeen/config.json
func (h *ConfigHandler) ReadFile(w http.ResponseWriter, r *http.Request) {
	filePath := r.URL.Query().Get("path")
	if filePath == "" {
		h.respondError(w, http.StatusBadRequest, "path parameter is required")
		return
	}

	// Validate path
	cleanPath, err := h.validator.Validate(filePath)
	if err != nil {
		h.respondError(w, http.StatusForbidden, err.Error())
		return
	}

	// Read file
	data, err := os.ReadFile(cleanPath)
	if err != nil {
		if os.IsNotExist(err) {
			h.respondError(w, http.StatusNotFound, "file not found")
			return
		}
		h.respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to read file: %v", err))
		return
	}

	// Validate based on file type
	isValid := true
	if isJSONFile(cleanPath) {
		// JSON/JSONC validation
		jsonData, err := utils.JSONCtoJSON(data)
		if err != nil {
			h.respondError(w, http.StatusBadRequest, fmt.Sprintf("failed to parse JSONC: %v", err))
			return
		}
		isValid = json.Valid(jsonData)
	}
	// YAML files - no validation, always "valid"

	h.respondJSON(w, http.StatusOK, ReadFileResponse{
		Path:    cleanPath,
		Content: string(data),
		Valid:   isValid,
	})
}
```

**Step 7: Update WriteFile to skip JSON validation for YAML**

Replace WriteFile function (lines 177-241):
```go
// WriteFile saves content to a config file with automatic backup.
// POST /api/config/file
func (h *ConfigHandler) WriteFile(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 10*1024*1024)

	var req WriteFileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
		return
	}

	if req.Path == "" {
		h.respondError(w, http.StatusBadRequest, "path is required")
		return
	}

	if req.Content == "" {
		h.respondError(w, http.StatusBadRequest, "content is required")
		return
	}

	// Validate path
	cleanPath, err := h.validator.Validate(req.Path)
	if err != nil {
		h.respondError(w, http.StatusForbidden, err.Error())
		return
	}

	// Validate based on file type
	if isJSONFile(cleanPath) {
		// JSON/JSONC validation
		jsonData, err := utils.JSONCtoJSON([]byte(req.Content))
		if err != nil {
			h.respondError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSONC: %v", err))
			return
		}
		if !json.Valid(jsonData) {
			h.respondError(w, http.StatusBadRequest, "invalid JSON content")
			return
		}
	}
	// YAML files - no validation

	// Create backup
	backupPath, err := h.createBackup(cleanPath)
	if err != nil {
		log.Printf("Warning: failed to create backup for %s: %v", cleanPath, err)
	}

	// Ensure parent directory exists
	parentDir := filepath.Dir(cleanPath)
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		h.respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create parent directory: %v", err))
		return
	}

	// Write file
	if err := os.WriteFile(cleanPath, []byte(req.Content), 0644); err != nil {
		h.respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to write file: %v", err))
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"path":    cleanPath,
		"backup":  backupPath,
	})
}
```

**Step 8: Update RegisterConfigRoutes**

Replace RegisterConfigRoutes function (lines 653-663):
```go
// RegisterConfigRoutes registers config-related routes.
func RegisterConfigRoutes(r *mux.Router, handler *ConfigHandler) {
	r.HandleFunc("/config/mode", handler.GetMode).Methods("GET")
	r.HandleFunc("/config/files", handler.ListFiles).Methods("GET")
	r.HandleFunc("/config/file", handler.ReadFile).Methods("GET")
	r.HandleFunc("/config/file", handler.WriteFile).Methods("POST")
	r.HandleFunc("/config/file", handler.DeleteFile).Methods("DELETE")
	r.HandleFunc("/config/create", handler.CreateFile).Methods("POST")
	r.HandleFunc("/config/rename", handler.RenameFile).Methods("POST")
	r.HandleFunc("/config/backups", handler.ListBackups).Methods("GET")
	r.HandleFunc("/config/backups/content", handler.GetBackupContent).Methods("GET")
	r.HandleFunc("/config/restore", handler.RestoreBackup).Methods("POST")
}
```

**Step 9: Run tests**

```bash
cd xkeen-go && go test -v ./internal/handlers/... -run Config
```
Expected: May have some failures due to NewConfigHandler signature change

**Step 10: Commit**

```bash
cd xkeen-go
git add internal/handlers/config.go
git commit -m "feat(handlers): add mode support and YAML file handling to ConfigHandler

- Add GetMode endpoint for mode availability
- Update ListFiles to filter by mode
- Skip JSON validation for YAML files
- Support both Xray (JSON) and Mihomo (YAML) configs

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Task 4: Update Server to Pass Mihomo Config Dir

**Files:**
- Modify: `xkeen-go/internal/server/server.go:106-108` (NewConfigHandler call)

**Step 1: Update NewConfigHandler call**

Replace line 106:
```go
	s.configHandler = handlers.NewConfigHandler(cfg.AllowedRoots, backupDir, cfg.XrayConfigDir, cfg.MihomoConfigDir)
```

**Step 2: Run tests**

```bash
cd xkeen-go && go build ./...
```
Expected: No errors

**Step 3: Commit**

```bash
cd xkeen-go
git add internal/server/server.go
git commit -m "feat(server): pass Mihomo config dir to ConfigHandler

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Task 5: Update Service Handler for Mihomo

**Files:**
- Modify: `xkeen-go/internal/handlers/service.go`

**Step 1: Read current service handler**

First, read the file to understand the structure:
```bash
head -100 xkeen-go/internal/handlers/service.go
```

**Step 2: Add Mihomo to allowed services**

Find the service whitelist and add mihomo. The exact changes depend on current implementation.

**Step 3: Run tests**

```bash
cd xkeen-go && go test -v ./internal/handlers/... -run Service
```

**Step 4: Commit**

```bash
cd xkeen-go
git add internal/handlers/service.go
git commit -m "feat(service): add Mihomo service control support

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Task 6: Update Logs Handler for Mihomo Log Paths

**Files:**
- Modify: `xkeen-go/internal/handlers/logs.go`

**Step 1: Add Mihomo log paths**

Update LogFiles configuration to include Mihomo paths:
```go
LogFiles: []string{
	"/opt/var/log/xray/access.log",
	"/opt/var/log/xray/error.log",
	"/opt/var/log/mihomo/access.log",
	"/opt/var/log/mihomo/error.log",
},
```

**Step 2: Commit**

```bash
cd xkeen-go
git add internal/handlers/logs.go
git commit -m "feat(logs): add Mihomo log file paths

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Task 7: Update Import Map for YAML Language

**Files:**
- Modify: `xkeen-go/web/index.html:444-456` (importmap)

**Step 1: Add YAML language to importmap**

Update the importmap script block:
```html
<script type="importmap">
{
    "imports": {
        "codemirror": "/static/vendor/codemirror/6.0.1/index.js",
        "@codemirror/lang-json": "/static/vendor/@codemirror/lang-json/6.0.1/index.js",
        "@codemirror/lang-yaml": "/static/vendor/@codemirror/lang-yaml/6.0.1/index.js",
        "@codemirror/theme-one-dark": "/static/vendor/@codemirror/theme-one-dark/6.1.2/index.js",
        "@codemirror/merge": "/static/vendor/@codemirror/merge/6.0.0/index.js",
        "@codemirror/state": "/static/vendor/@codemirror/state/6.4.1/index.js",
        "@codemirror/view": "/static/vendor/@codemirror/view/6.26.3/index.js"
    }
}
</script>
```

**Step 2: Commit**

```bash
cd xkeen-go
git add web/index.html
git commit -m "feat(ui): add CodeMirror YAML language to importmap

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Task 8: Update Store for Mode Support

**Files:**
- Modify: `xkeen-go/web/static/js/store.js`

**Step 1: Add mode state**

Add after `activeTab: 'editor'`:
```javascript
        // Mode state
        currentMode: 'xray',        // 'xray' | 'mihomo'
        xrayAvailable: true,
        mihomoAvailable: false,
```

**Step 2: Add mode API functions**

Create new file `xkeen-go/web/static/js/services/mode.js`:
```javascript
// services/mode.js - Mode management API

import { get } from './api.js';

export async function getModeInfo() {
    return get('/api/config/mode');
}
```

**Step 3: Update loadFiles to use mode**

Replace the `loadFiles` method:
```javascript
        async loadFiles() {
            try {
                const data = await get(`/api/config/files?mode=${this.currentMode}`);
                this.files = data.files || [];
            } catch (err) {
                this.showToast('Failed to load files', 'error');
            }
        },
```

**Step 4: Add switchMode method**

Add after `loadFiles`:
```javascript
        async switchMode(mode) {
            if (mode === 'mihomo' && !this.mihomoAvailable) {
                this.showToast('Mihomo is not installed', 'error');
                return;
            }
            if (mode === 'xray' && !this.xrayAvailable) {
                this.showToast('Xray is not installed', 'error');
                return;
            }

            const previousMode = this.currentMode;
            this.currentFile = null;
            this.currentMode = mode;

            // Update default log file based on mode
            if (mode === 'mihomo') {
                this.logFile = '/opt/var/log/mihomo/access.log';
            } else {
                this.logFile = '/opt/var/log/xray/access.log';
            }

            // Reload files for new mode
            await this.loadFiles();

            // Dispatch mode change event for editor
            window.dispatchEvent(new CustomEvent('mode:change', { detail: mode }));
        },

        async checkModeAvailability() {
            try {
                const data = await get('/api/config/mode?current=' + this.currentMode);
                this.xrayAvailable = data.xray_available;
                this.mihomoAvailable = data.mihomo_available;
            } catch (err) {
                console.error('Failed to check mode availability:', err);
            }
        },
```

**Step 5: Update init to check mode availability**

Replace init method:
```javascript
        init() {
            this.checkModeAvailability();
            this.loadFiles();
            this.loadXraySettings();
            this.checkUpdate();
            // Connect to SSE status stream
            statusService.connectStatusStream((status) => {
                this.serviceStatus = status;
            });
        }
```

**Step 6: Import get in store**

Add at top of store.js:
```javascript
import { get } from './services/api.js';
```

**Step 7: Commit**

```bash
cd xkeen-go
git add web/static/js/store.js web/static/js/services/mode.js
git commit -m "feat(store): add mode switching support

- Add currentMode state and availability flags
- Add switchMode and checkModeAvailability methods
- Update loadFiles to use mode parameter

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Task 9: Update Editor Component for Dynamic Language Mode

**Files:**
- Modify: `xkeen-go/web/static/js/components/editor.js`

**Step 1: Add YAML import**

Add to imports:
```javascript
import { yaml } from '@codemirror/lang-yaml';
```

**Step 2: Add mode tracking and recreate logic**

Replace the entire editor.js:
```javascript
// components/editor.js - CodeMirror editor component

import { EditorView, basicSetup } from 'codemirror';
import { json } from '@codemirror/lang-json';
import { yaml } from '@codemirror/lang-yaml';
import { oneDark } from '@codemirror/theme-one-dark';

document.addEventListener('alpine:init', () => {
    Alpine.data('editor', function() {
        return {
            instance: null,
            ready: false,
            pendingFile: null,
            currentLanguage: 'json',

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

                // Listen for mode changes
                this._modeChangeHandler = (e) => {
                    if (e.detail) {
                        this.updateLanguage(e.detail);
                    }
                };
                window.addEventListener('mode:change', this._modeChangeHandler);

                // Listen for save keyboard shortcut
                this._saveHandler = () => this.save();
                window.addEventListener('editor:save', this._saveHandler);

                // Listen for load content event (from backups modal)
                this._loadHandler = (e) => {
                    if (this.instance && e.detail) {
                        this.instance.dispatch({
                            changes: {
                                from: 0,
                                to: this.instance.state.doc.length,
                                insert: e.detail
                            }
                        });
                    }
                };
                window.addEventListener('editor:loadContent', this._loadHandler);

                // Listen for show diff event
                this._showDiffHandler = () => this.showDiff();
                window.addEventListener('editor:showDiff', this._showDiffHandler);
            },

            destroy() {
                if (this._modeChangeHandler) {
                    window.removeEventListener('mode:change', this._modeChangeHandler);
                }
                if (this._saveHandler) {
                    window.removeEventListener('editor:save', this._saveHandler);
                }
                if (this._loadHandler) {
                    window.removeEventListener('editor:loadContent', this._loadHandler);
                }
                if (this._showDiffHandler) {
                    window.removeEventListener('editor:showDiff', this._showDiffHandler);
                }
                if (this.instance) {
                    this.instance.destroy();
                }
            },

            async initEditor() {
                const lang = this.$store.app.currentMode === 'mihomo' ? yaml() : json();
                this.currentLanguage = this.$store.app.currentMode === 'mihomo' ? 'yaml' : 'json';

                this.instance = new EditorView({
                    doc: '// Select a file to edit',
                    extensions: [
                        basicSetup,
                        lang,
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

            updateLanguage(mode) {
                const newLang = mode === 'mihomo' ? 'yaml' : 'json';
                if (newLang === this.currentLanguage) return;

                this.currentLanguage = newLang;
                const langExtension = mode === 'mihomo' ? yaml() : json();

                // Recreate editor with new language
                const currentContent = this.instance ? this.instance.state.doc.toString() : '';

                if (this.instance) {
                    this.instance.destroy();
                }

                this.instance = new EditorView({
                    doc: currentContent || '// Select a file to edit',
                    extensions: [
                        basicSetup,
                        langExtension,
                        oneDark,
                        EditorView.lineWrapping,
                        EditorView.theme({
                            '&': { height: '100%' },
                            '.cm-scroller': { overflow: 'auto' }
                        })
                    ],
                    parent: this.$refs.editor
                });
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
            },

            // === Backups Modal - delegates to store ===
            async showBackups() {
                await this.$store.app.showBackups();
            },

            // === Diff Modal ===
            showDiff() {
                const currentContent = this.getContent();
                const savedContent = this.$store.app.lastSavedContent || '';

                if (currentContent === savedContent) {
                    this.$store.app.showToast('No changes since last save', '');
                    return;
                }

                // Compute diff and show in modal
                const diffContent = this.$store.app.computeDiff(currentContent, savedContent);
                this.$store.app.diffModal.diffContent = diffContent;
                this.$store.app.diffModal.show = true;
            }
        };
    });
});
```

**Step 3: Commit**

```bash
cd xkeen-go
git add web/static/js/components/editor.js
git commit -m "feat(editor): add dynamic language mode switching

- Import YAML language support
- Recreate editor when mode changes
- Support both JSON and YAML files

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Task 10: Update Config Service for Mode Parameter

**Files:**
- Modify: `xkeen-go/web/static/js/services/config.js`

**Step 1: Update listFiles to accept mode**

Replace the file content:
```javascript
// services/config.js - Config file operations

import { get, post } from './api.js';

export async function listFiles(mode = 'xray') {
    const data = await get(`/api/config/files?mode=${mode}`);
    return data.files || [];
}

export async function getFile(path) {
    return get(`/api/config/file?path=${encodeURIComponent(path)}`);
}

export async function saveFile(path, content) {
    return post('/api/config/file', { path, content });
}

export async function getBackups(filePath) {
    const data = await get(`/api/config/backups?path=${encodeURIComponent(filePath)}`);
    return data.backups || [];
}

export async function getBackupContent(backupPath) {
    const data = await get(`/api/config/backups/content?backup_path=${encodeURIComponent(backupPath)}`);
    return data.content || '';
}
```

**Step 2: Commit**

```bash
cd xkeen-go
git add web/static/js/services/config.js
git commit -m "feat(config): add mode parameter to listFiles

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Task 11: Add Mode Switcher to Settings Tab

**Files:**
- Modify: `xkeen-go/web/index.html` (Settings tab section)

**Step 1: Add mode switcher section**

Find the Settings section (around line 135) and add after the header:
```html
            <!-- Settings Tab -->
            <section x-show="$store.app.activeTab === 'settings'"
                     x-data="{}"
                     x-cloak
                     class="tab-content"
                     :class="{ 'active': $store.app.activeTab === 'settings' }">
                <div class="settings-container">
                    <div class="settings-header">
                        <h2>Xray Settings</h2>

                        <!-- Mode Switcher -->
                        <div class="mode-switcher">
                            <button @click="$store.app.switchMode('xray')"
                                    :class="{ active: $store.app.currentMode === 'xray' }"
                                    :disabled="!$store.app.xrayAvailable"
                                    class="btn btn-sm">Xray</button>
                            <button @click="$store.app.switchMode('mihomo')"
                                    :class="{ active: $store.app.currentMode === 'mihomo' }"
                                    :disabled="!$store.app.mihomoAvailable"
                                    class="btn btn-sm">Mihomo</button>
                        </div>

                        <div class="settings-actions">
                            <button @click="$store.app.restartService()"
                                    class="btn btn-primary">Apply & Restart Xray</button>
                            <button @click="$store.app.loadXraySettings()"
                                    class="btn">Refresh Settings</button>
                        </div>
                    </div>
```

**Step 2: Update header to show current mode**

Change `<h2>Xray Settings</h2>` to:
```html
                        <h2 x-text="$store.app.currentMode === 'mihomo' ? 'Mihomo Settings' : 'Xray Settings'"></h2>
```

**Step 3: Commit**

```bash
cd xkeen-go
git add web/index.html
git commit -m "feat(ui): add mode switcher to Settings tab

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Task 12: Update Logs Tab for Dynamic Log Paths

**Files:**
- Modify: `xkeen-go/web/index.html` (Logs tab section, around line 96)

**Step 1: Make log dropdown dynamic**

Replace the log file select (around line 97-100):
```html
                    <select x-model="$store.app.logFile" @change="$store.app.loadLogs()">
                        <template x-if="$store.app.currentMode === 'xray'">
                            <template>
                                <option value="/opt/var/log/xray/access.log">Access Log</option>
                                <option value="/opt/var/log/xray/error.log">Error Log</option>
                            </template>
                        </template>
                        <template x-if="$store.app.currentMode === 'mihomo'">
                            <template>
                                <option value="/opt/var/log/mihomo/access.log">Access Log</option>
                                <option value="/opt/var/log/mihomo/error.log">Error Log</option>
                            </template>
                        </template>
                    </select>
```

**Step 2: Commit**

```bash
cd xkeen-go
git add web/index.html
git commit -m "feat(ui): make log file paths dynamic based on mode

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Task 13: Add CSS for Mode Switcher

**Files:**
- Modify: `xkeen-go/web/static/css/style.css`

**Step 1: Add mode switcher styles**

Add at the end of the file:
```css
/* Mode Switcher */
.mode-switcher {
    display: inline-flex;
    gap: 0.25rem;
    background: var(--bg-secondary);
    padding: 0.25rem;
    border-radius: 0.5rem;
}

.mode-switcher .btn {
    padding: 0.375rem 0.75rem;
    font-size: 0.875rem;
    background: transparent;
    color: var(--text-secondary);
    border: none;
    border-radius: 0.375rem;
    transition: all 0.2s;
}

.mode-switcher .btn:hover:not(:disabled) {
    background: var(--bg-tertiary);
    color: var(--text);
}

.mode-switcher .btn.active {
    background: var(--primary);
    color: white;
}

.mode-switcher .btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
}
```

**Step 2: Commit**

```bash
cd xkeen-go
git add web/static/css/style.css
git commit -m "style: add mode switcher button styles

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Task 14: Build and Test

**Step 1: Build the project**

```bash
cd xkeen-go && make build
```
Expected: Binary created successfully

**Step 2: Run unit tests**

```bash
cd xkeen-go && make test-unit
```
Expected: All tests pass

**Step 3: Manual verification**

Start the server and verify:
1. Mode switcher appears in Settings tab
2. Switching to Mihomo shows YAML files
3. Switching to Xray shows JSON files
4. Logs dropdown updates with mode
5. Toast appears when switching to unavailable mode

**Step 4: Final commit if needed**

```bash
cd xkeen-go
git add -A
git commit -m "chore: final integration for Mihomo support

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Summary

After all tasks:
- Backend supports both Xray (JSON) and Mihomo (YAML) configs
- Frontend has mode switcher in Settings
- Editor dynamically switches between JSON and YAML modes
- Log paths update based on mode
- Graceful handling when Mihomo is not installed
