<template>
  <div class="page">
    <div class="page-header">
      <h1>AmneziaWG</h1>
      <p class="page-desc">Управление AWG-интерфейсами и конфигурациями</p>
    </div>

    <div class="card" v-if="error" style="margin-bottom:16px">
      <div class="card-body">
        <div class="alert alert-error">{{ error }}</div>
      </div>
    </div>

    <!-- Upload new config -->
    <div class="card" style="margin-bottom:16px">
      <div class="card-header">
        <h3>Добавить конфигурацию</h3>
      </div>
      <div class="card-body">
        <div class="upload-row">
          <input type="file" accept=".conf" ref="fileInput" @change="handleFileSelect" />
          <button class="btn btn-primary" @click="uploadFile" :disabled="!selectedFile || uploading">
            {{ uploading ? 'Загрузка...' : 'Загрузить' }}
          </button>
        </div>
        <p class="s-row-desc" style="margin-top:8px">
          Файл должен содержать секции [Interface] и [Peer] (формат WireGuard/AmneziaWG).
        </p>
      </div>
    </div>

    <!-- Interface list -->
    <div class="card" v-if="interfaces && interfaces.length === 0 && !loading">
      <div class="card-body">
        <p class="text-muted">Нет AWG-конфигураций. Загрузите .conf файл выше.</p>
      </div>
    </div>

    <div v-for="iface in (interfaces || [])" :key="iface.name" class="card" style="margin-bottom:12px">
      <div class="card-body">
        <div class="awg-iface-header">
          <div>
            <strong class="iface-name">{{ iface.name }}</strong>
            <span v-if="iface.active" class="badge badge-success" style="margin-left:8px">Активен</span>
            <span v-else class="badge badge-muted" style="margin-left:8px">Остановлен</span>
          </div>
          <div class="iface-actions">
            <button v-if="!iface.active" class="btn btn-primary btn-sm" @click="startIface(iface.name)" :disabled="actionLoading">
              Старт
            </button>
            <button v-if="iface.active" class="btn btn-warning btn-sm" @click="stopIface(iface.name)" :disabled="actionLoading">
              Стоп
            </button>
            <button class="btn btn-danger btn-sm" @click="deleteIface(iface.name)" :disabled="actionLoading">
              Удалить
            </button>
          </div>
        </div>

        <div class="iface-info">
          <div class="iface-info-row">
            <span class="info-label">Mark:</span>
            <code>{{ iface.mark }}</code>
          </div>
          <div class="iface-info-row">
            <span class="info-label">Файл:</span>
            <code class="info-path">{{ iface.conf_path }}</code>
          </div>
          <div class="iface-info-row" v-if="iface.address">
            <span class="info-label">Адрес:</span>
            <code>{{ iface.address }}</code>
          </div>
        </div>

        <div v-if="iface.active" class="awg-actions-hint" style="margin-top:8px;font-size:12px;color:var(--text-muted)">
          Трафик Xray с mark {{ iface.mark }} → интерфейс {{ iface.name }}
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
.awg-iface-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 12px;
}
.iface-name {
  font-size: 16px;
}
.iface-actions {
  display: flex;
  gap: 8px;
}
.iface-info {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
}
.iface-info-row {
  display: flex;
  align-items: center;
  gap: 6px;
}
.info-label {
  font-size: 12px;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.5px;
}
.info-path {
  font-size: 12px;
  max-width: 300px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.upload-row {
  display: flex;
  gap: 12px;
  align-items: center;
}
.badge-muted {
  background: var(--bg-tertiary);
  color: var(--text-muted);
}
.btn-sm {
  padding: 4px 12px;
  font-size: 12px;
}
</style>
