<template>
  <div class="awg-container">
    <!-- Error banner -->
    <div v-if="error" class="awg-banner awg-banner-error">
      <span class="banner-icon">⚠</span>
      <span>{{ error }}</span>
      <button class="banner-close" @click="error = ''">✕</button>
    </div>

    <!-- Loading state -->
    <div v-if="loading && interfaces.length === 0" class="awg-loading">
      Загрузка интерфейсов…
    </div>

    <!-- Empty state -->
    <div v-else-if="interfaces.length === 0" class="awg-empty">
      <div class="awg-empty-icon">🔗</div>
      <p class="awg-empty-title">Нет AWG-конфигураций</p>
      <p class="awg-empty-desc">Загрузите конфигурацию в формате WireGuard/AmneziaWG (.conf)</p>
    </div>

    <!-- Upload card -->
    <div v-if="!loading" class="awg-card">
      <div class="awg-card-header">
        <h3 class="awg-card-title">Добавить конфигурацию</h3>
      </div>
      <div class="awg-card-body">
        <div class="awg-upload-row">
          <div class="awg-file-input-wrapper">
            <input type="file" accept=".conf" ref="fileInput" @change="handleFileSelect" class="awg-file-input" id="awg-file" />
            <label for="awg-file" class="awg-file-label">
              <span v-if="selectedFile">{{ selectedFile.name }}</span>
              <span v-else class="awg-file-placeholder">Выберите .conf файл…</span>
            </label>
          </div>
          <button class="btn btn-primary" @click="uploadFile" :disabled="!selectedFile || uploading">
            {{ uploading ? 'Загрузка…' : 'Загрузить' }}
          </button>
        </div>
        <p class="awg-hint">
          Файл должен содержать секции <code>[Interface]</code> и <code>[Peer]</code>
        </p>
      </div>
    </div>

    <!-- Interface cards -->
    <div v-for="iface in interfaces" :key="iface.name" class="awg-card">
      <div class="awg-card-header awg-card-header-row">
        <div class="awg-card-title-row">
          <h3 class="awg-card-title awg-card-title-iface">{{ iface.name }}</h3>
          <span v-if="iface.active" class="awg-status awg-status-active">Активен</span>
          <span v-else class="awg-status awg-status-inactive">Остановлен</span>
        </div>
        <div class="awg-card-actions">
          <button v-if="!iface.active" class="btn btn-primary btn-sm" @click="startIface(iface.name)"
                  :disabled="actionLoading">Старт</button>
          <button v-if="iface.active" class="btn btn-sm" @click="stopIface(iface.name)"
                  :disabled="actionLoading">Стоп</button>
          <button class="btn btn-danger btn-sm" @click="deleteIface(iface.name)"
                  :disabled="actionLoading">Удалить</button>
        </div>
      </div>
      <div class="awg-card-body">
        <div class="awg-meta-grid">
          <div class="awg-meta-item">
            <span class="awg-meta-label">Mark</span>
            <code class="awg-meta-value">fwmark {{ iface.mark }}</code>
          </div>
          <div class="awg-meta-item" v-if="iface.address">
            <span class="awg-meta-label">Address</span>
            <code class="awg-meta-value">{{ iface.address }}</code>
          </div>
          <div class="awg-meta-item awg-meta-item-wide">
            <span class="awg-meta-label">Config</span>
            <code class="awg-meta-value awg-meta-path">{{ iface.conf_path }}</code>
          </div>
        </div>
        <div v-if="iface.active" class="awg-hint awg-route-hint">
          Xray → mark {{ iface.mark }} → table {{ iface.mark }} → dev {{ iface.name }}
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue';
import * as awgApi from '../services/awg.js';

const interfaces = ref([]);
const loading = ref(false);
const error = ref('');
const actionLoading = ref(false);

const fileInput = ref(null);
const selectedFile = ref(null);
const uploading = ref(false);

onMounted(() => {
  loadInterfaces();
});

async function loadInterfaces() {
  loading.value = true;
  error.value = '';
  try {
    interfaces.value = await awgApi.listInterfaces();
  } catch (e) {
    error.value = 'Не удалось загрузить интерфейсы: ' + (e.message || e);
  } finally {
    loading.value = false;
  }
}

function handleFileSelect(e) {
  selectedFile.value = e.target.files[0] || null;
}

async function uploadFile() {
  if (!selectedFile.value) return;
  uploading.value = true;
  error.value = '';
  try {
    await awgApi.uploadConfig(selectedFile.value);
    selectedFile.value = null;
    if (fileInput.value) fileInput.value.value = '';
    await loadInterfaces();
  } catch (e) {
    error.value = 'Ошибка загрузки: ' + (e.message || e);
  } finally {
    uploading.value = false;
  }
}

async function startIface(name) {
  actionLoading.value = true;
  error.value = '';
  try {
    await awgApi.upInterface(name);
    await loadInterfaces();
    // Retry a few times while waiting for the interface to come up
    for (let attempt = 0; attempt < 5; attempt++) {
      const iface = interfaces.value.find(i => i.name === name);
      if (iface && iface.active) break;
      await new Promise(r => setTimeout(r, 600));
      await loadInterfaces();
    }
  } catch (e) {
    error.value = 'Ошибка запуска: ' + (e.message || e);
  } finally {
    actionLoading.value = false;
  }
}

async function stopIface(name) {
  actionLoading.value = true;
  error.value = '';
  try {
    await awgApi.downInterface(name);
    await loadInterfaces();
    // Retry a few times if the backend still reports the interface as active
    for (let attempt = 0; attempt < 5; attempt++) {
      const iface = interfaces.value.find(i => i.name === name);
      if (!iface || !iface.active) break;
      await new Promise(r => setTimeout(r, 600));
      await loadInterfaces();
    }
    const stillActive = interfaces.value.some(i => i.name === name && i.active);
    if (stillActive) {
      error.value = 'Интерфейс не остановился. Попробуйте ещё раз.';
    }
  } catch (e) {
    error.value = 'Ошибка остановки: ' + (e.message || e);
  } finally {
    actionLoading.value = false;
  }
}

async function deleteIface(name) {
  if (!confirm(`Удалить конфигурацию "${name}"? Интерфейс будет остановлен, роутинг очищен.`)) return;
  actionLoading.value = true;
  error.value = '';
  try {
    await awgApi.deleteConfig(name);
    await loadInterfaces();
  } catch (e) {
    error.value = 'Ошибка удаления: ' + (e.message || e);
  } finally {
    actionLoading.value = false;
  }
}
</script>

<style scoped>
/* ── Container & States ────────────────────────────────────────── */
.awg-container {
  height: 100%;
  overflow-y: auto;
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.awg-loading,
.awg-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 48px 16px;
  color: var(--help-text);
  text-align: center;
}

.awg-empty-icon {
  font-size: 32px;
  margin-bottom: 12px;
  opacity: 0.6;
}

.awg-empty-title {
  font-size: var(--text-h4);
  font-weight: 600;
  color: var(--text-gray);
  margin-bottom: 4px;
}

.awg-empty-desc {
  font-size: var(--text-small);
  color: var(--help-text);
  max-width: 300px;
}

/* ── Error Banner ──────────────────────────────────────────────── */
.awg-banner {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  border-radius: var(--radius);
  font-size: var(--text-small);
  line-height: var(--lh-small);
}

.awg-banner-error {
  background: var(--status-warning-background);
  border: 1px solid var(--status-warning-border);
  color: var(--error);
}

.banner-icon {
  flex-shrink: 0;
  font-size: 14px;
}

.banner-close {
  margin-left: auto;
  background: none;
  border: none;
  color: inherit;
  cursor: pointer;
  font-size: 14px;
  padding: 2px;
  opacity: 0.6;
  transition: opacity 0.05s;
}

.banner-close:hover {
  opacity: 1;
}

/* ── Card ──────────────────────────────────────────────────────── */
.awg-card {
  background: var(--menu-background);
  border: 1px solid var(--menu-border);
  border-radius: var(--radius);
  box-shadow: var(--box-shadow-1);
  overflow: hidden;
}

.awg-card-header {
  padding: 12px 14px;
  border-bottom: 1px solid var(--menu-border);
  background: var(--menu-active-item);
}

.awg-card-header-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.awg-card-title {
  font-size: var(--text-h4);
  font-weight: 700;
  color: var(--primary-text);
  margin: 0;
  line-height: var(--lh-h4);
}

.awg-card-title-row {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.awg-card-title-iface {
  font-family: var(--font-mono);
  font-size: var(--text-body);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.awg-card-actions {
  display: flex;
  gap: 6px;
  flex-shrink: 0;
}

.awg-card-body {
  padding: 14px;
}

/* ── Status badges ─────────────────────────────────────────────── */
.awg-status {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 2px 8px;
  border-radius: var(--radius-sm);
  font-size: var(--text-small);
  font-weight: 500;
  line-height: var(--lh-small);
  white-space: nowrap;
  flex-shrink: 0;
}

.awg-status::before {
  content: '';
  width: 6px;
  height: 6px;
  border-radius: 50%;
  flex-shrink: 0;
}

.awg-status-active {
  background: var(--status-success-background);
  color: var(--status-success-text);
  border: 1px solid var(--status-success-border);
}

.awg-status-active::before {
  background: var(--status-success-text);
}

.awg-status-inactive {
  background: var(--menu-active-item);
  color: var(--text-gray);
  border: 1px solid var(--stroke);
}

.awg-status-inactive::before {
  background: var(--indicator-offline);
}

/* ── Upload ────────────────────────────────────────────────────── */
.awg-upload-row {
  display: flex;
  gap: 10px;
  align-items: center;
}

.awg-file-input-wrapper {
  flex: 1;
  position: relative;
}

.awg-file-input {
  position: absolute;
  width: 1px;
  height: 1px;
  opacity: 0;
  overflow: hidden;
  pointer-events: none;
}

.awg-file-label {
  display: flex;
  align-items: center;
  padding: 7px 12px;
  background: var(--background);
  border: 1px solid var(--stroke);
  border-radius: var(--radius);
  color: var(--primary-text);
  font-size: var(--text-body);
  cursor: pointer;
  transition: border-color 0.05s;
  min-height: 34px;
}

.awg-file-label:hover {
  border-color: var(--primary-color);
}

.awg-file-placeholder {
  color: var(--help-text);
}

.awg-hint {
  margin-top: 8px;
  font-size: var(--text-small);
  color: var(--help-text);
  line-height: var(--lh-small);
}

.awg-hint code {
  background: var(--menu-active-item);
  padding: 1px 4px;
  border-radius: var(--radius-sm);
  font-family: var(--font-mono);
  font-size: var(--text-small);
  color: var(--primary-color);
}

.awg-route-hint {
  margin-top: 10px;
  padding-top: 10px;
  border-top: 1px solid var(--menu-border);
  font-family: var(--font-mono);
  font-size: var(--text-small);
  color: var(--text-gray);
}

/* ── Meta Grid ─────────────────────────────────────────────────── */
.awg-meta-grid {
  display: flex;
  flex-wrap: wrap;
  gap: 16px;
}

.awg-meta-item {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 100px;
}

.awg-meta-item-wide {
  flex: 1;
  min-width: 160px;
}

.awg-meta-label {
  font-size: var(--text-small);
  color: var(--help-text);
  text-transform: uppercase;
  letter-spacing: 0.04em;
  font-weight: 500;
}

.awg-meta-value {
  font-family: var(--font-mono);
  font-size: var(--text-small);
  color: var(--primary-text);
  background: var(--menu-active-item);
  padding: 2px 6px;
  border-radius: var(--radius-sm);
  display: inline-block;
  word-break: break-all;
}

.awg-meta-path {
  max-width: 320px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* ── Responsive ────────────────────────────────────────────────── */
@media (max-width: 600px) {
  .awg-container {
    padding: 10px;
    gap: 8px;
  }

  .awg-card-header-row {
    flex-direction: column;
    align-items: flex-start;
  }

  .awg-card-actions {
    width: 100%;
  }

  .awg-upload-row {
    flex-direction: column;
    align-items: stretch;
  }

  .awg-meta-grid {
    flex-direction: column;
    gap: 8px;
  }
}
</style>
