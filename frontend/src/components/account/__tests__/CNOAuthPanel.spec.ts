import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import CNOAuthPanel from '../CNOAuthPanel.vue'
import { cnOAuthRequest } from '@/api/admin/cnOAuth'
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/api/admin/cnOAuth', () => ({ cnOAuthRequest: vi.fn() }))
const session = () => ({ session_id: 's', user_code: 'CODE', authorize_url: 'https://platform.deepseek.com/dsh/authorize', expires_at: new Date(Date.now() + 60000).toISOString(), interval_seconds: 2, status: 'pending' as const })
describe('CNOAuthPanel', () => {
  beforeEach(() => { vi.useFakeTimers(); vi.mocked(cnOAuthRequest).mockReset(); vi.mocked(cnOAuthRequest).mockResolvedValue(session()) })
  afterEach(() => vi.useRealTimers())
  it.each(['kimi', 'minimax'] as const)('%s offers CN/global selection, shows user code and binds proxy and relink account', async (platform) => {
    const wrapper = mount(CNOAuthPanel, { props: { platform, accountId: 42, proxyId: 3 } })
    const globalOption = wrapper.findAll('option').find(option => option.text().includes('domestic.international'))!
    await wrapper.get('select').setValue(globalOption.element.value)
    await wrapper.get('button').trigger('click'); await flushPromises()
    expect(cnOAuthRequest).toHaveBeenCalledWith(platform, 'start', { region: 'global', account_id: 42, proxy_id: 3 })
    expect(wrapper.get('a').attributes('rel')).toBe('noopener noreferrer')
    expect(wrapper.get('[data-testid="cn-oauth-user-code"]').text()).toBe('CODE')
    wrapper.unmount()
  })
  it('explains DeepSeek loopback and saves server-held authorization using account metadata', async () => {
    const wrapper = mount(CNOAuthPanel, { props: { platform: 'deepseek', accountInput: { name: 'DS', group_ids: [1], concurrency: 2 } } })
    expect(wrapper.find('select').exists()).toBe(false)
    await wrapper.get('button').trigger('click'); await flushPromises()
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
})
