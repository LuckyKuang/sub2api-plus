import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount, shallowMount } from '@vue/test-utils'
import type { AxiosAdapter, InternalAxiosRequestConfig } from 'axios'
import { apiClient } from '@/api/client'
import CreateAccountModal from '@/components/account/CreateAccountModal.vue'
import ZhipuLinkPanel from '@/components/account/ZhipuLinkPanel.vue'

const notify = vi.hoisted(() => ({ error: vi.fn(), success: vi.fn() }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: notify.error, showSuccess: notify.success, showInfo: vi.fn(), showWarning: vi.fn() }) }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ isSimpleMode: true }) }))
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)

type AuthURL = { auth_url: string; session_id: string; state: string }
const originalAdapter = apiClient.defaults.adapter
let created: Record<string, unknown>[]
let requests: { url: string; body: Record<string, unknown> }[]
let generate: (body: Record<string, unknown>) => Promise<AuthURL>
let exchange: (body: Record<string, unknown>) => Promise<Record<string, unknown>>
let refreshToken: (body: Record<string, unknown>) => Promise<Record<string, unknown>>
let passwordLogin: (body: Record<string, unknown>) => Promise<Record<string, unknown>>
let ssoImport: (body: Record<string, unknown>) => Promise<Record<string, unknown>>
let accountCreate: (body: Record<string, unknown>) => Promise<Record<string, unknown>>
let deviceStart: () => Promise<Record<string, unknown>>
let devicePoll: () => Promise<Record<string, unknown>>
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((ok, fail) => { resolve = ok; reject = fail })
  return { promise, resolve, reject }
}
function response(config: InternalAxiosRequestConfig, data: unknown) {
  return { config, status: 200, statusText: 'OK', headers: {}, data: { code: 0, data } }
}
const adapter: AxiosAdapter = async config => {
  const url = config.url || ''
  const body = typeof config.data === 'string' ? JSON.parse(config.data) : (config.data || {})
  requests.push({ url, body })
  if (url === '/admin/zhipu/oauth/capabilities') return response(config, { enabled: true, providers: ['bigmodel'], plan_kinds: ['individual-coding-plan'], supported_plan_kinds: ['individual-coding-plan'] })
  if (url === '/admin/zhipu/oauth/start') return response(config, await zcodeStart(body))
  if (url === '/admin/zhipu/oauth/poll') return response(config, await zcodePoll(body))
  if (url === '/admin/zhipu/oauth/exchange-code') return response(config, await zcodeExchange(body))
  if (url === '/admin/zhipu/oauth/create-from-oauth') {
    created.push(body)
    return response(config, await zcodeCreate())
  }
  if (config.method === 'get') {
    if (url.endsWith('/tls-fingerprint-profiles')) return response(config, [])
    if (url.endsWith('/web-search-emulation')) return response(config, { enabled: false, providers: [] })
    if (url === '/admin/settings') return response(config, {})
    if (url === '/admin/accounts/antigravity/default-model-mapping') return response(config, {})
  }
  if (url.endsWith('/device-code/start')) return response(config, await deviceStart())
  if (url.endsWith('/device-code/poll')) return response(config, await devicePoll())
  if (url.endsWith('/generate-auth-url') || url.endsWith('/oauth/auth-url')) return response(config, await generate(body))
  if (url.endsWith('/exchange-code')) return response(config, await exchange(body))
  if (url.endsWith('/refresh-token')) return response(config, await refreshToken(body))
  if (url === '/admin/grok/oauth/password') return response(config, await passwordLogin(body))
  if (url === '/admin/grok/sso-to-oauth') return response(config, await ssoImport(body))
  if (url === '/admin/accounts' && config.method === 'post') {
    created.push(body)
    return response(config, await accountCreate(body))
  }
  throw new Error(`Unexpected request: ${config.method} ${url}`)
}
beforeEach(() => {
  vi.clearAllMocks()
  created = []
  requests = []
  generate = async body => ({ session_id: `route-${body.proxy_id}`, state: `state-${body.proxy_id}`, auth_url: `https://login.example/authorize?state=state-${body.proxy_id}` })
  exchange = async () => ({ access_token: 'current-access', refresh_token: 'current-refresh', expires_at: 1999999999 })
  refreshToken = async () => ({ access_token: 'current-access', refresh_token: 'current-refresh', expires_at: 1999999999 })
  passwordLogin = async () => ({ access_token: 'current-access', refresh_token: 'current-refresh', expires_at: 1999999999 })
  ssoImport = async () => ({ created: [{ id: 81 }], failed: [] })
  accountCreate = async body => ({ ...body, id: 81 })
  deviceStart = async () => ({ session_id: 'old-device', user_code: 'OLD-DEVICE-CODE', verification_url: 'https://login.example/device', interval_seconds: 1 })
  devicePoll = async () => ({ pending: true })
  apiClient.defaults.adapter = adapter
})
afterEach(() => { apiClient.defaults.adapter = originalAdapter })

function button(wrapper: ReturnType<typeof openModal>, key: string) {
  const found = wrapper.findAll('button').find(candidate => candidate.text() === key)
  expect(found, key).toBeDefined()
  return found!
}
function openModal() {
  return shallowMount(CreateAccountModal, {
    props: { show: true, groups: [], proxies: [31, 32].map(id => ({ id, name: `Route ${id}`, host: `route${id}.example`, protocol: 'http', port: 8080, status: 'active' })) as any },
    global: { stubs: {
      BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' },
      OAuthAuthorizationFlow: false, ProxySelector: false, Icon: true,
      Transition: { template: '<div><slot /></div>' }
    } }
  })
}
async function chooseRoute(wrapper: ReturnType<typeof openModal>, id: number) {
  const selector = wrapper.getComponent({ name: 'ProxySelector' })
  await selector.get('.select-trigger').trigger('click')
  const option = selector.findAll('.select-option').find(option => option.text().includes(`Route ${id}`))
  expect(option, selector.html()).toBeDefined()
  await option!.trigger('click')
}
async function enterFlow(wrapper: ReturnType<typeof openModal>, platform: 'openai' | 'grok', route = 31) {
  await button(wrapper, platform === 'openai' ? 'OpenAI' : 'Grok').trigger('click')
  await wrapper.get('[data-tour="account-form-name"]').setValue('Current account')
  await chooseRoute(wrapper, route)
  await wrapper.get('#create-account-form').trigger('submit')
  await flushPromises()
}
async function generateURL(wrapper: ReturnType<typeof openModal>) {
  const platform = wrapper.getComponent({ name: 'OAuthAuthorizationFlow' }).props('platform')
  await button(wrapper, `admin.accounts.oauth.${platform}.generateAuthUrl`).trigger('click')
  await flushPromises()
}
async function changeRoute(wrapper: ReturnType<typeof openModal>) {
  await button(wrapper, 'common.back').trigger('click')
  await chooseRoute(wrapper, 32)
  await wrapper.get('#create-account-form').trigger('submit')
  await flushPromises()
}
async function enterCallback(wrapper: ReturnType<typeof openModal>, state: string) {
  await wrapper.get('textarea').setValue(`http://localhost/callback?code=old-code&state=${state}`)
  await button(wrapper, 'admin.accounts.oauth.completeAuth').trigger('click')
  await flushPromises()
}

// A login belongs to the modal generation, account form and captured proxy.
// Returning to basic settings, closing or replacing it revokes the UI's right
// to publish its URL/token or create an account from a late response. These
// requirements hold even if a server finishes an already-issued request.
describe('OAuth proxy change and late browser callback isolation', () => {
  it.each(['openai', 'grok'] as const)('%s ignores a late URL when the user starts on another route', async platform => {
    const old = deferred<AuthURL>()
    generate = body => body.proxy_id === 31 ? old.promise : Promise.resolve({ session_id: 'new-session', state: 'new-state', auth_url: 'https://login.example/current?state=new-state' })
    const wrapper = openModal()
    await enterFlow(wrapper, platform)
    await generateURL(wrapper)
    await changeRoute(wrapper)
    await generateURL(wrapper)
    expect(wrapper.findAll<HTMLInputElement>('input').some(input => input.element.value.includes('/current'))).toBe(true)
    old.resolve({ session_id: 'old-session', state: 'old-state', auth_url: 'https://login.example/old?state=old-state' })
    await flushPromises()
    expect(wrapper.findAll<HTMLInputElement>('input').some(input => input.element.value.includes('/old'))).toBe(false)
    expect(wrapper.findAll<HTMLInputElement>('input').some(input => input.element.value.includes('/current'))).toBe(true)
    expect(created).toEqual([])
  })

  it.each(['openai', 'grok'] as const)('%s cannot create from a late exchange after a route change, and the replacement can finish', async platform => {
    const old = deferred<Record<string, unknown>>()
    exchange = body => body.session_id === 'route-31' ? old.promise : Promise.resolve({ access_token: 'new-access', refresh_token: 'new-refresh', expires_at: 1999999999 })
    const wrapper = openModal()
    await enterFlow(wrapper, platform)
    await generateURL(wrapper)
    await enterCallback(wrapper, 'state-31')
    expect(requests.filter(request => request.url.endsWith('/exchange-code'))).toHaveLength(1)
    await changeRoute(wrapper)
    await generateURL(wrapper)
    old.resolve({ access_token: 'old-access', refresh_token: 'old-refresh', expires_at: 1999999999 })
    await flushPromises()
    expect(created).toEqual([])
    expect(wrapper.emitted('created')).toBeUndefined()
    await enterCallback(wrapper, 'state-32')
    expect(created).toHaveLength(1)
    expect(created[0]).toMatchObject({ platform, proxy_id: 32, name: 'Current account', credentials: { access_token: 'new-access', refresh_token: 'new-refresh' } })
    expect(wrapper.emitted('created')).toHaveLength(1)
  })

  it.each(['openai', 'grok'] as const)('%s stops an old RT batch before any second validation or account write', async platform => {
    const old = deferred<Record<string, unknown>>()
    refreshToken = () => old.promise
    const wrapper = openModal()
    await enterFlow(wrapper, platform)
    await wrapper.get('input[value="refresh_token"]').setValue(true)
    await wrapper.get('textarea').setValue('old-first-rt\nold-second-rt')
    await button(wrapper, `admin.accounts.oauth.${platform}.validateAndCreate`).trigger('click')
    await flushPromises()
    expect(requests.filter(request => request.url.endsWith('/refresh-token'))).toHaveLength(1)
    await changeRoute(wrapper)
    await generateURL(wrapper)
    old.resolve({ access_token: 'old-access', refresh_token: 'old-refresh', expires_at: 1999999999 })
    await flushPromises()
    expect(requests.filter(request => request.url.endsWith('/refresh-token'))).toHaveLength(1)
    expect(created).toEqual([])
    expect(wrapper.emitted('created')).toBeUndefined()
    expect(notify.error).not.toHaveBeenCalled()
  })

  it.each(['openai', 'grok'] as const)('%s cannot create from an exchange after the dialog closes', async platform => {
    const old = deferred<Record<string, unknown>>()
    exchange = () => old.promise
    const wrapper = openModal()
    await enterFlow(wrapper, platform)
    await generateURL(wrapper)
    await enterCallback(wrapper, 'state-31')
    await wrapper.setProps({ show: false })
    old.resolve({ access_token: 'old-access', refresh_token: 'old-refresh', expires_at: 1999999999 })
    await flushPromises()
    expect(created).toEqual([])
    expect(wrapper.emitted('created')).toBeUndefined()
  })
})

describe('Grok independent import and saved-account completion boundaries', () => {
  it('stops the old password batch after its route is replaced', async () => {
    const old = deferred<Record<string, unknown>>()
    passwordLogin = () => old.promise
    const wrapper = openModal()
    await enterFlow(wrapper, 'grok')
    // The capability-controlled password UI is currently hidden by this modal.
    // Exercise its retained parent event handler independently; this does not
    // claim that the visible UI offers password login.
    wrapper.getComponent({ name: 'OAuthAuthorizationFlow' }).vm.$emit('authorize-password', 'first@example.test----first-secret\nsecond@example.test----second-secret')
    await flushPromises()
    expect(requests.filter(request => request.url === '/admin/grok/oauth/password')).toHaveLength(1)
    await changeRoute(wrapper)
    await generateURL(wrapper)
    old.resolve({ access_token: 'old-password-token', refresh_token: 'old-password-refresh', expires_at: 1999999999 })
    await flushPromises()
    expect(requests.filter(request => request.url === '/admin/grok/oauth/password')).toHaveLength(1)
    expect(created).toEqual([])
    expect(wrapper.emitted('created')).toBeUndefined()
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(notify.error).not.toHaveBeenCalled()
    expect(notify.success).not.toHaveBeenCalled()
  })

  it.each(['success', 'failure'] as const)('ignores an old SSO import %s after the replacement authorization starts', async outcome => {
    const old = deferred<Record<string, unknown>>()
    ssoImport = () => old.promise
    const wrapper = openModal()
    await enterFlow(wrapper, 'grok')
    await wrapper.get('input[value="sso_cookie"]').setValue(true)
    await wrapper.get('textarea').setValue('old-ephemeral-sso')
    await button(wrapper, 'admin.accounts.oauth.grok.convertSSOAndCreate').trigger('click')
    await flushPromises()
    expect(requests.filter(request => request.url === '/admin/grok/sso-to-oauth')).toHaveLength(1)
    expect(requests.find(request => request.url === '/admin/grok/sso-to-oauth')?.body.proxy_id).toBe(31)
    await changeRoute(wrapper)
    const replacement = deferred<AuthURL>()
    generate = () => replacement.promise
    await generateURL(wrapper)
    expect(wrapper.getComponent({ name: 'OAuthAuthorizationFlow' }).props('loading')).toBe(true)
    if (outcome === 'success') old.resolve({ created: [{ id: 81 }], failed: [] })
    else old.reject(new Error('old SSO request failed'))
    await flushPromises()
    expect(wrapper.emitted('created')).toBeUndefined()
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(notify.error).not.toHaveBeenCalled()
    expect(notify.success).not.toHaveBeenCalled()
    expect(wrapper.getComponent({ name: 'OAuthAuthorizationFlow' }).props('error')).toBe('')
    expect(wrapper.getComponent({ name: 'OAuthAuthorizationFlow' }).props('loading')).toBe(true)
    replacement.resolve({ session_id: 'replacement-session', state: 'replacement-state', auth_url: 'https://login.example/replacement' })
    await flushPromises()
    expect(wrapper.getComponent({ name: 'OAuthAuthorizationFlow' }).props('loading')).toBe(false)
    expect(wrapper.findAll<HTMLInputElement>('input').some(input => input.element.value.includes('/replacement'))).toBe(true)
  })

  it.each(['success', 'failure'] as const)('an old account POST %s cannot finish the replacement Grok flow', async outcome => {
    const old = deferred<Record<string, unknown>>()
    accountCreate = () => old.promise
    const wrapper = openModal()
    await enterFlow(wrapper, 'grok')
    await generateURL(wrapper)
    await enterCallback(wrapper, 'state-31')
    expect(created).toHaveLength(1)
    expect(created[0]).toMatchObject({ platform: 'grok', proxy_id: 31, credentials: { access_token: 'current-access' } })
    await changeRoute(wrapper)
    await generateURL(wrapper)
    if (outcome === 'success') old.resolve({ id: 81, ...created[0] })
    else old.reject(new Error('old account write failed'))
    await flushPromises()
    // An already-issued POST may finish server-side. Its UI completion and
    // follow-up requests belong exclusively to the discarded modal generation.
    expect(created).toHaveLength(1)
    expect(requests.filter(request => request.url.endsWith('/sync-upstream-models'))).toEqual([])
    expect(wrapper.emitted('created')).toBeUndefined()
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(notify.error).not.toHaveBeenCalled()
    expect(notify.success).not.toHaveBeenCalled()
    expect(wrapper.findAll<HTMLInputElement>('input').some(input => input.element.value.includes('state-32'))).toBe(true)
  })
})


describe('OpenAI device login route and disposal boundaries', () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => vi.useRealTimers())

  it.each(['start', 'poll'])('ignores a late device %s after returning to change proxy', async stage => {
    const old = deferred<Record<string, unknown>>()
    if (stage === 'start') deviceStart = () => old.promise
    else devicePoll = () => old.promise
    const wrapper = openModal()
    await enterFlow(wrapper, 'openai')
    await wrapper.get('input[value="device_code"]').setValue(true)
    await button(wrapper, 'admin.accounts.oauth.openai.startDeviceCode').trigger('click')
    await flushPromises()
    await changeRoute(wrapper)
    await generateURL(wrapper)
    old.resolve(stage === 'start'
      ? { session_id: 'old-device', user_code: 'OLD-DEVICE-CODE', verification_url: 'https://login.example/device', interval_seconds: 1 }
      : { access_token: 'old-device-token', refresh_token: 'old-device-refresh', expires_at: 1999999999 })
    await flushPromises()
    await vi.advanceTimersByTimeAsync(2000)
    expect(created).toEqual([])
    expect(wrapper.text()).not.toContain('OLD-DEVICE-CODE')
    expect(wrapper.emitted('created')).toBeUndefined()
    expect(notify.error).not.toHaveBeenCalled()
  })

  it('does not issue another device poll after its modal unmounts during the interval', async () => {
    const wrapper = openModal()
    await enterFlow(wrapper, 'openai')
    await wrapper.get('input[value="device_code"]').setValue(true)
    await button(wrapper, 'admin.accounts.oauth.openai.startDeviceCode').trigger('click')
    await flushPromises()
    expect(requests.filter(request => request.url.endsWith('/device-code/poll'))).toHaveLength(1)
    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(3000)
    expect(requests.filter(request => request.url.endsWith('/device-code/poll'))).toHaveLength(1)
    expect(created).toEqual([])
  })
})

type ZCodeSession = { session_id: string; provider: string; authorize_url: string; expires_at: string; interval_seconds: number }
const zcodeSession = (id: string): ZCodeSession => ({ session_id: id, provider: 'bigmodel', authorize_url: `https://bigmodel.example/authorize?state=${id}`, expires_at: new Date(Date.now() + 60000).toISOString(), interval_seconds: 2 })
const zcodeReady = { provider: 'bigmodel', access_token: 'old-zcode-token', zcode_jwt_token: 'old-zcode-jwt', user: { user_id: 'old-user' } }
let zcodeStart: (body: Record<string, unknown>) => Promise<ZCodeSession>
let zcodePoll: (body: Record<string, unknown>) => Promise<object>
let zcodeExchange: (body: Record<string, unknown>) => Promise<object>
let zcodeCreate: () => Promise<object>

describe('ZCode link proxy-generation boundaries through production API adapter', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    zcodeStart = async body => zcodeSession(`route-${body.proxy_id}`)
    zcodePoll = async () => ({ pending: true })
    zcodeExchange = async () => ({ pending: true })
    zcodeCreate = async () => ({ id: 88 })
  })
  afterEach(() => vi.useRealTimers())

  async function openPanel() {
    const wrapper = mount(ZhipuLinkPanel, { props: { proxyId: 31 }, global: { stubs: { Icon: true } } })
    await flushPromises()
    await wrapper.get('[data-testid="zhipu-link-start"]').trigger('click')
    await flushPromises()
    return wrapper
  }

  it('drops a delayed start after proxy replacement and retains the replacement link', async () => {
    const old = deferred<ZCodeSession>()
    zcodeStart = body => body.proxy_id === 31 ? old.promise : Promise.resolve(zcodeSession('replacement'))
    const wrapper = await openPanel()
    await wrapper.setProps({ proxyId: 32 })
    await wrapper.get('[data-testid="zhipu-link-start"]').trigger('click')
    await flushPromises()
    old.resolve(zcodeSession('old-start'))
    await flushPromises()
    expect(wrapper.get('[data-testid="zhipu-link-open"]').attributes('href')).toBe('https://bigmodel.example/authorize?state=replacement')
    expect(created).toEqual([])
  })

  it('drops a delayed poll after proxy replacement without exposing old credentials', async () => {
    const old = deferred<object>()
    zcodePoll = () => old.promise
    const wrapper = await openPanel()
    await vi.advanceTimersByTimeAsync(2000)
    await wrapper.setProps({ proxyId: 32 })
    await wrapper.get('[data-testid="zhipu-link-start"]').trigger('click')
    await flushPromises()
    old.resolve({ pending: false, ready: zcodeReady })
    await flushPromises()
    expect(wrapper.find('[data-testid="zhipu-link-ready"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="zhipu-link-open"]').attributes('href')).toContain('route-32')
    expect(created).toEqual([])
  })

  it('drops a delayed callback and clears its old-route input when a proxy is replaced', async () => {
    const old = deferred<object>()
    zcodeExchange = () => old.promise
    const wrapper = await openPanel()
    await wrapper.findAll('button').find(candidate => candidate.text().endsWith('.showFallback'))!.trigger('click')
    await wrapper.get('[data-testid="zhipu-link-callback"]').setValue('https://zcode.example/callback?code=old&state=old')
    await wrapper.findAll('button').find(candidate => candidate.text().endsWith('.fallbackSubmit'))!.trigger('click')
    await flushPromises()
    await wrapper.setProps({ proxyId: 32 })
    await wrapper.get('[data-testid="zhipu-link-start"]').trigger('click')
    await flushPromises()
    old.resolve({ pending: false, ready: zcodeReady })
    await flushPromises()
    expect(wrapper.find('[data-testid="zhipu-link-ready"]').exists()).toBe(false)
    const input = wrapper.find<HTMLTextAreaElement>('[data-testid="zhipu-link-callback"]')
    expect(input.exists() ? input.element.value : '').toBe('')
    expect(created).toEqual([])
  })

  it('does not report an old submitted creation as completion of a replacement link', async () => {
    const old = deferred<object>()
    zcodePoll = async () => ({ pending: false, ready: zcodeReady })
    zcodeCreate = () => old.promise
    const wrapper = await openPanel()
    await vi.advanceTimersByTimeAsync(2000)
    await wrapper.get('[data-testid="zhipu-link-create"]').trigger('click')
    await flushPromises()
    expect(created).toHaveLength(1)
    expect(created[0]).toMatchObject({ proxy_id: 31, session_id: 'route-31', access_token: 'old-zcode-token' })
    await wrapper.setProps({ proxyId: 32 })
    await wrapper.get('[data-testid="zhipu-link-start"]').trigger('click')
    await flushPromises()
    old.resolve({ id: 88 })
    await flushPromises()
    expect(wrapper.emitted('created')).toBeUndefined()
    expect(wrapper.get('[data-testid="zhipu-link-open"]').attributes('href')).toContain('route-32')
  })
})
