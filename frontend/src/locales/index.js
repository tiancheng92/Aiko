/** i18n initialization — detects locale and creates vue-i18n instance. */
import { createI18n } from 'vue-i18n'
import zhCN from './zh-CN.json'
import en from './en.json'
import ja from './ja.json'
import ko from './ko.json'

const SUPPORTED = ['zh-CN', 'en', 'ja', 'ko']
const FALLBACK = 'en'

const messages = { 'zh-CN': zhCN, en, ja, ko }

/**
 * detectLocale resolves the UI language from config preference and system locale.
 * @param {string} configLanguage - value from Go config.Language, empty means follow system
 * @returns {string} a supported locale code
 */
export function detectLocale(configLanguage) {
  if (configLanguage && SUPPORTED.includes(configLanguage)) return configLanguage
  const sys = navigator.language       // e.g. "zh-Hans-CN"
  const short = sys.slice(0, 2)        // "zh"
  return SUPPORTED.find(l => l.startsWith(short)) || FALLBACK
}

/**
 * createI18nInstance creates a vue-i18n instance for the given locale.
 * @param {string} locale - one of SUPPORTED
 * @returns {import('vue-i18n').I18n}
 */
export function createI18nInstance(locale) {
  return createI18n({
    legacy: false,
    locale,
    fallbackLocale: FALLBACK,
    messages,
  })
}
