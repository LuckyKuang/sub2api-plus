import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, enableAutoUnmount } from '@vue/test-utils'
import LiveRequestsPanel from '../LiveRequestsPanel.vue'
import type { LiveRequest } from '@/api/liveRequests'
const { read, viewport } = vi.hoisted(() => ({ read: vi.fn(), viewport: { mobile: false } }))
vi.mock('@/api/liveRequests', () => ({ getLiveRequests: read }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ user: { id: 7 } }) }))
vi.mock('@vueuse/core', async original => ({ ...await original<typeof import('@vueuse/core')>(), useMediaQuery: () => ({ value: !viewport.mobile, __v_isRef: true }) }))
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
const table = { props: ['data', 'columns'], template: '<div />' }
const snapshot = (items: Partial<LiveRequest>[]) => ({ items, server_now: '2026-10-21T10:00:00Z', window_seconds: 300, limit: 200, poll_after_seconds: 2, truncated: false })
const processing = { id: 'logical-request', version: 1, status: 'processing' as const, request_type: 'stream' as const, started_at: '2026-10-21T09:59:55Z', elapsed_ms: 5000, model: 'test-model', key_name: 'key', user_id: 7 }
function setup(mode: 'user' | 'admin' = 'user') { return mount(LiveRequestsPanel, { props: { mode }, global: { stubs: { DataTable: table, Select: true, LiveElapsed: true } } }) }
enableAutoUnmount(afterEach)
beforeEach(() => { vi.useFakeTimers(); localStorage.clear(); read.mockReset(); viewport.mobile = false; Object.defineProperty(document, 'hidden', { configurable: true, value: false }) })
afterEach(() => { vi.useRealTimers(); localStorage.clear() })
// Accepted design: performance is visible, client/IP hidden, no side detail, and
// unknown/pending observations cannot enter the successful or abnormal filters.
describe('approved real-time request list', () => {
  it('shows all approved default columns including performance for both roles', async () => {
    read.mockResolvedValue(snapshot([processing]))
    for (const mode of ['user', 'admin'] as const) {
      const wrapper = setup(mode); await flushPromises()
      const keys = wrapper.findComponent(table).props('columns').map((column: { key: string }) => column.key)
      expect(keys).toEqual(['started_at', ...(mode === 'admin' ? ['user_id'] : []), 'model', 'request_type', 'group_name', 'status', 'speed', 'performance', 'tokens', 'actual_cost'])
      expect(wrapper.find('[role="switch"]').attributes('aria-checked')).toBe('true')
      expect(wrapper.findComponent({ name: 'DetailWorkspace' }).exists()).toBe(false)
      wrapper.unmount()
    }
  })
  it('keeps unknown and handler-returned observations outside terminal filters', async () => {
    read.mockResolvedValue(snapshot(['completed', 'failed', 'unknown', 'finished'].map((status, i) => ({ ...processing, id: String(i), status: status as LiveRequest['status'] }))))
    const wrapper = setup(); await flushPromises()
    const buttons = wrapper.find('[role="group"]').findAll('button')
    await buttons[1].trigger('click')
    expect(wrapper.findComponent(table).props('data').map((row: LiveRequest) => row.status)).toEqual(['completed'])
    await buttons[2].trigger('click')
    expect(wrapper.findComponent(table).props('data').map((row: LiveRequest) => row.status)).toEqual(['failed'])
  })
  it('keeps mobile expansion and marks a lost active observation unknown without fetching details', async () => {
    viewport.mobile = true
    read.mockResolvedValueOnce(snapshot([processing])).mockResolvedValue(snapshot([]))
    const wrapper = setup(); await flushPromises()
    await wrapper.find('article button[aria-controls]').trigger('click')
    expect(wrapper.find('article dl').exists()).toBe(true)
    await vi.advanceTimersByTimeAsync(2000); await flushPromises()
    expect(wrapper.find('article dl').exists()).toBe(true)
    expect(wrapper.text()).toContain('requestList.status.unknown')
    expect(read).toHaveBeenCalledTimes(2)
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
  })
  it('waits at least five seconds for an empty snapshot and supports temporary off/manual refresh', async () => {
    read.mockResolvedValue(snapshot([]))
    const wrapper = setup(); await flushPromises()
    await vi.advanceTimersByTimeAsync(4000); expect(read).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(1000); expect(read).toHaveBeenCalledTimes(2)
    await wrapper.find('[role="switch"]').trigger('click')
    await vi.advanceTimersByTimeAsync(10000); expect(read).toHaveBeenCalledTimes(2)
    const manual = wrapper.findAll('button').find(button => button.text() === 'common.refresh')!
    await manual.trigger('click'); await flushPromises()
    expect(read).toHaveBeenCalledTimes(3)
    expect(wrapper.find('[role="switch"]').attributes('aria-checked')).toBe('false')
  })
  it('updates expanded records while holding insertions behind the new-record notice', async () => {
    viewport.mobile = true
    read.mockResolvedValueOnce(snapshot([processing])).mockResolvedValue(snapshot([
      { ...processing, id: 'new-request', model: 'new-model' },
      { ...processing, version: 2, status: 'completed', actual_cost: 0 },
    ]))
    const wrapper = setup(); await flushPromises()
    await wrapper.find('article button[aria-controls]').trigger('click')
    await vi.advanceTimersByTimeAsync(2000); await flushPromises()
    expect(wrapper.findAll('article')).toHaveLength(1)
    expect(wrapper.text()).toContain('requestList.status.completed')
    expect(wrapper.find('article dl').exists()).toBe(true)
    const notice = wrapper.findAll('button').find(button => button.text() === 'inspection.newRecords')!
    expect(notice.exists()).toBe(true)
    await notice.trigger('click')
    expect(wrapper.findAll('article')).toHaveLength(2)
    expect(wrapper.text()).toContain('new-model')
  })
})
