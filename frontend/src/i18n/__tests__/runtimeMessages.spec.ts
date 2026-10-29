import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import { i18n, loadLocaleMessages } from '../index'

// Vitest and the application use the same compiler-inclusive bundler alias.
// Verify rendered translation contracts in addition to locale syntax compilation.
describe('runtime locale messages', () => {
  beforeAll(async () => {
    await Promise.all([loadLocaleMessages('en'), loadLocaleMessages('zh')])
  })

  afterEach(() => {
    i18n.global.locale.value = 'en'
    vi.restoreAllMocks()
  })

  it('interpolates lazy-loaded English and Chinese messages after locale changes', () => {
    i18n.global.locale.value = 'en'
    expect(i18n.global.t('batchImage.pagination.pageNumber', { page: 3 })).toBe('Page 3')
    i18n.global.locale.value = 'zh'
    expect(i18n.global.t('batchImage.pagination.pageNumber', { page: 3 })).toBe('第 3 页')
    i18n.global.locale.value = 'en'
    expect(i18n.global.t('batchImage.pagination.pageNumber', { page: 4 })).toBe('Page 4')
  })

  it('selects the real batch-job singular and plural messages', () => {
    i18n.global.locale.value = 'en'
    expect(i18n.global.t('batchImage.list.selectedJobs', { count: 1 })).toBe('Selected 1 job')
    expect(i18n.global.t('batchImage.list.selectedJobs', { count: 2 })).toBe('Selected 2 jobs')
    i18n.global.locale.value = 'zh'
    expect(i18n.global.t('batchImage.list.selectedJobs', { count: 2 })).toBe('已选择 2 个任务')
  })

  it('interpolates list values and named values when falling back to English', () => {
    const { global } = createI18n({
      legacy: false,
      locale: 'zh',
      fallbackLocale: 'en',
      missingWarn: false,
      fallbackWarn: false,
      messages: {
        en: { page: 'Page {0} of {1}', fallback: 'Hello {name}' },
        zh: { page: '第 {0} 页，共 {1} 页' },
      },
    })
    expect(global.t('page', [2, 8])).toBe('第 2 页，共 8 页')
    expect(global.t('fallback', { name: 'Ada' })).toBe('Hello Ada')
    global.locale.value = 'en'
    expect(global.t('page', [2, 8])).toBe('Page 2 of 8')
  })

  it('compiles string messages without evaluating JavaScript source', () => {
    const { global } = createI18n({
      legacy: false,
      locale: 'en',
      messages: { en: { csp: 'CSP-safe translation for {name}' } },
    })
    const dynamicFunction = vi.spyOn(globalThis, 'Function').mockImplementation(() => {
      throw new Error('unsafe-eval is disabled')
    })
    expect(global.t('csp', { name: 'Ada' })).toBe('CSP-safe translation for Ada')
    expect(dynamicFunction).not.toHaveBeenCalled()
  })

  it('preserves the trusted onboarding markup used by the tour', () => {
    i18n.global.locale.value = 'en'
    const html = i18n.global.t('onboarding.admin.welcome.description')
    const content = document.createElement('template')
    content.innerHTML = html
    expect(content.content.querySelectorAll('ul > li')).toHaveLength(4)
    expect(content.content.querySelector('li b')?.textContent).toBe('Group Management')
    expect(content.content.textContent).toContain('Sub2API Plus is a powerful AI service gateway')
  })

  it('neutralizes srcdoc in translated HTML with escaped parameters', () => {
    // GHSA-83rm-6c8r-g728: escaping the attribute value alone is insufficient,
    // since the browser decodes it before parsing an iframe document.
    vi.spyOn(console, 'warn').mockImplementation(() => undefined)
    const { global } = createI18n({
      legacy: false,
      locale: 'en',
      warnHtmlMessage: false,
      escapeParameter: true,
      messages: {
        en: {
          literal: '<iframe srcdoc="<b>unexpected document</b>"></iframe>',
          parameter: '<iframe srcdoc="{content}"></iframe>',
        },
      },
    })
    for (const html of [
      global.t('literal'),
      global.t('parameter', { content: '<img src=x onerror=alert(1)>' }),
    ]) {
      const content = document.createElement('template')
      content.innerHTML = html
      const frame = content.content.querySelector('iframe')
      expect(frame).not.toBeNull()
      expect(frame!.hasAttribute('srcdoc')).toBe(false)
    }
  })
})
