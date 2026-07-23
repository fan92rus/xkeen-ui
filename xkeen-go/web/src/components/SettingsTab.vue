<script setup>
import { ref, onMounted, inject } from 'vue';
import { useAppStore } from '../stores/app.js';
import { useI18nStore } from '../stores/i18n.js';
import * as sub from '../services/subscription.js';
import * as metrics from '../services/metrics.js';
import * as installApi from '../services/install.js';
import { checkNetwork } from '../services/diagnostics.js';
import { getProxyEntware, setProxyEntware } from '../services/routing.js';
import { getAutoUpdate, updateAutoUpdate } from '../services/update.js';
import { getChangelog } from '../services/changelog.js';
import { getXkeenVersion, getSpeedBalancer, updateSpeedBalancer, getSpeedBalancerStatus } from '../services/xkeen.js';

const app = useAppStore();
const i18n = useI18nStore();
const reloadMetricsState = inject('reloadMetricsState', () => {});

const autoApply = ref({ enabled: false, cron: '0 */6 * * *', next_run: '' });
const autoApplySaving = ref(false);

// Metrics settings
const metricsPort = ref(0);
const metricsLoading = ref(false);
const metricsSaving = ref(false);

const observatoryConcurrency = ref(false);

// Auto-update (stable releases, 10-min check)
const autoUpdate = ref(true);
const changelogData = ref(null);
const sbAvailable = ref(false);
const sbSettings = ref({ enabled: false, interval: 15, hysteresis: 25, balancer: 'default-balancer', max_time: 8, test_url: '', routing_file: '05_routing.json', outbounds_file: '04_outbounds.json', log: false });
const sbSaving = ref(false);
const sbStatusLoading = ref(false);

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

async function loadObservatoryConcurrency() {
	try {
		const d = await metrics.getObservatoryConcurrency();
		observatoryConcurrency.value = d.enabled || false;
	} catch { /* ignore */ }
}

async function saveObservatoryConcurrency() {
	try {
		await metrics.updateObservatoryConcurrency(observatoryConcurrency.value);
		app.showToast(
			observatoryConcurrency.value ? i18n.t('settings.obs_concurrency') + ': ON' : i18n.t('settings.obs_concurrency') + ': OFF',
			'success',
		);
	} catch (e) {
		app.showToast(e.message || i18n.t('settings.save_error'), 'error');
	}
}

async function loadAutoUpdate() {
	try {
		const d = await getAutoUpdate();
		autoUpdate.value = d.enabled ?? true;
	} catch { /* ignore — default ON */ }
}

async function saveAutoUpdate() {
	try {
		await updateAutoUpdate(autoUpdate.value);
		app.showToast(autoUpdate.value ? i18n.t('settings.auto_update_enabled') : i18n.t('settings.auto_update_disabled'), 'success');
	} catch (e) {
		app.showToast(e.message || i18n.t('settings.save_error'), 'error');
	}
}

async function loadChangelog() {
	try {
		const data = await getChangelog();
		if (data.ok) changelogData.value = data.changelog;
	} catch {
		/* changelog is non-critical */
	}
}

function changelogIcon(type) {
	return { feat: '✨', fix: '🐛', tweak: '🔧' }[type] || '•';
}

async function loadSpeedBalancer() {
	try {
		const [ver, sb] = await Promise.all([getXkeenVersion(), getSpeedBalancer()]);
		sbAvailable.value = ver.info?.speed_balancer_supported || false;
		if (sb.settings) sbSettings.value = { ...sbSettings.value, ...sb.settings };
	} catch {
		/* non-critical */
	}
}

async function saveSpeedBalancer() {
	sbSaving.value = true;
	try {
		await updateSpeedBalancer(sbSettings.value);
		app.showToast(i18n.t('settings.saved'), 'success');
	} catch (e) {
		app.showToast(e.message || i18n.t('settings.save_error'), 'error');
	} finally {
		sbSaving.value = false;
	}
}

async function loadSpeedBalancerStatus() {
	sbStatusLoading.value = true;
	try {
		const res = await getSpeedBalancerStatus();
		app.modal.output = res.output || '';
		app.modal.error = res.error || '';
		app.modal.command = 'xkeen -sb status';
		app.modal.show = true;
		app.commandComplete = true;
	} catch (e) {
		app.showToast(e.message || i18n.t('settings.save_error'), 'error');
	} finally {
		sbStatusLoading.value = false;
	}
}

async function saveMetricsPort() {
	metricsSaving.value = true;
	try {
		await metrics.updateMetricsPort(metricsPort.value);
		app.showToast(
			metricsPort.value > 0 ? i18n.t('settings.metrics_on', { port: metricsPort.value }) : i18n.t('settings.metrics_off'),
			'success',
		);
		reloadMetricsState();
	} catch (e) {
		app.showToast(e.message || i18n.t('settings.save_error'), 'error');
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
    app.showToast(autoApply.value.enabled ? i18n.t('settings.auto_update_on', { cron: autoApply.value.cron }) : i18n.t('settings.auto_update_off'), 'success');
  } catch (e) {
    app.showToast(e.message || i18n.t('settings.error_generic'), 'error');
  } finally {
    autoApplySaving.value = false;
  }
}

import { fmtTime as fmtNextRun } from '../utils/format.js';

/* ---- network diagnostics ---- */
const netCheck = ref({ loading: false, ip: '', source: '', latency: null, domain: '', error: '', checked: false });

async function runNetworkCheck() {
  netCheck.value = { loading: true, ip: '', source: '', latency: null, domain: '', error: '', checked: false };
  try {
    const r = await checkNetwork();
    netCheck.value = {
      loading: false,
      ip: r.exit_ip || '',
      source: r.source || '',
      latency: r.latency_ms ?? null,
      domain: r.check_domain || '',
      error: r.error || '',
      checked: true,
    };
  } catch (e) {
    netCheck.value = { loading: false, ip: '', source: '', latency: null, domain: '', error: e.message || String(e), checked: true };
  }
}

const proxyEntware = ref({ loading: true, enabled: false, saving: false, error: '' });

async function loadProxyEntware() {
  proxyEntware.value.loading = true;
  try {
    const data = await getProxyEntware();
    proxyEntware.value.enabled = !!data.enabled;
    proxyEntware.value.error = '';
  } catch (e) {
    proxyEntware.value.error = e.message || String(e);
  } finally {
    proxyEntware.value.loading = false;
  }
}

async function toggleProxyEntware() {
  const next = !proxyEntware.value.enabled;
  proxyEntware.value.saving = true;
  proxyEntware.value.error = '';
  try {
    const data = await setProxyEntware(next);
    proxyEntware.value.enabled = !!data.enabled;
    if (data.error) {
      proxyEntware.value.error = data.error;
    }
  } catch (e) {
    proxyEntware.value.error = e.message || String(e);
  } finally {
    proxyEntware.value.saving = false;
  }
}

const sections = [
  { id: 'mode', icon: '⚡', label: i18n.t('settings.mode_section') },
  { id: 'logging', icon: '📋', label: i18n.t('settings.logs_section') },
  { id: 'updates', icon: '🔄', label: i18n.t('settings.update_section') },
  { id: 'security', icon: '🔒', label: i18n.t('settings.security_section') },
  { id: 'autoapply', icon: '📅', label: i18n.t('settings.subs_section') },
  { id: 'network', icon: '🔍', label: i18n.t('settings.net_section') },
  { id: 'routing', icon: '🔀', label: i18n.t('settings.routing_section') },
  { id: 'metrics', icon: '📊', label: i18n.t('settings.metrics_section') },
  { id: 'awg', icon: '🔗', label: 'AmneziaWG' },
  { id: 'lang', icon: '🌐', label: i18n.t('settings.lang_section') },
];

const awg = ref({ installed: false, interfaces: '', installing: false, uninstalling: false, error: '', progress: '' });

async function checkAWG() {
  try {
    const s = await installApi.getAWGStatus();
    awg.value.installed = s.installed;
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
        app.showToast(i18n.t('settings.awg_install_ok'), 'success');
        checkAWG();
      },
      onError: (err) => {
        awg.value.installing = false;
        awg.value.error = err.error || err.message || i18n.t('settings.awg_install_error');
        awg.value.progress = '';
      },
    });
  } catch (e) {
    awg.value.installing = false;
    awg.value.error = e.message || i18n.t('settings.awg_install_error');
    awg.value.progress = '';
  }
}

async function uninstallAWG() {
  if (!awg.value.installed) { return; }
  if (!confirm(i18n.t('settings.awg_uninstall_confirm'))) return;
  awg.value.uninstalling = true;
  awg.value.error = '';
  awg.value.progress = '';
  try {
    await installApi.uninstallAWG({
      onProgress: (data) => {
        awg.value.progress = data.status || '';
      },
      onComplete: () => {
        awg.value.installed = false;
        awg.value.uninstalling = false;
        awg.value.progress = '';
        app.showToast(i18n.t('settings.awg_uninstalled_ok'), 'success');
        checkAWG();
      },
      onError: (err) => {
        awg.value.uninstalling = false;
        awg.value.error = err.error || err.message || i18n.t('settings.awg_uninstall_error');
        awg.value.progress = '';
      },
    });
  } catch (e) {
    awg.value.uninstalling = false;
    awg.value.error = e.message || i18n.t('settings.awg_uninstall_error');
    awg.value.progress = '';
  }
}

onMounted(() => {
	loadAutoApply();
	loadMetricsPort();
	loadObservatoryConcurrency();
	loadAutoUpdate();
	loadChangelog();
	loadSpeedBalancer();
	app.loadXraySettings();
  checkAWG();
  loadProxyEntware();
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
          <h2 class="s-title">{{ sections[0].icon }} {{ sections[0].label }}</h2>
          <div class="s-block">
            <div class="s-row">
              <div class="s-row-main">
                <div class="s-row-label">{{ i18n.t('settings.mode_label') }}</div>
                <div class="s-row-desc">{{ i18n.t('settings.mode_desc') }}</div>
              </div>
              <div class="mode-selector">
                <button
                  :class="{ active: app.currentMode === 'xray' }"
                  :disabled="!app.xrayAvailable"
                  class="btn-mode" @click="app.switchMode('xray')"
                >
                  <span class="mode-icon">X</span> Xray
                </button>
                <button
                  :class="{ active: app.currentMode === 'mihomo' }"
                  :disabled="!app.mihomoAvailable"
                  class="btn-mode" @click="app.switchMode('mihomo')"
                >
                  <span class="mode-icon">M</span> Mihomo
                </button>
              </div>
            </div>
            <div v-show="!app.mihomoAvailable" class="s-row">
              <div class="s-row-main">
                <div class="s-row-label s-muted">Mihomo</div>
                <div class="s-row-desc s-warn">{{ i18n.t('settings.mode_no_config') }}</div>
              </div>
            </div>
          </div>
        </section>

        <!-- Logging -->
        <section :id="sections[1].id" class="s-section">
          <h2 class="s-title">{{ sections[1].icon }} {{ sections[1].label }}</h2>
          <div class="s-block">
            <div class="s-row">
              <div class="s-row-main">
                <div class="s-row-label">{{ i18n.t('settings.log_level') }}</div>
                <div class="s-row-desc">
                  Access: <code>{{ app.xraySettings.accessLog }}</code> · Error: <code>{{ app.xraySettings.errorLog }}</code>
                </div>
              </div>
              <select v-model="app.xraySettings.logLevel" class="s-select" @change="app.updateLogLevel()">
                <option v-for="level in app.xraySettings.logLevels" :key="level" :value="level">{{ level.toUpperCase() }}</option>
              </select>
            </div>
          </div>
        </section>

        <!-- Updates -->
        <section :id="sections[2].id" class="s-section">
          <h2 class="s-title">{{ sections[2].icon }} {{ sections[2].label }}</h2>
          <div class="s-block">
            <div class="s-row">
              <div class="s-row-main">
                <div class="s-row-label">{{ i18n.t('settings.current_version') }}</div>
              </div>
              <div class="s-row-right">
                <span class="ver-badge">{{ app.currentVersion }}</span>
                <span v-show="app.updateInfo.is_prerelease" class="dev-tag">dev</span>
              </div>
            </div>
            <div v-show="app.updateInfo.latest_version" class="s-row">
              <div class="s-row-main">
                <div class="s-row-label">{{ i18n.t('settings.latest_version') }}</div>
              </div>
              <div class="s-row-right">
                <span class="ver-badge">{{ app.updateInfo.latest_version }}</span>
                <a v-show="app.updateInfo.release_url" :href="app.updateInfo.release_url" target="_blank" class="s-link">{{ i18n.t('settings.release_notes') }}</a>
              </div>
            </div>
            <div v-if="app.updateInfo.update_available" class="s-callout s-callout-info">
              {{ i18n.t('settings.update_available', { dev: app.updateInfo.is_prerelease ? ' dev' : '' }) }}
            </div>
            <p v-else-if="app.updateInfo.latest_version" class="s-ok">{{ i18n.t('settings.up_to_date') }}</p>
            <div class="s-row">
              <div class="s-row-main">
                <div class="s-row-label">{{ i18n.t('settings.dev_channel') }}</div>
                <div class="s-row-desc">{{ i18n.t('settings.dev_channel_desc') }}</div>
              </div>
              <label class="toggle">
                <input v-model="app.checkDevUpdates" type="checkbox" @change="app.checkUpdate()">
                <span class="toggle-slider" />
              </label>
            </div>
            <div class="s-row">
              <div class="s-row-main">
                <div class="s-row-label">{{ i18n.t('settings.branch') }}</div>
                <div class="s-row-desc">{{ i18n.t('settings.branch_desc') }}</div>
              </div>
              <select v-model="app.selectedBranch" class="s-select" @change="app.checkUpdate()">
                <option v-if="!app.availableBranches.length" value="" disabled>{{ i18n.t('settings.loading') }}</option>
                <option v-for="b in app.availableBranches" :key="b.name" :value="b.name">
                  {{ b.name }} ({{ b.latest_version }})
                </option>
              </select>
              <span v-if="app.currentBranch && app.selectedBranch !== app.currentBranch" class="s-hint">
                {{ i18n.t('settings.branch_differs') }}
              </span>
            </div>
            <div class="s-row s-row-actions">
              <button :disabled="app.updateChecking || app.updating" class="btn" @click="app.checkUpdate()">
                {{ app.updateChecking ? i18n.t('settings.checking') : i18n.t('settings.check_btn') }}
              </button>
              <button v-if="app.updateInfo.update_available" :disabled="app.updating" class="btn btn-primary" @click="app.startUpdate()">
                {{ app.updating ? i18n.t('settings.updating') : i18n.t('settings.update_btn') }}
              </button>
            </div>
            <div v-show="app.updating" class="s-progress">
              <div class="s-progress-bar"><div :style="'width:' + app.updateProgress + '%'" /></div>
              <span class="s-progress-text">{{ app.updateStatus }}</span>
            </div>
            <div class="s-row">
              <div class="s-row-main">
                <div class="s-row-label">{{ i18n.t('settings.auto_update_title') }}</div>
                <div class="s-row-desc">{{ i18n.t('settings.auto_update_title_desc') }}</div>
              </div>
              <label class="toggle">
                <input v-model="autoUpdate" type="checkbox" @change="saveAutoUpdate()">
                <span class="toggle-slider" />
              </label>
            </div>
            <details class="s-changelog">
              <summary>{{ i18n.t('settings.changelog') }}</summary>
              <div v-if="changelogData" class="s-changelog-body">
                <div v-if="changelogData.unreleased && changelogData.unreleased.length" class="s-changelog-rel">
                  <div class="s-changelog-ver">{{ i18n.t('settings.changelog_unreleased') }}</div>
                  <ul>
                    <li v-for="(c, i) in changelogData.unreleased" :key="i" :class="{ 's-changelog-important': c.important }">
                      <span class="s-changelog-type">{{ changelogIcon(c.type) }}</span> {{ c.text }}
                    </li>
                  </ul>
                </div>
                <div v-for="rel in changelogData.releases" :key="rel.version" class="s-changelog-rel">
                  <div class="s-changelog-ver">v{{ rel.version }} <span class="s-changelog-date">{{ rel.date }}</span></div>
                  <ul>
                    <li v-for="(c, i) in rel.changes" :key="i" :class="{ 's-changelog-important': c.important }">
                      <span class="s-changelog-type">{{ changelogIcon(c.type) }}</span> {{ c.text }}
                    </li>
                  </ul>
                </div>
              </div>
              <p v-else class="s-row-desc">{{ i18n.t('settings.loading') }}</p>
            </details>
          </div>
        </section>

        <!-- Security -->
        <section :id="sections[3].id" class="s-section">
          <h2 class="s-title">{{ sections[3].icon }} {{ sections[3].label }}</h2>
          <div class="s-block">
            <div class="pw-grid">
              <input
                v-model="app.passwordChange.currentPassword" type="password"
                :placeholder="i18n.t('settings.current_pwd')" autocomplete="current-password" class="s-input"
              >
              <input
                v-model="app.passwordChange.newPassword" type="password"
                :placeholder="i18n.t('settings.new_pwd')" autocomplete="new-password" class="s-input"
              >
              <input
                v-model="app.passwordChange.confirmPassword" type="password"
                :placeholder="i18n.t('settings.confirm_pwd')" autocomplete="new-password" class="s-input"
              >
            </div>
            <div v-show="app.passwordChange.error" class="s-callout s-callout-err">{{ app.passwordChange.error }}</div>
            <div v-show="app.passwordChange.success" class="s-callout s-callout-ok">{{ i18n.t('settings.pwd_changed') }}</div>
            <div class="s-row s-row-actions">
              <button :disabled="app.passwordChange.loading" class="btn btn-primary" @click="app.changePassword()">
                {{ app.passwordChange.loading ? i18n.t('settings.changing') : i18n.t('settings.change_btn') }}
              </button>
              <button :disabled="app.passwordChange.loading" class="btn" @click="app.clearPasswordForm()">{{ i18n.t('settings.clear') }}</button>
            </div>
          </div>
        </section>

        <!-- Auto-Apply -->
        <section :id="sections[4].id" class="s-section">
          <h2 class="s-title">{{ sections[4].icon }} {{ sections[4].label }}</h2>
          <div class="s-block">
            <div class="s-row">
              <div class="s-row-main">
                <div class="s-row-label">{{ i18n.t('settings.auto_update_enable') }}</div>
                <div class="s-row-desc">{{ i18n.t('settings.auto_update_desc') }}</div>
              </div>
              <label class="toggle">
                <input v-model="autoApply.enabled" type="checkbox">
                <span class="toggle-slider" />
              </label>
            </div>
            <div class="s-row">
              <div class="s-row-main">
                <div class="s-row-label">{{ i18n.t('settings.auto_update_schedule') }}</div>
                <div class="s-row-desc s-cron-hint">
                  <code>0 */6 * * *</code> каждые 6ч &nbsp;·&nbsp;
                  <code>0 0 * * *</code> ежедневно &nbsp;·&nbsp;
                  <code>*/30 * * * *</code> каждые 30м
                </div>
              </div>
              <input
                v-model="autoApply.cron" type="text" placeholder="0 */6 * * *"
                :disabled="!autoApply.enabled" class="s-input s-input-mono"
              >
            </div>
            <div v-if="autoApply.enabled && autoApply.next_run" class="s-callout s-callout-info">
              {{ i18n.t('settings.next_run', { time: fmtNextRun(autoApply.next_run) }) }}
            </div>
            <div class="s-row s-row-actions">
              <button :disabled="autoApplySaving" class="btn btn-primary" @click="saveAutoApply()">
                {{ autoApplySaving ? i18n.t('settings.saving') : i18n.t('settings.save') }}
              </button>
            </div>
          </div>
        </section>

        <!-- Speed Balancer (conditional on XKeen version) -->
        <section v-if="sbAvailable" id="speed-balancer" class="s-section">
          <h2 class="s-title">⚡ {{ i18n.t('settings.sb_title') }}</h2>
          <div class="s-block">
            <div class="s-row">
              <div class="s-row-main">
                <div class="s-row-label">{{ i18n.t('settings.sb_enable') }}</div>
                <div class="s-row-desc">{{ i18n.t('settings.sb_enable_desc') }}</div>
              </div>
              <label class="toggle">
                <input v-model="sbSettings.enabled" type="checkbox">
                <span class="toggle-slider" />
              </label>
            </div>
            <div class="sb-grid">
              <label class="sb-field">
                <span class="s-row-label">{{ i18n.t('settings.sb_interval') }}</span>
                <span class="s-row-desc">{{ i18n.t('settings.sb_interval_desc') }}</span>
                <input v-model.number="sbSettings.interval" type="number" min="1" class="s-input">
              </label>
              <label class="sb-field">
                <span class="s-row-label">{{ i18n.t('settings.sb_hysteresis') }}</span>
                <span class="s-row-desc">{{ i18n.t('settings.sb_hysteresis_desc') }}</span>
                <input v-model.number="sbSettings.hysteresis" type="number" min="0" class="s-input">
              </label>
              <label class="sb-field">
                <span class="s-row-label">{{ i18n.t('settings.sb_maxtime') }}</span>
                <span class="s-row-desc">{{ i18n.t('settings.sb_maxtime_desc') }}</span>
                <input v-model.number="sbSettings.max_time" type="number" min="1" class="s-input">
              </label>
              <label class="sb-field">
                <span class="s-row-label">{{ i18n.t('settings.sb_balancer') }}</span>
                <span class="s-row-desc">{{ i18n.t('settings.sb_balancer_desc') }}</span>
                <input v-model="sbSettings.balancer" type="text" class="s-input">
              </label>
            </div>
            <div class="s-row s-row-col">
              <div class="s-row-label">{{ i18n.t('settings.sb_test_url') }}</div>
              <div class="s-row-desc">{{ i18n.t('settings.sb_test_url_desc') }}</div>
              <input v-model="sbSettings.test_url" type="text" class="s-input" style="width:100%; font-family:var(--font-mono)">
            </div>
            <div class="sb-grid">
              <label class="sb-field">
                <span class="s-row-label">{{ i18n.t('settings.sb_routing_file') }}</span>
                <span class="s-row-desc">{{ i18n.t('settings.sb_routing_file_desc') }}</span>
                <input v-model="sbSettings.routing_file" type="text" class="s-input" style="font-family:var(--font-mono)">
              </label>
              <label class="sb-field">
                <span class="s-row-label">{{ i18n.t('settings.sb_outbounds_file') }}</span>
                <span class="s-row-desc">{{ i18n.t('settings.sb_outbounds_file_desc') }}</span>
                <input v-model="sbSettings.outbounds_file" type="text" class="s-input" style="font-family:var(--font-mono)">
              </label>
            </div>
            <div class="s-row">
              <div class="s-row-main">
                <div class="s-row-label">{{ i18n.t('settings.sb_log') }}</div>
                <div class="s-row-desc">{{ i18n.t('settings.sb_log_desc') }}</div>
              </div>
              <label class="toggle">
                <input v-model="sbSettings.log" type="checkbox">
                <span class="toggle-slider" />
              </label>
            </div>
            <div class="s-row s-row-actions">
              <button :disabled="sbSaving" class="btn btn-primary" @click="saveSpeedBalancer()">
                {{ sbSaving ? i18n.t('settings.saving') : i18n.t('settings.save') }}
              </button>
              <button :disabled="sbStatusLoading" class="btn" @click="loadSpeedBalancerStatus()">
                {{ sbStatusLoading ? i18n.t('settings.loading') : i18n.t('settings.sb_status') }}
              </button>
            </div>
          </div>
        </section>

        <!-- Network diagnostics -->
        <section id="network" class="s-section">
          <h2 class="s-title">🔍 {{ i18n.t('settings.net_section') }}</h2>
          <div class="s-block">
            <div class="s-row">
              <div class="s-row-main">
                <div class="s-row-label">{{ i18n.t('settings.net_check_title') }}</div>
                <div class="s-row-desc">{{ i18n.t('settings.net_check_desc') }}</div>
              </div>
              <button :disabled="netCheck.loading" class="btn btn-primary" @click="runNetworkCheck()">
                {{ netCheck.loading ? i18n.t('settings.net_checking') : i18n.t('settings.net_check_btn') }}
              </button>
            </div>

            <div v-if="netCheck.checked && !netCheck.loading" class="s-row">
              <div class="s-row-main">
                <template v-if="netCheck.error">
                  <div class="s-row-label" style="color: var(--status-error, #e74c3c)">Error</div>
                  <div class="s-row-desc">{{ netCheck.error }}</div>
                </template>
                <template v-else>
                  <div class="s-row-label">
                    {{ i18n.t('settings.net_exit_ip') }}:
                    <code style="font-size: 14px; font-weight: 600">{{ netCheck.ip }}</code>
                    <span v-if="netCheck.latency !== null" style="color: var(--text-gray); margin-left: 8px">{{ netCheck.latency }}ms</span>
                  </div>
                  <div class="s-row-desc">
                    {{ i18n.t('settings.net_check_domain') }}: <code>{{ netCheck.domain }}</code>
                  </div>
                </template>
              </div>
            </div>
          </div>
        </section>

        <!-- Routing (proxy_entware) -->
        <section id="routing" class="s-section">
          <h2 class="s-title">🔀 {{ i18n.t('settings.routing_section') }}</h2>
          <div class="s-block">
            <div class="s-row">
              <div class="s-row-content">
                <div class="s-row-label">{{ i18n.t('settings.proxy_entware_title') }}</div>
                <div class="s-row-desc">{{ i18n.t('settings.proxy_entware_desc') }}</div>
              </div>
              <button
                :disabled="proxyEntware.loading || proxyEntware.saving"
                class="btn"
                :class="proxyEntware.enabled ? 'btn-danger' : 'btn-primary'"
                @click="toggleProxyEntware()"
              >
                {{ proxyEntware.saving
                  ? i18n.t('settings.proxy_entware_saving')
                  : (proxyEntware.enabled
                    ? i18n.t('settings.proxy_entware_off')
                    : i18n.t('settings.proxy_entware_on')) }}
              </button>
            </div>
            <div v-if="proxyEntware.error" class="s-row">
              <div class="s-row-content">
                <div class="s-row-desc" style="color: var(--danger, #e74c3c)">{{ proxyEntware.error }}</div>
              </div>
            </div>
          </div>
        </section>

        <!-- Metrics -->
        <section :id="sections[5].id" class="s-section">
          <h2 class="s-title">{{ sections[5].icon }} {{ sections[5].label }}</h2>
          <div class="s-block">
            <div class="s-row">
              <div class="s-row-main">
                <div class="s-row-label">{{ i18n.t('settings.metrics_enable') }}</div>
                <div class="s-row-desc">{{ i18n.t('settings.metrics_desc') }}</div>
              </div>
              <label class="toggle">
                <input type="checkbox" :checked="metricsPort > 0" @change="$event.target.checked ? metricsPort = 11111 : metricsPort = 0">
                <span class="toggle-slider" />
              </label>
            </div>
            <div v-if="metricsPort > 0" class="s-row">
              <div class="s-row-main">
                <div class="s-row-label">{{ i18n.t('settings.metrics_port') }}</div>
                <div class="s-row-desc">{{ i18n.t('settings.metrics_listen') }} <code>127.0.0.1:{{ metricsPort }}</code>{{ i18n.t('settings.metrics_restart_hint') }}</div>
              </div>
              <input
                v-model.number="metricsPort" type="number" min="1" max="65535"
                :disabled="metricsSaving" class="s-input s-input-port"
              >
            </div>
            <div class="s-row s-row-actions">
              <button :disabled="metricsSaving" class="btn btn-primary" @click="saveMetricsPort()">
                {{ metricsSaving ? i18n.t('settings.saving') : i18n.t('settings.save') }}
              </button>
            </div>
            <div class="s-row">
              <div class="s-row-main">
                <div class="s-row-label">{{ i18n.t('settings.obs_concurrency') }}</div>
                <div class="s-row-desc">{{ i18n.t('settings.obs_concurrency_desc') }}</div>
              </div>
              <label class="toggle">
                <input v-model="observatoryConcurrency" type="checkbox" @change="saveObservatoryConcurrency()">
                <span class="toggle-slider" />
              </label>
            </div>
          </div>
        </section>

        <!-- AWG -->
        <section :id="sections[6].id" class="s-section">
          <h2 class="s-title">{{ sections[6].icon }} {{ sections[6].label }}</h2>

          <!-- Install -->
          <div class="s-block">
            <div class="s-row">
              <div class="s-row-main">
                <div class="s-row-label">amneziawg-go + amneziawg-tools</div>
                <div class="s-row-desc">{{ i18n.t('settings.awg_desc') }}</div>
              </div>
              <div v-if="awg.installed" class="badge badge-success">{{ i18n.t('settings.awg_installed') }}</div>
              <div v-else class="badge badge-muted">{{ i18n.t('settings.awg_not_installed') }}</div>
            </div>
            <div v-if="awg.progress" class="s-callout s-callout-info">{{ awg.progress }}</div>
            <div v-if="awg.error" class="s-callout s-callout-err">{{ awg.error }}</div>
            <div class="s-row s-row-actions">
              <button v-if="!awg.installed && !awg.installing" class="btn btn-primary" @click="installAWG()">
                {{ i18n.t('settings.awg_install_btn') }}
              </button>
              <button v-if="awg.installing" disabled class="btn btn-primary">{{ i18n.t('settings.awg_installing') }}</button>
              <button v-if="awg.installed && !awg.uninstalling" class="btn btn-danger" style="margin-left:auto" @click="uninstallAWG()">
                {{ i18n.t('settings.awg_uninstall_btn') }}
              </button>
              <button v-if="awg.uninstalling" disabled class="btn btn-danger">{{ i18n.t('settings.awg_uninstalling') }}</button>
            </div>
          </div>
        </section>

        <!-- Language -->
        <section :id="sections[7].id" class="s-section">
          <h2 class="s-title">{{ sections[7].icon }} {{ sections[7].label }}</h2>
          <div class="s-block">
            <div class="s-row">
              <div class="s-row-main">
                <div class="s-row-label">{{ i18n.t('settings.lang_label') }}</div>
              </div>
              <select :value="i18n.lang" class="s-select" @change="i18n.setLang($event.target.value)">
                <option value="ru">Русский</option>
                <option value="en">English</option>
              </select>
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
  display: flex;
}

/* ── Layout: sidebar nav + content ── */
.s-layout {
  display: flex;
  max-width: 960px;
  margin: 0 auto;
  width: 100%;
  min-height: 100%;
}

/* ── Sidebar nav — never scrolls ── */
.s-nav {
  width: 180px;
  flex-shrink: 0;
  padding: 14px 0 14px 14px;
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

/* ── Content — this scrolls ── */
.s-content {
  flex: 1;
  padding: 14px 24px 40px;
  min-width: 0;
  overflow-y: auto;
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
.s-row-col {
  flex-direction: column;
  align-items: stretch;
  gap: 6px;
}
.sb-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
  grid-template-rows: auto auto auto;
  gap: 4px 16px;
  padding: 10px 16px;
  border-bottom: 1px solid var(--menu-border);
}
.sb-field {
  display: grid;
  grid-template-rows: subgrid;
  grid-row: span 3;
  row-gap: 2px;
}
.sb-field .s-input {
  width: 100%;
  align-self: end;
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
.s-hint {
  font-size: 12px;
  color: var(--text-gray, #999);
  margin-left: 8px;
  white-space: nowrap;
}

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
  .s { height: auto; display: block; }
  .s-layout { flex-direction: column; min-height: auto; }
  .s-nav {
    width: auto;
    flex-direction: row;
    flex-wrap: wrap;
    padding: 10px 14px;
    gap: 4px;
  }
  .s-content { padding: 10px 14px 40px; overflow-y: visible; }
  .s-row { flex-direction: column; align-items: flex-start; gap: 8px; }
  .pw-grid { flex-direction: column; }
}

.s-changelog {
  margin-top: 16px;
}
.s-changelog > summary {
  cursor: pointer;
  font-weight: 600;
  padding: 8px 0 8px 20px;
  list-style: none;
}
.s-changelog > summary::-webkit-details-marker {
  display: none;
}
.s-changelog-body {
  max-height: 400px;
  overflow-y: auto;
  padding: 4px 0;
}
.s-changelog-rel {
  margin-bottom: 16px;
  margin-left: 10px;
}
.s-changelog-ver {
  font-weight: 700;
  margin-bottom: 4px;
}
.s-changelog-date {
  font-weight: 400;
  color: var(--text-muted, #888);
  font-size: 0.85rem;
  margin-left: 8px;
}
.s-changelog-body ul {
  margin: 0;
  padding-left: 20px;
}
.s-changelog-body li {
  padding: 2px 0;
  line-height: 1.4;
}
.s-changelog-important {
  font-weight: 600;
}
.s-changelog-type {
  margin-right: 4px;
}
</style>
