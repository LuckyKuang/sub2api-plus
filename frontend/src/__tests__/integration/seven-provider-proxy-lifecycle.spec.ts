import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import type { AxiosAdapter, InternalAxiosRequestConfig } from 'axios'
import { apiClient } from '@/api/client'
import type { Account, Proxy } from '@/types'
import ProxiesView from '@/views/admin/ProxiesView.vue'
import EditAccountModal from '@/components/account/EditAccountModal.vue'

const messages = vi.hoisted(() => ({ error: vi.fn(), success: vi.fn() }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: messages.error, showSuccess: messages.success, showInfo: vi.fn() }) }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ isSimpleMode: true }) }))
vi.mock('@/components/account/OutboundIdentityEditor.vue', () => ({ default: { template: '<div />' } }))
vi.mock('vue-i18n', async original => ({
  ...await original<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string, params?: object) => key === 'admin.proxies.batchQualityDone' ? `${key}:${JSON.stringify(params)}` : key })
}))
enableAutoUnmount(afterEach)

const platforms = ['openai', 'grok', 'deepseek', 'kimi', 'minimax', 'stepfun', 'zhipu'] as const
const Dialog = { props: ['show', 'title'], template: '<section v-if="show" :data-dialog="title"><slot /><slot name="footer" /></section>' }
const Table = {
  props: ['data'],
  template: `<div><section v-for="row in data" :key="row.id" :data-row="row.id">
    <slot name="cell-name" :value="row.name" :row="row" />
    <slot name="cell-address" :row="row" />
    <span data-probe>{{ row.latency_status }} {{ row.country }} {{ row.latency_ms }}</span>
    <span data-quality>{{ row.quality_status }} {{ row.quality_summary }}</span>
    <slot name="cell-actions" :row="row" />
  </section></div>`
}
const layoutStubs = {
  BaseDialog: Dialog, Icon: true,
  AppLayout: { template: '<main><slot /></main>' },
  TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /></div>' },
  DataTable: Table,
  GroupSelector: true, ModelWhitelistSelector: true
}

type Call = { method: string; url: string; body: Record<string, unknown> }
type Probe = { success: boolean; message: string; latency_ms?: number; country?: string }
const originalAdapter = apiClient.defaults.adapter
let proxies: Proxy[]
let account: Account
let calls: Call[]
let nextProxy: number
let probe: () => Promise<Probe>
let quality: () => Promise<object>
let visibleProxyIDs: number[] | undefined

function clone<T>(value: T): T { return JSON.parse(JSON.stringify(value)) }
function reply(config: InternalAxiosRequestConfig, data: unknown) {
  return { config, status: 200, statusText: 'OK', headers: {}, data: { code: 0, data } }
}

// The adapter is a stateful service boundary. Components use the real admin API,
// Axios request serialization and response interceptor. It is not a backend or
// browser E2E; transport, persistence and proxy-in-use enforcement are covered
// independently by the Go integration tests.
const adapter: AxiosAdapter = async config => {
  const method = config.method || 'get'
  const url = config.url || ''
  const body = typeof config.data === 'string' ? JSON.parse(config.data) : (config.data || {})
  calls.push({ method, url, body: clone(body) })
  const enriched = () => proxies.map(p => ({ ...p, account_count: account?.proxy_id === p.id ? 1 : 0 }))
  if (method === 'get' && url === '/admin/proxies') return reply(config, { items: config.params?.page_size === 200 || !visibleProxyIDs ? enriched() : enriched().filter(p => visibleProxyIDs!.includes(p.id)), total: proxies.length, pages: 1 })
  if (method === 'get' && url === '/admin/proxies/all') return reply(config, enriched())
  if (method === 'post' && url === '/admin/proxies') {
    const created = { ...body, id: nextProxy++, status: 'active' } as Proxy
    proxies.push(created)
    return reply(config, clone(created))
  }
  const proxyID = Number(url.match(/^\/admin\/proxies\/(\d+)(?:\/(?:test|quality-check))?$/)?.[1])
  if (proxyID && method === 'put') {
    const index = proxies.findIndex(p => p.id === proxyID)
    proxies[index] = { ...proxies[index], ...body } as Proxy
    return reply(config, clone(proxies[index]))
  }
  if (proxyID && method === 'delete') {
    if (account.proxy_id === proxyID) throw new Error('test contract: bound proxy must not be deleted')
    proxies = proxies.filter(p => p.id !== proxyID)
    return reply(config, { message: 'deleted' })
  }
  if (proxyID && method === 'post' && url.endsWith('/test')) return reply(config, await probe())
  if (proxyID && method === 'post' && url.endsWith('/quality-check')) return reply(config, await quality())
  if (method === 'put' && url === `/admin/accounts/${account.id}`) {
    account = { ...account, ...body, proxy_id: body.proxy_id === 0 ? null : body.proxy_id } as Account
    return reply(config, clone(account))
  }
  if (method === 'get' && url === '/admin/settings/web-search-emulation') return reply(config, { enabled: false, providers: [] })
  if (method === 'get' && url === '/admin/settings') return reply(config, {})
  if (method === 'get' && url === '/admin/accounts/71/grok-media-eligibility') return reply(config, { mode: 'auto', eligible: true })
  if (method === 'get' && url === '/admin/tls-fingerprint-profiles') return reply(config, [])
  throw new Error(`Unexpected API request: ${method} ${url}`)
}

beforeEach(() => {
  vi.clearAllMocks()
  proxies = []
  calls = []
  nextProxy = 31
  visibleProxyIDs = undefined
  probe = async () => ({ success: true, message: 'working', latency_ms: 23, country: 'NL' })
  quality = async () => ({ score: 100, grade: 'A', summary: 'current', challenge_count: 0, failed_count: 0, warn_count: 0, items: [] })
  apiClient.defaults.adapter = adapter
})
afterEach(() => { apiClient.defaults.adapter = originalAdapter })

function button(wrapper: VueWrapper, text: string) {
  const match = wrapper.findAll('button').find(candidate => candidate.text() === text)
  expect(match, `visible button ${text}`).toBeDefined()
  return match!
}
function row(view: VueWrapper, id = 31) { return view.get(`[data-row="${id}"]`) }
async function refresh(view: VueWrapper) {
  await view.get('button[title="common.refresh"]').trigger('click')
  await flushPromises()
}
async function createProxy(view: VueWrapper, name = 'Business route', host = 'initial.proxy.example') {
  await button(view, 'admin.proxies.createProxy').trigger('click')
  await view.get('#create-proxy-form input[placeholder="admin.proxies.enterProxyName"]').setValue(name)
  await view.get('#create-proxy-form input[placeholder="admin.proxies.form.hostPlaceholder"]').setValue(host)
  await view.get('#create-proxy-form').trigger('submit')
  await flushPromises()
}
async function editProxy(view: VueWrapper, previousHost: string, nextHost: string) {
  await button(view.get(`[data-row="31"]`) as unknown as VueWrapper, 'common.edit').trigger('click')
  const host = view.findAll<HTMLInputElement>('#edit-proxy-form input').find(input => input.element.value === previousHost)!
  await host.setValue(nextHost)
  await view.get('#edit-proxy-form').trigger('submit')
  await flushPromises()
}
async function bindProxy(id: number | null) {
  const editor = mount(EditAccountModal, {
    props: { show: true, account: clone(account), proxies: clone(proxies), groups: [] },
    global: { stubs: layoutStubs }
  })
  await flushPromises()
  const selector = editor.getComponent({ name: 'ProxySelector' })
  await selector.get('.select-trigger').trigger('click')
  const option = selector.findAll('.select-option').find(candidate => id === null
    ? candidate.text().includes('admin.accounts.noProxy')
    : candidate.text().includes(proxies.find(p => p.id === id)!.name))!
  await option.trigger('click')
  await editor.get('#edit-account-form').trigger('submit')
  await flushPromises()
  expect(editor.emitted('updated')).toHaveLength(1)
  expect(editor.emitted('close')).toHaveLength(1)
  expect(account.proxy_id).toBe(id)
  const update = calls.filter(call => call.url === `/admin/accounts/${account.id}` && call.method === 'put').at(-1)!
  expect(update.body.proxy_id).toBe(id ?? 0)
  const secret = account.type === 'oauth' ? 'access_token' : 'api_key'
  if (account.type === 'apikey') expect(update.body.credentials).toMatchObject({ api_key: `private-${account.platform}` })
  expect(account.credentials[secret]).toBe(`private-${account.platform}`)
  expect(account.name).toBe(`Business ${account.platform}`)
  editor.unmount()
}

// Requirements: proxy management is platform-independent; editing a route must
// retain a saved binding, binding deletion is blocked, explicit unbinding sends
// the backend's zero sentinel, and repairs can be selected without recreating
// the account or losing its credentials. These are user-visible invariants,
// not expectations calculated from component implementation.
describe('seven-provider proxy management business chain', () => {
  it.each(platforms.flatMap(platform => ['apikey', 'oauth'].map(type => ({ platform, type }))))('$platform $type creates, binds, edits, repairs and deletes through actual API adapters', async ({ platform, type }) => {
    account = {
      id: 71, name: `Business ${platform}`, platform, type,
      credentials: {
        [type === 'oauth' ? 'access_token' : 'api_key']: `private-${platform}`,
        base_url: 'https://upstream.example/v1',
        ...(platform === 'stepfun' && type === 'oauth' ? {
          oauth_provider: 'stepfun', oauth_region: 'cn', account_mode: 'coding',
          api_protocol: 'chat_completions', base_url: 'https://api.stepfun.com/step_plan/v1',
        } : {}),
      },
      extra: {}, proxy_id: null, status: 'active', concurrency: 1, priority: 1,
      rate_multiplier: 1, group_ids: [], expires_at: null, auto_pause_on_expired: false
    } as Account
    const view = mount(ProxiesView, { global: { stubs: layoutStubs } })
    await flushPromises()
    await createProxy(view)
    expect(row(view).text()).toContain('initial.proxy.example:8080')
    expect(calls.find(call => call.method === 'post' && call.url === '/admin/proxies')!.body).toMatchObject({
      name: 'Business route', host: 'initial.proxy.example', port: 8080, protocol: 'http', fallback_mode: 'none'
    })
    await bindProxy(31)
    await refresh(view)
    await button(row(view) as unknown as VueWrapper, 'common.delete').trigger('click')
    expect(messages.error).toHaveBeenLastCalledWith('admin.proxies.deleteBlockedInUse')
    expect(calls.filter(call => call.method === 'delete')).toHaveLength(0)

    await editProxy(view, 'initial.proxy.example', 'offline.proxy.example')
    expect(account.proxy_id).toBe(31)
    probe = async () => ({ success: false, message: 'proxy refused connection' })
    await button(row(view) as unknown as VueWrapper, 'admin.proxies.testConnection').trigger('click')
    await flushPromises()
    expect(row(view).get('[data-probe]').text()).toContain('failed')
    expect(messages.error).toHaveBeenLastCalledWith('proxy refused connection')
    expect(account.proxy_id).toBe(31)

    // A probe issued before repair cannot certify the replacement endpoint.
    let finishOld!: (result: Probe) => void
    probe = () => new Promise(resolve => { finishOld = resolve })
    await button(row(view) as unknown as VueWrapper, 'admin.proxies.testConnection').trigger('click')
    await flushPromises()
    await editProxy(view, 'offline.proxy.example', 'repaired.proxy.example')
    finishOld({ success: true, message: 'old route', country: 'STALE', latency_ms: 99 })
    await flushPromises()
    expect(row(view).text()).toContain('repaired.proxy.example:8080')
    expect(row(view).get('[data-probe]').text()).not.toContain('STALE')
    expect(messages.success).not.toHaveBeenCalledWith('admin.proxies.proxyWorkingWithLatency')

    probe = async () => ({ success: true, message: 'repaired', country: 'NL', latency_ms: 23 })
    await button(row(view) as unknown as VueWrapper, 'admin.proxies.testConnection').trigger('click')
    await flushPromises()
    expect(row(view).get('[data-probe]').text()).toContain('success NL 23')
    expect(account.proxy_id).toBe(31)
    await bindProxy(null)
    await refresh(view)
    await button(row(view) as unknown as VueWrapper, 'common.delete').trigger('click')
    await button(view.get('[data-dialog="admin.proxies.deleteProxy"]') as unknown as VueWrapper, 'common.delete').trigger('click')
    await flushPromises()
    expect(proxies).toEqual([])
    expect(view.find('[data-row="31"]').exists()).toBe(false)
    expect(calls.filter(call => call.method === 'delete').map(call => call.url)).toEqual(['/admin/proxies/31'])

    await createProxy(view, 'Replacement route', 'replacement.proxy.example')
    await bindProxy(32)
    expect(proxies).toHaveLength(1)
    expect(account.id).toBe(71)
    expect(account.credentials[type === 'oauth' ? 'access_token' : 'api_key']).toBe(`private-${platform}`)
  })
})


describe('proxy quality observations after an in-flight route edit', () => {
  it.each(['single', 'batch'])('%s quality results cannot certify a repaired endpoint', async mode => {
    account = { id: 71, proxy_id: null } as Account
    const view = mount(ProxiesView, { global: { stubs: layoutStubs } })
    await flushPromises()
    await createProxy(view)
    let finishOld!: (result: object) => void
    quality = () => new Promise(resolve => { finishOld = resolve })
    await (mode === 'single'
      ? button(row(view) as unknown as VueWrapper, 'admin.proxies.qualityCheck')
      : button(view, 'admin.proxies.batchQualityCheck')).trigger('click')
    await flushPromises()
    await editProxy(view, 'initial.proxy.example', 'repaired.proxy.example')
    finishOld({
      score: 100, grade: 'A', summary: 'STALE QUALITY', challenge_count: 0,
      failed_count: 0, warn_count: 0, items: [{ target: 'base_connectivity', status: 'pass' }],
      country: 'STALE COUNTRY', base_latency_ms: 99
    })
    await flushPromises()
    expect(row(view).get('[data-quality]').text()).not.toContain('STALE')
    expect(row(view).get('[data-probe]').text()).not.toContain('STALE')
    expect(view.find('[data-dialog="admin.proxies.qualityReportTitle"]').exists()).toBe(false)
    expect(messages.success).not.toHaveBeenCalledWith('admin.proxies.qualityCheckDone')
    if (mode === 'batch') expect(messages.success).toHaveBeenCalledWith('admin.proxies.batchQualityDone:{"count":1,"healthy":0,"warn":0,"challenge":0,"failed":0}')
  })
})


it('batch connectivity tests still reach proxies outside the displayed page', async () => {
  account = { id: 71, proxy_id: null } as Account
  proxies = [31, 32].map(id => ({ id, name: `Route ${id}`, protocol: 'http', host: `proxy${id}.example`, port: 8080, status: 'active' }) as Proxy)
  visibleProxyIDs = [31]
  const view = mount(ProxiesView, { global: { stubs: layoutStubs } })
  await flushPromises()
  expect(view.find('[data-row="32"]').exists()).toBe(false)
  await button(view, 'admin.proxies.testConnection').trigger('click')
  await flushPromises()
  expect(calls.filter(call => call.url.endsWith('/test')).map(call => call.url)).toEqual(['/admin/proxies/31/test', '/admin/proxies/32/test'])
})
