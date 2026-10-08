import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import OutboundIdentityEditor from '../OutboundIdentityEditor.vue'
import { identityPolicyFixture } from './identityPolicyFixture'
import IdentityRuntimeField from '../IdentityRuntimeField.vue'
import { getOutboundIdentity, previewOutboundIdentity, type OutboundIdentityView } from '@/api/admin/outboundIdentity'

vi.mock('vue-i18n', async (original) => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/api/admin/outboundIdentity', async (original) => ({
  ...await original<typeof import('@/api/admin/outboundIdentity')>(),
  getOutboundIdentity: vi.fn(), previewOutboundIdentity: vi.fn()
}))

// The backend declares which headers a preset renders and which of them accept
// an account value. Only the Kimi Code device set is configurable today.
const declarationsView = (): OutboundIdentityView => ({
  account_policies: identityPolicyFixture(),
  settings: { profiles: {}, defaults: {} },
  presets: [],
  effective: [],
  declarations: [{ preset: 'kimi', headers: [
    { name: 'User-Agent', class: 'derived', editable: false, builtin: 'kimi-code-cli/2.1.1', value: 'kimi-code-cli/2.1.1' },
    { name: 'X-Msh-Platform', class: 'pinned', editable: false, builtin: 'kimi_code_cli', value: 'kimi_code_cli' },
    { name: 'X-Msh-Version', class: 'derived', editable: false, builtin: '2.1.1', value: '2.1.1' },
    { name: 'X-Msh-Device-Name', class: 'runtime', editable: true, builtin: 'kimi-gateway', value: 'kimi-gateway' },
    { name: 'X-Msh-Device-Model', class: 'pinned', editable: false, builtin: 'Linux 6.8.0-31-generic x64', value: 'Linux 6.8.0-31-generic x64' },
    { name: 'X-Msh-Os-Version', class: 'pinned', editable: false, builtin: '6.8.0-31-generic', value: '6.8.0-31-generic' },
    { name: 'X-Msh-Device-Id', class: 'runtime', editable: true, builtin: '11111111-1111-4111-8111-111111111111', value: '11111111-1111-4111-8111-111111111111' }
  ] }]
})

describe('OutboundIdentityEditor', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.mocked(previewOutboundIdentity).mockResolvedValue({ preset: 'claude', user_agent: 'claude-cli/2.9.1 (external, cli)', originator: 'claude-cli', version: '2.9.1', source: 'global', headers: { 'User-Agent': 'claude-cli/2.9.1 (external, cli)' } })
    vi.mocked(getOutboundIdentity).mockResolvedValue(declarationsView())
  })
  afterEach(() => { vi.useRealTimers(); vi.clearAllMocks() })

  it('limits native OAuth to its own client family and previews inheritance', async () => {
    const wrapper = mount(OutboundIdentityEditor, { props: { platform: 'anthropic', accountType: 'oauth', modelValue: null } })
    await flushPromises()
    expect(wrapper.get('[data-testid="fixed-identity-family"]').exists()).toBe(true)
    expect(wrapper.find('select[aria-label="admin.settings.outboundIdentity.title"]').exists()).toBe(false)
    await vi.advanceTimersByTimeAsync(250)
    await flushPromises()
    expect(previewOutboundIdentity).toHaveBeenCalledWith('anthropic', 'oauth', undefined)
    expect(wrapper.text()).toContain('claude-cli/2.9.1')
    expect(wrapper.text()).toContain('sources.global')
    wrapper.unmount()
  })

  it.each([
    ['openai', 'apikey'], ['gemini', 'apikey'], ['anthropic', 'apikey'], ['grok', 'apikey'], ['antigravity', 'upstream'], ['typesafe', 'apikey'], ['opencode_go', 'apikey']
  ])('lets compatible %s/%s accounts select an existing identity', async (platform, accountType) => {
    const wrapper = mount(OutboundIdentityEditor, { props: { platform, accountType, modelValue: null } })
    await flushPromises()
    expect(wrapper.get('select').findAll('option').map(option => option.attributes('value'))).toEqual(['', 'codex', 'claude', 'gemini', 'grok', 'antigravity', 'deepseek', 'minimax', 'minimax_apikey', 'kimi', 'zcode', 'stepfun'].map(value => `string:${value}`))
    await wrapper.get('select').setValue('string:grok')
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([{ preset: 'grok' }])
    await wrapper.setProps({ modelValue: { preset: 'grok', version: '3.9.1' } })
    await vi.advanceTimersByTimeAsync(250)
    expect(previewOutboundIdentity).toHaveBeenLastCalledWith(platform, accountType, { preset: 'grok', version: '3.9.1' })
    await wrapper.get('select').setValue('string:')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([null])
    wrapper.unmount()
  })

  it.each([
    ['deepseek', 'apikey'], ['deepseek', 'oauth'], ['kimi', 'apikey'], ['kimi', 'oauth'],
    ['minimax', 'apikey'], ['minimax', 'oauth'], ['zhipu', 'apikey'], ['zhipu', 'oauth'],
    ['stepfun', 'apikey'], ['stepfun', 'oauth'], ['anthropic', 'bedrock'],
    ['anthropic', 'service_account'], ['gemini', 'service_account']
  ])('pins the client family for %s/%s without a family dropdown', async (platform, accountType) => {
    const wrapper = mount(OutboundIdentityEditor, { props: { platform, accountType, modelValue: null } })
    await flushPromises()
    expect(wrapper.get('[data-testid="fixed-identity-family"]').exists()).toBe(true)
    expect(wrapper.find('select[aria-label="admin.settings.outboundIdentity.title"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('allows a native account version override and can restore inheritance', async () => {
    const wrapper = mount(OutboundIdentityEditor, { props: { platform: 'deepseek', accountType: 'apikey', modelValue: null } })
    await flushPromises()
    await wrapper.get('input[aria-label="admin.settings.outboundIdentity.version"]').setValue('0.3.0')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([{ preset: 'deepseek', version: '0.3.0' }])
    await wrapper.setProps({ modelValue: { preset: 'deepseek', version: '0.3.0' } })
    await wrapper.get('button').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([null])
    wrapper.unmount()
  })

  it('does not expose an unrestricted selector if policy loading fails', async () => {
    vi.mocked(getOutboundIdentity).mockRejectedValueOnce(new Error('unavailable'))
    const wrapper = mount(OutboundIdentityEditor, { props: { platform: 'kimi', accountType: 'apikey', modelValue: null } })
    await flushPromises()
    expect(wrapper.find('select').exists()).toBe(false)
    expect(wrapper.get('[role="alert"]').text()).toContain('loadFailed')
    wrapper.unmount()
  })

  it('keeps a loaded account override while the policy request is pending', async () => {
    let resolvePolicy!: (view: OutboundIdentityView) => void
    vi.mocked(getOutboundIdentity).mockReturnValueOnce(new Promise(resolve => { resolvePolicy = resolve }))
    const wrapper = mount(OutboundIdentityEditor, { props: { platform: 'deepseek', accountType: 'apikey', modelValue: null } })
    await wrapper.setProps({ platform: 'kimi', modelValue: { preset: 'kimi', version: '2.2.0' } })
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    resolvePolicy(declarationsView())
    await flushPromises()
    expect(wrapper.get('input[aria-label="admin.settings.outboundIdentity.version"]').element.value).toBe('2.2.0')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    wrapper.unmount()
  })

  it('keeps the MiniMax API Key SDK version read-only and OAuth native', async () => {
    const wrapper = mount(OutboundIdentityEditor, { props: { platform: 'minimax', accountType: 'apikey', modelValue: { preset: 'minimax_apikey' } } })
    await flushPromises()
    expect(wrapper.find('input').exists()).toBe(false)
    expect(wrapper.text()).toContain('pinnedSdkHint')
    await wrapper.setProps({ accountType: 'oauth', modelValue: { preset: 'minimax' } })
    expect(wrapper.find('select[aria-label="admin.settings.outboundIdentity.title"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('versionlessHint')
    wrapper.unmount()
  })

  it('uses the resolved type mapping when showing inherited timezone controls', async () => {
    vi.mocked(previewOutboundIdentity).mockResolvedValue({ preset: 'minimax_apikey', user_agent: 'Anthropic/JS 0.91.1', originator: 'Anthropic', version: '0.91.1', source: 'global', headers: {}, timezone: 'Asia/Shanghai' })
    const wrapper = mount(OutboundIdentityEditor, { props: { platform: 'gemini', accountType: 'apikey', modelValue: null } })
    await vi.advanceTimersByTimeAsync(250)
    await flushPromises()
    expect(wrapper.getComponent(IdentityRuntimeField).props('name')).toBe('timezone')
    expect(wrapper.getComponent(IdentityRuntimeField).props('fallback')).toBe('Asia/Shanghai')
    wrapper.getComponent(IdentityRuntimeField).vm.$emit('update:modelValue', 'UTC')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([{ preset: 'minimax_apikey', timezone: 'UTC' }])
    wrapper.unmount()
  })

  it('sets and clears an account timezone while retaining its preset', async () => {
    const wrapper = mount(OutboundIdentityEditor, { props: { platform: 'minimax', accountType: 'oauth', modelValue: null } })
    await flushPromises()
    wrapper.getComponent(IdentityRuntimeField).vm.$emit('update:modelValue', 'Asia/Shanghai')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([{ preset: 'minimax', timezone: 'Asia/Shanghai' }])
    await wrapper.setProps({ modelValue: { preset: 'minimax', timezone: 'Asia/Shanghai' } })
    wrapper.getComponent(IdentityRuntimeField).vm.$emit('update:modelValue', '')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([{ preset: 'minimax' }])
    wrapper.unmount()
  })

  it('keeps the fixed environment read-only and device information editable', async () => {
    const wrapper = mount(OutboundIdentityEditor, { props: { platform: 'kimi', accountType: 'oauth', modelValue: { preset: 'kimi' } } })
    await flushPromises()
    const environment = wrapper.get('[data-testid="identity-environment-summary"]')
    expect(environment.text()).toContain('Ubuntu 24.04')
    expect(environment.find('input').exists()).toBe(false)
    expect(wrapper.find('input[aria-label="X-Msh-Device-Id"]').exists()).toBe(true)
    expect(wrapper.find('input[aria-label="X-Msh-Device-Model"]').exists()).toBe(false)
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
    const wrapper = mount(OutboundIdentityEditor, { props: { platform: 'minimax', accountType: 'oauth', modelValue: { preset: 'minimax' } } })
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

  it('renders the ZCode product declaration for a Zhipu account', async () => {
    vi.mocked(previewOutboundIdentity).mockResolvedValue({ preset: 'zcode', user_agent: 'ZCode/3.14.3', originator: 'ZCode', version: '3.14.3', source: 'account', headers: { 'User-Agent': 'ZCode/3.14.3' } })
    const wrapper = mount(OutboundIdentityEditor, { props: { platform: 'zhipu', accountType: 'apikey', modelValue: { preset: 'zcode' } } })
    await vi.advanceTimersByTimeAsync(250)
    await flushPromises()
    const headers = wrapper.get('[data-testid="outbound-identity-account-headers"]')
    expect(headers.text()).toContain('User-Agent')
    expect(headers.text()).toContain('ZCode/3.14.3')
    // ZCode declares no Originator and no standalone version header, so neither
    // reaches the rendered header list.
    expect(headers.text()).not.toContain('Originator')
    expect(wrapper.text()).not.toContain('versionlessHint')
    wrapper.unmount()
  })

  it('selects and clears DeepSeek account language while preserving timezone', async () => {
    const wrapper = mount(OutboundIdentityEditor, { props: { platform: 'deepseek', accountType: 'oauth', modelValue: { preset: 'deepseek', timezone: 'Asia/Shanghai' } } })
    await vi.advanceTimersByTimeAsync(250)
    await flushPromises()
    const language = wrapper.get('select[aria-label="language"]')
    expect(language.findAll('option').map(option => option.attributes('value'))).toEqual(['string:', 'string:zh-CN', 'string:en-US'])
    await language.setValue('string:en-US')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([{ preset: 'deepseek', timezone: 'Asia/Shanghai', language: 'en-US' }])
    await wrapper.setProps({ modelValue: { preset: 'deepseek', timezone: 'Asia/Shanghai', language: 'en-US' } })
    await language.setValue('string:')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([{ preset: 'deepseek', timezone: 'Asia/Shanghai' }])
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

  it('exposes only the runtime declarations and renders the full Kimi Code block', async () => {
    vi.mocked(previewOutboundIdentity).mockResolvedValue({
      preset: 'kimi',
      user_agent: 'kimi-code-cli/2.1.1',
      originator: 'kimi-code-cli',
      version: '2.1.1',
      source: 'account',
      headers: { 'User-Agent': 'kimi-code-cli/2.1.1', 'X-Msh-Platform': 'kimi_code_cli', 'X-Msh-Version': '2.1.1', 'X-Msh-Device-Name': 'kimi-gateway', 'X-Msh-Device-Model': 'Linux 6.8.0-31-generic x64', 'X-Msh-Os-Version': '6.8.0-31-generic', 'X-Msh-Device-Id': '11111111-1111-4111-8111-111111111111' }
    })
    const wrapper = mount(OutboundIdentityEditor, { props: { platform: 'kimi', accountType: 'apikey', modelValue: { preset: 'kimi' } } })
    await vi.advanceTimersByTimeAsync(250)
    await flushPromises()

    // The saved identity preview reports every declaration that reaches the wire.
    const preview = wrapper.get('[data-testid="outbound-identity-account-headers"]')
    for (const name of ['User-Agent', 'X-Msh-Platform', 'X-Msh-Version', 'X-Msh-Device-Name', 'X-Msh-Device-Id']) {
      expect(preview.text()).toContain(name)
    }

    // Only the runtime device declarations are editable.
    const runtime = wrapper.get('[data-testid="outbound-identity-account-runtime-headers"]')
    const inputs = runtime.findAll('input')
    expect(inputs.map(input => input.attributes('aria-label'))).toEqual(['X-Msh-Device-Name', 'X-Msh-Device-Id'])
    expect(inputs[0].attributes('placeholder')).toBe('kimi-gateway')

    await inputs[0].setValue('kimi-gateway-2')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([{ preset: 'kimi', headers: { 'X-Msh-Device-Name': 'kimi-gateway-2' } }])

    // Clearing a value removes the override instead of pinning an empty header.
    await inputs[0].setValue('')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([{ preset: 'kimi', headers: undefined }])
    wrapper.unmount()
  })

  it('does not offer runtime declarations when the account opts out of the native family', async () => {
    const wrapper = mount(OutboundIdentityEditor, { props: { platform: 'gemini', accountType: 'apikey', modelValue: { preset: 'codex' } } })
    await vi.advanceTimersByTimeAsync(250)
    await flushPromises()
    expect(wrapper.find('[data-testid="outbound-identity-account-runtime-headers"]').exists()).toBe(false)
    wrapper.unmount()
  })
})
