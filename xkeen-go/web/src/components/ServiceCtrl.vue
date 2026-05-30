<script setup>
import { useAppStore } from '../stores/app.js';
const app = useAppStore();
</script>

<template>
  <div class="service-controls">
    <span :class="'status-' + app.serviceStatus">{{ app.serviceStatus }}</span>
    <button @click="app.startService()" :disabled="app.serviceStatus === 'running'" class="btn btn-sm">Запуск</button>
    <button @click="app.stopService()" :disabled="app.serviceStatus === 'stopped'" class="btn btn-sm">Стоп</button>
  </div>
</template>
