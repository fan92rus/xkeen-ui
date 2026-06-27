// @vitest-environment happy-dom
// Tests for i18n utility and Pinia store
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { setActivePinia, createPinia } from 'pinia';
import { t as resolveT, detectLang, persistLang } from '../src/utils/i18n.js';

const mockTranslations = {
  nav: { editor: 'Editor', logs: 'Logs' },
  toast: { saved: 'Saved', greet: 'Hello {name}' },
  missing: '',
};

describe('i18n utils — detectLang', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it('returns "ru" when navigator.language starts with ru', () => {
    Object.defineProperty(navigator, 'language', { value: 'ru-RU', configurable: true });
    expect(detectLang()).toBe('ru');
  });

  it('returns "en" when navigator.language is non-Russian', () => {
    Object.defineProperty(navigator, 'language', { value: 'en-US', configurable: true });
    expect(detectLang()).toBe('en');
  });

  it('returns stored preference from localStorage', () => {
    localStorage.setItem('xkeen_lang', 'ru');
    Object.defineProperty(navigator, 'language', { value: 'en-US', configurable: true });
    expect(detectLang()).toBe('ru');
  });

  it('falls back to "en" when no preference and no navigator', () => {
    const nav = global.navigator;
    delete global.navigator;
    expect(detectLang()).toBe('en');
    global.navigator = nav;
  });
});

describe('i18n utils — persistLang', () => {
  beforeEach(() => { localStorage.clear(); });

  it('saves to localStorage', () => {
    persistLang('ru');
    expect(localStorage.getItem('xkeen_lang')).toBe('ru');
  });
});

describe('i18n utils — t (pure function)', () => {
  it('resolves dot-notation keys', () => {
    expect(resolveT('nav.editor', undefined, mockTranslations)).toBe('Editor');
    expect(resolveT('toast.saved', undefined, mockTranslations)).toBe('Saved');
  });

  it('returns the key itself when not found', () => {
    expect(resolveT('nonexistent.key', undefined, mockTranslations)).toBe('nonexistent.key');
  });

  it('handles {param} interpolation', () => {
    expect(resolveT('toast.greet', { name: 'World' }, mockTranslations)).toBe('Hello World');
  });

  it('returns key when translations is undefined', () => {
    expect(resolveT('nav.editor', undefined, undefined)).toBe('nav.editor');
  });
});

describe('useI18nStore — Pinia integration', () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    localStorage.clear();
  });

  it('switches lang and updates t() output', async () => {
    const { useI18nStore } = await import('../src/stores/i18n.js');
    const store = useI18nStore();

    // set to English first
    store.setLang('en');
    expect(store.lang).toBe('en');
    expect(store.t('nav.editor')).toBe('Editor');

    // switch to Russian
    store.setLang('ru');
    expect(store.lang).toBe('ru');
    expect(store.t('nav.editor')).toBe('Редактор');
  });

  it('setLang ignores invalid language codes', async () => {
    const { useI18nStore } = await import('../src/stores/i18n.js');
    const store = useI18nStore();
    store.setLang('fr');
    expect(store.lang).toBe('en'); // fallback
  });
});
