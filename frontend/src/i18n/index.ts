import i18next, { type i18n as I18nInstance } from 'i18next'
import LanguageDetector from 'i18next-browser-languagedetector'
import { initReactI18next } from 'react-i18next'

export type LocaleCode = 'en' | 'zh'

const LOCALE_KEY = 'sub2api_locale'
const DEFAULT_LOCALE: LocaleCode = 'en'

type LocaleMessages = Record<string, unknown>

const localeLoaders: Record<LocaleCode, () => Promise<{ default: LocaleMessages }>> = {
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

export function getDefaultLocale(): LocaleCode {
  const stored = getStoredLocale()
  if (stored) return stored
  const browserLang = navigator.language.toLowerCase()
  if (browserLang.startsWith('zh')) return 'zh'
  return DEFAULT_LOCALE
}

const loadedLocales = new Set<LocaleCode>()
/** Bundles fetched before `initI18n()` ran; applied when the instance starts. */
const pendingBundles = new Map<LocaleCode, LocaleMessages>()

function applyBundle(instance: I18nInstance, locale: LocaleCode, messages: LocaleMessages): void {
  if (instance.isInitialized) {
    instance.addResourceBundle(locale, 'translation', messages, true, true)
  } else {
    pendingBundles.set(locale, messages)
  }
}

export async function loadLocaleMessages(locale: LocaleCode): Promise<void> {
  if (loadedLocales.has(locale)) return
  const module = await localeLoaders[locale]()
  applyBundle(i18next, locale, module.default)
  loadedLocales.add(locale)
}

export async function initI18n(): Promise<void> {
  const current = getDefaultLocale()
  await loadLocaleMessages(current)

  const resources: Record<string, { translation: LocaleMessages }> = {}
  for (const [locale, messages] of pendingBundles) {
    resources[locale] = { translation: messages }
  }
  resources[current] = { translation: (await localeLoaders[current]()).default }

  await i18next.use(LanguageDetector).use(initReactI18next).init({
    resources,
    lng: current,
    fallbackLng: DEFAULT_LOCALE,
    defaultNS: 'translation',
    interpolation: { escapeValue: false },
    returnNull: false,
    react: { useSuspense: false }
  })

  // Anything loaded during `await` above still needs to land on the instance.
  for (const [locale, messages] of pendingBundles) {
    i18next.addResourceBundle(locale, 'translation', messages, true, true)
  }
  pendingBundles.clear()

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
