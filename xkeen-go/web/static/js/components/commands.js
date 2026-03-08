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
                    { name: '-start', description: 'Запуск' },
                    { name: '-stop', description: 'Остановка' },
                    { name: '-restart', description: 'Перезапуск' },
                    { name: '-status', description: 'Статус работы' },
                    { name: '-tpx', description: 'Порты, шлюз и протокол' },
                    { name: '-auto', description: 'Вкл/выкл автозапуск' },
                    { name: '-d', description: 'Задержка автозапуска' },
                    { name: '-fd', description: 'Контроль файловых дескрипторов' },
                    { name: '-diag', description: 'Диагностика' },
                    { name: '-channel', description: 'Переключить канал (Stable/Dev)' },
                    { name: '-xray', description: 'Переключить на ядро Xray' },
                    { name: '-mihomo', description: 'Переключить на ядро Mihomo' },
                    { name: '-ipv6', description: 'Вкл/выкл IPv6' },
                    { name: '-dns', description: 'Вкл/выкл перенаправление DNS' }
                ]
            },
            {
                name: 'Обновление',
                commands: [
                    { name: '-uk', description: 'Обновить XKeen' },
                    { name: '-ug', description: 'Обновить GeoFile' },
                    { name: '-ux', description: 'Обновить Xray' },
                    { name: '-um', description: 'Обновить Mihomo' },
                    { name: '-ugc', description: 'Задача автообновления GeoFile' }
                ]
            },
            {
                name: 'Установка',
                commands: [
                    { name: '-i', description: 'Установка XKeen + Xray + GeoFile + Mihomo' },
                    { name: '-io', description: 'OffLine установка XKeen' }
                ]
            },
            {
                name: 'Резервное копирование | XKeen',
                commands: [
                    { name: '-kb', description: 'Создать резервную копию' },
                    { name: '-kbr', description: 'Восстановить из резервной копии' }
                ]
            },
            {
                name: 'Резервное копирование | Xray',
                commands: [
                    { name: '-cb', description: 'Создать резервную копию' },
                    { name: '-cbr', description: 'Восстановить из резервной копии' }
                ]
            },
            {
                name: 'Резервное копирование | Mihomo',
                commands: [
                    { name: '-mb', description: 'Создать резервную копию' },
                    { name: '-mbr', description: 'Восстановить из резервной копии' }
                ]
            },
            {
                name: 'Переустановка',
                commands: [
                    { name: '-k', description: 'Переустановить XKeen' },
                    { name: '-g', description: 'Переустановить GeoFile' }
                ]
            },
            {
                name: 'Порты прокси-клиента',
                commands: [
                    { name: '-ap', description: 'Добавить порт' },
                    { name: '-dp', description: 'Удалить порт' },
                    { name: '-cp', description: 'Показать порты' }
                ]
            },
            {
                name: 'Исключённые порты',
                commands: [
                    { name: '-ape', description: 'Добавить порт' },
                    { name: '-dpe', description: 'Удалить порт' },
                    { name: '-cpe', description: 'Показать порты' }
                ]
            },
            {
                name: 'Регистрация в системе',
                commands: [
                    { name: '-rrk', description: 'Регистрация XKeen' },
                    { name: '-rrx', description: 'Регистрация Xray' },
                    { name: '-rrm', description: 'Регистрация Mihomo' },
                    { name: '-ri', description: 'Автозапуск через init.d' }
                ]
            },
            {
                name: 'Удаление | Компоненты',
                commands: [
                    { name: '-dgs', description: 'Удалить GeoSite' },
                    { name: '-dgi', description: 'Удалить GeoIP' },
                    { name: '-dx', description: 'Удалить Xray' },
                    { name: '-dm', description: 'Удалить Mihomo' },
                    { name: '-dk', description: 'Удалить XKeen' },
                    { name: '-remove', description: 'Полная деинсталляция XKeen' }
                ]
            },
            {
                name: 'Удаление | Регистрации',
                commands: [
                    { name: '-drk', description: 'Удалить регистрацию XKeen' },
                    { name: '-drx', description: 'Удалить регистрацию Xray' },
                    { name: '-drm', description: 'Удалить регистрацию Mihomo' }
                ]
            },
            {
                name: 'Удаление | Задачи',
                commands: [
                    { name: '-dgc', description: 'Удалить автообновление GeoFile' }
                ]
            },
            {
                name: 'Модули',
                commands: [
                    { name: '-modules', description: 'Перенести модули' },
                    { name: '-delmodules', description: 'Удалить модули' }
                ]
            },
            {
                name: 'Информация',
                commands: [
                    { name: '-v', description: 'Версия XKeen' },
                    { name: '-about', description: 'О программе' },
                    { name: '-ad', description: 'Поддержать разработчиков' },
                    { name: '-af', description: 'Обратная связь' }
                ]
            }
        ],

        // Dangerous commands that require confirmation
        dangerousCommands: [
            '-stop', '-restart',
            '-i', '-io',
            '-remove', '-dgs', '-dgi', '-dx', '-dm', '-dk',
            '-drk', '-drx', '-drm'
        ],

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
