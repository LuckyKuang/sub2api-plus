import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, shallowMount } from '@vue/test-utils'
import type { AxiosAdapter, InternalAxiosRequestConfig } from 'axios'
import { apiClient } from '@/api/client'
import type { Account } from '@/types'
import ReAuthAccountModal from '@/components/admin/account/ReAuthAccountModal.vue'

const notice = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: notice.success, showError: notice.error }) }))
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)

const entries = ['openai exchange', 'openai RT', 'grok exchange', 'grok RT', 'grok SSO'] as const
type Entry = typeof entries[number]
type Call = { url: string; body: Record<string, unknown> }
const originalAdapter = apiClient.defaults.adapter
let calls: Call[]
let grant: () => Promise<Record<string, unknown>>
let apply: (id: number, body: Record<string, unknown>) => Promise<Record<string, unknown>>
let deviceStart: () => Promise<Record<string, unknown>>
let devicePoll: () => Promise<Record<string, unknown>>
function deferred() {
  let resolve!: (value: Record<string, unknown>) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<Record<string, unknown>>((ok, fail) => { resolve = ok; reject = fail })
  return { promise, resolve, reject }
}
function token(value: string) { return { access_token: value, refresh_token: `${value}-refresh`, expires_at: 1999999999 } }
function account(entry: Entry, id = 81, proxy = 31) {
  return { id, name: `Account ${id}`, platform: entry.startsWith('openai') ? 'openai' : 'grok', type: 'oauth',
    credentials: {}, credentials_status: { has_access_token: true, has_refresh_token: true },
    oauth_credential_owner: (id === 81 && proxy === 31 ? 'a' : 'b').repeat(64), extra: { quota_limit: 100 },
    proxy: { id: proxy, name: `Route ${proxy}`, protocol: 'http', host: 'proxy.example', port: 8080, status: 'active', updated_at: '2026-10-25T10:00:00Z' },
    proxy_id: proxy, status: 'active', schedulable: true, concurrency: 1, priority: 1, group_ids: [] } as unknown as Account
}
function reply(config: InternalAxiosRequestConfig, data: unknown) {
  return { config, status: 200, statusText: 'OK', headers: {}, data: { code: 0, data } }
}
const adapter: AxiosAdapter = async config => {
  const url = config.url || ''
  const body = typeof config.data === 'string' ? JSON.parse(config.data) : (config.data || {})
  calls.push({ url, body })
  if (url.endsWith('/generate-auth-url') || url.endsWith('/oauth/auth-url')) {
    return reply(config, { session_id: `route-${body.proxy_id}`, state: `state-${body.proxy_id}`, auth_url: `https://login.example/authorize?state=state-${body.proxy_id}` })
  }
  if (url.endsWith('/exchange-code') || url.endsWith('/refresh-token') || url.endsWith('/sso-token')) return reply(config, await grant())
  if (url.endsWith('/device-code/start')) return reply(config, await deviceStart())
  if (url.endsWith('/device-code/poll')) return reply(config, await devicePoll())
  const owner = Number(url.match(/^\/admin\/accounts\/(\d+)\/apply-oauth-credentials$/)?.[1])
  if (owner) return reply(config, await apply(owner, body))
  throw new Error(`Unexpected reauthorization request: ${config.method} ${url}`)
}
beforeEach(() => {
  vi.clearAllMocks()
  calls = []
  grant = async () => token('current-grant')
  apply = async (id, body) => ({ ...account('grok RT', id), ...body })
  deviceStart = async () => ({ session_id: 'device-session', user_code: 'CURRENT-CODE', verification_url: 'https://login.example/device', interval_seconds: 1 })
  devicePoll = async () => token('device-grant')
  apiClient.defaults.adapter = adapter
})

async function startDevice(view: Modal) {
  await view.get('input[value="device_code"]').setValue(true)
  await button(view, 'admin.accounts.oauth.openai.startDeviceCode').trigger('click')
  await flushPromises()
}

describe('OpenAI device existing-account reauthorization boundaries', () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => vi.useRealTimers())

  it('applies the current device grant only to its initiating account', async () => {
    const view = open('openai RT')
    await startDevice(view)
    expect(writes()).toHaveLength(1)
    expect(writes()[0]).toMatchObject({ url: '/admin/accounts/81/apply-oauth-credentials', body: { expected_owner: 'a'.repeat(64), credentials: { access_token: 'device-grant' } } })
    expect(view.emitted('reauthorized')).toHaveLength(1)
    expect(view.emitted('close')).toHaveLength(1)
    expect(notice.success).toHaveBeenCalledOnce()
    expect(notice.error).not.toHaveBeenCalled()
  })

  it.each(['another account', 'same account route', 'close and reopen', 'unmount'].flatMap(boundary =>
    ['start', 'poll', 'apply'].flatMap(stage => ['success', 'failure'].map(outcome => [boundary, stage, outcome] as const))))('discards old %s %s %s and the replacement device flow can finish', async (boundary, stage, outcome) => {
    const old = deferred()
    if (stage === 'start') deviceStart = () => old.promise
    else if (stage === 'poll') devicePoll = () => old.promise
    else apply = () => old.promise
    const previous = open('openai RT')
    await startDevice(previous)
    expect(writes()).toHaveLength(stage === 'apply' ? 1 : 0)
    let view = previous
    let next = account('openai RT', 81, 32)
    if (boundary === 'unmount') {
      previous.unmount()
      view = open('openai RT')
      await view.setProps({ account: next })
    } else next = await replace(previous, 'openai RT', boundary)
    const oldCloseCount = previous.emitted('close')?.length || 0
    const newCloseCount = view.emitted('close')?.length || 0
    expect(view.getComponent({ name: 'OAuthAuthorizationFlow' }).props('deviceUserCode')).toBe('')
    expect(view.getComponent({ name: 'OAuthAuthorizationFlow' }).props('deviceCodePolling')).toBe(false)
    expect(view.getComponent({ name: 'OAuthAuthorizationFlow' }).props('loading')).toBe(false)
    const current = deferred()
    deviceStart = async () => ({ session_id: 'new-device-session', user_code: 'NEW-CODE', verification_url: 'https://login.example/new-device', interval_seconds: 1 })
    devicePoll = () => current.promise
    apply = async (id, body) => ({ ...account('openai RT', id, 32), ...body })
    await startDevice(view)
    expect(view.getComponent({ name: 'OAuthAuthorizationFlow' }).props('deviceCodePolling')).toBe(true)
    notice.success.mockClear()
    notice.error.mockClear()
    if (outcome === 'failure') old.reject(new Error('discarded device operation failed'))
    else old.resolve(stage === 'start'
      ? { session_id: 'old-device-session', user_code: 'OLD-CODE', verification_url: 'https://login.example/old-device', interval_seconds: 1 }
      : stage === 'poll' ? token('OLD-DEVICE-GRANT') : { ...account('openai RT'), credentials: token('OLD-DEVICE-GRANT') })
    await flushPromises()
    expect(writes()).toHaveLength(stage === 'apply' ? 1 : 0)
    expect(previous.emitted('reauthorized')).toBeUndefined()
    expect(previous.emitted('close')?.length || 0).toBe(oldCloseCount)
    expect(notice.success).not.toHaveBeenCalled()
    expect(notice.error).not.toHaveBeenCalled()
    expect(view.getComponent({ name: 'OAuthAuthorizationFlow' }).props('deviceUserCode')).toBe('NEW-CODE')
    expect(view.getComponent({ name: 'OAuthAuthorizationFlow' }).props('deviceCodePolling')).toBe(true)
    expect(view.getComponent({ name: 'OAuthAuthorizationFlow' }).props('error')).toBe('')
    current.resolve(token('new-device-grant'))
    await flushPromises()
    expect(writes()).toHaveLength(stage === 'apply' ? 2 : 1)
    expect(writes().at(-1)).toMatchObject({ url: `/admin/accounts/${next.id}/apply-oauth-credentials`, body: { expected_owner: next.oauth_credential_owner, credentials: { access_token: 'new-device-grant' } } })
    expect(view.emitted('reauthorized')).toHaveLength(1)
    expect(view.emitted('close')).toHaveLength(newCloseCount + 1)
    expect(notice.success).toHaveBeenCalledOnce()
    expect(notice.error).not.toHaveBeenCalled()
  })

  it.each(['another account', 'same account route', 'close and reopen', 'unmount'])('%s stops an old device loop during its polling interval', async boundary => {
    devicePoll = async () => ({ pending: true })
    const view = open('openai RT')
    await startDevice(view)
    expect(calls.filter(call => call.url.endsWith('/device-code/poll'))).toHaveLength(1)
    if (boundary === 'unmount') view.unmount()
    else await replace(view, 'openai RT', boundary)
    await vi.advanceTimersByTimeAsync(2000)
    expect(calls.filter(call => call.url.endsWith('/device-code/poll'))).toHaveLength(1)
    expect(writes()).toEqual([])
    expect(notice.success).not.toHaveBeenCalled()
    expect(notice.error).not.toHaveBeenCalled()
  })
})
afterEach(() => { apiClient.defaults.adapter = originalAdapter })
function open(entry: Entry) {
  return shallowMount(ReAuthAccountModal, { props: { show: true, account: account(entry) }, global: { stubs: {
    BaseDialog: { props: ['show'], template: '<section v-if="show"><slot /><slot name="footer" /></section>' },
    OAuthAuthorizationFlow: false, Icon: true,
  } } })
}
type Modal = ReturnType<typeof open>
function button(view: Modal, key: string) {
  const matches = view.findAll('button').filter(button => button.text() === key)
  expect(matches, key).toHaveLength(1)
  return matches[0]!
}
async function start(view: Modal, entry: Entry, proxy = 31) {
  const provider = entry.split(' ')[0]
  if (entry.endsWith('exchange')) {
    await view.get('input[value="manual"]').setValue(true)
    await button(view, `admin.accounts.oauth.${provider}.generateAuthUrl`).trigger('click')
    await flushPromises()
    await view.get('textarea').setValue(`http://localhost/callback?code=one-use&state=state-${proxy}`)
    await button(view, 'admin.accounts.oauth.completeAuth').trigger('click')
  } else {
    const sso = entry.endsWith('SSO')
    await view.get(`input[value="${sso ? 'sso_cookie' : 'refresh_token'}"]`).setValue(true)
    await view.get('textarea').setValue(sso ? 'ephemeral-sso' : 'input-refresh')
    await button(view, `admin.accounts.oauth.${provider}.${sso ? 'convertSSOAndCreate' : 'validateAndCreate'}`).trigger('click')
  }
  await flushPromises()
}
async function replace(view: Modal, entry: Entry, boundary: string) {
  const next = boundary === 'another account' ? account(entry, 82, 32) : account(entry, 81, 32)
  if (boundary === 'redacted credential owner') {
    Object.assign(next, account(entry, 81, 31), { oauth_credential_owner: 'c'.repeat(64) })
    const previous = view.props('account')!
    expect({ ...next, oauth_credential_owner: previous.oauth_credential_owner }).toEqual(previous)
  } else if (boundary === 'same proxy endpoint') {
    Object.assign(next, account(entry, 81, 31), { oauth_credential_owner: 'c'.repeat(64), proxy: { ...account(entry).proxy, host: 'repaired.example', updated_at: '2026-10-25T10:01:00Z' } })
  }
  if (boundary === 'close and reopen') {
    await button(view, 'common.cancel').trigger('click')
    await view.setProps({ show: false, account: null })
    await view.setProps({ show: true, account: next })
  } else await view.setProps({ account: next })
  await flushPromises()
  return next
}
function writes() { return calls.filter(call => call.url.endsWith('/apply-oauth-credentials')) }

// Existing-account reauthorization is an independent write entrypoint. Its
// owner includes the modal generation, account, saved credentials and route.
// Requests already sent can finish; they cannot authorize a replacement owner
// or publish completion/error/loading changes into its visible flow.
describe('existing-account reauthorization owner boundaries', () => {
  it.each((['openai RT', 'grok SSO'] as const).flatMap(entry => ['usage metadata', 'proxy name and timestamp'].map(metadata => [entry, metadata] as const)))('%s keeps its active login when a fresh redacted DTO changes only %s', async (entry, metadata) => {
    const pending = deferred()
    grant = () => pending.promise
    const view = open(entry)
    await start(view, entry)
    const previous = view.props('account')!
    const next = metadata === 'usage metadata'
      ? { ...previous, updated_at: '2026-10-25T10:05:00Z', extra: { ...previous.extra, quota_fetched_at: 1999999999 } }
      : { ...previous, proxy: { ...previous.proxy!, name: 'Renamed saved route', updated_at: '2026-10-25T10:05:00Z' } }
    expect(next.oauth_credential_owner).toBe(previous.oauth_credential_owner)
    await view.setProps({ account: next })
    expect(view.getComponent({ name: 'OAuthAuthorizationFlow' }).props('loading')).toBe(true)
    pending.resolve(token('same-owner-grant'))
    await flushPromises()
    expect(writes()).toHaveLength(1)
    expect(writes()[0]).toMatchObject({ url: '/admin/accounts/81/apply-oauth-credentials', body: { expected_owner: 'a'.repeat(64), credentials: { access_token: 'same-owner-grant' } } })
    expect(view.emitted('reauthorized')).toHaveLength(1)
    expect(notice.success).toHaveBeenCalledOnce()
    expect(notice.error).not.toHaveBeenCalled()
  })
  it('selects Grok refresh-token reauthorization from the real redacted credential-status DTO', async () => {
    const view = open('grok RT')
    expect(view.getComponent({ name: 'OAuthAuthorizationFlow' }).props('initialInputMethod')).toBe('refresh_token')
    expect(view.get('input[value="refresh_token"]').element).toHaveProperty('checked', true)
    expect(calls).toEqual([])
  })
  it.each(entries)('%s applies the current grant only to its initiating account', async entry => {
    const view = open(entry)
    await start(view, entry)
    expect(writes()).toHaveLength(1)
    expect(writes()[0]).toMatchObject({ url: '/admin/accounts/81/apply-oauth-credentials', body: { expected_owner: 'a'.repeat(64), type: 'oauth', credentials: { access_token: 'current-grant' } } })
    expect(writes()[0]!.body.credentials).not.toHaveProperty('sso_token')
    expect(writes()[0]!.body.credentials).not.toHaveProperty('password')
    expect(view.emitted('reauthorized')).toHaveLength(1)
    expect(view.emitted('close')).toHaveLength(1)
    expect(notice.success).toHaveBeenCalledOnce()
    expect(notice.error).not.toHaveBeenCalled()
  })

  it.each(entries.flatMap(entry => ['another account', 'same account route', 'close and reopen', ...(entry === 'openai RT' || entry === 'grok SSO' ? ['redacted credential owner', 'same proxy endpoint'] : [])].flatMap(boundary =>
    ['grant', 'apply'].flatMap(stage => ['success', 'failure'].map(outcome => [entry, boundary, stage, outcome] as const)))))('%s discards old %s %s %s and the replacement can finish', async (entry, boundary, stage, outcome) => {
    const old = deferred()
    const current = deferred()
    if (stage === 'grant') grant = () => old.promise
    else apply = () => old.promise
    const view = open(entry)
    await start(view, entry)
    expect(writes()).toHaveLength(stage === 'apply' ? 1 : 0)
    const next = await replace(view, entry, boundary)
    const initialCloseCount = view.emitted('close')?.length || 0
    const replacementFlow = view.getComponent({ name: 'OAuthAuthorizationFlow' })
    expect(replacementFlow.props('loading')).toBe(false)
    expect(replacementFlow.props('authUrl')).toBe('')
    expect(replacementFlow.props('error')).toBe('')
    grant = () => current.promise
    apply = async (id, body) => ({ ...account(entry, id, 32), ...body })
    await start(view, entry, next.proxy_id!)
    expect(view.getComponent({ name: 'OAuthAuthorizationFlow' }).props('loading')).toBe(true)
    notice.success.mockClear()
    notice.error.mockClear()
    if (outcome === 'success') old.resolve(stage === 'grant' ? token('OLD-GRANT') : { ...account(entry), credentials: token('OLD-GRANT') })
    else old.reject(new Error('discarded authorization failed'))
    await flushPromises()
    expect(writes()).toHaveLength(stage === 'apply' ? 1 : 0)
    expect(view.emitted('reauthorized')).toBeUndefined()
    expect(view.emitted('close')?.length || 0).toBe(initialCloseCount)
    expect(notice.success).not.toHaveBeenCalled()
    expect(notice.error).not.toHaveBeenCalled()
    expect(view.getComponent({ name: 'OAuthAuthorizationFlow' }).props('loading')).toBe(true)
    expect(view.getComponent({ name: 'OAuthAuthorizationFlow' }).props('error')).toBe('')
    current.resolve(token('replacement-grant'))
    await flushPromises()
    expect(writes()).toHaveLength(stage === 'apply' ? 2 : 1)
    expect(writes().at(-1)).toMatchObject({ url: `/admin/accounts/${next.id}/apply-oauth-credentials`, body: { expected_owner: next.oauth_credential_owner, credentials: { access_token: 'replacement-grant' } } })
    expect(view.emitted('reauthorized')).toHaveLength(1)
    expect(view.emitted('close')).toHaveLength(initialCloseCount + 1)
    expect(notice.success).toHaveBeenCalledOnce()
    expect(notice.error).not.toHaveBeenCalled()
  })
})
