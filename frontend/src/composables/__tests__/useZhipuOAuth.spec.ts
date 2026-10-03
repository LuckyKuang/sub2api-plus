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

  it('exposes plan-kind metadata for the form', () => {
    expect(zhipuPlanKindLabels['off-peak']).toBe('Off-peak Idle Plan')
    expect(zhipuPlanNeedsTeamScope('team-coding-plan')).toBe(true)
    expect(zhipuPlanNeedsTeamScope('individual-coding-plan')).toBe(false)
    expect(zhipuPlanNeedsTeamScope('off-peak')).toBe(false)
  })
})
