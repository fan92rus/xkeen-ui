// components/commands.js - Commands tab with categorized XKeen commands

import { InteractiveSession } from '../services/interactive.js';
import { AnsiUp } from 'https://esm.sh/ansi_up@6.0.2';

const ansi_up = new AnsiUp();

function commandsComponent() {
    return {
        // State
        executingCommand: '',

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
            this.$store.app.commandComplete = false;
            this.$store.app.inputValue = '';

            try {
                await this.executeInteractive(command);
            } catch (err) {
                this.$store.app.modal.error = 'Failed to execute command: ' + err.message;
            } finally {
                this.executingCommand = '';
                this.$store.app.commandComplete = true;
            }
        },

        executeInteractive(command) {
            return new Promise((resolve, reject) => {
                this.$store.app.interactiveSession = new InteractiveSession(
                    command,
                    (msg) => this.handleStreamMessage(msg),
                    (msg) => {
                        this.$store.app.interactiveSession = null;
                        this.$store.app.commandComplete = true;
                        if (!msg.success && !this.$store.app.modal.error) {
                            this.$store.app.modal.error = `Command failed with exit code ${msg.exitCode}`;
                        }
                        resolve();
                    },
                    (error) => {
                        this.$store.app.interactiveSession = null;
                        this.$store.app.commandComplete = true;
                        reject(new Error('WebSocket connection error'));
                    }
                );
                this.$store.app.interactiveSession.connect();
            });
        },

        handleStreamMessage(msg) {
            if (msg.type === 'output') {
                const html = ansi_up.ansi_to_html(msg.text, { use_classes: false });
                this.$store.app.modal.output += html + '\n';
                this.scrollToBottom();
            } else if (msg.type === 'error') {
                const html = ansi_up.ansi_to_html(msg.text, { use_classes: false });
                this.$store.app.modal.error += (this.$store.app.modal.error ? '\n' : '') + html;
                this.scrollToBottom();
            } else if (msg.type === 'complete') {
                this.$store.app.commandComplete = true;
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
