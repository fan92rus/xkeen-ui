// stores/i18n.js — Pinia store for internationalisation
import { defineStore } from 'pinia';
import { ref, computed } from 'vue';
import { t as resolveT, detectLang, persistLang } from '../utils/i18n.js';
import ru from '../locales/ru.json';
import en from '../locales/en.json';

const LOCALE_MAP = { ru, en };

export const useI18nStore = defineStore('i18n', () => {
  const lang = ref(detectLang());

  const translations = computed(() => LOCALE_MAP[lang.value] || LOCALE_MAP.en);

  function setLang(newLang) {
    if (newLang !== 'ru' && newLang !== 'en') return;
    lang.value = newLang;
    persistLang(newLang);
  }

  /**
   * Translate a dot-notation key, optionally interpolating {params}.
   * Re-computes when lang changes via the `lang` ref dependency.
   */
  function t(key, params) {
    return resolveT(key, params, translations.value);
  }

  return { lang, setLang, t };
});
