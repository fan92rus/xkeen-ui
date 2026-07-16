<script setup>
import { ref, onMounted, onUnmounted, watch, inject, nextTick } from 'vue';
import { EditorView, basicSetup } from 'codemirror';
import { json } from '@codemirror/lang-json';
import { yaml } from '@codemirror/lang-yaml';
import { oneDark } from '@codemirror/theme-one-dark';
import { useAppStore } from '../stores/app.js';
import { useI18nStore } from '../stores/i18n.js';

const app = useAppStore();
const i18n = useI18nStore();
const editorRef = ref(null);
const loading = ref(false);
let instance = null;
let ready = false;
let pendingFile = null;

const isDark = inject('isDark', ref(true));

function getLanguageExtension(filePath) {
    if (!filePath) return json();
    const lower = filePath.toLowerCase();
    return (lower.endsWith('.yml') || lower.endsWith('.yaml')) ? yaml() : json();
}

function createEditor(content, parent, filePath) {
    if (instance) instance.destroy();
    const extensions = [
        basicSetup,
        getLanguageExtension(filePath),
        EditorView.lineWrapping,
        EditorView.theme({ '&': { height: '100%' }, '.cm-scroller': { overflow: 'auto' } })
    ];
    if (isDark.value) extensions.push(oneDark);
    instance = new EditorView({ doc: content, extensions, parent });
    return instance;
}

function loadContent(file) {
    if (!editorRef.value) return;
    loading.value = true;
    createEditor(file.content, editorRef.value, file.path);
    app.isValidJson = file.valid;
    loading.value = false;
}

function getContent() {
    return instance ? instance.state.doc.toString() : '';
}

async function save() {
    await app.saveFile(getContent());
}

function diff() {
    app.openDiffModal(getContent(), app.lastSavedContent || '');
}

function loadText(content) {
    if (instance && content) {
        instance.dispatch({ changes: { from: 0, to: instance.state.doc.length, insert: content } });
    }
}

watch(() => app.currentFile, (file) => {
    if (!file) return;
    if (ready) loadContent(file);
    else pendingFile = file;
});

// Recreate editor when theme changes
watch(isDark, () => {
    if (instance && ready) {
        const content = getContent();
        const file = app.currentFile;
        createEditor(content, editorRef.value, file?.path);
    }
});

watch(() => app.editorLoadContent, async (content) => {
    if (content) {
        loadText(content);
        await nextTick();
        app.editorLoadContent = null;
    }
});

onMounted(async () => {
    ready = true;
    // Reload from backend to ensure fresh content (fixes stale-cache after
    // Subscriptions 'Apply' rewrites files on disk).
    if (app.currentFile && app.currentFile.path) {
        try {
            await app.loadFile(app.currentFile.path);
        } catch {
            // If reload fails (file deleted, etc.), keep whatever we have
        }
    }
    const file = pendingFile || app.currentFile;
    if (file) { loadContent(file); pendingFile = null; }
    else createEditor('// Select a file to edit', editorRef.value);
});

onUnmounted(() => {
    if (instance) instance.destroy();
});

defineExpose({ getContent, save, diff, loadText });
</script>

<template>
  <div class="editor-container">
    <div v-if="loading" class="editor-loading">{{ i18n.t('editor.loading') }}</div>
    <div v-show="!loading" id="editor" ref="editorRef" />
  </div>
</template>
