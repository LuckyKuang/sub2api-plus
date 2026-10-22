import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, enableAutoUnmount } from '@vue/test-utils'
import LiveRequestsPanel from '../LiveRequestsPanel.vue'
import type { LiveRequest } from '@/api/liveRequests'

const { read } = vi.hoisted(() => ({ read: vi.fn() }))
vi.mock('@/api/liveRequests', () => ({ getLiveRequests: read }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ user: { id: 7 } }) }))
vi.mock('vue-i18n', async importOriginal => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
const table = { props: ['data'], emits: ['rowClick'], template: '<div />' }
const refreshControl = { props: ['state'], template: '<div />' }
const snapshot = (items: Partial<LiveRequest>[]) => ({ items, server_now: '2026-10-21T10:00:00Z', limit: 200, poll_after_seconds: 2, truncated: false })
const processing = { id: 'logical-request', version: 1, status: 'processing' as const, started_at: '2026-10-21T09:59:55Z', elapsed_ms: 5000, model: 'test-model' }
function setup() {
  return mount(LiveRequestsPanel, {
    props: { mode: 'user' },
    global: { stubs: { DataTable: table, RefreshControl: refreshControl, LiveElapsed: true,
      DetailWorkspace: { props: ['show'], template: '<div><slot /><div v-if="show"><slot name="detail" /></div></div>' } } },
  })
}
enableAutoUnmount(afterEach)
beforeEach(() => { vi.useFakeTimers(); localStorage.clear(); read.mockReset() })
afterEach(() => { vi.useRealTimers(); localStorage.clear() })

describe('live snapshot gaps', () => {
  it('marks a missing processing record unknown in both the retained list and detail', async () => {
    read.mockResolvedValueOnce(snapshot([processing])).mockResolvedValue(snapshot([]))
    const wrapper = setup()
    await flushPromises()
    wrapper.findComponent(table).vm.$emit('rowClick', wrapper.findComponent(table).props('data')[0])
    await wrapper.vm.$nextTick()
    await wrapper.findComponent(refreshControl).props('state').refresh()
    await flushPromises()
    expect(wrapper.findComponent(table).props('data')).toEqual([expect.objectContaining({ id: 'logical-request', status: 'unknown' })])
    expect(wrapper.text()).toContain('inspection.unknown')
    expect(wrapper.text()).not.toContain('inspection.failed')
  })
  it('waits at least five seconds for an empty visible snapshot even if the server suggests two', async () => {
    read.mockResolvedValue(snapshot([]))
    setup()
    await flushPromises()
    await vi.advanceTimersByTimeAsync(4000)
    expect(read).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(1000)
    expect(read).toHaveBeenCalledTimes(2)
  })
})
