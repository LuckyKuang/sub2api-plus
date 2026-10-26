import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, shallowMount } from '@vue/test-utils'
import ProxiesView from '../ProxiesView.vue'

const api = vi.hoisted(() => ({ list: vi.fn(), getAllWithCount: vi.fn(), update: vi.fn(), testProxy: vi.fn(), checkProxyQuality: vi.fn() }))
const notify = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn(), info: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: { proxies: api } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: notify.success, showError: notify.error, showInfo: notify.info }) }))
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)

let saved: { id: number; name: string; protocol: string; host: string; port: number; status: string; updated_at: string }
function deferred() {
  let resolve!: (value: object) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<object>((ok, fail) => { resolve = ok; reject = fail })
  return { promise, resolve, reject }
}
function result(country: string) {
  return { success: true, message: country, country, latency_ms: 23, score: 100, grade: 'A', summary: country,
    challenge_count: 0, failed_count: 0, warn_count: 0, items: [{ target: 'base_connectivity', status: 'pass' }], base_latency_ms: 23 }
}
beforeEach(() => {
  vi.clearAllMocks()
  saved = { id: 31, name: 'Business route', protocol: 'http', host: 'original.example', port: 8080, status: 'active', updated_at: '2026-10-25T10:00:00Z' }
  api.list.mockImplementation(async () => ({ items: [{ ...saved }], total: 1, pages: 1 }))
  api.getAllWithCount.mockImplementation(async () => [{ ...saved }])
  api.update.mockImplementation(async (_id, changes) => {
    saved = { ...saved, ...changes, updated_at: '2026-10-25T10:01:00Z' }
    return { ...saved }
  })
})
function openView() {
  return shallowMount(ProxiesView, { global: { stubs: {
    AppLayout: { template: '<div><slot /></div>' },
    TablePageLayout: { template: '<div><header><slot name="filters" /></header><slot name="table" /></div>' },
    DataTable: { props: ['data'], template: '<div v-for="row in data" :key="row.id" :data-row="row.id"><span data-result>{{ row.country }} {{ row.quality_summary }}</span><slot name="cell-actions" :row="row" /></div>' },
    BaseDialog: { props: ['show', 'title'], template: '<section v-if="show" :data-dialog="title"><slot /><slot name="footer" /></section>' },
    Icon: true,
  } } })
}
type View = ReturnType<typeof openView>
function single(view: View, kind: string) {
  const key = kind === 'connectivity' ? 'admin.proxies.testConnection' : 'admin.proxies.qualityCheck'
  return view.get('[data-row="31"]').findAll('button').find(button => button.text() === key)!
}
function batch(view: View, kind: string) {
  const key = kind === 'connectivity' ? 'admin.proxies.testConnection' : 'admin.proxies.batchQualityCheck'
  return view.get('header').findAll('button').find(button => button.text() === key)!
}

// The same visible row is reachable through independent single and batch
// buttons. A pending unchanged route must be shared; after an edit, only the
// replacement route's run may publish a result or release its pending state.
describe('proxy management single and batch probe ownership', () => {
  it.each(['connectivity', 'quality'])('%s batch shares a pending single probe for an unchanged route', async kind => {
    const pending = deferred()
    const send = kind === 'connectivity' ? api.testProxy : api.checkProxyQuality
    send.mockImplementation(() => pending.promise)
    const view = openView()
    await flushPromises()
    await single(view, kind).trigger('click')
    await batch(view, kind).trigger('click')
    await flushPromises()
    try {
      expect(send).toHaveBeenCalledTimes(1)
      expect(single(view, kind).attributes('disabled')).toBeDefined()
      expect(batch(view, kind).attributes('disabled')).toBeDefined()
    } finally {
      pending.resolve(result('NL'))
      await flushPromises()
    }
    expect(single(view, kind).attributes('disabled')).toBeUndefined()
    expect(batch(view, kind).attributes('disabled')).toBeUndefined()
  })

  it.each(['connectivity', 'quality'])('%s old single completion cannot release or certify a replacement batch route', async kind => {
    const old = deferred()
    const current = deferred()
    const send = kind === 'connectivity' ? api.testProxy : api.checkProxyQuality
    send.mockImplementationOnce(() => old.promise).mockImplementationOnce(() => current.promise)
    const view = openView()
    await flushPromises()
    await single(view, kind).trigger('click')
    await view.get('[data-row="31"]').findAll('button').find(button => button.text() === 'common.edit')!.trigger('click')
    const host = view.findAll<HTMLInputElement>('#edit-proxy-form input').find(input => input.element.value === 'original.example')!
    await host.setValue('repaired.example')
    await view.get('#edit-proxy-form').trigger('submit')
    await flushPromises()
    await batch(view, kind).trigger('click')
    await flushPromises()
    expect(send).toHaveBeenCalledTimes(2)
    notify.success.mockClear()
    try {
      old.resolve(result('STALE'))
      await flushPromises()
      expect(single(view, kind).attributes('disabled')).toBeDefined()
      expect(view.get('[data-result]').text()).not.toContain('STALE')
      expect(view.find('[data-dialog="admin.proxies.qualityReportTitle"]').exists()).toBe(false)
      expect(notify.success).not.toHaveBeenCalled()
    } finally {
      old.resolve(result('STALE'))
      current.resolve(result('NL'))
      await flushPromises()
    }
    expect(single(view, kind).attributes('disabled')).toBeUndefined()
    expect(batch(view, kind).attributes('disabled')).toBeUndefined()
  })

  it.each(['connectivity', 'quality'].flatMap(kind => ['single', 'batch'].flatMap(mode => ['success', 'failure'].map(outcome => [kind, mode, outcome]))))('%s %s %s is discarded after leaving the management page', async (kind, mode, outcome) => {
    const pending = deferred()
    const send = kind === 'connectivity' ? api.testProxy : api.checkProxyQuality
    send.mockImplementation(() => pending.promise)
    const view = openView()
    await flushPromises()
    await (mode === 'single' ? single(view, kind) : batch(view, kind)).trigger('click')
    await flushPromises()
    expect(send).toHaveBeenCalledTimes(1)
    const listCalls = api.list.mock.calls.length
    const allCalls = api.getAllWithCount.mock.calls.length
    notify.success.mockClear()
    notify.error.mockClear()
    notify.info.mockClear()
    view.unmount()
    if (outcome === 'success') pending.resolve(result('OLD PAGE'))
    else pending.reject(new Error('old page proxy unavailable'))
    await flushPromises()
    expect(notify.success).not.toHaveBeenCalled()
    expect(notify.error).not.toHaveBeenCalled()
    expect(notify.info).not.toHaveBeenCalled()
    expect(api.list).toHaveBeenCalledTimes(listCalls)
    expect(api.getAllWithCount).toHaveBeenCalledTimes(allCalls)
    expect(send).toHaveBeenCalledTimes(1)
  })
})
