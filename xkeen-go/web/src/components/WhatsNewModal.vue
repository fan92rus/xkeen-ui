<script setup>
import { ref, onMounted, nextTick } from 'vue';
import { getWhatsNew, ackWhatsNew } from '../services/changelog.js';
import { useI18nStore } from '../stores/i18n.js';

const i18n = useI18nStore();

const show = ref(false);
const fromVersion = ref('');
const toVersion = ref('');
const categories = ref([]);
const modalRef = ref(null);

onMounted(async () => {
	try {
		const data = await getWhatsNew();
		if (data.show) {
			show.value = true;
			fromVersion.value = data.from_version || '';
			toVersion.value = data.to_version || '';
			categories.value = data.categories || [];
			await nextTick();
			modalRef.value?.focus();
		}
	} catch {
		/* silently ignore — modal is non-critical */
	}
});

async function dismiss() {
	show.value = false;
	try {
		await ackWhatsNew();
	} catch {
		/* ignore ack errors */
	}
}

function onKeydown(e) {
	if (e.key === 'Escape') dismiss();
}
</script>

<template>
  <div v-if="show" class="wn-overlay" @click.self="dismiss">
    <div
      ref="modalRef"
      class="wn-box"
      tabindex="-1"
      role="dialog"
      aria-modal="true"
      @keydown="onKeydown"
    >
      <div class="wn-header">
        <h3>✨ {{ i18n.t('whatsnew.title') }} {{ toVersion }}</h3>
      </div>
      <div class="wn-body">
        <p v-if="fromVersion" class="wn-subtitle">
          {{ i18n.t('whatsnew.subtitle', { from: fromVersion }) }}
        </p>
        <div v-for="cat in categories" :key="cat.type" class="wn-category">
          <div class="wn-cat-title">{{ cat.icon }} {{ cat.label }}</div>
          <ul>
            <li
              v-for="(change, idx) in cat.changes"
              :key="idx"
              :class="{ 'wn-important': change.important }"
            >
              {{ change.text }}
              <span v-if="change.important" class="wn-star">★</span>
            </li>
          </ul>
        </div>
      </div>
      <div class="wn-footer">
        <button class="btn btn-primary" @click="dismiss">
          {{ i18n.t('whatsnew.dismiss') }}
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.wn-overlay {
	position: fixed;
	inset: 0;
	background: rgba(0, 0, 0, 0.55);
	display: flex;
	align-items: center;
	justify-content: center;
	z-index: 9999;
}

.wn-box {
	background: var(--bg-surface, #1e1e2e);
	border: 1px solid var(--border-color, #333346);
	border-radius: 12px;
	max-width: 480px;
	width: 90%;
	max-height: 80vh;
	overflow-y: auto;
	outline: none;
	box-shadow: 0 8px 32px rgba(0, 0, 0, 0.4);
}

.wn-header {
	padding: 20px 24px 12px;
}

.wn-header h3 {
	margin: 0;
	font-size: 1.2rem;
}

.wn-body {
	padding: 0 24px 16px;
}

.wn-subtitle {
	color: var(--text-muted, #888);
	font-size: 0.9rem;
	margin: 0 0 12px;
}

.wn-category {
	margin-bottom: 16px;
}

.wn-cat-title {
	font-weight: 600;
	margin-bottom: 6px;
	color: var(--text-primary, #cdd6f4);
}

.wn-category ul {
	margin: 0;
	padding-left: 20px;
	list-style: none;
}

.wn-category li {
	padding: 3px 0;
	color: var(--text-secondary, #a6adc8);
	line-height: 1.4;
}

.wn-category li::before {
	content: '•';
	color: var(--text-muted, #888);
	margin-right: 8px;
}

.wn-important {
	font-weight: 600;
}

.wn-star {
	color: #f9e2af;
	margin-left: 4px;
}

.wn-footer {
	padding: 12px 24px 20px;
	text-align: right;
}
</style>
