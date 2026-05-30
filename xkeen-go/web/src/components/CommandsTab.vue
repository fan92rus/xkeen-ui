<script setup>
import { ref, nextTick } from 'vue';
import { useAppStore } from '../stores/app.js';
import { InteractiveSession } from '../services/interactive.js';
import { AnsiUp } from 'ansi_up';

const app = useAppStore();
const ansiUp = new AnsiUp();
const executingCommand = ref('');

const categories = [
    { name: 'Управление прокси-клиентом', commands: [
        { name: '-start', description: 'Запуск' }, { name: '-stop', description: 'Остановка' },
        { name: '-restart', description: 'Перезапуск' }, { name: '-status', description: 'Статус работы' },
        { name: '-tpx', description: 'Порты, шлюз и протокол' }, { name: '-auto', description: 'Вкл/выкл автозапуск' },
        { name: '-d', description: 'Задержка автозапуска' }, { name: '-fd', description: 'Контроль файловых дескрипторов' },
        { name: '-diag', description: 'Диагностика' }, { name: '-channel', description: 'Переключить канал (Stable/Dev)' },
        { name: '-xray', description: 'Переключить на ядро Xray' }, { name: '-mihomo', description: 'Переключить на ядро Mihomo' },
        { name: '-ipv6', description: 'Вкл/выкл IPv6' }, { name: '-dns', description: 'Вкл/выкл перенаправление DNS' },
    ]},
    { name: 'Обновление', commands: [
        { name: '-uk', description: 'Обновить XKeen' }, { name: '-ug', description: 'Обновить GeoFile' },
        { name: '-ux', description: 'Обновить Xray' }, { name: '-um', description: 'Обновить Mihomo' },
        { name: '-ugc', description: 'Задача автообновления GeoFile' },
    ]},
    { name: 'Установка', commands: [
        { name: '-i', description: 'Установка XKeen + Xray + GeoFile + Mihomo' },
        { name: '-io', description: 'OffLine установка XKeen' },
    ]},
    { name: 'Резервное копирование | XKeen', commands: [
        { name: '-kb', description: 'Создать резервную копию' }, { name: '-kbr', description: 'Восстановить из резервной копии' },
    ]},
    { name: 'Резервное копирование | Xray', commands: [
        { name: '-cb', description: 'Создать резервную копию' }, { name: '-cbr', description: 'Восстановить из резервной копии' },
    ]},
    { name: 'Резервное копирование | Mihomo', commands: [
        { name: '-mb', description: 'Создать резервную копию' }, { name: '-mbr', description: 'Восстановить из резервной копии' },
    ]},
    { name: 'Переустановка', commands: [
        { name: '-k', description: 'Переустановить XKeen' }, { name: '-g', description: 'Переустановить GeoFile' },
    ]},
    { name: 'Порты прокси-клиента', commands: [
        { name: '-ap', description: 'Добавить порт' }, { name: '-dp', description: 'Удалить порт' },
        { name: '-cp', description: 'Показать порты' },
    ]},
    { name: 'Исключённые порты', commands: [
        { name: '-ape', description: 'Добавить порт' }, { name: '-dpe', description: 'Удалить порт' },
        { name: '-cpe', description: 'Показать порты' },
    ]},
    { name: 'Регистрация в системе', commands: [
        { name: '-rrk', description: 'Регистрация XKeen' }, { name: '-rrx', description: 'Регистрация Xray' },
        { name: '-rrm', description: 'Регистрация Mihomo' }, { name: '-ri', description: 'Автозапуск через init.d' },
    ]},
    { name: 'Удаление | Компоненты', commands: [
        { name: '-dgs', description: 'Удалить GeoSite' }, { name: '-dgi', description: 'Удалить GeoIP' },
        { name: '-dx', description: 'Удалить Xray' }, { name: '-dm', description: 'Удалить Mihomo' },
        { name: '-dk', description: 'Удалить XKeen' }, { name: '-remove', description: 'Полная деинсталляция XKeen' },
    ]},
    { name: 'Удаление | Регистрации', commands: [
        { name: '-drk', description: 'Удалить регистрацию XKeen' }, { name: '-drx', description: 'Удалить регистрацию Xray' },
        { name: '-drm', description: 'Удалить регистрацию Mihomo' },
    ]},
    { name: 'Удаление | Задачи', commands: [
        { name: '-dgc', description: 'Удалить автообновление GeoFile' },
    ]},
    { name: 'Модули', commands: [
        { name: '-modules', description: 'Перенести модули' }, { name: '-delmodules', description: 'Удалить модули' },
    ]},
    { name: 'Информация', commands: [
        { name: '-v', description: 'Версия XKeen' }, { name: '-about', description: 'О программе' },
        { name: '-ad', description: 'Поддержать разработчиков' }, { name: '-af', description: 'Обратная связь' },
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
