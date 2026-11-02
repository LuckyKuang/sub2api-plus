import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { defineComponent, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import LongContextPolicyFields from '../LongContextPolicyFields.vue'
import BillingRatePolicyFields from '../BillingRatePolicyFields.vue'
import type { BillingRatePolicy } from '@/types/billingRate'
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
