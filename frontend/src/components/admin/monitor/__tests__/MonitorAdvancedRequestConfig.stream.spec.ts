import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import MonitorAdvancedRequestConfig from '../MonitorAdvancedRequestConfig.vue'

enableAutoUnmount(afterEach)
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
describe('streaming probe selection', () => {
  it.each(['off', 'merge', 'replace'] as const)('has one explicit stream switch in %s mode', async bodyOverrideMode => {
    const wrapper = mount(MonitorAdvancedRequestConfig, { props: { provider: 'openai', apiMode: 'responses', streamProbe: false, extraHeaders: {}, bodyOverrideMode, bodyOverride: null } })
    const checkbox = wrapper.get('input[type="checkbox"]')
    expect((checkbox.element as HTMLInputElement).checked).toBe(false)
    await checkbox.setValue(true)
    expect(wrapper.emitted('update:streamProbe')).toEqual([[true]])
    await wrapper.setProps({ streamProbe: true })
    expect((checkbox.element as HTMLInputElement).checked).toBe(true)
    expect(wrapper.text()).toContain('monitorCommon.streamProbeHint')
  })
  it.each(['anthropic', 'gemini', 'antigravity'] as const)('does not offer an unsupported %s stream', provider => {
    const wrapper = mount(MonitorAdvancedRequestConfig, { props: { provider, extraHeaders: {}, bodyOverrideMode: 'off', bodyOverride: null } })
    expect(wrapper.find('input[type="checkbox"]').exists()).toBe(false)
  })
})
