import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import LongContextPolicyFields from '../LongContextPolicyFields.vue'
import BillingRatePolicyFields from '../BillingRatePolicyFields.vue'
import type { BillingRatePolicy } from '@/types/billingRate'
const api = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: api }))
beforeEach(() => api.get.mockResolvedValue({ data: { openai: [{ model: 'gpt-6.1-sol', threshold: 272000 }, { model: 'gpt-small', threshold: 0 }], anthropic: [{ model: 'claude-opus-5-5', threshold: 200000 }] } }))
// The two sibling editors must preserve the whole saved policy when editing
// ordinary rates, explicit zero, high-tier prices or a restored threshold.
it('retains zero and custom prices across edits from both policy editors', async () => {
 const page = mount(defineComponent({ components: { LongContextPolicyFields, BillingRatePolicyFields }, setup: () => ({ policy: ref<BillingRatePolicy>({ platform_multipliers: { openai: 1 }, model_multipliers: [], long_context_platforms: { openai: 0 }, long_context_models: [{ platform: 'openai', model: 'gpt-6.1-sol', threshold: 0, mode: 'prices', input: 10, output: 20, cache_read: 0 }] }) }), template: '<BillingRatePolicyFields v-model="policy" platform="openai" id-prefix="test" :show-models="false" /><LongContextPolicyFields v-model="policy" platform="openai" />' }), { global: { plugins: [createI18n({ legacy: false, locale: 'en', missingWarn: false, fallbackWarn: false, messages: { en: {} } })] } })
 await page.get('#test-openai-rate').setValue('2')
 expect(page.vm.policy.long_context_models![0]).toMatchObject({ threshold: 0, input: 10, output: 20, cache_read: 0 })
 const editor = page.getComponent(LongContextPolicyFields)
 await editor.findAll('input[type="number"]')[1]!.setValue('100000')
 expect(page.vm.policy.long_context_models![0]!.threshold).toBe(100000)
 expect(page.vm.policy.platform_multipliers.openai).toBe(2)
 expect(editor.vm.validate()).toBe(true); page.unmount()
})
describe('explicit threshold validation', () => {
 it('rejects fractional thresholds before saving', () => {
  const page = mount(LongContextPolicyFields, { props: { platform: 'composite', modelValue: { platform_multipliers: {}, model_multipliers: [], long_context_platforms: { openai: 1.5 } } }, global: { plugins: [createI18n({ legacy: false, locale: 'en', messages: { en: {} }, missingWarn: false })] } })
  expect(page.vm.validate()).toBe(false); page.unmount()
 })
})

it('shows actual default thresholds and zero without turning inherited values into overrides', async () => {
 const page = mount(LongContextPolicyFields, { props: { platform: 'composite', modelValue: { platform_multipliers: {}, model_multipliers: [], long_context_platforms: { anthropic: 123456 } } }, global: { plugins: [createI18n({ legacy: false, locale: 'en', messages: { en: {} }, missingWarn: false })] } })
 await flushPromises()
 expect(page.get('details').attributes('open')).toBeDefined()
 expect(page.get('[data-platform="openai"] input').attributes('placeholder')).toBe('0 / 272000')
 expect(page.get('[data-platform="deepseek"] input').attributes('placeholder')).toBe('0')
 expect(page.get('[data-platform="anthropic"] input').element).toHaveProperty('value', '123456')
 expect(page.get('[data-platform="anthropic"] input').attributes('placeholder')).toBe('200000')
 expect(page.emitted('update:modelValue')).toBeUndefined()
 await page.get('[data-platform="openai"] input').setValue('0')
 expect(page.emitted('update:modelValue')![0]![0]).toMatchObject({ long_context_platforms: { anthropic: 123456, openai: 0 } })
 page.unmount()
})

it('shows unknown after a defaults failure and can retry without overwriting saved thresholds', async () => {
 api.get.mockRejectedValueOnce(new Error('unavailable'))
 const page = mount(LongContextPolicyFields, { props: { platform: 'composite', modelValue: { platform_multipliers: {}, model_multipliers: [], long_context_platforms: { openai: 0 } } }, global: { plugins: [createI18n({ legacy: false, locale: 'en', messages: { en: {} }, missingWarn: false })] } })
 await flushPromises()
 expect(page.get('[role="alert"]').text()).toContain('longContext.defaultsFailed')
 expect(page.get('[data-platform="openai"] input').attributes('placeholder')).toBe('longContext.defaultsUnknown')
 expect(page.get('[data-platform="openai"] input').element).toHaveProperty('value', '0')
 await page.get('[role="alert"] button').trigger('click'); await flushPromises()
 expect(page.find('[role="alert"]').exists()).toBe(false)
 expect(page.get('[data-platform="openai"] input').attributes('placeholder')).toBe('0 / 272000')
 expect(page.emitted('update:modelValue')).toBeUndefined()
 page.unmount()
})
