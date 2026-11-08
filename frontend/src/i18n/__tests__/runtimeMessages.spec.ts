import { beforeAll, describe, expect, it } from 'vitest'
import i18next from 'i18next'
import { initReactI18next } from 'react-i18next'

import { initI18n, loadLocaleMessages } from '../index'

async function createInstance(
  locale: 'en' | 'zh',
  messages: Record<string, unknown>,
  options: { escapeValue?: boolean } = {}
) {
  const instance = i18next.createInstance()
  await instance.use(initReactI18next).init({
    lng: locale,
    fallbackLng: locale,
    defaultNS: 'translation',
    resources: { [locale]: { translation: messages } },
    interpolation: { escapeValue: options.escapeValue ?? false },
    initImmediate: false,
    react: { useSuspense: false }
  })
  return instance
}

describe('runtime locale messages', () => {
  beforeAll(async () => {
    await initI18n()
    await Promise.all([loadLocaleMessages('en'), loadLocaleMessages('zh')])
  })

  it('interpolates lazy-loaded English and Chinese messages after locale changes', async () => {
    await i18next.changeLanguage('en')
    expect(i18next.t('batchImage.pagination.pageNumber', { page: 3 })).toBe('Page 3')
    await i18next.changeLanguage('zh')
    expect(i18next.t('batchImage.pagination.pageNumber', { page: 3 })).toBe('第 3 页')
    await i18next.changeLanguage('en')
    expect(i18next.t('batchImage.pagination.pageNumber', { page: 4 })).toBe('Page 4')
  })

  it('selects the real batch-job singular and plural messages', async () => {
    await i18next.changeLanguage('en')
    expect(i18next.t('batchImage.list.selectedJobs', { count: 1 })).toBe('Selected 1 job')
    expect(i18next.t('batchImage.list.selectedJobs', { count: 2 })).toBe('Selected 2 jobs')
    await i18next.changeLanguage('zh')
    expect(i18next.t('batchImage.list.selectedJobs', { count: 2 })).toBe('已选择 2 个任务')
  })

  it('interpolates named values when falling back to English', async () => {
    const instance = await createInstance('zh', {}, {})
    instance.addResourceBundle('en', 'translation', { fallback: 'Hello {{name}}' }, true, true)
    await instance.changeLanguage('en')
    expect(instance.t('fallback', { name: 'Ada' })).toBe('Hello Ada')
  })

  it('preserves the trusted onboarding markup used by the tour', async () => {
    await i18next.changeLanguage('en')
    const html = i18next.t('onboarding.admin.welcome.description')
    const content = document.createElement('template')
    content.innerHTML = html
    expect(content.content.querySelectorAll('ul > li')).toHaveLength(4)
    expect(content.content.querySelector('li b')?.textContent).toBe('Group Management')
    expect(content.content.textContent).toContain('Sub2API Plus is a powerful AI service gateway')
  })

  it('escapes interpolated HTML parameters so translated markup cannot inject nodes', async () => {
    // GHSA-83rm-6c8r-g728: an interpolated value must never be able to open an
    // element inside a translated string. The application renders rich tour
    // copy, so only interpolated parameters are escaped, never the template.
    const instance = await createInstance(
      'en',
      { parameter: '<span>{{content}}</span>' },
      { escapeValue: true }
    )
    const html = instance.t('parameter', { content: '<img src=x onerror=alert(1)>' })
    const content = document.createElement('template')
    content.innerHTML = html
    expect(content.content.querySelector('img')).toBeNull()
    expect(content.content.textContent).toBe('<img src=x onerror=alert(1)>')
  })

  it('loads both locales independently of the shared instance', async () => {
    await loadLocaleMessages('zh')
    expect(i18next.exists('batchImage.pagination.pageNumber')).toBe(true)
    await i18next.changeLanguage('zh')
    expect(i18next.t('batchImage.pagination.pageNumber', { page: 7 })).toBe('第 7 页')
  })
})
