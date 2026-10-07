import { beforeEach, describe, expect, it, vi } from 'vitest'
import { effectScope } from 'vue'
import { useZhipuOAuth, zhipuPlanKindLabels, zhipuPlanNeedsTeamScope } from '../useZhipuOAuth'
import {
  createZhipuAccountFromLink,
  exchangeZhipuLink,
  getZhipuOAuthCapabilities,
  pollZhipuLink,
  startZhipuLink
} from '@/api/admin/zhipu'

vi.mock('@/api/admin/zhipu', () => ({
  getZhipuOAuthCapabilities: vi.fn(),
  startZhipuLink: vi.fn(),
  pollZhipuLink: vi.fn(),
  exchangeZhipuLink: vi.fn(),
  createZhipuAccountFromLink: vi.fn()
}))

const session = {
  session_id: 's-1',
  provider: 'bigmodel' as const,
  authorize_url: 'https://bigmodel.cn/login?appId=zcode&state=st-1',
  expires_at: '2026-01-01T00:10:00Z',
  interval_seconds: 2
}

const ready = {
  provider: 'bigmodel' as const,
  access_token: 'bm-token',
  refresh_token: 'rt',
  zcode_jwt_token: 'zcode-jwt',
  user: { user_id: 'u-1', user_name: 'Ada' }
}

describe('useZhipuOAuth', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-01-01T00:00:00Z'))
    vi.mocked(getZhipuOAuthCapabilities).mockResolvedValue({
      enabled: true,
      providers: ['bigmodel', 'zai'],
      plan_kinds: ['individual-coding-plan', 'team-coding-plan', 'start-plan', 'off-peak'],
      supported_plan_kinds: ['individual-coding-plan', 'team-coding-plan', 'start-plan', 'off-peak'],
      handshake_url: 'https://zcode.z.ai/api/v1'
    })
    vi.mocked(startZhipuLink).mockResolvedValue(session)
    vi.mocked(pollZhipuLink).mockResolvedValue({ pending: true })
  })

  it('starts a link and polls at the server-provided interval', async () => {
    const scope = effectScope()
    const api = scope.run(() => useZhipuOAuth())!
    expect(await api.startLink('bigmodel')).toBe(true)
    expect(startZhipuLink).toHaveBeenCalledWith({ provider: 'bigmodel' })
    expect(api.session.value?.session_id).toBe('s-1')
    expect(api.polling.value).toBe(true)

    await vi.advanceTimersByTimeAsync(2000)
    expect(pollZhipuLink).toHaveBeenCalledWith('s-1')

    // Nothing polls before the interval elapses.
    vi.mocked(pollZhipuLink).mockClear()
    await vi.advanceTimersByTimeAsync(1000)
    expect(pollZhipuLink).not.toHaveBeenCalled()
    scope.stop()
  })

  it('stops polling once the platform reports the authorization is ready', async () => {
    const scope = effectScope()
    const api = scope.run(() => useZhipuOAuth())!
    vi.mocked(pollZhipuLink).mockResolvedValue({ pending: false, ready })
    await api.startLink('bigmodel')

    await vi.advanceTimersByTimeAsync(2000)
    expect(api.ready.value?.access_token).toBe('bm-token')
    expect(api.polling.value).toBe(false)

    vi.mocked(pollZhipuLink).mockClear()
    await vi.advanceTimersByTimeAsync(10000)
    expect(pollZhipuLink).not.toHaveBeenCalled()
    scope.stop()
  })

  it('keeps the session alive when a poll fails transiently', async () => {
    const scope = effectScope()
    const api = scope.run(() => useZhipuOAuth())!
    vi.mocked(pollZhipuLink).mockRejectedValue(new Error('gateway unavailable'))
    await api.startLink('bigmodel')

    await vi.advanceTimersByTimeAsync(2000)
    expect(api.error.value).toBe('gateway unavailable')
    expect(api.session.value?.session_id).toBe('s-1', 'a transient failure must not cancel the authorization')

    vi.mocked(pollZhipuLink).mockResolvedValue({ pending: false, ready })
    await vi.advanceTimersByTimeAsync(2000)
    expect(api.ready.value?.access_token).toBe('bm-token')
    scope.stop()
  })

  it('stops polling when the scope is disposed', async () => {
    const scope = effectScope()
    const api = scope.run(() => useZhipuOAuth())!
    await api.startLink('bigmodel')
    scope.stop()
    vi.mocked(pollZhipuLink).mockClear()
    await vi.advanceTimersByTimeAsync(10000)
    expect(pollZhipuLink).not.toHaveBeenCalled()
  })

  it('redeems a pasted callback URL and stops the poll loop', async () => {
    const scope = effectScope()
    const api = scope.run(() => useZhipuOAuth())!
    await api.startLink('zai')
    vi.mocked(exchangeZhipuLink).mockResolvedValue({ pending: false, ready: { ...ready, provider: 'zai' } })

    expect(await api.exchangeLink('https://zcode.z.ai/app/oauth/login?code=c&state=s')).toBe(true)
    expect(exchangeZhipuLink).toHaveBeenCalledWith('s-1', 'https://zcode.z.ai/app/oauth/login?code=c&state=s', undefined)
    expect(api.ready.value?.provider).toBe('zai')

    vi.mocked(pollZhipuLink).mockClear()
    await vi.advanceTimersByTimeAsync(10000)
    expect(pollZhipuLink).not.toHaveBeenCalled()
    scope.stop()
  })

  it('rejects an empty callback and an unauthorized create', async () => {
    const scope = effectScope()
    const api = scope.run(() => useZhipuOAuth())!
    expect(await api.createAccount({ plan_kind: 'individual-coding-plan' })).toBe(false)
    expect(api.error.value).toContain('Authorize')

    await api.startLink('bigmodel')
    expect(await api.exchangeLink('   ')).toBe(false)
    expect(api.error.value).toContain('required')
    scope.stop()
  })

  it('creates the account from the session token material', async () => {
    const scope = effectScope()
    const api = scope.run(() => useZhipuOAuth())!
    vi.mocked(pollZhipuLink).mockResolvedValue({ pending: false, ready })
    vi.mocked(createZhipuAccountFromLink).mockResolvedValue({})
    await api.startLink('bigmodel')
    await api.pollOnce()

    expect(await api.createAccount({ name: 'GLM', plan_kind: 'individual-coding-plan', concurrency: 3 })).toBe(true)
    expect(createZhipuAccountFromLink).toHaveBeenCalledWith({
      name: 'GLM',
      plan_kind: 'individual-coding-plan',
      concurrency: 3,
      session_id: 's-1',
      provider: 'bigmodel',
      access_token: 'bm-token',
      refresh_token: 'rt',
      zcode_jwt_token: 'zcode-jwt'
    })
    expect(api.session.value).toBeUndefined()
    scope.stop()
  })

  it('surfaces the server error message', async () => {
    const scope = effectScope()
    const api = scope.run(() => useZhipuOAuth())!
    vi.mocked(startZhipuLink).mockRejectedValue({ response: { data: { message: 'plan "off-peak" cannot be linked yet' } } })
    expect(await api.startLink('bigmodel')).toBe(false)
    expect(api.error.value).toBe('plan "off-peak" cannot be linked yet')
    scope.stop()
  })

  it('ignores a late start after cancellation', async () => {
    const scope = effectScope()
    const api = scope.run(() => useZhipuOAuth())!
    let resolve!: (value: typeof session) => void
    vi.mocked(startZhipuLink).mockReturnValueOnce(new Promise(done => { resolve = done }))
    const pending = api.startLink('bigmodel')
    api.cancelLink()
    resolve(session)
    expect(await pending).toBe(false)
    expect(api.session.value).toBeUndefined()
    expect(api.loading.value).toBe(false)
    scope.stop()
  })

  it('deduplicates polls and ignores their responses after cancellation', async () => {
    const scope = effectScope()
    const api = scope.run(() => useZhipuOAuth())!
    await api.startLink('bigmodel')
    let resolve!: (value: { pending: boolean; ready: typeof ready }) => void
    vi.mocked(pollZhipuLink).mockReturnValueOnce(new Promise(done => { resolve = done }))
    const first = api.pollOnce()
    const second = api.pollOnce()
    expect(pollZhipuLink).toHaveBeenCalledTimes(1)
    api.cancelLink()
    resolve({ pending: false, ready })
    await Promise.all([first, second])
    expect(api.ready.value).toBeUndefined()
    expect(api.session.value).toBeUndefined()
    scope.stop()
  })

  it('expires ready credentials and prevents their submission', async () => {
    const scope = effectScope()
    const api = scope.run(() => useZhipuOAuth())!
    vi.mocked(pollZhipuLink).mockResolvedValue({ pending: false, ready })
    await api.startLink('bigmodel')
    await api.pollOnce()
    await vi.advanceTimersByTimeAsync(600000)
    expect(api.expired.value).toBe(true)
    expect(api.ready.value).toBeUndefined()
    expect(await api.createAccount({ plan_kind: 'start-plan' })).toBe(false)
    expect(createZhipuAccountFromLink).not.toHaveBeenCalled()
    scope.stop()
  })

  it('stops retrying a terminal poll rejection', async () => {
    const scope = effectScope()
    const api = scope.run(() => useZhipuOAuth())!
    vi.mocked(pollZhipuLink).mockRejectedValue({ response: { status: 400, data: { message: 'Authorization denied' } } })
    await api.startLink('bigmodel')
    await vi.advanceTimersByTimeAsync(10000)
    expect(pollZhipuLink).toHaveBeenCalledTimes(1)
    expect(api.session.value).toBeUndefined()
    expect(api.error.value).toBe('Authorization denied')
    scope.stop()
  })

  it('resumes polling after a failed callback exchange', async () => {
    const scope = effectScope()
    const api = scope.run(() => useZhipuOAuth())!
    await api.startLink('bigmodel')
    vi.mocked(exchangeZhipuLink).mockRejectedValue(new Error('Invalid code'))
    expect(await api.exchangeLink('bad-code')).toBe(false)
    await vi.advanceTimersByTimeAsync(2000)
    expect(pollZhipuLink).toHaveBeenCalledTimes(1)
    scope.stop()
  })

  it('ignores a late callback exchange from a cancelled authorization', async () => {
    const scope = effectScope()
    const api = scope.run(() => useZhipuOAuth())!
    await api.startLink('bigmodel')
    let resolve!: (value: { pending: boolean; ready: typeof ready }) => void
    vi.mocked(exchangeZhipuLink).mockReturnValueOnce(new Promise(done => { resolve = done }))
    const pending = api.exchangeLink('code')
    api.cancelLink()
    resolve({ pending: false, ready })
    expect(await pending).toBe(false)
    expect(api.ready.value).toBeUndefined()
    scope.stop()
  })

  it('reports an accepted create even when the response arrives after session expiry', async () => {
    const scope = effectScope()
    const api = scope.run(() => useZhipuOAuth())!
    vi.mocked(pollZhipuLink).mockResolvedValue({ pending: false, ready })
    await api.startLink('bigmodel')
    await api.pollOnce()
    let resolve!: (value: Record<string, never>) => void
    vi.mocked(createZhipuAccountFromLink).mockReturnValueOnce(new Promise(done => { resolve = done }))
    const pending = api.createAccount({ plan_kind: 'start-plan' })
    expect(await api.createAccount({ plan_kind: 'start-plan' })).toBe(false)
    await vi.advanceTimersByTimeAsync(600000)
    resolve({})
    expect(await pending).toBe(true)
    expect(createZhipuAccountFromLink).toHaveBeenCalledTimes(1)
    expect(api.session.value).toBeUndefined()
    scope.stop()
  })

  it('exposes plan-kind metadata for the form', () => {
    expect(zhipuPlanKindLabels['off-peak']).toBe('Off-peak Idle Plan')
    expect(zhipuPlanNeedsTeamScope('team-coding-plan')).toBe(true)
    expect(zhipuPlanNeedsTeamScope('individual-coding-plan')).toBe(false)
    expect(zhipuPlanNeedsTeamScope('off-peak')).toBe(false)
  })
})
