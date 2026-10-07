import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { cnOAuthRequest, type CNOAuthSession } from '@/api/admin/cnOAuth'
import { useCNOAuth } from '../useCNOAuth'
vi.mock('@/api/admin/cnOAuth', () => ({ cnOAuthRequest: vi.fn() }))
const pending = (): CNOAuthSession => ({ session_id: 'session', authorize_url: 'https://auth.kimi.com/verify', user_code: 'CODE', expires_at: new Date(Date.now() + 60000).toISOString(), interval_seconds: 2, status: 'pending' })
function setup(platform: 'kimi' | 'deepseek' | 'minimax' = 'kimi') {
  let api!: ReturnType<typeof useCNOAuth>
  const wrapper = mount(defineComponent({ setup() { api = useCNOAuth(platform); return () => h('div') } }))
  return { api, wrapper }
}
describe('native OAuth login lifecycle', () => {
  beforeEach(() => { vi.useFakeTimers(); vi.mocked(cnOAuthRequest).mockReset(); vi.mocked(cnOAuthRequest).mockResolvedValue(pending()) })
  afterEach(() => { vi.useRealTimers() })
  it('uses server polling intervals including slow-down, then stops on ready and completes without secrets', async () => {
    const { api, wrapper } = setup()
    await api.start({ region: 'global', proxy_id: 3 })
    vi.mocked(cnOAuthRequest).mockResolvedValueOnce({ ...pending(), interval_seconds: 7 })
    await vi.advanceTimersByTimeAsync(2000)
    expect(cnOAuthRequest).toHaveBeenLastCalledWith('kimi', 'poll', { session_id: 'session' })
    const count = vi.mocked(cnOAuthRequest).mock.calls.length
    await vi.advanceTimersByTimeAsync(6000)
    expect(cnOAuthRequest).toHaveBeenCalledTimes(count)
    vi.mocked(cnOAuthRequest).mockResolvedValueOnce({ ...pending(), status: 'ready' })
    await vi.advanceTimersByTimeAsync(1000)
    expect(api.session.value?.status).toBe('ready')
    await vi.advanceTimersByTimeAsync(20000)
    expect(cnOAuthRequest).toHaveBeenCalledTimes(count + 1)
    vi.mocked(cnOAuthRequest).mockResolvedValueOnce({ ...pending(), status: 'completed', account_id: 42 })
    expect(await api.complete({ name: 'Kimi', group_ids: [2] })).toBe(42)
    expect(cnOAuthRequest).toHaveBeenLastCalledWith('kimi', 'complete', { session_id: 'session', name: 'Kimi', group_ids: [2] })
    wrapper.unmount()
    expect(cnOAuthRequest).not.toHaveBeenCalledWith('kimi', 'cancel', expect.anything())
  })
  it('cancels a late start response after unmount', async () => {
    let resolve!: (value: CNOAuthSession) => void
    vi.mocked(cnOAuthRequest).mockImplementationOnce(() => new Promise(r => { resolve = r }))
    const { api, wrapper } = setup()
    const starting = api.start({ region: 'cn' })
    wrapper.unmount()
    resolve(pending())
    await starting
    expect(api.session.value).toBeNull()
    expect(cnOAuthRequest).toHaveBeenLastCalledWith('kimi', 'cancel', { session_id: 'session' })
    await vi.advanceTimersByTimeAsync(10000)
    expect(cnOAuthRequest).toHaveBeenCalledTimes(2)
  })
  it('ignores a poll response after cancellation and stops at expiry', async () => {
    const { api, wrapper } = setup('minimax')
    await api.start({ region: 'cn' })
    let resolve!: (value: CNOAuthSession) => void
    vi.mocked(cnOAuthRequest).mockImplementationOnce(() => new Promise(r => { resolve = r }))
    const polling = api.advance()
    api.cancel()
    resolve({ ...pending(), status: 'ready' })
    await polling
    expect(api.session.value).toBeNull()
    vi.mocked(cnOAuthRequest).mockResolvedValueOnce({ ...pending(), expires_at: new Date(Date.now() + 1000).toISOString() })
    await api.start({ region: 'cn' })
    const count = vi.mocked(cnOAuthRequest).mock.calls.length
    await vi.advanceTimersByTimeAsync(5000)
    expect(api.expired.value).toBe(true)
    expect(cnOAuthRequest).toHaveBeenCalledTimes(count)
    wrapper.unmount()
  })
  it('exchanges DeepSeek callback explicitly without polling and surfaces denied authorization', async () => {
    const { api, wrapper } = setup('deepseek')
    await api.start({ region: 'cn', account_id: 42 })
    await vi.advanceTimersByTimeAsync(10000)
    expect(cnOAuthRequest).toHaveBeenCalledTimes(1)
    vi.mocked(cnOAuthRequest).mockResolvedValueOnce({ ...pending(), status: 'cancelled' })
    await api.advance('http://127.0.0.1:53682/oauth/callback?code=c&state=s')
    expect(api.failed.value).toBe(true)
    await flushPromises()
    wrapper.unmount()
  })
})
