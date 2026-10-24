import { defineComponent, h, nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import { flushPromises, mount, type DOMWrapper, type VueWrapper } from '@vue/test-utils'
import { afterAll, afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import TitleTooltip from '@/components/common/TitleTooltip.vue'
import en from '@/i18n/locales/en/channelMonitorV2'
import zh from '@/i18n/locales/zh/channelMonitorV2'
import MonitorPlatformManager from '../MonitorPlatformManager.vue'

// Match vite.config.ts: the CSP-safe runtime compiles real locale messages in JIT mode.
vi.hoisted(() => { vi.stubGlobal('__INTLIFY_JIT_COMPILATION__', true) })
afterAll(() => vi.unstubAllGlobals())

vi.mock('@/api/channelMonitorV2', () => ({ getModelCatalog: vi.fn(async () => ({ items: [
  { platform: 'custom', model: 'Alpha', sources: ['success'], success_requests: 1, error_requests: 0 },
  { platform: 'custom', model: 'Beta', sources: ['error'], success_requests: 0, error_requests: 1 },
] })) }))
const copy = {
  zh: {
    name: '单独展示\n保存后，此模型单独成行，继续参与监控统计。',
    other: '移至其他\n保存后，此模型合并至“其他”，仍参与监控统计。',
    exclude: '排除监控\n保存后，此模型不再显示，也不计入“其他”、平台汇总等任何监控统计。',
    restore: '恢复监控\n保存后恢复统计；全部模型模式下单独展示，指定模型模式下归入“其他”。',
  },
  en: {
    name: 'Show individually\nAfter saving, this model gets its own row and still counts toward monitoring statistics.',
    other: 'Move to Other\nAfter saving, this model joins Other and still counts toward monitoring statistics.',
    exclude: 'Exclude from monitoring\nAfter saving, this model is hidden and excluded from all monitoring statistics, including Other and platform totals.',
    restore: 'Restore monitoring\nAfter saving, this model counts again: it gets its own row in All models mode or joins Other in Selected models mode.',
  },
}
let wrapper: VueWrapper | undefined
afterEach(async () => {
  wrapper?.unmount()
  wrapper = undefined
  await nextTick()
  document.body.innerHTML = ''
  vi.restoreAllMocks()
})
beforeEach(() => {
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({ x: 40, y: 80, left: 40, top: 80, right: 280, bottom: 112, width: 240, height: 32, toJSON: () => ({}) })
})
async function start(locale: 'zh' | 'en') {
  const i18n = createI18n({ legacy: false, locale, fallbackWarn: false, missingWarn: false, messages: { en, zh } })
  wrapper = mount(defineComponent({ setup: () => () => h('div', [
    h(MonitorPlatformManager, { modelValue: [{ platform: 'custom', enabled: true, models: ['Alpha'], model_display_mode: 'selected', excluded_models: ['Hidden'] }], groupIds: [] }),
    h(TitleTooltip),
  ]) }), { attachTo: document.body, global: { plugins: [i18n], stubs: { teleport: false } } })
  await flushPromises()
  return { i18n, view: wrapper.findComponent(MonitorPlatformManager) }
}
function card() { return document.querySelector<HTMLElement>('[role="tooltip"]')! }
async function show(button: DOMWrapper<Element>, keyboard = false) {
  if (keyboard) (button.element as HTMLButtonElement).focus()
  else button.element.dispatchEvent(new MouseEvent('pointerover', { bubbles: true }))
  await nextTick(); await nextTick()
  expect(card().matches('.title-tooltip.surface-panel')).toBe(true)
  expect(button.attributes('title')).toBe('')
  expect(button.attributes('aria-describedby')?.split(' ')).toContain(card().id)
}
async function hide(button: DOMWrapper<Element>) {
  (button.element as HTMLButtonElement).blur()
  button.element.dispatchEvent(new MouseEvent('pointerout', { bubbles: true }))
  await nextTick()
}

describe('approved model action tooltip semantics', () => {
  it.each(['zh', 'en'] as const)('shows shared card explanations for all four dynamically rendered actions in %s without editing config', async locale => {
    const { view } = await start(locale)
    for (const [category, action, keyboard] of [['named', 'other', false], ['named', 'exclude', true], ['other', 'name', false], ['excluded', 'restore', true]] as const) {
      await view.get(`[data-category="${category}"]`).trigger('click')
      const button = view.get(`[data-action="${action}"]`)
      await show(button, keyboard)
      expect(card().textContent).toBe(copy[locale][action])
      await hide(button)
    }
    expect(view.emitted('update:modelValue')).toBeUndefined()
  })

  it('updates the visible consequence when switching languages at runtime without editing config', async () => {
    const { view, i18n } = await start('zh')
    const button = view.get('[data-action="exclude"]')
    await show(button)
    expect(card().textContent).toBe(copy.zh.exclude)
    i18n.global.locale.value = 'en'
    await flushPromises(); await nextTick()
    expect(card().textContent).toBe(copy.en.exclude)
    expect(button.attributes('title')).toBe('')
    expect(button.attributes('aria-label')).toBe('Exclude Alpha from display and all statistics')
    expect(view.emitted('update:modelValue')).toBeUndefined()
  })
})
