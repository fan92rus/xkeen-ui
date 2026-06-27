// utils/i18n.js — pure i18n helpers
// Translation files are loaded by the i18n store, not here.
// This module provides the pure lookup function and language detection.

const STORAGE_KEY = 'xkeen_lang';
const FALLBACK_LANG = 'en';

/**
 * Detect browser language: 'ru' if navigator starts with 'ru', else 'en'.
 */
export function detectLang() {
  try {
    const stored = localStorage.getItem(STORAGE_KEY);
    if (stored === 'ru' || stored === 'en') return stored;
  } catch { /* localStorage unavailable */ }

  if (typeof navigator !== 'undefined' && navigator.language) {
    if (navigator.language.startsWith('ru')) return 'ru';
  }
  return FALLBACK_LANG;
}

/**
 * Save language preference to localStorage.
 */
export function persistLang(lang) {
  try {
    localStorage.setItem(STORAGE_KEY, lang);
  } catch { /* noop */ }
}

/**
 * Translate key using the given translations object.
 * Key is dot-notation: 'nav.editor' -> translations.nav.editor
 * Params: t('greeting', { name: 'World' }, translations) for "{name}" replacement.
 * Returns the key itself if not found (visible fallback).
 */
export function t(key, params, translations) {
  if (!translations) return key;
  let value = key.split('.').reduce((o, k) => (o && typeof o === 'object' ? o[k] : undefined), translations);
  if (value === undefined || value === null) return key;

  if (params && typeof params === 'object') {
    for (const [k, v] of Object.entries(params)) {
      value = String(value).replace(`{${k}}`, String(v));
    }
  }
  return String(value);
}
