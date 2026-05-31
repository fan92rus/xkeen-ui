<script setup>
import { ref, onMounted, onUnmounted, watch } from 'vue';
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

watch(() => app.editorLoadContent, (content) => {
    if (content) loadText(content);
});

onMounted(() => {
    createEditor('// Select a file to edit', editorRef.value);
    ready = true;
    if (pendingFile) { loadContent(pendingFile); pendingFile = null; }
});

onUnmounted(() => {
    if (instance) instance.destroy();
});

defineExpose({ getContent, save, diff, loadText });
</script>

<template>
  <div class="editor-container">
    <div ref="editorRef" id="editor"></div>
  </div>
</template>
