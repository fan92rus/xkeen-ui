# Backups UI Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Добавить UI для работы с бэкапами: кнопки Diff/Бэкапы, модалка со списком бэкапов, diff view с использованием @codemirror/merge.

**Architecture:** Backend добавляет endpoint для получения содержимого бэкапа и автоочистку старых бэкапов (лимит 5). Frontend использует @codemirror/merge для split-view diff.

**Tech Stack:** Go (backend), Alpine.js + CodeMirror 6 + @codemirror/merge (frontend)

---

## Task 1: Backend - Автоудаление старых бэкапов

**Files:**
- Modify: `xkeen-go/internal/handlers/config.go:242-267`

**Step 1: Добавить метод cleanupOldBackups**

Добавить новый метод после `createBackup`:

```go
// cleanupOldBackups removes old backups, keeping only the most recent ones.
func (h *ConfigHandler) cleanupOldBackups(filePath string, keep int) {
	entries, err := os.ReadDir(h.backupDir)
	if err != nil {
		return
	}

	baseName := filepath.Base(filePath)
	var backups []os.DirEntry
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, baseName+".") && strings.HasSuffix(name, ".bak") {
			backups = append(backups, entry)
		}
	}

	// Sort by modification time (newest first)
	sort.Slice(backups, func(i, j int) bool {
		infoI, errI := backups[i].Info()
		infoJ, errJ := backups[j].Info()
		if errI != nil || errJ != nil {
			return false
		}
		return infoI.ModTime().After(infoJ.ModTime())
	})

	// Remove old backups beyond keep limit
	for i := keep; i < len(backups); i++ {
		backupPath := filepath.Join(h.backupDir, backups[i].Name())
		os.Remove(backupPath)
	}
}
```

**Step 2: Добавить импорт sort**

В начало файла добавить в блок imports:

```go
import (
	// ... existing imports ...
	"sort"
)
```

**Step 3: Вызвать cleanup в createBackup**

Изменить метод `createBackup` (строки 242-267), добавить вызов cleanup в конце:

```go
// createBackup creates a timestamped backup of the specified file.
func (h *ConfigHandler) createBackup(filePath string) (string, error) {
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return "", nil
	}

	if err := os.MkdirAll(h.backupDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create backup directory: %w", err)
	}

	timestamp := time.Now().Format("20060102-150405")
	baseName := filepath.Base(filePath)
	backupName := fmt.Sprintf("%s.%s.bak", baseName, timestamp)
	backupPath := filepath.Join(h.backupDir, backupName)

	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read file for backup: %w", err)
	}

	if err := os.WriteFile(backupPath, data, 0644); err != nil {
		return "", fmt.Errorf("failed to write backup file: %w", err)
	}

	// Cleanup old backups (keep only 5)
	h.cleanupOldBackups(filePath, 5)

	return backupPath, nil
}
```

**Step 4: Проверить компиляцию**

Run: `cd xkeen-go && go build -o /dev/null .`
Expected: Success (no errors)

**Step 5: Commit**

```bash
git add xkeen-go/internal/handlers/config.go
git commit -m "feat(backend): auto-cleanup old backups, keep only 5 latest"
```

---

## Task 2: Backend - Endpoint для содержимого бэкапа

**Files:**
- Modify: `xkeen-go/internal/handlers/config.go` (add new handler)
- Modify: `xkeen-go/internal/handlers/config.go:560-570` (register route)

**Step 1: Добавить handler GetBackupContent**

Добавить после `RestoreBackup` (после строки 558):

```go
// GetBackupContent returns the content of a specific backup file.
// GET /api/config/backups/content?backup_path=<path>
func (h *ConfigHandler) GetBackupContent(w http.ResponseWriter, r *http.Request) {
	backupPath := r.URL.Query().Get("backup_path")
	if backupPath == "" {
		h.respondError(w, http.StatusBadRequest, "backup_path parameter is required")
		return
	}

	// Validate backup path is in backup directory
	if !strings.HasPrefix(backupPath, h.backupDir) {
		h.respondError(w, http.StatusForbidden, "backup path must be in backup directory")
		return
	}

	// Read backup content
	data, err := os.ReadFile(backupPath)
	if err != nil {
		if os.IsNotExist(err) {
			h.respondError(w, http.StatusNotFound, "backup not found")
			return
		}
		h.respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to read backup: %v", err))
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]interface{}{
		"success":     true,
		"backup_path": backupPath,
		"content":     string(data),
	})
}
```

**Step 2: Зарегистрировать route**

В функции `RegisterConfigRoutes` добавить строку после `/config/restore`:

```go
r.HandleFunc("/config/backups/content", handler.GetBackupContent).Methods("GET")
```

**Step 3: Проверить компиляцию**

Run: `cd xkeen-go && go build -o /dev/null .`
Expected: Success (no errors)

**Step 4: Commit**

```bash
git add xkeen-go/internal/handlers/config.go
git commit -m "feat(backend): add endpoint to get backup content"
```

---

## Task 3: Frontend - Добавить импорт @codemirror/merge

**Files:**
- Modify: `xkeen-go/web/index.html:252-261`

**Step 1: Добавить импорт в importmap**

Изменить блок importmap (строки 253-261):

```html
<script type="importmap">
{
    "imports": {
        "codemirror": "https://esm.sh/codemirror@6.0.1",
        "@codemirror/lang-json": "https://esm.sh/@codemirror/lang-json@6.0.1",
        "@codemirror/theme-one-dark": "https://esm.sh/@codemirror/theme-one-dark@6.1.2",
        "@codemirror/merge": "https://esm.sh/@codemirror/merge@6.0.0"
    }
}
</script>
```

**Step 2: Commit**

```bash
git add xkeen-go/web/index.html
git commit -m "feat(web): add @codemirror/merge import"
```

---

## Task 4: Frontend - Добавить getBackupContent в API сервис

**Files:**
- Modify: `xkeen-go/web/static/js/services/config.js`

**Step 1: Добавить функцию getBackupContent**

Добавить после `saveFile`:

```javascript
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
git add xkeen-go/web/static/js/services/config.js
git commit -m "feat(web): add getBackups and getBackupContent API methods"
```

---

## Task 5: Frontend - Добавить lastSavedContent в store

**Files:**
- Modify: `xkeen-go/web/static/js/store.js`

**Step 1: Добавить lastSavedContent в state**

После `isValidJson: true,` (строка 20) добавить:

```javascript
lastSavedContent: '',  // Content of last saved file for diff
```

**Step 2: Обновить loadFile для сохранения lastSavedContent**

Изменить метод `loadFile` (строки 86-100):

```javascript
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
            this.lastSavedContent = data.content;  // Store for diff
        }
    } catch (err) {
        this.showToast('Failed to load file', 'error');
    }
},
```

**Step 3: Обновить saveFile для обновления lastSavedContent**

Изменить метод `saveFile` (строки 102-116):

```javascript
async saveFile(content) {
    if (!this.currentFile) {
        this.showToast('No file selected', 'error');
        return false;
    }

    try {
        await configService.saveFile(this.currentFile.path, content);
        this.lastSavedContent = content;  // Update after successful save
        this.showToast('Saved successfully', 'success');
        return true;
    } catch (err) {
        this.showToast(err.message || 'Save failed', 'error');
        return false;
    }
},
```

**Step 4: Commit**

```bash
git add xkeen-go/web/static/js/store.js
git commit -m "feat(web): add lastSavedContent to store for diff functionality"
```

---

## Task 6: Frontend - Добавить кнопки Diff и Бэкапы в UI

**Files:**
- Modify: `xkeen-go/web/index.html:71-78`

**Step 1: Добавить кнопки в editor-header**

Изменить блок editor-header (строки 71-76):

```html
<div class="editor-header">
    <span x-text="$store.app.currentFile?.path || 'No file selected'"></span>
    <div class="editor-actions">
        <span :class="$store.app.isValidJson === false ? 'error' : ''"
              x-text="$store.app.isValidJson === false ? 'Invalid JSON' : 'Valid JSON'"></span>
        <button x-show="$store.app.currentFile"
                @click="showDiff()"
                class="btn btn-sm">Diff</button>
        <button x-show="$store.app.currentFile"
                @click="showBackups()"
                class="btn btn-sm">Backups</button>
    </div>
</div>
```

**Step 2: Commit**

```bash
git add xkeen-go/web/index.html
git commit -m "feat(web): add Diff and Backups buttons to editor header"
```

---

## Task 7: Frontend - Добавить модалки Backups и Diff

**Files:**
- Modify: `xkeen-go/web/index.html` (add modals before Toast)

**Step 1: Добавить модалки перед Toast (после строки 243)**

Добавить после Confirmation Dialog:

```html
<!-- Backups Modal -->
<div class="modal-overlay" x-show="backupsModal.show" x-transition @click.self="closeBackupsModal()">
    <div class="modal modal-large">
        <div class="modal-header">
            <h3>Backups: <span x-text="backupsModal.fileName"></span></h3>
            <button class="modal-close" @click="closeBackupsModal()">&times;</button>
        </div>
        <div class="modal-body">
            <div class="backups-list" x-show="backupsModal.backups.length > 0">
                <template x-for="(backup, index) in backupsModal.backups" :key="backup.path">
                    <div class="backup-item"
                         :class="{ 'selected': backupsModal.selectedBackup?.path === backup.path }"
                         @click="selectBackup(backup)">
                        <span class="backup-time" x-text="formatBackupTime(backup.modified)"></span>
                        <div class="backup-actions">
                            <button class="btn btn-sm" @click.stop="copyBackupContent(backup)">Copy</button>
                            <button class="btn btn-sm btn-primary" @click.stop="loadBackupToEditor(backup)">Load</button>
                        </div>
                    </div>
                </template>
            </div>
            <div class="backups-empty" x-show="backupsModal.backups.length === 0">
                <p>No backups available</p>
            </div>
            <div class="backup-diff" x-show="backupsModal.selectedBackup && backupsModal.diffContent">
                <h4>Diff with current file</h4>
                <div x-ref="backupDiffEditor" class="diff-editor"></div>
            </div>
        </div>
        <div class="modal-footer">
            <button class="btn" @click="closeBackupsModal()">Close</button>
        </div>
    </div>
</div>

<!-- Diff Modal -->
<div class="modal-overlay" x-show="diffModal.show" x-transition @click.self="closeDiffModal()">
    <div class="modal modal-large">
        <div class="modal-header">
            <h3>Changes since last save</h3>
            <button class="modal-close" @click="closeDiffModal()">&times;</button>
        </div>
        <div class="modal-body">
            <div x-ref="diffEditor" class="diff-editor"></div>
        </div>
        <div class="modal-footer">
            <button class="btn btn-primary" @click="closeDiffModal()">Close</button>
        </div>
    </div>
</div>
```

**Step 2: Commit**

```bash
git add xkeen-go/web/index.html
git commit -m "feat(web): add Backups and Diff modal templates"
```

---

## Task 8: Frontend - Добавить CSS для модалок

**Files:**
- Modify: `xkeen-go/web/static/css/style.css`

**Step 1: Добавить стили в конец файла**

```css
/* Editor actions */
.editor-actions {
    display: flex;
    align-items: center;
    gap: 8px;
}

/* Modal large */
.modal-large {
    max-width: 900px;
    width: 90%;
    max-height: 85vh;
}

.modal-large .modal-body {
    max-height: calc(85vh - 120px);
    overflow-y: auto;
}

/* Backups list */
.backups-list {
    margin-bottom: 16px;
}

.backup-item {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 10px 12px;
    border: 1px solid var(--border-color);
    border-radius: 4px;
    margin-bottom: 8px;
    cursor: pointer;
    transition: background-color 0.15s;
}

.backup-item:hover {
    background-color: var(--bg-hover);
}

.backup-item.selected {
    background-color: var(--primary-light);
    border-color: var(--primary-color);
}

.backup-time {
    font-family: monospace;
    color: var(--text-secondary);
}

.backup-actions {
    display: flex;
    gap: 8px;
}

.backups-empty {
    text-align: center;
    padding: 24px;
    color: var(--text-secondary);
}

/* Diff editor */
.backup-diff {
    margin-top: 16px;
    border-top: 1px solid var(--border-color);
    padding-top: 16px;
}

.backup-diff h4 {
    margin-bottom: 12px;
    color: var(--text-secondary);
}

.diff-editor {
    height: 300px;
    border: 1px solid var(--border-color);
    border-radius: 4px;
    overflow: hidden;
}
```

**Step 2: Commit**

```bash
git add xkeen-go/web/static/css/style.css
git commit -m "feat(web): add styles for backups and diff UI"
```

---

## Task 9: Frontend - Добавить логику в editor.js

**Files:**
- Modify: `xkeen-go/web/static/js/components/editor.js`

**Step 1: Добавить импорты**

Изменить импорты в начале файла:

```javascript
import { EditorView, basicSetup } from 'codemirror';
import { json } from '@codemirror/lang-json';
import { oneDark } from '@codemirror/theme-one-dark';
import { EditorView as MergeEditorView } from '@codemirror/merge';

import * as configService from '../services/config.js';
```

**Step 2: Добавить данные для модалок в Alpine.data**

Заменить весь Alpine.data на:

```javascript
Alpine.data('editor', function() {
    return {
        instance: null,
        ready: false,
        pendingFile: null,

        // Backups modal state
        backupsModal: {
            show: false,
            fileName: '',
            backups: [],
            selectedBackup: null,
            diffContent: ''
        },

        // Diff modal state
        diffModal: {
            show: false
        },

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
        },

        // === Backups Modal ===
        async showBackups() {
            const file = this.$store.app.currentFile;
            if (!file) return;

            this.backupsModal.fileName = file.path.split('/').pop();
            this.backupsModal.backups = await configService.getBackups(file.path);
            this.backupsModal.selectedBackup = null;
            this.backupsModal.diffContent = '';
            this.backupsModal.show = true;

            // Auto-select first backup if available
            if (this.backupsModal.backups.length > 0) {
                await this.selectBackup(this.backupsModal.backups[0]);
            }
        },

        closeBackupsModal() {
            this.backupsModal.show = false;
            this.backupsModal.selectedBackup = null;
            this.backupsModal.diffContent = '';
        },

        async selectBackup(backup) {
            this.backupsModal.selectedBackup = backup;

            // Get backup content and show diff
            const backupContent = await configService.getBackupContent(backup.path);
            const currentContent = this.$store.app.currentFile?.content || '';

            this.backupsModal.diffContent = this.computeDiff(currentContent, backupContent);

            // Render diff in editor
            this.$nextTick(() => {
                this.renderDiffEditor(this.$refs.backupDiffEditor, currentContent, backupContent);
            });
        },

        async copyBackupContent(backup) {
            try {
                const content = await configService.getBackupContent(backup.path);
                await navigator.clipboard.writeText(content);
                this.$store.app.showToast('Backup copied to clipboard', 'success');
            } catch (err) {
                this.$store.app.showToast('Failed to copy backup', 'error');
            }
        },

        loadBackupToEditor(backup) {
            // Load backup content into editor without saving
            configService.getBackupContent(backup.path).then(content => {
                if (this.instance) {
                    this.instance.dispatch({
                        changes: {
                            from: 0,
                            to: this.instance.state.doc.length,
                            insert: content
                        }
                    });
                }
                this.closeBackupsModal();
                this.$store.app.showToast('Backup loaded into editor', 'success');
            }).catch(() => {
                this.$store.app.showToast('Failed to load backup', 'error');
            });
        },

        formatBackupTime(timestamp) {
            const date = new Date(timestamp * 1000);
            return date.toLocaleString();
        },

        // === Diff Modal ===
        showDiff() {
            const currentContent = this.getContent();
            const savedContent = this.$store.app.lastSavedContent || '';

            if (currentContent === savedContent) {
                this.$store.app.showToast('No changes since last save', '');
                return;
            }

            this.diffModal.show = true;
            this.$nextTick(() => {
                this.renderDiffEditor(this.$refs.diffEditor, currentContent, savedContent);
            });
        },

        closeDiffModal() {
            this.diffModal.show = false;
        },

        // === Diff Rendering ===
        renderDiffEditor(container, a, b) {
            if (!container) return;

            // Clear previous content
            container.innerHTML = '';

            try {
                // Create merge view using @codemirror/merge
                const view = new MergeEditorView({
                    a: a,
                    b: b,
                    config: {
                        theme: oneDark,
                        extensions: [basicSetup, json()]
                    }
                });

                container.appendChild(view.dom);
            } catch (err) {
                // Fallback to simple text diff if merge fails
                container.innerHTML = `<pre class="diff-fallback">${this.computeDiff(a, b)}</pre>`;
            }
        },

        computeDiff(a, b) {
            // Simple line-by-line diff for fallback
            const linesA = a.split('\n');
            const linesB = b.split('\n');
            let result = [];

            for (let i = 0; i < Math.max(linesA.length, linesB.length); i++) {
                const lineA = linesA[i] || '';
                const lineB = linesB[i] || '';

                if (lineA === lineB) {
                    result.push('  ' + lineA);
                } else {
                    if (lineB) result.push('- ' + lineB);
                    if (lineA) result.push('+ ' + lineA);
                }
            }

            return result.join('\n');
        }
    };
});
```

**Step 3: Commit**

```bash
git add xkeen-go/web/static/js/components/editor.js
git commit -m "feat(web): add backups and diff modal logic to editor"
```

---

## Task 10: Тестирование и финальный commit

**Step 1: Собрать и запустить**

Run: `cd xkeen-go && make build && make run`
Expected: Server starts without errors

**Step 2: Проверить функционал вручную**

1. Открыть приложение в браузере
2. Выбрать файл в sidebar
3. Нажать "Diff" - должна открыться модалка с изменениями
4. Нажать "Backups" - должен открыться список бэкапов
5. Выбрать бэкап - должен показаться diff
6. Нажать "Copy" - содержимое должно скопироваться
7. Нажать "Load" - содержимое должно загрузиться в редактор
8. Сохранить файл - должен создаться бэкап
9. Проверить что старые бэкапы удаляются (лимит 5)

**Step 3: Финальный commit если всё работает**

```bash
git add -A
git commit -m "feat: complete backups UI with diff functionality"
```

---

## Summary

**Backend:**
- Автоудаление старых бэкапов (лимит 5)
- Новый endpoint `GET /api/config/backups/content`

**Frontend:**
- Импорт `@codemirror/merge`
- API методы `getBackups`, `getBackupContent`
- Store: `lastSavedContent` для diff
- Кнопки Diff/Backups в editor header
- Модалки Backups и Diff
- CSS стили для новых компонентов
- Логика в editor.js для работы с бэкапами и diff
