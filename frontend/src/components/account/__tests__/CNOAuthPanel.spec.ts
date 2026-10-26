import { chooseSelect } from '@/__tests__/helpers/select'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import CNOAuthPanel from '../CNOAuthPanel.vue'
import { cnOAuthRequest, type CNOAuthSession } from '@/api/admin/cnOAuth'
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/api/admin/cnOAuth', () => ({ cnOAuthRequest: vi.fn() }))
const session = () => ({ session_id: 's', user_code: 'CODE', authorize_url: 'https://platform.deepseek.com/dsh/authorize', expires_at: new Date(Date.now() + 60000).toISOString(), interval_seconds: 2, status: 'pending' as const })
describe('CNOAuthPanel', () => {
  beforeEach(() => { vi.useFakeTimers(); vi.mocked(cnOAuthRequest).mockReset(); vi.mocked(cnOAuthRequest).mockResolvedValue(session()) })
  afterEach(() => vi.useRealTimers())
  it('Step remote login opens authorization without exposing a callback credential field', async () => {
    const wrapper = mount(CNOAuthPanel, { props: { platform: 'stepfun' } })
    await wrapper.get('button.btn-secondary').trigger('click'); await flushPromises()
    expect(cnOAuthRequest).toHaveBeenCalledWith('stepfun', 'start', { region: 'cn', proxy_id: undefined, account_id: undefined })
    expect(wrapper.find('a').exists()).toBe(true)
    expect(wrapper.find('textarea').exists()).toBe(false)
    expect(wrapper.text()).toContain('admin.accounts.oauth.domestic.waiting')
    wrapper.unmount()
  })
  it.each(['kimi', 'minimax'] as const)('%s offers CN/global selection, shows user code and binds proxy and relink account', async (platform) => {
    const wrapper = mount(CNOAuthPanel, { props: { platform, accountId: 42, proxyId: 3 } })
    await chooseSelect(wrapper, 'admin.accounts.oauth.domestic.international')
    await wrapper.get('button.btn-secondary').trigger('click'); await flushPromises()
    expect(cnOAuthRequest).toHaveBeenCalledWith(platform, 'start', { region: 'global', account_id: 42, proxy_id: 3 })
    expect(wrapper.get('a').attributes('rel')).toBe('noopener noreferrer')
    expect(wrapper.get('[data-testid="cn-oauth-user-code"]').text()).toBe('CODE')
    wrapper.unmount()
  })
  it('explains DeepSeek loopback and saves server-held authorization using account metadata', async () => {
    const wrapper = mount(CNOAuthPanel, { props: { platform: 'deepseek', accountInput: { name: 'DS', group_ids: [1], concurrency: 2 } } })
    expect(wrapper.find('select').exists()).toBe(false)
    await wrapper.get('button.btn-secondary').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('admin.accounts.oauth.domestic.callbackHint')
    await wrapper.get('textarea').setValue('http://127.0.0.1:53682/oauth/callback?code=c&state=s')
    vi.mocked(cnOAuthRequest).mockResolvedValueOnce({ ...session(), status: 'ready' })
    await wrapper.findAll('button').find(b => b.text().includes('domestic.exchange'))!.trigger('click'); await flushPromises()
    vi.mocked(cnOAuthRequest).mockResolvedValueOnce({ ...session(), status: 'completed', account_id: 42 })
    await wrapper.findAll('button').find(b => b.text().includes('domestic.create'))!.trigger('click'); await flushPromises()
    expect(cnOAuthRequest).toHaveBeenLastCalledWith('deepseek', 'complete', { session_id: 's', name: 'DS', group_ids: [1], concurrency: 2 })
    expect(wrapper.emitted('completed')).toEqual([[42]])
    wrapper.unmount()
  })

  it.each(['deepseek', 'kimi', 'minimax', 'stepfun'] as const)('%s discards a late start after selecting a new proxy and keeps the new flow', async platform => {
    let finishOld!: (value: CNOAuthSession) => void
    const oldStart = new Promise<CNOAuthSession>(resolve => { finishOld = resolve })
    const old = { ...session(), session_id: 'old-route', user_code: 'OLD-CODE' }
    const current = { ...session(), session_id: 'new-route', user_code: 'NEW-CODE' }
    vi.mocked(cnOAuthRequest).mockImplementation(async (_, action, input) => {
      if (action === 'start') return input.proxy_id === 3 ? oldStart : current
      return { ...old, status: 'cancelled' as const }
    })
    const wrapper = mount(CNOAuthPanel, { props: { platform, proxyId: 3 } })
    await wrapper.get('button.btn-secondary').trigger('click')
    expect(cnOAuthRequest).toHaveBeenCalledWith(platform, 'start', { region: 'cn', proxy_id: 3, account_id: undefined })
    await wrapper.setProps({ proxyId: 4 })
    await wrapper.get('button.btn-secondary').trigger('click'); await flushPromises()
    expect(cnOAuthRequest).toHaveBeenCalledWith(platform, 'start', { region: 'cn', proxy_id: 4, account_id: undefined })
    expect(wrapper.get('[data-testid="cn-oauth-user-code"]').text()).toBe('NEW-CODE')
    finishOld(old); await flushPromises()
    expect(cnOAuthRequest).toHaveBeenCalledWith(platform, 'cancel', { session_id: 'old-route' })
    expect(wrapper.get('[data-testid="cn-oauth-user-code"]').text()).toBe('NEW-CODE')
    expect(wrapper.text()).not.toContain('OLD-CODE')
    expect(wrapper.emitted('completed')).toBeUndefined()
    wrapper.unmount()
  })

  it.each(['deepseek', 'kimi', 'minimax', 'stepfun'] as const)('%s ignores the old route poll or exchange after a proxy edit', async platform => {
    let finishOld!: (value: CNOAuthSession) => void
    const oldPoll = new Promise<CNOAuthSession>(resolve => { finishOld = resolve })
    const old = { ...session(), session_id: 'old-route', user_code: 'OLD-CODE' }
    const current = { ...session(), session_id: 'new-route', user_code: 'NEW-CODE' }
    vi.mocked(cnOAuthRequest).mockImplementation(async (_, action, input) => {
      if (action === 'start') return input.proxy_id === 3 ? old : current
      if (action === 'poll' || action === 'exchange') return oldPoll
      return { ...old, status: 'cancelled' as const }
    })
    const wrapper = mount(CNOAuthPanel, { props: { platform, proxyId: 3 } })
    await wrapper.get('button.btn-secondary').trigger('click'); await flushPromises()
    expect(cnOAuthRequest).toHaveBeenCalledWith(platform, 'start', { region: 'cn', proxy_id: 3, account_id: undefined })
    if (platform === 'deepseek') {
      await wrapper.get('textarea').setValue('http://127.0.0.1:53682/oauth/callback?code=old&state=old')
      await wrapper.findAll('button').find(button => button.text().includes('domestic.exchange'))!.trigger('click')
    } else {
      await vi.advanceTimersByTimeAsync(2000)
    }
    await wrapper.setProps({ proxyId: 4 })
    expect(cnOAuthRequest).toHaveBeenCalledWith(platform, 'cancel', { session_id: 'old-route' })
    await wrapper.get('button.btn-secondary').trigger('click'); await flushPromises()
    finishOld({ ...old, status: 'ready' }); await flushPromises()
    expect(wrapper.get('[data-testid="cn-oauth-user-code"]').text()).toBe('NEW-CODE')
    expect(wrapper.findAll('button').some(button => button.text().includes('domestic.create'))).toBe(false)
    expect(wrapper.emitted('ready-session')?.some(([id]) => id === 'old-route')).toBe(false)
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it.each(['deepseek', 'kimi', 'minimax', 'stepfun'] as const)('%s ignores an old completion after a proxy edit without completing the replacement flow', async platform => {
    let finishOld!: (value: CNOAuthSession) => void
    const oldCompletion = new Promise<CNOAuthSession>(resolve => { finishOld = resolve })
    const old = { ...session(), session_id: 'old-route', status: 'ready' as const }
    const current = { ...session(), session_id: 'new-route', user_code: 'NEW-CODE' }
    vi.mocked(cnOAuthRequest).mockImplementation(async (_, action, input) => {
      if (action === 'start') return input.proxy_id === 3 ? old : current
      if (action === 'complete') return oldCompletion
      return { ...old, status: 'cancelled' as const }
    })
    const wrapper = mount(CNOAuthPanel, { props: { platform, proxyId: 3, accountInput: { name: 'selected account' } } })
    await wrapper.get('button.btn-secondary').trigger('click'); await flushPromises()
    expect(cnOAuthRequest).toHaveBeenCalledWith(platform, 'start', { region: 'cn', proxy_id: 3, account_id: undefined })
    await wrapper.findAll('button').find(button => button.text().includes('domestic.create'))!.trigger('click')
    expect(cnOAuthRequest).toHaveBeenCalledWith(platform, 'complete', { session_id: 'old-route', name: 'selected account' })
    await wrapper.setProps({ proxyId: 4 })
    await wrapper.get('button.btn-secondary').trigger('click'); await flushPromises()
    finishOld({ ...old, status: 'completed', account_id: 91 }); await flushPromises()
    expect(wrapper.emitted('completed')).toBeUndefined()
    expect(wrapper.get('[data-testid="cn-oauth-user-code"]').text()).toBe('NEW-CODE')
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.emitted('ready-session')?.at(-1)).toEqual([undefined])
    wrapper.unmount()
  })
})
