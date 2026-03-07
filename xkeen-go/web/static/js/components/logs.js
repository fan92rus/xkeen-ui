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
