import { enableAutoUnmount, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import AutoRefreshButton from '../AutoRefreshButton.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, values?: { n: number }) => values ? `${key}:${values.n}` : key }) }))
enableAutoUnmount(afterEach)

describe('combined auto-refresh menu', () => {
  it('keeps enabling and interval selection in one dropdown', async () => {
    const wrapper = mount(AutoRefreshButton, { props: { enabled: false, intervalSeconds: 10, countdown: 7, intervals: [10, 30] } })
    expect(wrapper.find('select').exists()).toBe(false)
    expect(wrapper.findAll('button')).toHaveLength(1)
    await wrapper.get('button').trigger('click')
    expect(wrapper.emitted('update:enabled')).toBeUndefined()
    const enable = wrapper.findAll('button').find(button => button.text() === 'common.autoRefresh.enable')!
    await enable.trigger('click')
    expect(wrapper.emitted('update:enabled')).toEqual([[true]])
    await wrapper.setProps({ enabled: true })
    const interval = wrapper.findAll('button').find(button => button.text() === 'common.autoRefresh.seconds:30')!
    await interval.trigger('click')
    expect(wrapper.emitted('update:interval')).toEqual([[30]])
    expect(wrapper.findAll('button')).toHaveLength(4)
    document.body.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await wrapper.vm.$nextTick()
    expect(wrapper.findAll('button')).toHaveLength(1)
    expect(wrapper.emitted('update:enabled')).toEqual([[true]])
  })
})
