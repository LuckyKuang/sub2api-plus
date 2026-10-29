import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, enableAutoUnmount } from '@vue/test-utils'
import { computed, defineComponent, ref } from 'vue'
import LiveRequestsPanel from '../LiveRequestsPanel.vue'
import Pagination from '@/components/common/Pagination.vue'
import DataTable from '@/components/common/DataTable.vue'
import { USER_TABLE_LAYOUT_KEY } from '@/composables/useUserTableLayout'
import type { LiveRequest } from '@/api/liveRequests'
const { read, viewport } = vi.hoisted(() => ({ read: vi.fn(), viewport: { mobile: false } }))
vi.mock('@/api/liveRequests', () => ({ getLiveRequests: read }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ user: { id: 7 } }) }))
vi.mock('@vueuse/core', async original => ({ ...await original<typeof import('@vueuse/core')>(), useMediaQuery: () => ({ value: !viewport.mobile, __v_isRef: true }) }))
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
const table = defineComponent({
  props: ['data', 'columns'],
  setup(_props, { expose }) {
    const tableWrapperEl = ref<HTMLElement | null>(null)
    expose({ tableWrapperEl, scrollViewportEl: tableWrapperEl })
    return { tableWrapperEl }
  },
  template: '<div ref="tableWrapperEl" data-testid="live-table-scroll" />',
})
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
  // Audited user workspace: paginate the real 200-record observation window at
  // 20 records/page; filtering and export continue to cover the whole window.
  it('paginates the user window at 20 rows and leaves the admin window intact', async () => {
    const items = Array.from({ length: 200 }, (_, i) => ({ ...processing, id: `request-${i}` }))
    read.mockResolvedValue(snapshot(items))
    const user = setup(); await flushPromises()
    const firstPage: LiveRequest[] = user.findComponent(table).props('data')
    expect(firstPage).toHaveLength(20)
    expect(firstPage[0].id).toBe('request-0')
    expect(firstPage[19].id).toBe('request-19')
    expect(user.findComponent(Pagination).props()).toMatchObject({ page: 1, pageSize: 20, total: 200, showPageSizeSelector: false })
    user.findComponent(Pagination).vm.$emit('update:page', 10); await flushPromises()
    const lastPage: LiveRequest[] = user.findComponent(table).props('data')
    expect(lastPage).toHaveLength(20)
    expect(lastPage[0].id).toBe('request-180')
    expect(lastPage[19].id).toBe('request-199')
    expect(user.findComponent(table).element.contains(user.findComponent(Pagination).element)).toBe(false)
    user.unmount()
    const admin = setup('admin'); await flushPromises()
    expect(admin.findComponent(table).props('data')).toHaveLength(200)
    expect(admin.findComponent(Pagination).exists()).toBe(false)
  })
  it('resets the user page for filters and keeps its pagination card in the empty state', async () => {
    read.mockResolvedValue(snapshot(Array.from({ length: 45 }, (_, i) => ({
      ...processing, id: `request-${i}`, status: i === 44 ? 'completed' : 'processing',
    }))))
    const wrapper = setup(); await flushPromises()
    wrapper.findComponent(Pagination).vm.$emit('update:page', 3); await flushPromises()
    expect(wrapper.findComponent(table).props('data')).toHaveLength(5)
    const buttons = wrapper.find('[role="group"]').findAll('button')
    await buttons[1].trigger('click')
    expect(wrapper.findComponent(Pagination).props()).toMatchObject({ page: 1, total: 1 })
    expect(wrapper.findComponent(table).props('data').map((row: LiveRequest) => row.id)).toEqual(['request-44'])
    await buttons[2].trigger('click')
    expect(wrapper.findComponent(Pagination).props()).toMatchObject({ page: 1, total: 0 })
    expect(wrapper.findComponent(table).props('data')).toEqual([])
  })
  it.each([false, true])('resets the actual user viewport only when the page changes (mobile=%s)', async mobile => {
    viewport.mobile = mobile
    const items = Array.from({ length: 45 }, (_, i) => ({ ...processing, id: `request-${i}` }))
    read.mockResolvedValueOnce(snapshot(items)).mockResolvedValue(snapshot(items.map(row => ({ ...row, version: 2, status: 'completed' }))))
    const wrapper = setup(); await flushPromises()
    const scroll = (mobile
      ? wrapper.get('div[aria-busy]')
      : wrapper.get('[data-testid="live-table-scroll"]')).element as HTMLElement
    scroll.scrollTop = 240
    await vi.advanceTimersByTimeAsync(2000); await flushPromises()
    expect(read).toHaveBeenCalledTimes(2)
    expect(scroll.scrollTop).toBe(240)
    wrapper.findComponent(Pagination).vm.$emit('update:page', 2); await flushPromises()
    expect(scroll.scrollTop).toBe(0)
    scroll.scrollTop = 240
    await vi.advanceTimersByTimeAsync(2000); await flushPromises()
    expect(read).toHaveBeenCalledTimes(3)
    expect(wrapper.findComponent(Pagination).props('page')).toBe(2)
    expect(scroll.scrollTop).toBe(240)
  })
  it('resets the real DataTable card viewport at desktop widths without resetting ordinary polls', async () => {
    // A desktop viewport may still use native DataTable cards: 1,400px of
    // selected columns cannot fit the independently supplied 800px container.
    vi.stubGlobal('matchMedia', vi.fn((query: string) => ({
      matches: true, media: query, addEventListener: vi.fn(), removeEventListener: vi.fn(),
      addListener: vi.fn(), removeListener: vi.fn(),
    })))
    vi.spyOn(Element.prototype, 'clientWidth', 'get').mockReturnValue(800)
    vi.spyOn(HTMLElement.prototype, 'offsetWidth', 'get').mockReturnValue(824)
    vi.spyOn(Element.prototype, 'scrollWidth', 'get').mockImplementation(function (this: Element) {
      return this.matches('table') ? 1400 : 800
    })
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
      return new DOMRect(0, 0, this.matches('table') ? 1400 : 824, 480)
    })
    const items = Array.from({ length: 45 }, (_, i) => ({ ...processing, id: `request-${i}` }))
    read.mockResolvedValueOnce(snapshot(items)).mockResolvedValue(snapshot(items.map(row => ({ ...row, version: 2, status: 'completed' }))))
    const wrapper = mount(LiveRequestsPanel, {
      props: { mode: 'user', workspace: true },
      global: {
        provide: { [USER_TABLE_LAYOUT_KEY as symbol]: computed(() => true) },
        stubs: { Select: true, Icon: true },
      },
    })
    try {
      await flushPromises()
      expect(wrapper.findComponent(DataTable).vm.tableWrapperEl).toBeNull()
      const scroll = wrapper.get('.user-record-cards.workspace-table-viewport').element as HTMLElement
      scroll.scrollTop = 240
      await vi.advanceTimersByTimeAsync(2000); await flushPromises()
      expect(read).toHaveBeenCalledTimes(2)
      expect(scroll.scrollTop).toBe(240)
      wrapper.findComponent(Pagination).vm.$emit('update:page', 2); await flushPromises()
      expect(wrapper.get('.user-record-cards').element).toBe(scroll)
      expect(wrapper.findComponent(DataTable).props('data')[0].id).toBe('request-20')
      expect(scroll.scrollTop).toBe(0)
      scroll.scrollTop = 240
      await vi.advanceTimersByTimeAsync(2000); await flushPromises()
      expect(read).toHaveBeenCalledTimes(3)
      expect(wrapper.findComponent(Pagination).props('page')).toBe(2)
      expect(scroll.scrollTop).toBe(240)
    } finally {
      wrapper.unmount()
      vi.restoreAllMocks(); vi.unstubAllGlobals()
    }
  })
  it('clamps a later user page when updated observations leave fewer filtered results', async () => {
    const items = Array.from({ length: 45 }, (_, i) => ({ ...processing, id: `request-${i}`, status: 'completed' }))
    read.mockResolvedValueOnce(snapshot(items)).mockResolvedValue(snapshot(items.map((row, i) => ({ ...row, status: i < 2 ? 'completed' : 'failed' }))))
    const wrapper = setup(); await flushPromises()
    await wrapper.find('[role="group"]').findAll('button')[1].trigger('click')
    wrapper.findComponent(Pagination).vm.$emit('update:page', 3); await flushPromises()
    expect(wrapper.findComponent(table).props('data')).toHaveLength(5)
    await vi.advanceTimersByTimeAsync(2000); await flushPromises()
    expect(wrapper.findComponent(Pagination).props()).toMatchObject({ page: 1, total: 2 })
    expect(wrapper.findComponent(table).props('data').map((row: LiveRequest) => row.id)).toEqual(['request-0', 'request-1'])
  })
  it('exports the full filtered observation window while viewing a later page', async () => {
    read.mockResolvedValue(snapshot(Array.from({ length: 45 }, (_, i) => ({ ...processing, id: `request-${i}`, model: `model-${i}` }))))
    const wrapper = setup(); await flushPromises()
    wrapper.findComponent(Pagination).vm.$emit('update:page', 2); await flushPromises()
    let csv = ''
    const OriginalBlob = globalThis.Blob
    const originalCreate = URL.createObjectURL, originalRevoke = URL.revokeObjectURL
    vi.stubGlobal('Blob', vi.fn(function (parts: BlobPart[], options?: BlobPropertyBag) {
      csv = parts.join('')
      return new OriginalBlob(parts, options)
    }))
    URL.createObjectURL = vi.fn(() => 'blob:live-window')
    URL.revokeObjectURL = vi.fn()
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
    try {
      await wrapper.findAll('button').find(button => button.text() === 'requestList.exportWindow')!.trigger('click')
      expect(csv.split('\r\n')).toHaveLength(46)
      expect(csv).toContain('model-0')
      expect(csv).toContain('model-44')
      expect(read).toHaveBeenCalledTimes(1)
    } finally {
      click.mockRestore()
      URL.createObjectURL = originalCreate; URL.revokeObjectURL = originalRevoke
      vi.unstubAllGlobals()
    }
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
