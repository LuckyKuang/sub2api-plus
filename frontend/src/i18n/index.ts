import i18next from 'i18next'
import LanguageDetector from 'i18next-browser-languagedetector'
import { initReactI18next } from 'react-i18next'

export type LocaleCode = 'en' | 'zh'

const LOCALE_KEY = 'sub2api_locale'
const DEFAULT_LOCALE: LocaleCode = 'en'

const localeLoaders: Record<LocaleCode, () => Promise<{ default: Record<string, unknown> }>> = {
  en: () => import('./locales/en'),
  zh: () => import('./locales/zh')
}

export function isLocaleCode(value: string): value is LocaleCode {
  return value === 'en' || value === 'zh'
}

function getStoredLocale(): LocaleCode | null {
  try {
    const saved = localStorage.getItem(LOCALE_KEY)
    return saved && isLocaleCode(saved) ? saved : null
  } catch {
    return null
  }
}

function getDefaultLocale(): LocaleCode {
  const stored = getStoredLocale()
  if (stored) return stored
  const browserLang = navigator.language.toLowerCase()
  if (browserLang.startsWith('zh')) return 'zh'
  return DEFAULT_LOCALE
}

const loadedLocales = new Set<LocaleCode>()

export async function loadLocaleMessages(locale: LocaleCode): Promise<void> {
  if (loadedLocales.has(locale)) return
  const module = await localeLoaders[locale]()
  i18next.addResourceBundle(locale, 'translation', module.default, true, true)
  loadedLocales.add(locale)
}

export async function initI18n(): Promise<void> {
  const current = getDefaultLocale()
  await loadLocaleMessages(current)
  await i18next
    .use(LanguageDetector)
    .use(initReactI18next)
    .init({
      resources: {
        [current]: { translation: (await localeLoaders[current]()).default }
      },
      lng: current,
      fallbackLng: DEFAULT_LOCALE,
      defaultNS: 'translation',
      interpolation: { escapeValue: false },
      // 禁用 HTML 消息警告 - 引导步骤使用富文本内容（driver.js 支持 HTML）
      // 这些内容是内部定义的，不存在 XSS 风险
      returnNull: false,
      react: { useSuspense: false }
    })
  document.documentElement.setAttribute('lang', current)
}

export async function setLocale(locale: string): Promise<void> {
  if (!isLocaleCode(locale)) return
  const changed = getLocale() !== locale
  await loadLocaleMessages(locale)
  await i18next.changeLanguage(locale)
  try {
    localStorage.setItem(LOCALE_KEY, locale)
  } catch {
    // ignore storage failures
  }
  document.documentElement.setAttribute('lang', locale)
  if (changed) {
    const { notifyLocaleChanged } = await import('@/utils/localeRefresh')
    notifyLocaleChanged()
    const { refreshDocumentTitle } = await import('@/router/title')
    refreshDocumentTitle()
  }
}

export function getLocale(): LocaleCode {
  const current = i18next.resolvedLanguage || i18next.language
  return isLocaleCode(current) ? current : DEFAULT_LOCALE
}

export const availableLocales = [
  { code: 'en', name: 'English', flag: '🇺🇸' },
  { code: 'zh', name: '中文', flag: '🇨🇳' }
] as const

export default i18next
