// Logger utility — wraps console with environment-aware toggling.
// In production (Vite build), console output is disabled to avoid
// leaking debug info and to keep the bundle clean.
// In dev (vite dev server), all log levels pass through.
//
// Usage: import { log, warn, error } from '../utils/logger.js';
//        error('[sub] failed:', e);

const isDev = typeof import.meta !== 'undefined' && import.meta.env?.MODE === 'development';

const noop = () => {};

export const log   = isDev ? console.log.bind(console, '[xkeen]')   : noop;
export const warn  = isDev ? console.warn.bind(console, '[xkeen]')  : noop;
export const error = isDev ? console.error.bind(console, '[xkeen]') : noop;
