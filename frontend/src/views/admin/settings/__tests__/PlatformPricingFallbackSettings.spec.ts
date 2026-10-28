import { enableAutoUnmount, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import { chooseSelect, openSelect } from '@/__tests__/helpers/select'
import {
  BUILTIN_PLATFORM_CATALOG,
  resetPlatformCatalog,
  setPlatformCatalog,
} from '@/constants/platformCatalog'
import enSettings from '@/i18n/locales/en/admin/settings'
import zhSettings from '@/i18n/locales/zh/admin/settings'
import PlatformPricingFallbackSettings from '../PlatformPricingFallbackSettings.vue'

vi.mock('@/api/admin/accounts', () => ({
  getAntigravityDefaultModelMapping: vi.fn().mockResolvedValue({}),
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, string>) => {
      const field = key.replace('admin.settings.pricingFallback.', '')
      const message = (enSettings.settings.pricingFallback as Record<string, string>)[field] ?? key
      return message.replace(/\{(\w+)\}/g, (_, name: string) => params?.[name] ?? `{${name}}`)
    },
  }),
}))

enableAutoUnmount(afterEach)
afterEach(() => {
  resetPlatformCatalog()
  document.body.innerHTML = ''
  vi.restoreAllMocks()
})

describe('platform pricing fallback settings', () => {
  it('offers all concrete platforms with model suggestions inside the shared dropdown', async () => {
    const wrapper = mount(PlatformPricingFallbackSettings, { props: { modelValue: {} } })
    const platforms = wrapper.findAll('[data-pricing-fallback-platform]')
      .map(row => row.attributes('data-pricing-fallback-platform'))

    expect(platforms).toEqual([
      'anthropic', 'openai', 'gemini', 'antigravity', 'grok', 'kimi', 'zhipu',
      'deepseek', 'minimax', 'stepfun', 'opencode_go', 'typesafe', 'command_code', 'cline',
    ])
    expect(wrapper.find('select').exists()).toBe(false)
    expect(wrapper.find('input').exists()).toBe(false)

    const row = wrapper.get('[data-pricing-fallback-platform="openai"]')
    const panel = await openSelect(row)
    expect(panel.element.parentElement).toBe(document.body)
    expect(panel.find('.select-search-input').exists()).toBe(true)
    expect(panel.findAll('[role="option"]').map(option => option.text())).toContain('gpt-6.1-terra')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('keeps a saved aggregate-platform SKU visible even when the platform has no suggestions', async () => {
    const wrapper = mount(PlatformPricingFallbackSettings, {
      props: { modelValue: { command_code: 'gpt-6.1-sol' } },
    })
    const row = wrapper.get('[data-pricing-fallback-platform="command_code"]')
    expect(row.get('.select-trigger').text()).toContain('gpt-6.1-sol')
    const panel = await openSelect(row)
    expect(panel.findAll('[role="option"]').map(option => option.text())).toEqual(['Disabled', 'gpt-6.1-sol'])
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('accepts an exact custom SKU for an aggregate platform without a platform-reference restriction', async () => {
    const saved = { openai: 'gpt-6.1-sol', command_code: 'claude-opus-4-8', future_platform: 'custom-sku' }
    const wrapper = mount(PlatformPricingFallbackSettings, { props: { modelValue: saved } })
    const row = wrapper.get('[data-pricing-fallback-platform="command_code"]')
    const panel = await openSelect(row)
    await panel.get('.select-search-input').setValue('  gpt-6.1-terra  ')
    await chooseSelect(row, 'Use model SKU "gpt-6.1-terra"')

    expect(wrapper.emitted('update:modelValue')).toEqual([[{
      openai: 'gpt-6.1-sol', command_code: 'gpt-6.1-terra', future_platform: 'custom-sku',
    }]])
    expect(saved.command_code).toBe('claude-opus-4-8')
  })

  it('stores an explicit empty string when clearing, preserving every other platform', async () => {
    const saved = { openai: 'gpt-6.1-sol', cline: 'claude-opus-4-8', future_platform: 'custom-sku' }
    const wrapper = mount(PlatformPricingFallbackSettings, { props: { modelValue: saved } })
    await wrapper.get('[data-pricing-fallback-platform="openai"] .select-clear').trigger('click')

    const cleared = { openai: '', cline: 'claude-opus-4-8', future_platform: 'custom-sku' }
    expect(wrapper.emitted('update:modelValue')).toEqual([[cleared]])
    expect(saved.openai).toBe('gpt-6.1-sol')
    await wrapper.setProps({ modelValue: cleared })
    const row = wrapper.get('[data-pricing-fallback-platform="openai"]')
    expect(row.get('.select-trigger').text()).toContain('Disabled')
    expect(row.get('[data-testid="pricing-fallback-disabled"]').text()).toContain('disabled')
  })

  it('restores defaults with an empty map while distinguishing inherited and disabled values', async () => {
    const wrapper = mount(PlatformPricingFallbackSettings, {
      props: { modelValue: { openai: '', cline: 'gpt-6.1-sol' } },
    })
    await wrapper.get('[data-testid="pricing-fallback-restore"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([[{}]])
    await wrapper.setProps({ modelValue: {} })
    expect(wrapper.get('[data-testid="pricing-fallback-defaults-pending"]').text()).toContain('when you save')
    expect(wrapper.get('[data-pricing-fallback-platform="openai"] .select-trigger').text()).toContain('Project default')
    expect(wrapper.find('[data-testid="pricing-fallback-disabled"]').exists()).toBe(false)
  })

  it('includes newly registered platforms and permits custom priced SKUs with an empty directory', async () => {
    const wrapper = mount(PlatformPricingFallbackSettings, { props: { modelValue: {} } })
    setPlatformCatalog({
      ...BUILTIN_PLATFORM_CATALOG,
      platforms: [...BUILTIN_PLATFORM_CATALOG.platforms, {
        id: 'future_aggregate', display_name: 'Future Aggregate', gateway: 'openai', cn_provider: false,
        multi_protocol: { default_mode: 'payg', routing: 'by_inbound', modes: [] },
      }],
    })
    await nextTick()
    const row = wrapper.get('[data-pricing-fallback-platform="future_aggregate"]')
    const panel = await openSelect(row)
    expect(panel.findAll('[role="option"]').map(option => option.text())).toEqual(['Disabled'])
    await panel.get('.select-search-input').setValue('claude-opus-4-8')
    await chooseSelect(row, 'Use model SKU "claude-opus-4-8"')
    expect(wrapper.emitted('update:modelValue')).toEqual([[{ future_aggregate: 'claude-opus-4-8' }]])
  })

  it('can disable an inherited platform default before the defaults are saved', async () => {
    const wrapper = mount(PlatformPricingFallbackSettings, { props: { modelValue: {} } })
    await chooseSelect(wrapper.get('[data-pricing-fallback-platform="openai"]'), 'Disabled')
    expect(wrapper.emitted('update:modelValue')).toEqual([[{ openai: '' }]])
  })

  it('disables edits and reset while the containing settings form is unavailable', async () => {
    const wrapper = mount(PlatformPricingFallbackSettings, {
      props: { modelValue: { openai: 'gpt-6.1-sol' }, disabled: true },
    })
    for (const trigger of wrapper.findAll('.select-trigger')) {
      expect(trigger.attributes('disabled')).toBeDefined()
    }
    await wrapper.get('[data-testid="pricing-fallback-restore"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('explains pricing precedence and unchanged request models in both languages', () => {
    expect(enSettings.settings.pricingFallback.precedence).toBe('Pricing priority: configured model price → system price for the same model SKU → this platform’s fallback model price.')
    expect(zhSettings.settings.pricingFallback.precedence).toBe('价格优先级：配置模型价格 → 系统同模型 SKU 价格 → 该平台兜底模型价格。')
    expect(enSettings.settings.pricingFallback.description).toContain('request and upstream model stay unchanged')
    expect(zhSettings.settings.pricingFallback.description).toContain('不改变请求模型或上游模型')
  })
})
