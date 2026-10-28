import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import GroupModelRateFields from '../GroupModelRateFields.vue'
import type { BillingRatePolicy } from '@/types/billingRate'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

describe('model price card selling factors', () => {
  it('edits each model independently in the group policy, preserving platform and unit rules', async () => {
    const policy: BillingRatePolicy = { platform_multipliers: { openai: 2 }, model_multipliers: [
      { platform: 'openai', model: 'a', multiplier: 3 },
      { platform: 'openai', model: 'a', multiplier: 4, billing_unit: 'image' },
      { platform: 'openai', model: 'b', multiplier: 5 },
      { platform: 'grok', model: 'a', multiplier: 6 }
    ] }
    const w = mount(GroupModelRateFields, { props: { modelValue: policy, models: ['a', 'b'], platform: 'openai', idPrefix: 'test' } })
    const fields = w.findAll('input[type="number"]')
    expect(fields.map(input => (input.element as HTMLInputElement).value)).toEqual(['3', '5'])
    await fields[1]!.setValue('0')
    expect(w.emitted('update:modelValue')?.at(-1)?.[0]).toEqual({ ...policy, model_multipliers: [policy.model_multipliers[0], policy.model_multipliers[1], policy.model_multipliers[3], { platform: 'openai', model: 'b', multiplier: 0 }] })
    expect(policy.model_multipliers[2]!.multiplier).toBe(5)
  })
  it('clears a model rule without dropping a prefix or specific unit rule', async () => {
    const policy: BillingRatePolicy = { platform_multipliers: {}, model_multipliers: [{ platform: 'openai', model: 'a', multiplier: 2 }, { platform: 'openai', model: 'a*', multiplier: 7 }] }
    const w = mount(GroupModelRateFields, { props: { modelValue: policy, models: ['a'], platform: 'openai', idPrefix: 'test' } })
    await w.get('input[type="number"]').setValue('')
    expect(w.emitted('update:modelValue')?.at(-1)?.[0]).toEqual({ platform_multipliers: {}, model_multipliers: [{ platform: 'openai', model: 'a*', multiplier: 7 }] })
  })
  it('requires a concrete supplier before offering mixed-group model rates', () => {
    const w = mount(GroupModelRateFields, { props: { modelValue: { platform_multipliers: {}, model_multipliers: [] }, models: ['a'], platform: 'composite', idPrefix: 'test' } })
    expect(w.find('input[type="number"]').exists()).toBe(false)
    expect(w.text()).toContain('billingRates.editor.choosePlatform')
  })
})
