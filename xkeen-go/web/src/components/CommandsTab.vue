<script setup>
import { ref, nextTick } from 'vue';
import { useAppStore } from '../stores/app.js';
import { InteractiveSession } from '../services/interactive.js';
import { AnsiUp } from 'ansi_up';

const app = useAppStore();
const ansiUp = new AnsiUp();
const executingCommand = ref('');

const categories = [
    { name: 'Управление', commands: [
        { name: '-start', description: 'Запуск' }, { name: '-stop', description: 'Остановка' },
        { name: '-restart', description: 'Перезапуск' }, { name: '-status', description: 'Статус' },
        { name: '-tpx', description: 'Порты, шлюз, протокол' }, { name: '-auto', description: 'Автозапуск вкл/выкл' },
        { name: '-d', description: 'Задержка автозапуска' }, { name: '-fd', description: 'Файловые дескрипторы' },
        { name: '-diag', description: 'Диагностика' }, { name: '-channel', description: 'Канал (Stable/Dev)' },
        { name: '-xray', description: 'Ядро Xray' }, { name: '-mihomo', description: 'Ядро Mihomo' },
        { name: '-ipv6', description: 'IPv6 вкл/выкл' }, { name: '-dns', description: 'DNS перенаправление' },
    ]},
    { name: 'Информация', commands: [
        { name: '-v', description: 'Версия XKeen' }, { name: '-about', description: 'О программе' },
        { name: '-ad', description: 'Поддержать разработчиков' }, { name: '-af', description: 'Обратная связь' },
    ]},
    { name: 'Обновление', commands: [
        { name: '-uk', description: 'Обновить XKeen' }, { name: '-ug', description: 'Обновить GeoFile' },
        { name: '-ux', description: 'Обновить Xray' }, { name: '-um', description: 'Обновить Mihomo' },
        { name: '-ugc', description: 'Автообновление GeoFile' },
    ]},
    { name: 'Порты', commands: [
        { name: '-ap', description: 'Добавить порт' }, { name: '-dp', description: 'Удалить порт' },
        { name: '-cp', description: 'Показать порты' },
    ]},
    { name: 'Исключённые порты', commands: [
        { name: '-ape', description: 'Добавить порт' }, { name: '-dpe', description: 'Удалить порт' },
        { name: '-cpe', description: 'Показать порты' },
    ]},
    { name: 'Бэкап XKeen', commands: [
        { name: '-kb', description: 'Создать копию' }, { name: '-kbr', description: 'Восстановить' },
    ]},
    { name: 'Бэкап Xray', commands: [
        { name: '-cb', description: 'Создать копию' }, { name: '-cbr', description: 'Восстановить' },
    ]},
    { name: 'Бэкап Mihomo', commands: [
        { name: '-mb', description: 'Создать копию' }, { name: '-mbr', description: 'Восстановить' },
    ]},
    { name: 'Модули', commands: [
        { name: '-modules', description: 'Перенести модули' }, { name: '-delmodules', description: 'Удалить модули' },
    ]},
    { name: 'Регистрация', commands: [
        { name: '-rrk', description: 'XKeen' }, { name: '-rrx', description: 'Xray' },
        { name: '-rrm', description: 'Mihomo' }, { name: '-ri', description: 'Автозапуск init.d' },
    ]},
    { name: 'Установка', commands: [
        { name: '-i', description: 'XKeen + Xray + GeoFile + Mihomo' },
        { name: '-io', description: 'OffLine установка' },
    ]},
    { name: 'Переустановка', commands: [
        { name: '-k', description: 'Переустановить XKeen' }, { name: '-g', description: 'Переустановить GeoFile' },
    ]},
    { name: 'Удаление компонентов', commands: [
        { name: '-dgs', description: 'GeoSite' }, { name: '-dgi', description: 'GeoIP' },
        { name: '-dx', description: 'Xray' }, { name: '-dm', description: 'Mihomo' },
        { name: '-dk', description: 'XKeen' }, { name: '-remove', description: 'Полная деинсталляция' },
    ]},
    { name: 'Удаление регистраций', commands: [
        { name: '-drk', description: 'XKeen' }, { name: '-drx', description: 'Xray' },
        { name: '-drm', description: 'Mihomo' },
    ]},
    { name: 'Удаление задач', commands: [
        { name: '-dgc', description: 'Автообновление GeoFile' },
    ]},
];

const dangerousCommands = [
    '-stop', '-restart', '-i', '-io', '-remove', '-dgs', '-dgi', '-dx', '-dm', '-dk', '-drk', '-drx', '-drm'
];

function isDangerous(cmd) { return dangerousCommands.includes(cmd); }

function getCommandInfo(name) {
    for (const cat of categories) {
        const cmd = cat.commands.find(c => c.name === name);
        if (cmd) return cmd;
    }
    return null;
}

function executeCommand(command) {
    if (isDangerous(command)) {
        const info = getCommandInfo(command);
        app.confirm.description = info?.description || `Выполнить команду ${command}`;
        app.confirm.onConfirm = () => doExecute(command);
        app.confirm.show = true;
    } else {
        doExecute(command);
    }
}

async function doExecute(command) {
    executingCommand.value = command;
    app.modal.error = ''; app.modal.output = ''; app.modal.command = command;
    app.modal.show = true; app.commandComplete = false; app.inputValue = '';

    try {
        await new Promise((resolve, reject) => {
            app.interactiveSession = new InteractiveSession(
                command,
                (msg) => handleStreamMessage(msg),
                (msg) => {
                    app.interactiveSession = null; app.commandComplete = true;
                    if (!msg.success && !app.modal.error) app.modal.error = `Команда завершилась с кодом ${msg.exitCode}`;
                    resolve();
                },
                () => {
                    app.interactiveSession = null; app.commandComplete = true;
                    reject(new Error('Ошибка WebSocket соединения'));
                }
            );
            app.interactiveSession.connect();
        });
    } catch (err) {
        app.modal.error = 'Ошибка выполнения команды: ' + err.message;
    } finally {
        executingCommand.value = '';
        app.commandComplete = true;
    }
}

function handleStreamMessage(msg) {
    if (msg.type === 'output') {
        let text = msg.text.replace(/\r/g, '');
        app.modal.output += ansiUp.ansi_to_html(text, { use_classes: false });
        scrollToBottom();
    } else if (msg.type === 'error') {
        let text = msg.text.replace(/\r/g, '');
        app.modal.error += (app.modal.error ? '\n' : '') + ansiUp.ansi_to_html(text, { use_classes: false });
        scrollToBottom();
    } else if (msg.type === 'complete') {
        app.commandComplete = true;
        if (!msg.success && !app.modal.error) app.modal.error = `Команда завершилась с кодом ${msg.exitCode}`;
    }
}

function scrollToBottom() {
    nextTick(() => {
        const el = document.getElementById('modal-output');
        if (el) el.scrollTop = el.scrollHeight;
    });
}

function isLoading(command) { return executingCommand.value === command; }
</script>

<template>
  <div class="commands-container">
    <div class="commands-grid">
      <div v-for="category in categories" :key="category.name" class="command-category-block">
        <h3 class="category-title">{{ category.name }}</h3>
        <div class="category-commands-list">
          <div v-for="cmd in category.commands" :key="cmd.name" class="command-item">
            <div class="command-info">
              <span class="command-name">{{ cmd.name }}</span>
              <span class="command-desc">{{ cmd.description }}</span>
            </div>
            <button class="btn"
                    :class="isDangerous(cmd.name) ? 'btn-danger' : 'btn-primary'"
                    @click="executeCommand(cmd.name)"
                    :disabled="isLoading(cmd.name)">
              {{ isLoading(cmd.name) ? 'Выполнение...' : (isDangerous(cmd.name) ? 'Выполнить' : 'Запустить') }}
            </button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
