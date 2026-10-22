import { ref, nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import LiveElapsed from '../LiveElapsed.vue'
import type { LiveRequest } from '@/api/liveRequests'

describe('observed elapsed time', () => {
  it('uses server time, freezes on stale data, and never advances terminal rows', async () => {
    const clock = { now: ref(100), receivedAt: ref(100), serverNow: ref(10000), healthy: ref(true) }
    const row = { id: 'request-a', status: 'processing', started_at: '1970-01-01T00:00:05Z', elapsed_ms: 4000 } as LiveRequest
    const wrapper = mount(LiveElapsed, { props: { row, clock } })
    expect(wrapper.text()).toBe('5.0 s')
    clock.now.value = 1350; await nextTick()
    expect(wrapper.text()).toBe('6.3 s')
    clock.healthy.value = false; await nextTick()
    expect(wrapper.text()).toBe('6.3 s')
    clock.healthy.value = true; clock.now.value = 16000; await nextTick()
    expect(wrapper.text()).toBe('6.3 s')
    await wrapper.setProps({ row: { ...row, status: 'finished', elapsed_ms: 7500 } })
    clock.now.value = 200; await nextTick()
    expect(wrapper.text()).toBe('7.5 s')
    clock.healthy.value = false
    await wrapper.setProps({ row: { ...row, id: 'request-b', elapsed_ms: 1000 } })
    expect(wrapper.text()).toBe('1.0 s')
    wrapper.unmount()
  })
})
