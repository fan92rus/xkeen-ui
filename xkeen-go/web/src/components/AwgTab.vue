<template>
  <div class="awg-root">
    <!-- Sticky header: sub-tabs + upload -->
    <div class="awg-header">
      <div class="awg-tabs">
        <button class="awg-tab" :class="{ 'awg-tab-active': subTab === 'client' }" @click="subTab = 'client'">
          <span class="awg-tab-icon">🔌</span>
          <span>Клиенты</span>
          <span v-if="clientIfaces.length" class="awg-tab-badge">{{ clientIfaces.length }}</span>
        </button>
        <button class="awg-tab" :class="{ 'awg-tab-active': subTab === 'server' }" @click="subTab = 'server'">
          <span class="awg-tab-icon">🖥️</span>
          <span>Серверы</span>
          <span v-if="serverIfaces.length" class="awg-tab-badge">{{ serverIfaces.length }}</span>
        </button>
      </div>
      <button class="btn btn-sm awg-upload-btn" @click="$refs.fileInput.click()" :disabled="uploading">
        <span v-if="!uploading">📤 Загрузить .conf</span>
        <span v-else>Загрузка…</span>
      </button>
      <input type="file" accept=".conf" ref="fileInput" @change="onFilePicked" class="awg-file-hidden" />
    </div>

    <!-- Error banner -->
    <div v-if="error" class="awg-banner awg-banner-error">
      <span>⚠</span>
      <span>{{ error }}</span>
      <button class="awg-banner-close" @click="error = ''">✕</button>
    </div>

    <!-- Scrollable content -->
    <div class="awg-content">
      <!-- Loading -->
      <div v-if="loading && interfaces.length === 0" class="awg-state">
        <div class="awg-state-icon">⏳</div>
        <p>Загрузка…</p>
      </div>

      <!-- Empty: client -->
      <div v-else-if="subTab === 'client' && clientIfaces.length === 0" class="awg-state">
        <div class="awg-state-icon">🔌</div>
        <p class="awg-state-title">Нет клиентских конфигураций</p>
        <p class="awg-state-desc">Загрузите .conf WARP или VPN-провайдера.<br />
          Клиент — это исходящий туннель с <code>Endpoint</code> в [Peer].</p>
        <button class="btn btn-primary" @click="$refs.fileInput.click()">📤 Загрузить конфиг</button>
      </div>

      <!-- Client cards -->
      <template v-else-if="subTab === 'client'">
        <div v-for="iface in clientIfaces" :key="iface.name" class="awg-card">
          <div class="awg-card-header">
            <div class="awg-card-title-group">
              <span class="awg-iface-icon">🔌</span>
              <span class="awg-iface-name">{{ iface.name }}</span>
              <span class="awg-pill" :class="iface.active ? 'awg-pill-on' : 'awg-pill-off'">
                <span class="awg-pill-dot"></span>{{ iface.active ? 'активен' : 'остановлен' }}
              </span>
            </div>
            <div class="awg-card-actions">
              <button v-if="!iface.active" class="btn btn-primary btn-sm" @click="startIface(iface.name)" :disabled="busy">Старт</button>
              <button v-if="iface.active" class="btn btn-sm" @click="stopIface(iface.name)" :disabled="busy">Стоп</button>
              <button class="btn btn-danger btn-sm" @click="deleteIface(iface.name)" :disabled="busy">Удалить</button>
            </div>
          </div>
          <div class="awg-card-body">
            <div class="awg-chips">
              <div class="awg-chip"><span class="awg-chip-label">Mark</span><code>{{ iface.mark }}</code></div>
              <div class="awg-chip" v-if="iface.address"><span class="awg-chip-label">Address</span><code>{{ iface.address }}</code></div>
            </div>
            <div v-if="iface.active" class="awg-route-chain">
              Xray → mark {{ iface.mark }} → table {{ iface.mark }} → dev {{ iface.name }}
            </div>
          </div>
        </div>
      </template>

      <!-- Server tab: settings + interfaces -->
      <template v-else-if="subTab === 'server'">
        <!-- Firewall interface settings -->
        <div class="awg-card awg-settings-card">
          <div class="awg-card-header">
            <div class="awg-card-title-group">
              <span class="awg-iface-icon">⚙</span>
              <span class="awg-iface-name">Интерфейсы роутера</span>
            </div>
          </div>
          <div class="awg-card-body">
            <p class="awg-settings-desc">LAN/WAN для preset «Полный туннель» (iptables FORWARD/NAT). Пусто = авто-детект.</p>
            <div class="awg-iface-row">
              <label class="awg-iface-field">
                <span class="awg-iface-field-label">LAN</span>
                <input v-model="awgIface.lan" type="text" placeholder="br0" :disabled="awgIfaceSaving" />
              </label>
              <label class="awg-iface-field">
                <span class="awg-iface-field-label">WAN</span>
                <input v-model="awgIface.wan" type="text" placeholder="eth3" :disabled="awgIfaceSaving" />
              </label>
              <button class="btn btn-primary btn-sm" @click="saveIfaceSettings()" :disabled="awgIfaceSaving">
                {{ awgIfaceSaving ? 'Сохранение…' : 'Сохранить' }}
              </button>
            </div>
          </div>
        </div>

        <!-- Empty state -->
        <div v-if="serverIfaces.length === 0" class="awg-state">
          <div class="awg-state-icon">🖥️</div>
          <p class="awg-state-title">Нет серверных конфигураций</p>
          <p class="awg-state-desc">Загрузите серверный .conf (с <code>ListenPort</code> и без <code>Endpoint</code>).<br />
            Сервер — это входящий VPN для удалённого доступа к домашней сети.</p>
          <button class="btn btn-primary" @click="$refs.fileInput.click()">📤 Загрузить конфиг</button>
        </div>

        <!-- Server interface cards -->
        <div v-for="iface in serverIfaces" :key="iface.name" class="awg-card awg-card-server">
          <div class="awg-card-header">
            <div class="awg-card-title-group">
              <span class="awg-iface-icon">🖥️</span>
              <span class="awg-iface-name">{{ iface.name }}</span>
              <span class="awg-pill" :class="iface.active ? 'awg-pill-on' : 'awg-pill-off'">
                <span class="awg-pill-dot"></span>{{ iface.active ? 'активен' : 'остановлен' }}
              </span>
              <span v-if="iface.active" class="awg-pill awg-pill-info">🔒 full-tunnel</span>
            </div>
            <div class="awg-card-actions">
              <button v-if="!iface.active" class="btn btn-primary btn-sm" @click="startIface(iface.name)" :disabled="busy">Старт</button>
              <button v-if="iface.active" class="btn btn-sm" @click="stopIface(iface.name)" :disabled="busy">Стоп</button>
              <button class="btn btn-danger btn-sm" @click="deleteIface(iface.name)" :disabled="busy">Удалить</button>
            </div>
          </div>
          <div class="awg-card-body">
            <!-- Peers section -->
            <div class="awg-peers">
              <div class="awg-peers-head">
                <span class="awg-peers-title">Клиенты ({{ (peers[iface.name] || []).length }})</span>
                <button class="btn btn-primary btn-sm" @click="openAddPeer(iface.name)" :disabled="busy">+ Добавить клиента</button>
              </div>

              <div v-if="peerLoading[iface.name]" class="awg-peers-status">Загрузка…</div>
              <div v-else-if="!(peers[iface.name] || []).length" class="awg-peers-status">Нет клиентов. Добавьте, чтобы создать конфиг для подключения.</div>
              <div v-else class="awg-peer-list">
                <div v-for="peer in (peers[iface.name] || [])" :key="peer.public_key" class="awg-peer-row">
                  <div class="awg-peer-info">
                    <span class="awg-peer-ip">{{ peer.ip }}</span>
                    <span class="awg-peer-key" :title="peer.public_key">{{ shortenKey(peer.public_key) }}</span>
                    <span v-if="peer.label" class="awg-peer-tag">{{ peer.label }}</span>
                  </div>
                  <div class="awg-peer-actions">
                    <button v-if="peer.has_client_config" class="btn btn-sm" @click="showPeerQR(iface.name, peer)" :disabled="busy" title="Показать QR-код">📱</button>
                    <button class="btn btn-danger btn-sm" @click="removePeer(iface.name, peer)" :disabled="busy" title="Удалить">✕</button>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>
      </template>
    </div>

    <!-- Add Peer Dialog -->
    <div v-if="addPeer.show" class="awg-modal-overlay" @click.self="closeAddPeer">
      <div class="awg-modal">
        <div class="awg-modal-head">
          <h3>Новый клиент — {{ addPeer.server }}</h3>
          <button class="awg-banner-close" @click="closeAddPeer">✕</button>
        </div>
        <div class="awg-modal-body">
          <!-- Form -->
          <template v-if="!addPeer.result">
            <label class="awg-modal-label">Название (необязательно)</label>
            <input v-model="addPeer.label" type="text" class="awg-modal-input" placeholder="например: phone, laptop"
                   @keyup.enter="doAddPeer" />
            <p class="awg-modal-hint">Будет сгенерирован ключ, назначен IP и создан готовый клиентский конфиг.</p>
            <div class="awg-modal-actions">
              <button class="btn" @click="closeAddPeer">Отмена</button>
              <button class="btn btn-primary" @click="doAddPeer" :disabled="addPeer.loading">
                {{ addPeer.loading ? 'Генерация…' : 'Сгенерировать конфиг' }}
              </button>
            </div>
          </template>
          <!-- Result -->
          <template v-else>
            <div class="awg-result-banner">
              ✅ Клиент <code>{{ addPeer.result.client_ip }}</code> добавлен в <code>{{ addPeer.server }}</code>
            </div>
            <!-- View toggle -->
            <div class="awg-config-toggle">
              <button :class="['awg-toggle-btn', { 'awg-toggle-active': configView === 'qr' }]"
                      @click="configView = 'qr'">📱 QR-код</button>
              <button :class="['awg-toggle-btn', { 'awg-toggle-active': configView === 'text' }]"
                      @click="configView = 'text'">📄 Текст</button>
            </div>
            <!-- QR view -->
            <div v-if="configView === 'qr'" class="awg-qr-wrap">
              <img v-if="qrDataUrl" :src="qrDataUrl" alt="QR-код конфига" class="awg-qr-img" />
              <p v-else class="awg-peers-status">QR-код недоступен</p>
            </div>
            <!-- Text view -->
            <div v-else class="awg-config-wrap">
              <pre class="awg-config-text">{{ addPeer.result.client_config }}</pre>
              <button class="awg-copy-btn" @click="copyConfig" :title="'Копировать'">{{ copied ? '✓' : '📋' }}</button>
            </div>
            <div class="awg-modal-actions">
              <button class="btn" @click="downloadConfig">⬇ Скачать .conf</button>
              <button class="btn btn-primary" @click="closeAddPeer">Готово</button>
            </div>
          </template>
        </div>
      </div>
    </div>

    <!-- Existing Peer Viewer (QR/download) -->
    <div v-if="peerView.show" class="awg-modal-overlay" @click.self="closePeerView">
      <div class="awg-modal">
        <div class="awg-modal-head">
          <h3>📱 Клиент {{ peerView.ip }}</h3>
          <button class="awg-banner-close" @click="closePeerView">✕</button>
        </div>
        <div class="awg-modal-body">
          <div v-if="peerView.loading" class="awg-peers-status">Загрузка…</div>
          <template v-else-if="peerView.error">
            <div class="awg-result-banner awg-result-error">⚠ {{ peerView.error }}</div>
            <div class="awg-modal-actions">
              <button class="btn btn-primary" @click="closePeerView">Закрыть</button>
            </div>
          </template>
          <template v-else>
            <!-- View toggle -->
            <div class="awg-config-toggle">
              <button :class="['awg-toggle-btn', { 'awg-toggle-active': configView === 'qr' }]"
                      @click="configView = 'qr'">📱 QR-код</button>
              <button :class="['awg-toggle-btn', { 'awg-toggle-active': configView === 'text' }]"
                      @click="configView = 'text'">📄 Текст</button>
            </div>
            <!-- QR view -->
            <div v-if="configView === 'qr'" class="awg-qr-wrap">
              <img v-if="qrDataUrl" :src="qrDataUrl" alt="QR-код конфига" class="awg-qr-img" />
              <p v-else class="awg-peers-status">QR-код недоступен</p>
            </div>
            <!-- Text view -->
            <div v-else class="awg-config-wrap">
              <pre class="awg-config-text">{{ peerView.config }}</pre>
              <button class="awg-copy-btn" @click="copyConfig" :title="'Копировать'">{{ copied ? '✓' : '📋' }}</button>
            </div>
            <div class="awg-modal-actions">
              <button class="btn" @click="downloadPeerConfig">⬇ Скачать .conf</button>
              <button class="btn btn-primary" @click="closePeerView">Закрыть</button>
            </div>
          </template>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, computed, watch, onMounted } from 'vue';
import * as awgApi from '../services/awg.js';
import * as xkeen from '../services/xkeen.js';
import { useAppStore } from '../stores/app.js';
import { log } from '../utils/logger.js';
import QRCode from 'qrcode';

const app = useAppStore();

const interfaces = ref([]);
const loading = ref(false);
const error = ref('');
const busy = ref(false);

const fileInput = ref(null);
const uploading = ref(false);

// Server firewall interface settings (LAN/WAN)
const awgIface = ref({ lan: '', wan: '' });
const awgIfaceSaving = ref(false);

const subTab = ref(localStorage.getItem('awg_subtab') || 'client');
watch(subTab, (v) => localStorage.setItem('awg_subtab', v));

const clientIfaces = computed(() => interfaces.value.filter(i => i.role !== 'server'));
const serverIfaces = computed(() => interfaces.value.filter(i => i.role === 'server'));

// Peers
const peers = reactive({});
const peerLoading = reactive({});

// Add peer dialog
const addPeer = reactive({ show: false, server: '', label: '', loading: false, result: null });
const copied = ref(false);

// QR code state
const configView = ref('qr'); // 'qr' | 'text'
const qrDataUrl = ref('');

// Existing peer viewer
const peerView = reactive({ show: false, server: '', ip: '', loading: false, config: '', error: '' });

onMounted(() => {
  loadInterfaces();
  loadIfaceSettings();
});

// ── Data loading ──

async function loadInterfaces() {
  loading.value = true;
  error.value = '';
  try {
    interfaces.value = await awgApi.listInterfaces();
    for (const iface of interfaces.value) {
      if (iface.role === 'server') loadPeers(iface.name);
    }
  } catch (e) {
    error.value = 'Не удалось загрузить: ' + (e.message || e);
  } finally {
    loading.value = false;
  }
}

async function loadIfaceSettings() {
  try {
    const d = await xkeen.getAWGInterfaces();
    awgIface.value = { lan: d.lan_iface || '', wan: d.wan_iface || '' };
  } catch { /* ignore */ }
}

async function saveIfaceSettings() {
  awgIfaceSaving.value = true;
  try {
    await xkeen.updateAWGInterfaces(awgIface.value.lan, awgIface.value.wan);
    app.showToast(
      awgIface.value.lan || awgIface.value.wan
        ? 'Интерфейсы сохранены'
        : 'Интерфейсы: авто-детект',
      'success',
    );
  } catch (e) {
    app.showToast(e.message || 'Ошибка сохранения', 'error');
  } finally {
    awgIfaceSaving.value = false;
  }
}

async function loadPeers(name) {
  peerLoading[name] = true;
  try {
    const res = await awgApi.listPeers(name);
    peers[name] = res.peers || [];
  } catch (e) {
    log('loadPeers failed', name, e);
    peers[name] = [];
  } finally {
    peerLoading[name] = false;
  }
}

// ── Upload ──

function onFilePicked(e) {
  const file = e.target.files[0];
  e.target.value = '';
  if (file) doUpload(file);
}

async function doUpload(file) {
  uploading.value = true;
  error.value = '';
  try {
    const res = await awgApi.uploadConfig(file);
    await loadInterfaces();
    // Auto-switch to the tab where the config landed
    if (res.role === 'server') subTab.value = 'server';
    else subTab.value = 'client';
    app.showToast(`«${res.name || file.name}» загружен (${res.role === 'server' ? 'сервер' : 'клиент'})`, 'success');
  } catch (e) {
    error.value = 'Ошибка загрузки: ' + (e.message || e);
  } finally {
    uploading.value = false;
  }
}

// ── Interface lifecycle ──

async function startIface(name) {
  busy.value = true;
  error.value = '';
  try {
    await awgApi.upInterface(name);
    await pollStatus(name, true);
  } catch (e) {
    error.value = 'Ошибка запуска: ' + (e.message || e);
  } finally {
    busy.value = false;
  }
}

async function stopIface(name) {
  busy.value = true;
  error.value = '';
  try {
    await awgApi.downInterface(name);
    await pollStatus(name, false);
  } catch (e) {
    error.value = 'Ошибка остановки: ' + (e.message || e);
  } finally {
    busy.value = false;
  }
}

async function pollStatus(name, wantActive) {
  for (let i = 0; i < 5; i++) {
    await loadInterfaces();
    const iface = interfaces.value.find(x => x.name === name);
    if (iface && iface.active === wantActive) return;
    await new Promise(r => setTimeout(r, 600));
  }
}

async function deleteIface(name) {
  const role = subTab.value === 'server' ? 'сервер' : 'клиент';
  if (!confirm(`Удалить ${role} «${name}»? Интерфейс будет остановлен, роутинг очищен.`)) return;
  busy.value = true;
  error.value = '';
  try {
    await awgApi.deleteConfig(name);
    await loadInterfaces();
  } catch (e) {
    error.value = 'Ошибка удаления: ' + (e.message || e);
  } finally {
    busy.value = false;
  }
}

// ── Peer management ──

function openAddPeer(serverName) {
  addPeer.show = true;
  addPeer.server = serverName;
  addPeer.label = '';
  addPeer.loading = false;
  addPeer.result = null;
  copied.value = false;
  qrDataUrl.value = '';
  configView.value = 'qr';
}

function closeAddPeer() {
  addPeer.show = false;
  addPeer.result = null;
  qrDataUrl.value = '';
}

async function doAddPeer() {
  addPeer.loading = true;
  try {
    addPeer.result = await awgApi.addPeer(addPeer.server, addPeer.label);
    await loadPeers(addPeer.server);
    // Generate QR code from the client config
    try {
      qrDataUrl.value = await QRCode.toDataURL(addPeer.result.client_config, {
        width: 280,
        margin: 2,
        errorCorrectionLevel: 'M',
        color: { dark: '#000000', light: '#ffffff' },
      });
    } catch (e) {
      log('QR generation failed', e);
      qrDataUrl.value = '';
      configView.value = 'text';
    }
  } catch (e) {
    error.value = 'Ошибка: ' + (e.message || e);
    closeAddPeer();
  } finally {
    addPeer.loading = false;
  }
}

async function removePeer(serverName, peer) {
  if (!confirm(`Удалить клиента ${peer.ip}?`)) return;
  busy.value = true;
  try {
    await awgApi.deletePeer(serverName, peer.public_key, peer.ip);
    await loadPeers(serverName);
  } catch (e) {
    error.value = 'Ошибка: ' + (e.message || e);
  } finally {
    busy.value = false;
  }
}

// ── Helpers ──

function shortenKey(key) {
  if (!key || key.length <= 12) return key;
  return key.slice(0, 8) + '…' + key.slice(-4);
}

// ── Existing peer viewer (QR/download for stored client configs) ──

async function showPeerQR(serverName, peer) {
  peerView.show = true;
  peerView.server = serverName;
  peerView.ip = peer.ip;
  peerView.loading = true;
  peerView.config = '';
  peerView.error = '';
  configView.value = 'qr';
  qrDataUrl.value = '';
  copied.value = false;
  try {
    const res = await awgApi.getPeerConfig(serverName, peer.ip);
    peerView.config = res.client_config || '';
    try {
      qrDataUrl.value = await QRCode.toDataURL(peerView.config, {
        width: 280,
        margin: 2,
        errorCorrectionLevel: 'M',
        color: { dark: '#000000', light: '#ffffff' },
      });
    } catch (e) {
      log('QR generation failed', e);
      configView.value = 'text';
    }
  } catch (e) {
    peerView.error = e.message || 'Конфиг не найден (приватный ключ не сохранён)';
    configView.value = 'text';
  } finally {
    peerView.loading = false;
  }
}

function closePeerView() {
  peerView.show = false;
  peerView.config = '';
  qrDataUrl.value = '';
}

function downloadPeerConfig() {
  if (!peerView.config) return;
  const blob = new Blob([peerView.config], { type: 'text/plain' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = `${peerView.server}-${peerView.ip}.conf`;
  a.click();
  URL.revokeObjectURL(url);
}

async function copyConfig() {
  const text = peerView.show ? peerView.config : addPeer.result?.client_config;
  if (!text) return;
  try {
    await navigator.clipboard.writeText(text);
    copied.value = true;
    setTimeout(() => { copied.value = false; }, 2000);
  } catch { /* ignore */ }
}

function downloadConfig() {
  if (!addPeer.result?.client_config) return;
  const label = addPeer.label || addPeer.result.client_ip || 'client';
  const blob = new Blob([addPeer.result.client_config], { type: 'text/plain' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = `${label}.conf`;
  a.click();
  URL.revokeObjectURL(url);
}
</script>

<style scoped>
/* ── Layout ── */
.awg-root {
  height: 100%;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

/* ── Sticky header ── */
.awg-header {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 8px 16px;
  border-bottom: 1px solid var(--menu-border);
  background: var(--menu-background);
  flex-shrink: 0;
}

.awg-tabs {
  display: flex;
  gap: 4px;
}

.awg-tab {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 14px;
  background: transparent;
  border: 1px solid transparent;
  border-radius: var(--radius);
  color: var(--text-gray);
  font-size: var(--text-body);
  cursor: pointer;
  transition: all 0.1s;
}

.awg-tab:hover {
  background: var(--menu-active-item);
}

.awg-tab-active {
  background: var(--menu-active-item);
  color: var(--primary-text);
  border-color: var(--stroke);
  font-weight: 600;
}

.awg-tab-icon {
  font-size: 14px;
}

.awg-tab-badge {
  background: var(--primary-color);
  color: var(--menu-background);
  font-size: 11px;
  font-weight: 700;
  padding: 1px 6px;
  border-radius: 10px;
  min-width: 18px;
  text-align: center;
}

.awg-upload-btn {
  margin-left: auto;
}

.awg-file-hidden {
  position: absolute;
  width: 1px;
  height: 1px;
  opacity: 0;
  pointer-events: none;
}

/* ── Content scroll area ── */
.awg-content {
  flex: 1;
  overflow-y: auto;
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

/* ── States ── */
.awg-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  text-align: center;
  padding: 48px 16px;
  gap: 8px;
}

.awg-state-icon {
  font-size: 36px;
  opacity: 0.5;
  margin-bottom: 8px;
}

.awg-state-title {
  font-size: var(--text-h4);
  font-weight: 600;
  color: var(--text-gray);
}

.awg-state-desc {
  font-size: var(--text-small);
  color: var(--help-text);
  max-width: 340px;
  line-height: 1.5;
}

.awg-state-desc code {
  background: var(--menu-active-item);
  padding: 1px 4px;
  border-radius: var(--radius-sm);
  font-family: var(--font-mono);
}

/* ── Banner ── */
.awg-banner {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 16px;
  font-size: var(--text-small);
  flex-shrink: 0;
}

.awg-banner-error {
  background: var(--status-warning-background);
  border-bottom: 1px solid var(--status-warning-border);
  color: var(--error);
}

.awg-banner-close {
  margin-left: auto;
  background: none;
  border: none;
  cursor: pointer;
  font-size: 14px;
  opacity: 0.6;
  padding: 2px;
}

.awg-banner-close:hover { opacity: 1; }

/* ── Card ── */
.awg-card {
  background: var(--menu-background);
  border: 1px solid var(--menu-border);
  border-radius: var(--radius);
  overflow: hidden;
}

.awg-card-server {
  border-left: 3px solid var(--primary-color);
}

.awg-card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 14px;
  border-bottom: 1px solid var(--menu-border);
  background: var(--menu-active-item);
  gap: 8px;
}

.awg-card-title-group {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.awg-iface-icon {
  font-size: 16px;
  flex-shrink: 0;
}

.awg-iface-name {
  font-family: var(--font-mono);
  font-size: var(--text-body);
  font-weight: 700;
  color: var(--primary-text);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.awg-card-actions {
  display: flex;
  gap: 6px;
  flex-shrink: 0;
}

.awg-card-body {
  padding: 14px;
}

/* ── Status pills ── */
.awg-pill {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 2px 8px;
  border-radius: var(--radius-sm);
  font-size: var(--text-small);
  font-weight: 500;
  white-space: nowrap;
}

.awg-pill-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
}

.awg-pill-on {
  background: var(--status-success-background);
  color: var(--status-success-text);
}

.awg-pill-on .awg-pill-dot { background: var(--status-success-text); }

.awg-pill-off {
  background: var(--menu-active-item);
  color: var(--text-gray);
}

.awg-pill-off .awg-pill-dot { background: var(--indicator-offline); }

.awg-pill-info {
  background: var(--status-info-background, var(--menu-active-item));
  color: var(--primary-color);
}

/* ── Chips (client meta) ── */
.awg-chips {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
}

.awg-chip {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.awg-chip-label {
  font-size: 10px;
  color: var(--help-text);
  text-transform: uppercase;
  letter-spacing: 0.05em;
  font-weight: 500;
}

.awg-chip code {
  font-family: var(--font-mono);
  font-size: var(--text-small);
  color: var(--primary-text);
}

.awg-route-chain {
  margin-top: 10px;
  padding-top: 10px;
  border-top: 1px solid var(--menu-border);
  font-family: var(--font-mono);
  font-size: var(--text-small);
  color: var(--text-gray);
}

/* ── Settings card (server firewall interfaces) ── */
.awg-settings-card {
  border-left: 3px solid var(--text-gray);
}

.awg-settings-desc {
  font-size: var(--text-small);
  color: var(--help-text);
  margin: 0 0 10px;
}

.awg-iface-row {
  display: flex;
  align-items: flex-end;
  gap: 12px;
  flex-wrap: wrap;
}

.awg-iface-field {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.awg-iface-field-label {
  font-size: 10px;
  color: var(--text-gray);
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.05em;
}

.awg-iface-field input {
  width: 120px;
  padding: 5px 10px;
  background: var(--background);
  border: 1px solid var(--stroke);
  border-radius: var(--radius-sm);
  color: var(--primary-text);
  font-family: var(--font-mono);
  font-size: var(--text-small);
}

.awg-iface-field input:focus {
  outline: none;
  border-color: var(--primary-color);
}

/* ── Peers ── */
.awg-peers {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.awg-peers-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.awg-peers-title {
  font-size: var(--text-body);
  font-weight: 600;
  color: var(--primary-text);
}

.awg-peers-status {
  font-size: var(--text-small);
  color: var(--help-text);
  padding: 4px 0;
}

.awg-peer-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.awg-peer-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 8px 12px;
  background: var(--menu-active-item);
  border-radius: var(--radius-sm);
  border: 1px solid var(--stroke);
}

.awg-peer-info {
  display: flex;
  align-items: center;
  gap: 12px;
  min-width: 0;
  flex: 1;
}

.awg-peer-actions {
  display: flex;
  gap: 4px;
  flex-shrink: 0;
}

.awg-peer-ip {
  font-family: var(--font-mono);
  font-size: var(--text-small);
  font-weight: 600;
  color: var(--primary-text);
  white-space: nowrap;
}

.awg-peer-key {
  font-family: var(--font-mono);
  font-size: var(--text-small);
  color: var(--text-gray);
  overflow: hidden;
  text-overflow: ellipsis;
}

.awg-peer-tag {
  font-size: var(--text-small);
  color: var(--primary-color);
  white-space: nowrap;
}

/* ── Modal ── */
.awg-modal-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1000;
  padding: 16px;
}

.awg-modal {
  background: var(--menu-background);
  border: 1px solid var(--menu-border);
  border-radius: var(--radius);
  box-shadow: var(--box-shadow-2);
  max-width: 560px;
  width: 100%;
  max-height: 80vh;
  overflow-y: auto;
}

.awg-modal-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 14px 16px;
  border-bottom: 1px solid var(--menu-border);
}

.awg-modal-head h3 {
  font-size: var(--text-h4);
  font-weight: 700;
  color: var(--primary-text);
  margin: 0;
}

.awg-modal-body {
  padding: 16px;
}

.awg-modal-label {
  display: block;
  font-size: var(--text-small);
  color: var(--help-text);
  margin-bottom: 6px;
}

.awg-modal-input {
  width: 100%;
  padding: 8px 12px;
  background: var(--background);
  border: 1px solid var(--stroke);
  border-radius: var(--radius);
  color: var(--primary-text);
  font-size: var(--text-body);
  margin-bottom: 8px;
}

.awg-modal-input:focus {
  outline: none;
  border-color: var(--primary-color);
}

.awg-modal-hint {
  font-size: var(--text-small);
  color: var(--help-text);
  margin: 8px 0 12px;
  line-height: 1.4;
}

.awg-modal-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 12px;
}

.awg-result-banner {
  padding: 10px 14px;
  background: var(--status-success-background);
  border-radius: var(--radius);
  color: var(--status-success-text);
  font-size: var(--text-body);
  margin-bottom: 8px;
}

.awg-result-banner code {
  font-family: var(--font-mono);
  font-weight: 600;
}

.awg-result-error {
  background: var(--status-warning-background);
  color: var(--error);
}

.awg-config-wrap {
  position: relative;
  background: var(--background);
  border: 1px solid var(--stroke);
  border-radius: var(--radius);
  overflow: hidden;
}

.awg-config-text {
  padding: 12px;
  font-family: var(--font-mono);
  font-size: var(--text-small);
  color: var(--primary-text);
  white-space: pre-wrap;
  word-break: break-all;
  margin: 0;
  max-height: 260px;
  overflow-y: auto;
}

.awg-copy-btn {
  position: absolute;
  top: 8px;
  right: 8px;
  background: var(--menu-background);
  border: 1px solid var(--stroke);
  border-radius: var(--radius-sm);
  cursor: pointer;
  font-size: 16px;
  padding: 4px 8px;
  line-height: 1;
}

.awg-copy-btn:hover { border-color: var(--primary-color); }

/* ── Config view toggle ── */
.awg-config-toggle {
  display: flex;
  gap: 4px;
  margin-bottom: 12px;
  background: var(--background);
  border: 1px solid var(--stroke);
  border-radius: var(--radius);
  padding: 3px;
}

.awg-toggle-btn {
  flex: 1;
  padding: 6px 12px;
  background: transparent;
  border: none;
  border-radius: calc(var(--radius) - 3px);
  color: var(--text-gray);
  font-size: var(--text-small);
  cursor: pointer;
  transition: all 0.1s;
}

.awg-toggle-btn:hover {
  color: var(--primary-text);
}

.awg-toggle-active {
  background: var(--menu-background);
  color: var(--primary-text);
  font-weight: 600;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.1);
}

/* ── QR code ── */
.awg-qr-wrap {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  padding: 12px;
  background: #ffffff;
  border: 1px solid var(--stroke);
  border-radius: var(--radius);
}

.awg-qr-img {
  width: 280px;
  height: 280px;
  display: block;
}

/* ── Responsive ── */
@media (max-width: 600px) {
  .awg-header {
    flex-wrap: wrap;
    padding: 8px 10px;
  }

  .awg-tab { padding: 6px 10px; }

  .awg-content { padding: 10px; }

  .awg-card-header {
    flex-direction: column;
    align-items: flex-start;
  }

  .awg-card-actions { width: 100%; }

  .awg-peer-row { flex-direction: column; align-items: flex-start; }
}
</style>
