import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, shallowMount, type VueWrapper } from '@vue/test-utils'
import type { AxiosAdapter, InternalAxiosRequestConfig } from 'axios'
import { apiClient } from '@/api/client'
import type { Account } from '@/types'
import ReAuthAccountModal from '@/components/admin/account/ReAuthAccountModal.vue'
import EditAccountModal from '@/components/account/EditAccountModal.vue'
import AccountActionMenu from '@/components/admin/account/AccountActionMenu.vue'

const notice = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: notice.success, showError: notice.error, showInfo: vi.fn() }) }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ isSimpleMode: true }) }))
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)

const providers = ['deepseek', 'kimi', 'minimax', 'stepfun'] as const
const hosts = ['menu reauthorization', 'account editor'] as const
type Provider = typeof providers[number]
type Host = typeof hosts[number]
type Call = { url: string; body: Record<string, unknown> }
const originalAdapter = apiClient.defaults.adapter
let calls: Call[]
let complete: (body: Record<string, unknown>) => Promise<Record<string, unknown>>
let reload: (id: number) => Promise<Account>
let current: Account
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((ok, fail) => { resolve = ok; reject = fail })
  return { promise, resolve, reject }
}
function account(platform: Provider | 'zhipu', id = 81, proxy = 31): Account {
  return { id, platform, type: 'oauth', name: `Native account ${id}`, proxy_id: proxy,
    credentials: { oauth_provider: platform, oauth_region: platform === 'deepseek' ? 'cn' : 'global', account_mode: platform === 'deepseek' ? 'payg' : 'coding', api_protocol: platform === 'stepfun' ? 'chat_completions' : 'anthropic' },
    credentials_status: { has_access_token: true }, oauth_credential_owner: (id === 81 && proxy === 31 ? 'a' : 'b').repeat(64),
    proxy: { id: proxy, name: `Route ${proxy}`, protocol: 'http', host: 'proxy.example', port: 8080, status: 'active', updated_at: '2026-10-25T10:00:00Z' },
    status: 'active', schedulable: true, concurrency: 1, priority: 1, rate_multiplier: 1, group_ids: [], extra: {}, auto_pause_on_expired: false,
  } as Account
}
function session(id: number, status = 'ready') {
  return { session_id: `native-${id}`, status, expires_at: new Date(Date.now() + 60000).toISOString(), interval_seconds: 2,
    authorize_url: 'https://official.example/native-login', account_id: status === 'completed' ? id : undefined }
}
function reply(config: InternalAxiosRequestConfig, data: unknown) {
  return { config, status: 200, statusText: 'OK', headers: {}, data: { code: 0, data } }
}
const adapter: AxiosAdapter = async config => {
  const url = config.url || ''
  const body = typeof config.data === 'string' ? JSON.parse(config.data) : (config.data || {})
  calls.push({ url, body })
  if (url.match(/^\/admin\/cn\/oauth\/(deepseek|kimi|minimax|stepfun)\/start$/)) return reply(config, session(Number(body.account_id)))
  if (url.match(/^\/admin\/cn\/oauth\/(deepseek|kimi|minimax|stepfun)\/complete$/)) return reply(config, await complete(body))
  if (url.endsWith('/cancel')) return reply(config, session(81, 'cancelled'))
  const accountID = Number(url.match(/^\/admin\/accounts\/(\d+)$/)?.[1])
  if (accountID && config.method === 'get') return reply(config, await reload(accountID))
  if (config.method === 'get' && url.endsWith('/tls-fingerprint-profiles')) return reply(config, [])
  if (config.method === 'get' && url === '/admin/settings/web-search-emulation') return reply(config, { enabled: false, providers: [] })
  if (config.method === 'get' && url === '/admin/settings') return reply(config, {})
  throw new Error(`Unexpected native reauthorization request: ${config.method} ${url}`)
}
beforeEach(() => {
  vi.clearAllMocks()
  calls = []
  complete = async () => session(current.id, 'completed')
  reload = async id => ({ ...current, id })
  apiClient.defaults.adapter = adapter
})
afterEach(() => { apiClient.defaults.adapter = originalAdapter })
const stubs = {
  BaseDialog: { props: ['show'], template: '<section v-if="show"><slot /><slot name="footer" /></section>' },
  CNOAuthPanel: false, Icon: true, Teleport: true,
}
function button(view: VueWrapper, key: string) {
  const found = view.findAll('button').filter(button => button.text() === key)
  expect(found, key).toHaveLength(1)
  return found[0]!
}
async function open(host: Host, provider: Provider) {
  current = account(provider)
  if (host === 'account editor') {
    const view = shallowMount(EditAccountModal, { props: { show: true, account: current, groups: [], proxies: [] }, global: { stubs } })
    await flushPromises()
    return view as VueWrapper
  }
  const menu = shallowMount(AccountActionMenu, { props: { show: true, account: current, anchorRect: new DOMRect(100, 100, 24, 24) }, global: { stubs } })
  await button(menu, 'admin.accounts.reAuthorize').trigger('click')
  expect(menu.emitted('reauth')).toEqual([[current]])
  const view = shallowMount(ReAuthAccountModal, { props: { show: true, account: menu.emitted('reauth')![0]![0] as Account }, global: { stubs } })
  await flushPromises()
  return view as VueWrapper
}
async function begin(view: VueWrapper, provider: Provider) {
  expect(view.findComponent({ name: 'CNOAuthPanel' }).exists()).toBe(true)
  expect(view.findComponent({ name: 'OAuthAuthorizationFlow' }).exists()).toBe(false)
  const startURL = `/admin/cn/oauth/${provider}/start`
  const previousStarts = calls.filter(call => call.url === startURL).length
  expect(button(view, 'admin.accounts.oauth.domestic.reauthorize').attributes('disabled')).toBeUndefined()
  await button(view, 'admin.accounts.oauth.domestic.reauthorize').trigger('click')
  // Axios interceptors and Vue rendering need not finish within one flush.
  // Observe this click's request and the usable ready state before editing its owner.
  await vi.waitFor(() => {
    const starts = calls.filter(call => call.url === startURL)
    expect(starts).toHaveLength(previousStarts + 1)
    expect(starts.at(-1)?.body).toMatchObject({ account_id: current.id, region: provider === 'deepseek' ? 'cn' : 'global' })
    expect(button(view, 'admin.accounts.oauth.domestic.save').attributes('disabled')).toBeUndefined()
    expect(view.getComponent({ name: 'CNOAuthPanel' }).find('[role="alert"]').exists()).toBe(false)
  })
  expect(calls.some(call => call.url.includes('/admin/accounts/generate-auth-url') || call.url.includes('exchange-code'))).toBe(false)
}
function reloadCalls() { return calls.filter(call => /^\/admin\/accounts\/\d+$/.test(call.url)) }

describe('native existing-account reauthorization management entrypoints', () => {
  it.each(([['menu reauthorization', 'kimi'], ['account editor', 'stepfun']] as const).flatMap(([host, provider]) => ['observation', 'proxy metadata'].map(metadata => [host, provider, metadata] as const)))('%s %s preserves ready native authorization during a same-owner %s refresh', async (host, provider, metadata) => {
    const view = await open(host, provider)
    await begin(view, provider)
    const owner = current.oauth_credential_owner
    current = metadata === 'observation'
      ? { ...current, updated_at: '2026-10-25T10:05:00Z', extra: { quota_fetched_at: 1999999999 } }
      : { ...current, proxy: { ...current.proxy!, name: 'Renamed saved route', updated_at: '2026-10-25T10:05:00Z' } }
    expect(current.oauth_credential_owner).toBe(owner)
    await view.setProps({ account: current })
    await flushPromises()
    expect(calls.filter(call => call.url.endsWith('/cancel'))).toEqual([])
    await button(view, 'admin.accounts.oauth.domestic.save').trigger('click')
    await flushPromises()
    expect(view.emitted(host === 'account editor' ? 'updated' : 'reauthorized')).toHaveLength(1)
    expect(notice.success).toHaveBeenCalledOnce()
    expect(notice.error).not.toHaveBeenCalled()
  })
  it.each(hosts.flatMap(host => providers.map(provider => [host, provider] as const)))('%s %s renews the saved owner through its native authorization service', async (host, provider) => {
    const view = await open(host, provider)
    await begin(view, provider)
    await button(view, 'admin.accounts.oauth.domestic.save').trigger('click')
    await flushPromises()
    expect(calls.filter(call => call.url.endsWith('/complete'))).toEqual([{ url: `/admin/cn/oauth/${provider}/complete`, body: { session_id: 'native-81' } }])
    expect(reloadCalls()).toHaveLength(1)
    expect(reloadCalls()[0]!.url).toBe('/admin/accounts/81')
    expect(view.emitted(host === 'account editor' ? 'updated' : 'reauthorized')).toHaveLength(1)
    expect(view.emitted('close')).toHaveLength(1)
    expect(notice.success).toHaveBeenCalledOnce()
    expect(notice.error).not.toHaveBeenCalled()
  })

  it.each(hosts.flatMap(host => providers.flatMap(provider => ['another account', 'same account route', 'close and reopen', ...((host === 'menu reauthorization' && provider === 'kimi') || (host === 'account editor' && provider === 'stepfun') ? ['redacted credential owner', 'same proxy endpoint'] : [])].flatMap(boundary =>
    ['complete', 'reload'].flatMap(stage => ['success', 'failure'].map(outcome => [host, provider, boundary, stage, outcome] as const))))))('%s %s discards old %s %s %s without finishing the replacement', async (host, provider, boundary, stage, outcome) => {
    const old = deferred<Record<string, unknown>>()
    const view = await open(host, provider)
    await begin(view, provider)
    if (stage === 'complete') complete = () => old.promise
    else reload = () => old.promise as Promise<Account>
    await button(view, 'admin.accounts.oauth.domestic.save').trigger('click')
    await flushPromises()
    expect(reloadCalls()).toHaveLength(stage === 'reload' ? 1 : 0)
    current = account(provider, boundary === 'another account' ? 82 : 81, 32)
    if (boundary === 'redacted credential owner') {
      Object.assign(current, account(provider, 81, 31), { oauth_credential_owner: 'c'.repeat(64) })
      const previous = view.props('account') as Account
      expect({ ...current, oauth_credential_owner: previous.oauth_credential_owner }).toEqual(previous)
    } else if (boundary === 'same proxy endpoint') {
      Object.assign(current, account(provider, 81, 31), { oauth_credential_owner: 'c'.repeat(64), proxy: { ...account(provider).proxy, host: 'repaired.example', updated_at: '2026-10-25T10:01:00Z' } })
    }
    if (boundary === 'close and reopen') {
      await view.setProps({ show: false, account: null })
      await view.setProps({ show: true, account: current })
    } else await view.setProps({ account: current })
    await flushPromises()
    await begin(view, provider)
    const pending = deferred<Record<string, unknown>>()
    complete = () => pending.promise
    reload = async id => ({ ...current, id })
    await button(view, 'admin.accounts.oauth.domestic.save').trigger('click')
    await flushPromises()
    if (outcome === 'success') old.resolve(stage === 'complete' ? session(81, 'completed') : account(provider))
    else old.reject(new Error('discarded native reauthorization failed'))
    await flushPromises()
    expect(reloadCalls()).toHaveLength(stage === 'reload' ? 1 : 0)
    expect(view.emitted('updated')).toBeUndefined()
    expect(view.emitted('reauthorized')).toBeUndefined()
    expect(view.emitted('close')).toBeUndefined()
    expect(notice.success).not.toHaveBeenCalled()
    expect(notice.error).not.toHaveBeenCalled()
    expect(view.getComponent({ name: 'CNOAuthPanel' }).text()).not.toContain('domestic.failed')
    pending.resolve(session(current.id, 'completed'))
    await flushPromises()
    expect(reloadCalls().at(-1)!.url).toBe(`/admin/accounts/${current.id}`)
    expect(view.emitted(host === 'account editor' ? 'updated' : 'reauthorized')).toHaveLength(1)
    expect(view.emitted('close')).toHaveLength(1)
    expect(notice.success).toHaveBeenCalledOnce()
    expect(notice.error).not.toHaveBeenCalled()
  })

  it('does not offer unsupported ZCode in-place UI relinking or send a Claude login', async () => {
    const menu = shallowMount(AccountActionMenu, { props: { show: true, account: account('zhipu'), anchorRect: new DOMRect(100, 100, 24, 24) }, global: { stubs } })
    expect(menu.findAll('button').some(button => button.text() === 'admin.accounts.reAuthorize')).toBe(false)
    expect(calls).toEqual([])
  })

  it.each(['deepseek', 'zhipu'] as const)('%s does not offer an unpublished business-login refresh operation', async provider => {
    const menu = shallowMount(AccountActionMenu, { props: { show: true, account: account(provider), anchorRect: new DOMRect(100, 100, 24, 24) }, global: { stubs } })
    expect(menu.findAll('button').some(button => button.text() === 'admin.accounts.refreshToken')).toBe(false)
    expect(calls).toEqual([])
  })

  it.each(['kimi', 'minimax', 'stepfun'] as const)('%s retains its supported account-refresh action', async provider => {
    const menu = shallowMount(AccountActionMenu, { props: { show: true, account: account(provider), anchorRect: new DOMRect(100, 100, 24, 24) }, global: { stubs } })
    await button(menu, 'admin.accounts.refreshToken').trigger('click')
    expect(menu.emitted('refresh-token')).toEqual([[account(provider)]])
    expect(calls).toEqual([])
  })
})
