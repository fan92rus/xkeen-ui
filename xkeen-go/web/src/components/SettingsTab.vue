<script setup>
import { ref, onMounted, inject } from 'vue';
import { useAppStore } from '../stores/app.js';
import * as sub from '../services/subscription.js';
import * as metrics from '../services/metrics.js';
import * as installApi from '../services/install.js';

const app = useAppStore();
const reloadMetricsState = inject('reloadMetricsState', () => {});

const autoApply = ref({ enabled: false, cron: '0 */6 * * *', next_run: '' });
const autoApplySaving = ref(false);

// Metrics settings
const metricsPort = ref(0);
const metricsLoading = ref(false);
const metricsSaving = ref(false);

async function loadMetricsPort() {
	metricsLoading.value = true;
	try {
		const d = await metrics.getMetricsPort();
		metricsPort.value = d.metrics_port || 0;
	} catch { /* ignore */ }
	finally {
		metricsLoading.value = false;
	}
}

async function saveMetricsPort() {
	metricsSaving.value = true;
	try {
		await metrics.updateMetricsPort(metricsPort.value);
		app.showToast(
			metricsPort.value > 0 ? `Метрики включены: порт ${metricsPort.value}` : 'Метрики отключены',
			'success',
		);
		reloadMetricsState();
	} catch (e) {
		app.showToast(e.message || 'Ошибка сохранения', 'error');
	} finally {
		metricsSaving.value = false;
	}
}

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

const sections = [
  { id: 'mode', icon: '⚡', label: 'Режим' },
  { id: 'logging', icon: '📋', label: 'Логирование' },
  { id: 'updates', icon: '🔄', label: 'Обновления' },
  { id: 'security', icon: '🔒', label: 'Безопасность' },
  { id: 'autoapply', icon: '📅', label: 'Подписки' },
  { id: 'metrics', icon: '📊', label: 'Метрики' },
  { id: 'awg', icon: '🔗', label: 'AmneziaWG' },
];

const awg = ref({ installed: false, hasInitScript: false, interfaces: '', installing: false, initSaving: false, error: '', progress: '' });

async function checkAWG() {
  try {
    const s = await installApi.getAWGStatus();
    awg.value.installed = s.installed;
    awg.value.hasInitScript = s.has_init_script;
    awg.value.interfaces = s.interfaces || '';
  } catch (e) { /* not available */ }
}

async function installAWG() {
  if (awg.value.installed) { return; }
  awg.value.installing = true;
  awg.value.error = '';
  awg.value.progress = '';
  try {
    await installApi.installAWG({
      onProgress: (data) => {
        awg.value.progress = data.status || '';
      },
      onComplete: () => {
        awg.value.installed = true;
        awg.value.installing = false;
        awg.value.progress = '';
        app.showToast('AmneziaWG успешно установлен!', 'success');
        checkAWG();
      },
      onError: (err) => {
        awg.value.installing = false;
        awg.value.error = err.error || err.message || 'Ошибка установки';
        awg.value.progress = '';
      },
    });
  } catch (e) {
    awg.value.installing = false;
    awg.value.error = e.message || 'Ошибка установки';
    awg.value.progress = '';
  }
}

async function setupAWGInit() {
  awg.value.initSaving = true;
  try {
    const r = await installApi.setupAWGInit();
    if (r.success) {
      app.showToast('Init-скрипт AWG создан', 'success');
      checkAWG();
    } else {
      app.showToast(r.message || 'Ошибка создания init-скрипта', 'error');
    }
  } catch (e) {
    app.showToast(e.message || 'Ошибка', 'error');
  } finally {
    awg.value.initSaving = false;
  }
}

onMounted(() => {
	loadAutoApply();
	loadMetricsPort();
	app.loadXraySettings();
  checkAWG();
});
</script>

<template>
  <div class="s">
    <div class="s-layout">
      <!-- Nav -->
      <nav class="s-nav">
        <a v-for="sec in sections" :key="sec.id" :href="'#' + sec.id" class="s-nav-item">
          <span class="s-nav-icon">{{ sec.icon }}</span>
          {{ sec.label }}
        </a>
      </nav>

      <!-- Content -->
      <div class="s-content">

        <!-- Mode -->
        <section :id="sections[0].id" class="s-section">
          <h2 class="s-title">{{ sections[0].icon }} Режим</h2>
          <div class="s-block">
            <div class="s-row">
              <div class="s-row-main">
                <div class="s-row-label">Ядро</div>
                <div class="s-row-desc">Активное ядро для обработки трафика</div>
              </div>
              <div class="mode-selector">
                <button @click="app.switchMode('xray')"
                        :class="{ active: app.currentMode === 'xray' }"
                        :disabled="!app.xrayAvailable" class="btn-mode">
                  <span class="mode-icon">X</span> Xray
                </button>
                <button @click="app.switchMode('mihomo')"
                        :class="{ active: app.currentMode === 'mihomo' }"
                        :disabled="!app.mihomoAvailable" class="btn-mode">
                  <span class="mode-icon">M</span> Mihomo
                </button>
              </div>
            </div>
            <div class="s-row" v-show="!app.mihomoAvailable">
              <div class="s-row-main">
                <div class="s-row-label s-muted">Mihomo</div>
                <div class="s-row-desc s-warn">Директория конфигураций не найдена</div>
              </div>
            </div>
          </div>
        </section>

        <!-- Logging -->
        <section :id="sections[1].id" class="s-section">
          <h2 class="s-title">📋 Логирование</h2>
          <div class="s-block">
            <div class="s-row">
              <div class="s-row-main">
                <div class="s-row-label">Уровень логов</div>
                <div class="s-row-desc">
                  Access: <code>{{ app.xraySettings.accessLog }}</code> · Error: <code>{{ app.xraySettings.errorLog }}</code>
                </div>
              </div>
              <select v-model="app.xraySettings.logLevel" @change="app.updateLogLevel()" class="s-select">
                <option v-for="level in app.xraySettings.logLevels" :key="level" :value="level">{{ level.toUpperCase() }}</option>
              </select>
            </div>
          </div>
        </section>

        <!-- Updates -->
        <section :id="sections[2].id" class="s-section">
          <h2 class="s-title">🔄 Обновления</h2>
          <div class="s-block">
            <div class="s-row">
              <div class="s-row-main">
                <div class="s-row-label">Текущая версия</div>
              </div>
              <div class="s-row-right">
                <span class="ver-badge">{{ app.currentVersion }}</span>
                <span v-show="app.updateInfo.is_prerelease" class="dev-tag">dev</span>
              </div>
            </div>
            <div class="s-row" v-show="app.updateInfo.latest_version">
              <div class="s-row-main">
                <div class="s-row-label">Последняя версия</div>
              </div>
              <div class="s-row-right">
                <span class="ver-badge">{{ app.updateInfo.latest_version }}</span>
                <a v-show="app.updateInfo.release_url" :href="app.updateInfo.release_url" target="_blank" class="s-link">примечания</a>
              </div>
            </div>
            <div v-if="app.updateInfo.update_available" class="s-callout s-callout-info">
              Доступна новая{{ app.updateInfo.is_prerelease ? ' dev' : '' }} версия!
            </div>
            <p v-else-if="app.updateInfo.latest_version" class="s-ok">✓ Установлена последняя версия</p>
            <div class="s-row">
              <div class="s-row-main">
                <div class="s-row-label">Dev-канал</div>
                <div class="s-row-desc">Development-сборки с последними функциями, могут быть нестабильны</div>
              </div>
              <label class="toggle">
                <input type="checkbox" v-model="app.checkDevUpdates">
                <span class="toggle-slider"></span>
              </label>
            </div>
            <div class="s-row s-row-actions">
              <button @click="app.checkUpdate()" :disabled="app.updateChecking || app.updating" class="btn">
                {{ app.updateChecking ? 'Проверка...' : 'Проверить обновления' }}
              </button>
              <button v-if="app.updateInfo.update_available" @click="app.startUpdate()" :disabled="app.updating" class="btn btn-primary">
                {{ app.updating ? 'Обновление...' : 'Обновить' }}
              </button>
            </div>
            <div v-show="app.updating" class="s-progress">
              <div class="s-progress-bar"><div :style="'width:' + app.updateProgress + '%'"></div></div>
              <span class="s-progress-text">{{ app.updateStatus }}</span>
            </div>
          </div>
        </section>

        <!-- Security -->
        <section :id="sections[3].id" class="s-section">
          <h2 class="s-title">🔒 Безопасность</h2>
          <div class="s-block">
            <div class="pw-grid">
              <input type="password" v-model="app.passwordChange.currentPassword"
                     placeholder="Текущий пароль" autocomplete="current-password" class="s-input">
              <input type="password" v-model="app.passwordChange.newPassword"
                     placeholder="Новый пароль (мин. 8 символов)" autocomplete="new-password" class="s-input">
              <input type="password" v-model="app.passwordChange.confirmPassword"
                     placeholder="Подтверждение пароля" autocomplete="new-password" class="s-input">
            </div>
            <div v-show="app.passwordChange.error" class="s-callout s-callout-err">{{ app.passwordChange.error }}</div>
            <div v-show="app.passwordChange.success" class="s-callout s-callout-ok">Пароль успешно изменён!</div>
            <div class="s-row s-row-actions">
              <button @click="app.changePassword()" :disabled="app.passwordChange.loading" class="btn btn-primary">
                {{ app.passwordChange.loading ? 'Изменение...' : 'Изменить пароль' }}
              </button>
              <button @click="app.clearPasswordForm()" :disabled="app.passwordChange.loading" class="btn">Очистить</button>
            </div>
          </div>
        </section>

        <!-- Auto-Apply -->
        <section :id="sections[4].id" class="s-section">
          <h2 class="s-title">📅 Автообновление подписки</h2>
          <div class="s-block">
            <div class="s-row">
              <div class="s-row-main">
                <div class="s-row-label">Автоматическое обновление</div>
                <div class="s-row-desc">Обновить прокси → фильтровать → записать конфиги → перезапустить</div>
              </div>
              <label class="toggle">
                <input type="checkbox" v-model="autoApply.enabled">
                <span class="toggle-slider"></span>
              </label>
            </div>
            <div class="s-row">
              <div class="s-row-main">
                <div class="s-row-label">Расписание</div>
                <div class="s-row-desc s-cron-hint">
                  <code>0 */6 * * *</code> каждые 6ч &nbsp;·&nbsp;
                  <code>0 0 * * *</code> ежедневно &nbsp;·&nbsp;
                  <code>*/30 * * * *</code> каждые 30м
                </div>
              </div>
              <input type="text" v-model="autoApply.cron" placeholder="0 */6 * * *"
                     :disabled="!autoApply.enabled" class="s-input s-input-mono">
            </div>
            <div v-if="autoApply.enabled && autoApply.next_run" class="s-callout s-callout-info">
              Следующий запуск: {{ fmtNextRun(autoApply.next_run) }}
            </div>
            <div class="s-row s-row-actions">
              <button @click="saveAutoApply()" :disabled="autoApplySaving" class="btn btn-primary">
                {{ autoApplySaving ? 'Сохранение...' : 'Сохранить' }}
              </button>
            </div>
          </div>
        </section>

        <!-- Metrics -->
        <section :id="sections[5].id" class="s-section">
          <h2 class="s-title">📊 Метрики Xray</h2>
          <div class="s-block">
            <div class="s-row">
              <div class="s-row-main">
                <div class="s-row-label">Сбор метрик</div>
                <div class="s-row-desc">Трафик и состояние прокси во вкладке «Монитор»</div>
              </div>
              <label class="toggle">
                <input type="checkbox" :checked="metricsPort > 0" @change="$event.target.checked ? metricsPort = 11111 : metricsPort = 0">
                <span class="toggle-slider"></span>
              </label>
            </div>
            <div class="s-row" v-if="metricsPort > 0">
              <div class="s-row-main">
                <div class="s-row-label">Порт</div>
                <div class="s-row-desc">Слушает <code>127.0.0.1:{{ metricsPort }}</code>. Перезапустите Xray для применения.</div>
              </div>
              <input type="number" v-model.number="metricsPort" min="1" max="65535"
                     :disabled="metricsSaving" class="s-input s-input-port">
            </div>
            <div class="s-row s-row-actions">
              <button @click="saveMetricsPort()" :disabled="metricsSaving" class="btn btn-primary">
                {{ metricsSaving ? 'Сохранение...' : 'Сохранить' }}
              </button>
            </div>
          </div>
        </section>

        <!-- AWG -->
        <section :id="sections[6].id" class="s-section">
          <h2 class="s-title">🔗 AmneziaWG</h2>

          <!-- Install -->
          <div class="s-block">
            <div class="s-row">
              <div class="s-row-main">
                <div class="s-row-label">amneziawg-go + amneziawg-tools</div>
                <div class="s-row-desc">Userspace AWG для Keenetic. Поддерживает WARP и другие AWG-конфиги.</div>
              </div>
              <div v-if="awg.installed" class="badge badge-success">Установлен</div>
              <div v-else class="badge badge-muted">Не установлен</div>
            </div>
            <div v-if="awg.progress" class="s-callout s-callout-info">{{ awg.progress }}</div>
            <div v-if="awg.error" class="s-callout s-callout-err">{{ awg.error }}</div>
            <div class="s-row s-row-actions">
              <button v-if="!awg.installed && !awg.installing" @click="installAWG()" class="btn btn-primary">
                Установить AmneziaWG
              </button>
              <button v-if="awg.installing" disabled class="btn btn-primary">Установка...</button>
            </div>
          </div>

          <!-- Init script -->
          <div class="s-block" v-if="awg.installed">
            <div class="s-row">
              <div class="s-row-main">
                <div class="s-row-label">Init-скрипт</div>
                <div class="s-row-desc">Автозапуск AWG при загрузке роутера</div>
              </div>
              <div v-if="awg.hasInitScript" class="badge badge-success">Создан</div>
              <div v-else class="badge badge-muted">Не создан</div>
            </div>
            <div v-if="awg.interfaces" class="s-callout s-callout-info">
              <pre style="margin:0;font-size:12px">{{ awg.interfaces }}</pre>
            </div>
            <div class="s-row s-row-actions">
              <button v-if="!awg.hasInitScript" @click="setupAWGInit()" :disabled="awg.initSaving" class="btn btn-primary">
                {{ awg.initSaving ? 'Создание...' : 'Создать init-скрипт' }}
              </button>
              <button v-if="awg.hasInitScript" @click="setupAWGInit()" :disabled="awg.initSaving" class="btn">
                {{ awg.initSaving ? 'Обновление...' : 'Обновить init-скрипт' }}
              </button>
            </div>
          </div>
        </section>

      </div>
    </div>
  </div>
</template>

<style scoped>
.s {
  height: 100%;
  overflow-y: auto;
}

/* ── Layout: sidebar nav + content ── */
.s-layout {
  display: flex;
  max-width: 960px;
  margin: 0 auto;
  min-height: 100%;
}

/* ── Sidebar nav ── */
.s-nav {
  width: 180px;
  flex-shrink: 0;
  padding: 14px 0 14px 14px;
  position: sticky;
  top: 0;
  align-self: flex-start;
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.s-nav-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 7px 12px;
  border-radius: 8px;
  font-size: 13px;
  color: var(--text-gray);
  text-decoration: none;
  transition: background 0.1s;
}
.s-nav-item:hover {
  background: var(--menu-active-item);
  color: var(--primary-text);
}
.s-nav-icon {
  font-size: 14px;
  width: 20px;
  text-align: center;
}

/* ── Content ── */
.s-content {
  flex: 1;
  padding: 14px 24px 40px;
  min-width: 0;
}

/* ── Section ── */
.s-section {
  margin-bottom: 8px;
}
.s-title {
  font-size: 16px;
  font-weight: 700;
  color: var(--primary-text);
  margin: 20px 0 6px;
  padding: 0 4px;
}
.s-title:first-child {
  margin-top: 0;
}

/* ── Block (white area) ── */
.s-block {
  background: var(--menu-background);
  border: 1px solid var(--menu-border);
  border-radius: 10px;
  overflow: hidden;
}

/* ── Row ── */
.s-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 10px 16px;
  border-bottom: 1px solid var(--menu-border);
}
.s-row:last-child {
  border-bottom: none;
}
.s-row-main {
  min-width: 0;
  flex: 1;
}
.s-row-label {
  font-size: 13px;
  font-weight: 500;
  color: var(--primary-text);
}
.s-row-desc {
  font-size: 12px;
  color: var(--help-text);
  margin-top: 2px;
  line-height: 1.4;
}
.s-row-desc code {
  background: var(--background);
  padding: 1px 4px;
  border-radius: 3px;
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--primary-color);
}
.s-row-right {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}
.s-row-actions {
  justify-content: flex-start;
  gap: 8px;
  padding-top: 12px;
  padding-bottom: 12px;
}
.s-muted { color: var(--help-text); }
.s-warn { color: var(--status-caution-text); }

/* ── Mode selector ── */
.mode-selector { display: inline-flex; gap: 4px; flex-shrink: 0; }
.btn-mode {
  display: flex; align-items: center; gap: 6px; padding: 5px 14px;
  font-size: 13px; background: var(--background); color: var(--text-gray);
  border: 1px solid var(--stroke); border-radius: 6px; cursor: pointer; transition: all 0.1s;
}
.btn-mode:hover:not(:disabled) { border-color: var(--primary-color); color: var(--primary-text); }
.btn-mode.active { background: var(--primary-color); color: #fff; border-color: var(--primary-color); }
.btn-mode:disabled { opacity: 0.4; cursor: not-allowed; }
.mode-icon {
  display: inline-flex; align-items: center; justify-content: center;
  width: 20px; height: 20px; font-weight: 700; font-size: 11px;
  background: var(--menu-active-item); border-radius: 4px;
}
.btn-mode.active .mode-icon { background: rgba(255,255,255,0.2); }

/* ── Toggle switch ── */
.toggle {
  position: relative;
  display: inline-block;
  width: 38px;
  height: 20px;
  flex-shrink: 0;
  cursor: pointer;
}
.toggle input {
  opacity: 0;
  width: 0;
  height: 0;
  position: absolute;
}
.toggle-slider {
  position: absolute;
  inset: 0;
  background: var(--stroke);
  border-radius: 20px;
  transition: background 0.15s;
}
.toggle-slider::before {
  content: '';
  position: absolute;
  width: 16px;
  height: 16px;
  left: 2px;
  top: 2px;
  background: #fff;
  border-radius: 50%;
  transition: transform 0.15s;
}
.toggle input:checked + .toggle-slider {
  background: var(--primary-color);
}
.toggle input:checked + .toggle-slider::before {
  transform: translateX(18px);
}

/* ── Select ── */
.s-select {
  padding: 5px 10px;
  background: var(--background);
  border: 1px solid var(--stroke);
  border-radius: 6px;
  color: var(--primary-text);
  font-size: 13px;
  cursor: pointer;
  flex-shrink: 0;
}
.s-select:hover { border-color: var(--primary-color); }
.s-select:focus { outline: none; border-color: var(--primary-color); }

/* ── Inputs ── */
.s-input {
  padding: 7px 10px;
  background: var(--background);
  border: 1px solid var(--stroke);
  border-radius: 6px;
  color: var(--primary-text);
  font-size: 13px;
  width: 100%;
  box-sizing: border-box;
}
.s-input:focus { outline: none; border-color: var(--primary-color); }
.s-input::placeholder { color: var(--help-text); }
.s-input-mono { font-family: var(--font-mono); max-width: 180px; }
.s-input-port { max-width: 90px; }

/* ── Password grid ── */
.pw-grid {
  display: flex;
  gap: 8px;
  padding: 12px 16px;
  border-bottom: 1px solid var(--menu-border);
}
.pw-grid .s-input {
  flex: 1;
}

/* ── Version badge ── */
.ver-badge {
  font-family: var(--font-mono);
  background: var(--menu-active-item);
  padding: 2px 8px;
  border-radius: 4px;
  font-size: 12px;
  color: var(--primary-text);
}
.dev-tag {
  display: inline-block;
  padding: 1px 5px;
  font-size: 10px;
  font-weight: 700;
  text-transform: uppercase;
  background: var(--status-caution-text);
  color: var(--background);
  border-radius: 3px;
}
.s-link {
  font-size: 12px;
  color: var(--text-gray);
  text-decoration: none;
}
.s-link:hover { color: var(--primary-color); }
.s-ok { color: var(--status-success-text); font-size: 13px; padding: 8px 16px; margin: 0; }

/* ── Callouts ── */
.s-callout {
  padding: 8px 16px;
  font-size: 12px;
  border-bottom: 1px solid var(--menu-border);
}
.s-callout code {
  background: var(--background);
  padding: 1px 4px;
  border-radius: 3px;
  font-family: var(--font-mono);
  font-size: 11px;
}
.s-callout-info {
  background: var(--status-special-background);
  border-left: 3px solid var(--primary-color);
  color: var(--primary-text);
}
.s-callout-err {
  background: var(--status-warning-background);
  color: var(--error);
}
.s-callout-ok {
  background: var(--status-success-background);
  color: var(--status-success-text);
}

/* ── Progress ── */
.s-progress {
  padding: 8px 16px 12px;
}
.s-progress-bar {
  height: 4px;
  background: var(--progressbar-background);
  border-radius: 2px;
  overflow: hidden;
}
.s-progress-bar div {
  height: 100%;
  background: var(--progressbar-fill);
  transition: width 0.3s ease;
}
.s-progress-text {
  font-size: 12px;
  color: var(--text-gray);
  margin-top: 4px;
  display: block;
}

/* ── Cron hint ── */
.s-cron-hint code {
  background: var(--background);
  padding: 1px 4px;
  border-radius: 3px;
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--primary-color);
}

/* ── Responsive ── */
@media (max-width: 700px) {
  .s-layout { flex-direction: column; }
  .s-nav {
    width: auto;
    position: static;
    flex-direction: row;
    flex-wrap: wrap;
    padding: 10px 14px;
    gap: 4px;
  }
  .s-content { padding: 10px 14px 40px; }
  .s-row { flex-direction: column; align-items: flex-start; gap: 8px; }
  .pw-grid { flex-direction: column; }
}
</style>
