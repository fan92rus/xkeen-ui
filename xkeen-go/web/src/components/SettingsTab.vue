<script setup>
import { ref, onMounted } from 'vue';
import { useAppStore } from '../stores/app.js';
import * as sub from '../services/subscription.js';

const app = useAppStore();

const autoApply = ref({ enabled: false, cron: '0 */6 * * *', next_run: '' });
const autoApplySaving = ref(false);

async function loadAutoApply() {
  try {
    const d = await sub.getAutoApply();
    autoApply.value = { enabled: d.enabled, cron: d.cron || '0 */6 * * *', next_run: d.next_run || '' };
  } catch { /* no subscriptions configured yet */ }
}

async function saveAutoApply() {
  autoApplySaving.value = true;
  try {
    const d = await sub.updateAutoApply({ enabled: autoApply.value.enabled, cron: autoApply.value.cron });
    autoApply.value.next_run = d.next_run || '';
    app.showToast(autoApply.value.enabled ? `Автообновление: ${autoApply.value.cron}` : 'Автообновление отключено', 'success');
  } catch (e) {
    app.showToast(e.message || 'Ошибка', 'error');
  } finally {
    autoApplySaving.value = false;
  }
}

import { fmtTime as fmtNextRun } from '../utils/format.js';

onMounted(loadAutoApply);
</script>

<template>
  <div class="settings-container">
    <div class="settings-grid">
      <!-- Mode -->
      <div class="settings-section">
        <h3>Режим</h3>
        <div class="setting-row">
          <label>Активный режим:</label>
          <div class="mode-selector">
            <button @click="app.switchMode('xray')"
                    :class="{ active: app.currentMode === 'xray' }"
                    :disabled="!app.xrayAvailable" class="btn btn-mode">
              <span class="mode-icon">X</span> Xray
            </button>
            <button @click="app.switchMode('mihomo')"
                    :class="{ active: app.currentMode === 'mihomo' }"
                    :disabled="!app.mihomoAvailable" class="btn btn-mode">
              <span class="mode-icon">M</span> Mihomo
            </button>
          </div>
        </div>
        <div class="setting-info">
          <p><strong>Текущий режим:</strong> {{ app.currentMode === 'mihomo' ? 'Mihomo (YAML конфигурации)' : 'Xray (JSON конфигурации)' }}</p>
          <p v-show="!app.mihomoAvailable" class="setting-warning-inline">Директория конфигураций Mihomo не найдена</p>
        </div>
        <div class="setting-info">
          <p><strong>Описание режимов:</strong></p>
          <ul>
            <li><strong>Xray</strong> — Использует ядро Xray с JSON файлами конфигурации</li>
            <li><strong>Mihomo</strong> — Использует ядро Mihomo (Clash.Meta) с YAML файлами конфигурации</li>
          </ul>
        </div>
      </div>

      <!-- Logging -->
      <div class="settings-section">
        <h3>Логирование</h3>
        <div class="setting-row">
          <label for="logLevel">Уровень логов:</label>
          <select id="logLevel" v-model="app.xraySettings.logLevel" @change="app.updateLogLevel()">
            <option v-for="level in app.xraySettings.logLevels" :key="level" :value="level">{{ level.toUpperCase() }}</option>
          </select>
        </div>
        <div class="setting-info">
          <p><strong>Описание уровней:</strong></p>
          <ul>
            <li><strong>DEBUG</strong> — Подробная отладочная информация</li>
            <li><strong>INFO</strong> — Общая информация о работе</li>
            <li><strong>WARNING</strong> — Только предупреждения</li>
            <li><strong>ERROR</strong> — Только ошибки</li>
            <li><strong>NONE</strong> — Отключить логирование</li>
          </ul>
        </div>
        <div class="setting-info">
          <p><strong>Текущие файлы логов:</strong></p>
          <p>Лог доступа: <code>{{ app.xraySettings.accessLog }}</code></p>
          <p>Лог ошибок: <code>{{ app.xraySettings.errorLog }}</code></p>
        </div>
        <div class="setting-warning" v-show="app.xraySettings.logLevel === 'none'">
          <p>Внимание: Логирование отключено. Логи не будут записываться в файлы.</p>
        </div>
      </div>

      <!-- Updates -->
      <div class="settings-section">
        <h3>Обновления</h3>
        <div class="update-status">
          <div class="setting-row">
            <label>Текущая версия:</label>
            <span class="version-info">{{ app.currentVersion }}</span>
            <span v-show="app.updateInfo.is_prerelease" class="dev-badge">dev</span>
          </div>
          <div class="setting-row" v-show="app.updateInfo.latest_version">
            <label>Последняя версия:</label>
            <span class="version-info">{{ app.updateInfo.latest_version }}</span>
            <span v-show="app.updateInfo.is_prerelease" class="dev-badge">dev</span>
            <a v-show="app.updateInfo.release_url" :href="app.updateInfo.release_url" target="_blank" class="release-link-small">примечания</a>
          </div>
          <div v-if="app.updateInfo.update_available" class="update-available">
            <p>{{ app.updateInfo.is_prerelease ? 'Доступна новая dev версия!' : 'Доступна новая версия!' }}</p>
          </div>
          <p v-if="!app.updateInfo.update_available && app.updateInfo.latest_version" class="up-to-date">
            {{ app.updateInfo.is_prerelease ? 'Установлена последняя dev версия.' : 'Установлена последняя версия.' }}
          </p>
        </div>
        <div class="setting-row checkbox-row">
          <label class="checkbox-label">
            <input type="checkbox" v-model="app.checkDevUpdates">
            <span>Проверять dev обновления</span>
          </label>
          <span class="setting-hint">Development-сборки содержат последние функции, но могут быть нестабильны</span>
        </div>
        <div class="update-actions">
          <button @click="app.checkUpdate()" :disabled="app.updateChecking || app.updating" class="btn">
            {{ app.updateChecking ? 'Проверка...' : 'Проверить обновления' }}
          </button>
          <button v-if="app.updateInfo.update_available" @click="app.startUpdate()" :disabled="app.updating" class="btn btn-primary">
            {{ app.updating ? 'Обновление...' : 'Обновить' }}
          </button>
        </div>
        <div v-show="app.updating" class="update-progress">
          <div class="progress-bar">
            <div class="progress-fill" :style="'width: ' + app.updateProgress + '%'"></div>
          </div>
          <p class="progress-status">{{ app.updateStatus }}</p>
        </div>
      </div>

      <!-- Security -->
      <div class="settings-section">
        <h3>Безопасность</h3>
        <div class="password-change-form">
          <div class="setting-row">
            <label for="currentPassword">Текущий пароль:</label>
            <input type="password" id="currentPassword" v-model="app.passwordChange.currentPassword"
                   placeholder="Введите текущий пароль" autocomplete="current-password">
          </div>
          <div class="setting-row">
            <label for="newPassword">Новый пароль:</label>
            <input type="password" id="newPassword" v-model="app.passwordChange.newPassword"
                   placeholder="Минимум 8 символов" autocomplete="new-password">
          </div>
          <div class="setting-row">
            <label for="confirmPassword">Подтверждение пароля:</label>
            <input type="password" id="confirmPassword" v-model="app.passwordChange.confirmPassword"
                   placeholder="Повторите новый пароль" autocomplete="new-password">
          </div>
          <div class="setting-info">
            <p><strong>Требования к паролю:</strong></p>
            <ul><li>Минимум 8 символов</li><li>Должен отличаться от текущего пароля</li></ul>
          </div>
          <div v-show="app.passwordChange.error" class="setting-error">
            <p>{{ app.passwordChange.error }}</p>
          </div>
          <div v-show="app.passwordChange.success" class="setting-success">
            <p>Пароль успешно изменён!</p>
          </div>
          <div class="update-actions">
            <button @click="app.changePassword()" :disabled="app.passwordChange.loading" class="btn btn-primary">
              {{ app.passwordChange.loading ? 'Изменение...' : 'Изменить пароль' }}
            </button>
            <button @click="app.clearPasswordForm()" :disabled="app.passwordChange.loading" class="btn">Очистить</button>
          </div>
        </div>
      </div>

      <!-- Auto-Apply Subscriptions -->
      <div class="settings-section">
        <h3>Автообновление подписки</h3>
        <div class="setting-row checkbox-row">
          <label class="checkbox-label">
            <input type="checkbox" v-model="autoApply.enabled">
            <span>Включить автоматическое обновление</span>
          </label>
          <span class="setting-hint">Автоматически: обновить прокси → фильтровать → записать конфиги → перезапустить xkeen</span>
        </div>
        <div class="setting-row">
          <label>Расписание (cron):</label>
          <input type="text" v-model="autoApply.cron" placeholder="0 */6 * * *"
                 :disabled="!autoApply.enabled" class="cron-input">
        </div>
        <div class="setting-info">
          <p><strong>Формат:</strong> <code>мин час день месяц день_недели</code></p>
          <ul>
            <li><code>0 */6 * * *</code> — каждые 6 часов</li>
            <li><code>0 0 * * *</code> — раз в сутки в полночь</li>
            <li><code>*/30 * * * *</code> — каждые 30 минут</li>
            <li><code>0 8,20 * * *</code> — в 08:00 и 20:00</li>
          </ul>
        </div>
        <div v-if="autoApply.enabled && autoApply.next_run" class="setting-info">
          <p>Следующий запуск: <strong>{{ fmtNextRun(autoApply.next_run) }}</strong></p>
        </div>
        <div class="update-actions">
          <button @click="saveAutoApply()" :disabled="autoApplySaving" class="btn btn-primary">
            {{ autoApplySaving ? 'Сохранение...' : 'Сохранить' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
