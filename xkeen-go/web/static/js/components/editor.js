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
