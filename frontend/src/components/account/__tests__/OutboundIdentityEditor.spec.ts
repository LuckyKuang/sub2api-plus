import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import OutboundIdentityEditor from '../OutboundIdentityEditor.vue'
import { previewOutboundIdentity } from '@/api/admin/outboundIdentity'

vi.mock('vue-i18n', async (original) => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/api/admin/outboundIdentity', async (original) => ({
  ...await original<typeof import('@/api/admin/outboundIdentity')>(),
  previewOutboundIdentity: vi.fn()
}))

describe('OutboundIdentityEditor', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.mocked(previewOutboundIdentity).mockResolvedValue({ preset: 'claude', user_agent: 'claude-cli/2.9.1 (external, cli)', originator: 'claude-cli', version: '2.9.1', source: 'global', headers: { 'User-Agent': 'claude-cli/2.9.1 (external, cli)' } })
  })
  afterEach(() => { vi.useRealTimers(); vi.clearAllMocks() })

  it('limits native OAuth to its own client family and previews inheritance', async () => {
    const wrapper = mount(OutboundIdentityEditor, { props: { platform: 'anthropic', accountType: 'oauth', modelValue: null } })
    expect(wrapper.findAll('option').map(option => option.attributes('value'))).toEqual(['', 'claude'])
    await vi.advanceTimersByTimeAsync(250)
    await flushPromises()
    expect(previewOutboundIdentity).toHaveBeenCalledWith('anthropic', 'oauth', undefined)
    expect(wrapper.text()).toContain('claude-cli/2.9.1')
    expect(wrapper.text()).toContain('sources.global')
    wrapper.unmount()
  })

  it.each([
    ['openai', 'apikey'], ['gemini', 'service_account'], ['anthropic', 'bedrock'], ['antigravity', 'upstream'], ['deepseek', 'apikey'], ['minimax', 'apikey']
  ])('lets compatible %s/%s accounts select an existing identity', async (platform, accountType) => {
    const wrapper = mount(OutboundIdentityEditor, { props: { platform, accountType, modelValue: null } })
    expect(wrapper.findAll('option')).toHaveLength(8)
    await wrapper.get('select').setValue('grok')
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([{ preset: 'grok' }])
    await wrapper.setProps({ modelValue: { preset: 'grok', version: '3.9.1' } })
    await vi.advanceTimersByTimeAsync(250)
    expect(previewOutboundIdentity).toHaveBeenLastCalledWith(platform, accountType, { preset: 'grok', version: '3.9.1' })
    await wrapper.get('select').setValue('')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([null])
    wrapper.unmount()
  })

  it('retains the established Codex editor for native OpenAI OAuth', async () => {
    const wrapper = mount(OutboundIdentityEditor, { props: { platform: 'openai', accountType: 'oauth', modelValue: null } })
    expect(wrapper.find('section').exists()).toBe(false)
    await vi.advanceTimersByTimeAsync(250)
    expect(previewOutboundIdentity).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('includes the existing Codex UA when previewing an OpenAI API key account', async () => {
    const ua = 'codex-tui/0.1.0 (Linux; x86_64)'
    const wrapper = mount(OutboundIdentityEditor, { props: { platform: 'openai', accountType: 'apikey', modelValue: null, codexUserAgent: ua } })
    await vi.advanceTimersByTimeAsync(250)
    expect(previewOutboundIdentity).toHaveBeenCalledWith('openai', 'apikey', undefined, ua)
    wrapper.unmount()
  })

  it('shows a preview failure without displaying stale identity data', async () => {
    vi.mocked(previewOutboundIdentity).mockRejectedValueOnce(new Error('unavailable'))
    const wrapper = mount(OutboundIdentityEditor, { props: { platform: 'gemini', accountType: 'apikey', modelValue: null } })
    await vi.advanceTimersByTimeAsync(250)
    expect(wrapper.get('[role="alert"]').text()).toContain('previewFailed')
    expect(wrapper.text()).not.toContain('claude-cli/2.9.1')
    wrapper.unmount()
  })

  it('renders the effective request headers and hides the version control for the versionless MiniMax family', async () => {
    vi.mocked(previewOutboundIdentity).mockResolvedValue({ preset: 'minimax', user_agent: 'MiniMaxAgent', originator: 'MiniMaxAgent', version: '', source: 'account', headers: { 'User-Agent': 'MiniMaxAgent' } })
    const wrapper = mount(OutboundIdentityEditor, { props: { platform: 'minimax', accountType: 'apikey', modelValue: { preset: 'minimax' } } })
    await vi.advanceTimersByTimeAsync(250)
    await flushPromises()
    const headers = wrapper.get('[data-testid="outbound-identity-account-headers"]')
    expect(headers.text()).toContain('User-Agent')
    expect(headers.text()).toContain('MiniMaxAgent')
    expect(wrapper.text()).toContain('versionlessHint')
    // The versionless family rejects a client version, so no version or UA input is offered.
    expect(wrapper.find('input').exists()).toBe(false)
    wrapper.unmount()
  })

  it('keeps the version control for a family that declares one', async () => {
    const wrapper = mount(OutboundIdentityEditor, { props: { platform: 'deepseek', accountType: 'apikey', modelValue: { preset: 'deepseek' } } })
    await vi.advanceTimersByTimeAsync(250)
    await flushPromises()
    expect(wrapper.find('input').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('versionlessHint')
    wrapper.unmount()
  })
})
