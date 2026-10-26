import { chooseSelect } from '@/__tests__/helpers/select'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import CNOAuthPanel from '../CNOAuthPanel.vue'
import CnBaseUrlPresets from '../CnBaseUrlPresets.vue'
import { cnOAuthRequest } from '@/api/admin/cnOAuth'
import { cnSupportsNativeResponses, defaultCNBaseUrl } from '../credentialsBuilder'
import { identityNames, versionlessIdentityPresets } from '@/api/admin/outboundIdentity'
import { CONCRETE_PLATFORM_OPTIONS } from '@/constants/platforms'
import { platformAccentColor } from '@/utils/platformColors'
import { getPlatformTagClass, getPlatformTextClass } from '@/components/admin/channel/types'

vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/api/admin/cnOAuth', () => ({ cnOAuthRequest: vi.fn() }))

// Step-Code packages/providers/src/step-provider/login-client.ts uses cloud
// init/poll and /cli-login-remote?flow_id=…; the browser never imports a model key.
const pending = () => ({ session_id: 'session', authorize_url: 'https://platform.stepfun.ai/cli-login-remote?flow_id=public-flow', expires_at: new Date(Date.now() + 60000).toISOString(), interval_seconds: 5, status: 'pending' as const,
  // Unexpected opaque response fields must never be rendered or echoed to poll.
  poll_secret: 'private-poll-secret', api_key: 'private-model-key'
})

describe('StepFun official access requirements', () => {
  beforeEach(() => { vi.useFakeTimers(); vi.mocked(cnOAuthRequest).mockReset(); vi.mocked(cnOAuthRequest).mockResolvedValue(pending()) })
  afterEach(() => vi.useRealTimers())

  it('uses the same cyan platform identity in charts and channel pricing', () => {
    expect(platformAccentColor('stepfun')).toBe('#06b6d4')
    expect(getPlatformTagClass('stepfun')).toContain('bg-cyan-100')
    expect(getPlatformTextClass('stepfun')).toContain('text-cyan-700')
  })

  it('offers regional cloud browser login and polls only the opaque session handle', async () => {
    const wrapper = mount(CNOAuthPanel, { props: { platform: 'stepfun', accountInput: { name: 'Step Plan' } } })
    await chooseSelect(wrapper, 'admin.accounts.oauth.domestic.international')
    await wrapper.get('button.btn-secondary').trigger('click'); await flushPromises()
    expect(cnOAuthRequest).toHaveBeenCalledWith('stepfun', 'start', { region: 'global', proxy_id: undefined, account_id: undefined })
    expect(wrapper.get('a').attributes('href')).toBe('https://platform.stepfun.ai/cli-login-remote?flow_id=public-flow')
    expect(wrapper.find('[data-testid="cn-oauth-user-code"]').exists()).toBe(false)
    expect(wrapper.find('textarea').exists()).toBe(false)
    expect(wrapper.text()).toContain('admin.accounts.oauth.domestic.stepfunDescription')
    await vi.advanceTimersByTimeAsync(4999)
    expect(cnOAuthRequest).toHaveBeenCalledTimes(1)
    vi.mocked(cnOAuthRequest).mockResolvedValueOnce({ ...pending(), status: 'ready' })
    await vi.advanceTimersByTimeAsync(1); await flushPromises()
    expect(cnOAuthRequest).toHaveBeenLastCalledWith('stepfun', 'poll', { session_id: 'session' })
    expect(wrapper.html()).not.toContain('private-poll-secret')
    expect(wrapper.html()).not.toContain('private-model-key')
    vi.mocked(cnOAuthRequest).mockResolvedValueOnce({ ...pending(), status: 'completed', account_id: 42 })
    await wrapper.findAll('button').find(b => b.text().includes('domestic.create'))!.trigger('click'); await flushPromises()
    expect(wrapper.emitted('completed')).toEqual([[42]])
    wrapper.unmount()
  })

  it('stops cloud polling on cancellation and expiry without exposing opaque credentials', async () => {
    const wrapper = mount(CNOAuthPanel, { props: { platform: 'stepfun' } })
    await wrapper.get('button.btn-secondary').trigger('click'); await flushPromises()
    expect(wrapper.find('textarea').exists()).toBe(false)
    expect(wrapper.html()).not.toContain('private-poll-secret')
    await wrapper.findAll('button').find(b => b.text().includes('common.cancel'))!.trigger('click'); await flushPromises()
    expect(cnOAuthRequest).toHaveBeenLastCalledWith('stepfun', 'cancel', { session_id: 'session' })
    const callsAfterCancel = vi.mocked(cnOAuthRequest).mock.calls.length
    await vi.advanceTimersByTimeAsync(61000)
    expect(cnOAuthRequest).toHaveBeenCalledTimes(callsAfterCancel)
    wrapper.unmount()

    vi.mocked(cnOAuthRequest).mockResolvedValue(pending())
    const expiring = mount(CNOAuthPanel, { props: { platform: 'stepfun' } })
    await expiring.get('button.btn-secondary').trigger('click'); await flushPromises()
    await vi.advanceTimersByTimeAsync(61000)
    expect(expiring.text()).toContain('admin.accounts.oauth.domestic.expired')
    expect(expiring.find('a').exists()).toBe(false)
    expect(expiring.find('textarea').exists()).toBe(false)
    expect(expiring.html()).not.toContain('private-poll-secret')
    expect(expiring.html()).not.toContain('private-model-key')
    expect(vi.mocked(cnOAuthRequest).mock.calls.filter(([, action]) => action === 'poll')).toHaveLength(11)
    const callsAfterExpiry = vi.mocked(cnOAuthRequest).mock.calls.length
    await vi.advanceTimersByTimeAsync(10000)
    expect(cnOAuthRequest).toHaveBeenCalledTimes(callsAfterExpiry)
    expiring.unmount()
    expect(cnOAuthRequest).toHaveBeenLastCalledWith('stepfun', 'cancel', { session_id: 'session' })
  })

  it('withdraws model-discovery access when a ready authorization expires', async () => {
    vi.mocked(cnOAuthRequest).mockResolvedValueOnce({ ...pending(), status: 'ready' })
    const wrapper = mount(CNOAuthPanel, { props: { platform: 'stepfun' } })
    await wrapper.get('button.btn-secondary').trigger('click'); await flushPromises()
    expect(wrapper.emitted('ready-session')?.at(-1)).toEqual(['session'])
    await vi.advanceTimersByTimeAsync(61000)
    expect(wrapper.emitted('ready-session')?.at(-1)).toEqual([undefined])
    wrapper.unmount()
  })

  it('keeps reauthorization on the existing account region', async () => {
    const wrapper = mount(CNOAuthPanel, { props: { platform: 'stepfun', accountId: 42, initialRegion: 'global' } })
    expect((wrapper.get('.select-trigger').element as HTMLButtonElement).disabled).toBe(true)
    await wrapper.get('button.btn-secondary').trigger('click'); await flushPromises()
    expect(cnOAuthRequest).toHaveBeenCalledWith('stepfun', 'start', { region: 'global', proxy_id: undefined, account_id: 42 })
    wrapper.unmount()
  })

  it.each([
    ['payg', 'https://api.stepfun.com/v1', 'https://api.stepfun.ai/v1'],
    ['coding', 'https://api.stepfun.com/step_plan/v1', 'https://api.stepfun.ai/step_plan/v1']
  ] as const)('selects the %s endpoint without crossing product paths', async (mode, cn, global) => {
    expect(defaultCNBaseUrl('stepfun', mode)).toBe(cn)
    const wrapper = mount(CnBaseUrlPresets, { props: { platform: 'stepfun', mode, protocol: 'chat_completions', currentUrl: cn } })
    expect(wrapper.findAll('.select-trigger')).toHaveLength(1)
    await chooseSelect(wrapper, 'admin.accounts.oauth.domestic.international')
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
