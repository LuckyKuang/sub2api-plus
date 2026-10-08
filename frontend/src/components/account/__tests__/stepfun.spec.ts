import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import CNOAuthPanel from '../CNOAuthPanel.vue'
import CnBaseUrlPresets from '../CnBaseUrlPresets.vue'
import { cnOAuthRequest } from '@/api/admin/cnOAuth'
import { cnSupportsNativeResponses, defaultCNBaseUrl } from '../credentialsBuilder'
import { identityNames, versionlessIdentityPresets } from '@/api/admin/outboundIdentity'
import { CONCRETE_PLATFORM_OPTIONS } from '@/constants/platforms'

vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/api/admin/cnOAuth', () => ({ cnOAuthRequest: vi.fn() }))

const pending = () => ({ session_id: 'session', authorize_url: 'https://platform.stepfun.ai/cli-login?port=53683&state=state', expires_at: new Date(Date.now() + 60000).toISOString(), interval_seconds: 5, status: 'pending' as const })

describe('StepFun official access requirements', () => {
  beforeEach(() => { vi.useFakeTimers(); vi.mocked(cnOAuthRequest).mockReset(); vi.mocked(cnOAuthRequest).mockResolvedValue(pending()) })
  afterEach(() => vi.useRealTimers())

  it('offers browser login in either region, requires explicit callback import and never device-polls', async () => {
    const wrapper = mount(CNOAuthPanel, { props: { platform: 'stepfun', accountInput: { name: 'Step Plan' } } })
    await wrapper.get('select').setValue(wrapper.findAll('option').find(option => option.text().includes('domestic.international'))!.element.value)
    await wrapper.get('button').trigger('click'); await flushPromises()
    expect(cnOAuthRequest).toHaveBeenCalledWith('stepfun', 'start', { region: 'global', proxy_id: undefined, account_id: undefined })
    expect(wrapper.get('a').attributes('href')).toBe('https://platform.stepfun.ai/cli-login?port=53683&state=state')
    expect(wrapper.find('[data-testid="cn-oauth-user-code"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('admin.accounts.oauth.domestic.stepfunDescription')
    await vi.advanceTimersByTimeAsync(20000)
    expect(cnOAuthRequest).toHaveBeenCalledTimes(1)
    const callback = 'http://127.0.0.1:53683/callback?state=state&api_key=private-key'
    await wrapper.get('textarea').setValue(callback)
    vi.mocked(cnOAuthRequest).mockResolvedValueOnce({ ...pending(), status: 'ready' })
    await wrapper.findAll('button').find(b => b.text().includes('domestic.exchange'))!.trigger('click'); await flushPromises()
    expect(cnOAuthRequest).toHaveBeenLastCalledWith('stepfun', 'exchange', { session_id: 'session', callback })
    expect(wrapper.html()).not.toContain('private-key')
    vi.mocked(cnOAuthRequest).mockResolvedValueOnce({ ...pending(), status: 'completed', account_id: 42 })
    await wrapper.findAll('button').find(b => b.text().includes('domestic.create'))!.trigger('click'); await flushPromises()
    expect(wrapper.emitted('completed')).toEqual([[42]])
    wrapper.unmount()
  })

  it('clears entered callback credentials on cancellation and expiry', async () => {
    const wrapper = mount(CNOAuthPanel, { props: { platform: 'stepfun' } })
    await wrapper.get('button').trigger('click'); await flushPromises()
    await wrapper.get('textarea').setValue('private-key')
    await vi.advanceTimersByTimeAsync(61000)
    expect(wrapper.find('textarea').exists()).toBe(false)
    expect(wrapper.html()).not.toContain('private-key')
    expect(cnOAuthRequest).toHaveBeenCalledTimes(1)
    wrapper.unmount()
    expect(cnOAuthRequest).toHaveBeenLastCalledWith('stepfun', 'cancel', { session_id: 'session' })
  })

  it('keeps reauthorization on the existing account region', async () => {
    const wrapper = mount(CNOAuthPanel, { props: { platform: 'stepfun', accountId: 42, initialRegion: 'global' } })
    expect(wrapper.get('select').element.disabled).toBe(true)
    await wrapper.get('button').trigger('click'); await flushPromises()
    expect(cnOAuthRequest).toHaveBeenCalledWith('stepfun', 'start', { region: 'global', proxy_id: undefined, account_id: 42 })
    wrapper.unmount()
  })

  it.each([
    ['payg', 'https://api.stepfun.com/v1', 'https://api.stepfun.ai/v1'],
    ['coding', 'https://api.stepfun.com/step_plan/v1', 'https://api.stepfun.ai/step_plan/v1']
  ] as const)('selects the %s endpoint without crossing product paths', async (mode, cn, global) => {
    expect(defaultCNBaseUrl('stepfun', mode)).toBe(cn)
    const wrapper = mount(CnBaseUrlPresets, { props: { platform: 'stepfun', mode, protocol: 'chat_completions', currentUrl: cn } })
    expect(wrapper.findAll('select')).toHaveLength(1)
    await wrapper.get('select').setValue(wrapper.findAll('option').find(option => option.text().includes('domestic.international'))!.element.value)
    expect(wrapper.emitted('select')).toEqual([[{ mode, protocol: 'chat_completions', label: mode === 'coding' ? 'Step Plan Intl' : 'StepFun Intl', url: global }]])
    wrapper.unmount()
  })

  it('registers one official versionless identity for both account types', () => {
    expect(CONCRETE_PLATFORM_OPTIONS).toContainEqual({ value: 'stepfun', label: 'StepFun' })
    expect(identityNames.stepfun).toBe('StepFun · Step-Code')
    expect(versionlessIdentityPresets).toContain('stepfun')
    expect(cnSupportsNativeResponses('stepfun')).toBe(false)
  })
})
