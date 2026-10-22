import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { mount, enableAutoUnmount } from '@vue/test-utils'
import { usePollingBudget } from '../usePollingBudget'

enableAutoUnmount(afterEach)
const Probe = defineComponent({
  props: { scope: { type: String, required: true } },
  setup: props => ({ peers: usePollingBudget(props.scope) }),
  template: '<span>{{ peers }}</span>',
})
beforeEach(() => { vi.useFakeTimers(); localStorage.clear() })
afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks(); vi.unstubAllGlobals(); localStorage.clear() })

describe('visible live tab polling budget', () => {
  it('shares the budget within an identity and removes closed registrations', async () => {
    const first = mount(Probe, { props: { scope: 'user-a' } })
    const second = mount(Probe, { props: { scope: 'user-a' } })
    const other = mount(Probe, { props: { scope: 'user-b' } })
    await vi.advanceTimersByTimeAsync(5000)
    expect(first.text()).toBe('2')
    expect(second.text()).toBe('2')
    expect(other.text()).toBe('1')
    second.unmount()
    await vi.advanceTimersByTimeAsync(5000)
    expect(first.text()).toBe('1')
  })
  it('does not register a hidden tab and tolerates HTTP environments without randomUUID', () => {
    vi.spyOn(document, 'hidden', 'get').mockReturnValue(true)
    vi.stubGlobal('crypto', {})
    const wrapper = mount(Probe, { props: { scope: 'user-a' } })
    expect(wrapper.text()).toBe('1')
    expect(localStorage.length).toBe(0)
  })
  it('uses a conservative budget when storage is unavailable', async () => {
    vi.stubGlobal('localStorage', { setItem: () => { throw new Error('unavailable') }, removeItem: vi.fn() })
    const wrapper = mount(Probe, { props: { scope: 'user-a' } })
    await wrapper.vm.$nextTick()
    expect(wrapper.text()).toBe('2')
  })
})
