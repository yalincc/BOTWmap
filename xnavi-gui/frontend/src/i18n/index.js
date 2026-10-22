import { createI18n } from 'vue-i18n'
import zhCN from './zh-CN'
import enUS from './en-US'

export const LOCALES = { SYSTEM: '', ZH: 'zh-CN', EN: 'en-US' }
const STORAGE_KEY = 'xnavi.lang'

function detectSystem() {
  const nav = (navigator.language || 'en').toLowerCase()
  return nav.startsWith('zh') ? LOCALES.ZH : LOCALES.EN
}

function initialLocale() {
  try {
    const saved = localStorage.getItem(STORAGE_KEY)
    if (saved === LOCALES.ZH || saved === LOCALES.EN) return saved
  } catch {}
  return detectSystem()
}

export const i18n = createI18n({
  legacy: false,
  globalInjection: true,
  locale: initialLocale(),
  fallbackLocale: LOCALES.EN,
  messages: { 'zh-CN': zhCN, 'en-US': enUS },
  missingWarn: false,
  fallbackWarn: false,
})

export function currentLocale() {
  try { return localStorage.getItem(STORAGE_KEY) || LOCALES.SYSTEM } catch { return LOCALES.SYSTEM }
}

export function isEnglish() {
  return i18n.global.locale.value === LOCALES.EN
}

export function setLocale(v) {
  const next = v === LOCALES.SYSTEM ? detectSystem() : v
  i18n.global.locale.value = next
  try {
    if (v === LOCALES.SYSTEM) localStorage.removeItem(STORAGE_KEY)
    else localStorage.setItem(STORAGE_KEY, v)
  } catch {}
}

