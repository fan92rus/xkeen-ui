<script setup>
import { ref, onMounted, onUnmounted, watch, nextTick } from 'vue';
import { EditorView, basicSetup } from 'codemirror';
import { json } from '@codemirror/lang-json';
import { yaml } from '@codemirror/lang-yaml';
import { oneDark } from '@codemirror/theme-one-dark';
import { useAppStore } from '../stores/app.js';

const app = useAppStore();
const editorRef = ref(null);
let instance = null;
let ready = false;
let pendingFile = null;

function getLanguageExtension(filePath) {
    if (!filePath) return json();
    const lower = filePath.toLowerCase();
    return (lower.endsWith('.yml') || lower.endsWith('.yaml')) ? yaml() : json();
}

function createEditor(content, parent, filePath) {
    if (instance) instance.destroy();
    instance = new EditorView({
        doc: content,
        extensions: [
            basicSetup,
            getLanguageExtension(filePath),
            oneDark,
            EditorView.lineWrapping,
            EditorView.theme({ '&': { height: '100%' }, '.cm-scroller': { overflow: 'auto' } })
        ],
        parent
    });
    return instance;
}

function loadContent(file) {
    if (!editorRef.value) return;
    createEditor(file.content, editorRef.value, file.path);
    app.isValidJson = file.valid;
}

function getContent() {
    return instance ? instance.state.doc.toString() : '';
}

async function save() {
    await app.saveFile(getContent());
}

function showDiff() {
    app.openDiffModal(getContent(), app.lastSavedContent || '');
}

function showBackups() {
    app.showBackups();
}

// Keyboard shortcut handler
function onSaveShortcut() { save(); }
function onLoadContent(e) {
    if (instance && e.detail) {
        instance.dispatch({ changes: { from: 0, to: instance.state.doc.length, insert: e.detail } });
    }
}

watch(() => app.currentFile, (file) => {
    if (!file) return;
    if (ready) loadContent(file);
    else pendingFile = file;
});

onMounted(() => {
    createEditor('// Select a file to edit', editorRef.value);
    ready = true;
    if (pendingFile) { loadContent(pendingFile); pendingFile = null; }

    window.addEventListener('editor:save', onSaveShortcut);
    window.addEventListener('editor:loadContent', onLoadContent);
});

onUnmounted(() => {
    window.removeEventListener('editor:save', onSaveShortcut);
    window.removeEventListener('editor:loadContent', onLoadContent);
    if (instance) instance.destroy();
});

// Expose for parent (header save button dispatches editor:save event, so this is for programmatic use)
defineExpose({ getContent, save });
</script>

<template>
  <div class="editor-wrapper">
  <aside class="sidebar">
    <h2>Файлы конфигурации</h2>
    <ul class="file-list">
      <li v-for="file in app.files" :key="file.path"
          :class="{ active: app.currentFile?.path === file.path }"
          @click="app.loadFile(file.path)">
        {{ file.name }}
      </li>
    </ul>
  </aside>
  <section class="editor-container">
    <div class="editor-header">
      <span>{{ app.currentFile?.path || 'Файл не выбран' }}</span>
      <div class="editor-actions">
        <span :class="app.isValidJson === false ? 'error' : ''">
          {{ app.isValidJson === false ? 'Invalid JSON' : 'Valid JSON' }}
        </span>
        <button v-show="app.currentFile" @click="showDiff()" class="btn btn-sm">Diff</button>
        <button v-show="app.currentFile" @click="showBackups()" class="btn btn-sm">Резервные копии</button>
      </div>
    </div>
    <div ref="editorRef" id="editor"></div>
  </section>
  </div>
</template>
