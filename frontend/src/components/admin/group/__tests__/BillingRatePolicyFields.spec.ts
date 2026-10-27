import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { createI18n } from 'vue-i18n'
import { ref } from 'vue'
import BillingRatePolicyFields from '../BillingRatePolicyFields.vue'
import Select from '@/components/common/Select.vue'
import { billingRatePolicyError, cloneBillingRatePolicy } from '@/utils/billingRate'

describe('group billing rate policy', () => {
  it('preserves explicit free rates and copies policies without mutating the loaded group', () => {
    const source = { platform_multipliers: { stepfun: 0 }, model_multipliers: [{ platform: 'openai', model: 'gpt-*', multiplier: 2 }] }
    const copied = cloneBillingRatePolicy(source)
    copied.platform_multipliers.stepfun = 3
    copied.model_multipliers[0]!.multiplier = 4
    expect(source.platform_multipliers.stepfun).toBe(0)
    expect(source.model_multipliers[0]!.multiplier).toBe(2)
  })

  it('rejects ambiguous, invalid and duplicated rules independently of the resolver', () => {
    const policy = (model: string, multiplier = 1) => ({ platform_multipliers: {}, model_multipliers: [{ platform: 'openai', model, multiplier }] })
    expect(billingRatePolicyError(policy('gpt-*', 0))).toBeNull()
    expect(billingRatePolicyError(policy('*'))).toBeNull()
    expect(billingRatePolicyError(policy('gpt-*-mini'))).toBe('invalidModel')
    expect(billingRatePolicyError(policy(''))).toBe('invalidModel')
    expect(billingRatePolicyError(policy('g'.repeat(256)))).toBeNull()
    expect(billingRatePolicyError(policy('g'.repeat(257)))).toBe('invalidModel')
    expect(billingRatePolicyError(policy('模'.repeat(86)))).toBe('invalidModel')
    for (const model of [' gpt-5', 'gpt-5 ', 'gpt 5', 'gpt\t5', 'gpt\u00a05']) {
      expect(billingRatePolicyError(policy(model))).toBe('invalidModel')
    }
    expect(billingRatePolicyError(policy('gpt-5', -1))).toBe('invalidMultiplier')
    expect(billingRatePolicyError(policy('gpt-5', Infinity))).toBe('invalidMultiplier')
    expect(billingRatePolicyError({ platform_multipliers: {}, model_multipliers: [{ platform: 'grok', model: 'grok-video', multiplier: 1, billing_unit: 'video' }] })).toBe('invalidUnit')
    expect(billingRatePolicyError({ platform_multipliers: { unknown: 1 }, model_multipliers: [] })).toBe('invalidPlatform')
    expect(billingRatePolicyError({ platform_multipliers: {}, model_multipliers: [
      { platform: 'openai', model: 'GPT-5', multiplier: 1 },
      { platform: 'openai', model: 'gpt-5', multiplier: 2 },
    ] })).toBe('duplicateRule')
  })

  it('offers every registered platform and removes a platform override to inherit the group rate', async () => {
    const wrapper = mount(BillingRatePolicyFields, {
      props: { modelValue: { platform_multipliers: { stepfun: 0 }, model_multipliers: [] }, platform: 'composite', idPrefix: 'test' },
      global: { plugins: [createI18n({ legacy: false, locale: 'en', messages: {} })] },
    })
    expect(wrapper.find('[data-platform="stepfun"]').exists()).toBe(true)
    expect(wrapper.find('[data-platform="cline"]').exists()).toBe(true)
    await wrapper.get('[data-platform="stepfun"] input[type="checkbox"]').setValue(false)
    expect(wrapper.emitted('update:modelValue')?.[0]?.[0]).toEqual({ platform_multipliers: {}, model_multipliers: [] })
  })

  it('keeps advanced and rate-only rules visible while exact model-card rules use the same policy', async () => {
    const policy = { platform_multipliers: {}, model_multipliers: [
      { platform: 'openai', model: 'a', multiplier: 2 },
      { platform: 'openai', model: 'a', billing_unit: 'image', multiplier: 3 },
      { platform: 'openai', model: 'a*', multiplier: 4 }
    ] }
    const w = mount(BillingRatePolicyFields, { props: { modelValue: policy, platform: 'composite', idPrefix: 'test', showPlatforms: false, cardModelKeys: ['openai:a'] }, global: { plugins: [createI18n({ legacy: false, locale: 'en', messages: {} })] } })
    expect(w.find('#test-model-0-name').exists()).toBe(false)
    expect(w.find('#test-model-1-name').exists()).toBe(true)
    expect(w.find('#test-model-2-name').exists()).toBe(true)
    await w.setProps({ cardModelKeys: [] })
    expect(w.find('#test-model-0-name').exists()).toBe(true)
    w.unmount()
  })

  it('preserves unfinished model and invalid numeric drafts when the platform editor changes', async () => {
    const w = mount({
      components: { Fields: BillingRatePolicyFields },
      setup() { return { policy: ref({ platform_multipliers: { openai: 1 }, model_multipliers: [] }) } },
      template: '<Fields v-model="policy" platform="openai" id-prefix="platform" :show-models="false" /><Fields v-model="policy" platform="openai" id-prefix="model" :show-platforms="false" />'
    }, { global: { plugins: [createI18n({ legacy: false, locale: 'en', messages: {} })] } })
    const editors = w.findAllComponents(BillingRatePolicyFields)
    await editors[1]!.get('button').trigger('click')
    await w.get('#platform-openai-rate').setValue('1.2')
    expect(w.get('#model-model-0-name').element).toHaveProperty('value', '')
    expect(editors[0]!.vm.validate()).toBe(false)
    expect(editors[1]!.vm.validate()).toBe(false)
    await w.get('#model-model-0-name').setValue('model-a')
    await w.get('#model-model-0-rate').setValue('')
    await w.get('#platform-openai-rate').setValue('1.3')
    expect(w.get('#model-model-0-rate').element).toHaveProperty('value', '')
    expect(w.get('#model-model-0-name').element).toHaveProperty('value', 'model-a')
    expect(editors[0]!.vm.validate()).toBe(false)
    await w.get('#model-model-0-rate').setValue('1.5')
    expect(editors.every(editor => editor.vm.validate())).toBe(true)
    expect(w.vm.policy).toEqual({ platform_multipliers: { openai: 1.3 }, model_multipliers: [{ platform: 'openai', model: 'model-a', multiplier: 1.5 }] })
    w.unmount()
  })

  it('keeps a newly entered advanced rule visible when its model matches a price card', async () => {
    const w = mount({
      components: { Fields: BillingRatePolicyFields },
      setup() { return { policy: ref({ platform_multipliers: { openai: 1 }, model_multipliers: [] }) } },
      template: '<Fields v-model="policy" platform="openai" id-prefix="platform" :show-models="false" /><Fields v-model="policy" platform="openai" id-prefix="advanced" :show-platforms="false" :card-model-keys="[\'openai:model-a\']" />'
    }, { global: { plugins: [createI18n({ legacy: false, locale: 'en', messages: {} })] } })
    const editors = w.findAllComponents(BillingRatePolicyFields)
    await editors[1]!.get('button').trigger('click')
    await w.get('#advanced-model-0-name').setValue('model-a')
    await w.get('#platform-openai-rate').setValue('1.2')
    expect(w.find('#advanced-model-0-unit').exists()).toBe(true)
    const unit = editors[1]!.findAllComponents(Select).find(select => select.props('id') === 'advanced-model-0-unit')!
    unit.vm.$emit('update:modelValue', 'image')
    await w.vm.$nextTick()
    await w.get('#advanced-model-0-rate').setValue('1.5')
    expect(w.vm.policy).toEqual({ platform_multipliers: { openai: 1.2 }, model_multipliers: [{ platform: 'openai', model: 'model-a', billing_unit: 'image', multiplier: 1.5 }] })
    w.unmount()
  })
})
