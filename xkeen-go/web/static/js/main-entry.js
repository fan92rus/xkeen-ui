// main-entry.js - Bundle entry point
// This file imports all application modules and starts Alpine.js

// Import Alpine.js first
import Alpine from 'alpinejs';

// Make Alpine globally available (required for x-data, x-store, etc.)
window.Alpine = Alpine;

// Import i18n first (sets up global translation functions)
import './i18n.js';

// Import all services (they don't register anything, just export functions)
import './services/api.js';
import './services/config.js';
import './services/logs.js';
import './services/xkeen.js';
import './services/update.js';
import './services/status.js';
import './services/mode.js';
import './services/interactive.js';

// Import store (registers Alpine.store on alpine:init)
import './store.js';

// Import app.js (keyboard shortcuts)
import './app.js';

// Import all components (they register Alpine.data on alpine:init)
import './components/editor.js';
import './components/logs.js';
import './components/service.js';
import './components/commands.js';

// Start Alpine.js after all modules are loaded
Alpine.start();

console.log('XKEEN-UI bundle loaded');
