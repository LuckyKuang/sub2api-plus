import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import OutboundIdentitySettings from './OutboundIdentitySettings.vue'
import { identityPolicyFixture } from '@/components/account/__tests__/identityPolicyFixture'
import IdentityRuntimeField from '@/components/account/IdentityRuntimeField.vue'
import { getOutboundIdentity, updateOutboundIdentity, identityPresets, versionlessIdentityPresets, type IdentityDeclaration, type OutboundIdentityView, type PresetDeclarations } from '@/api/admin/outboundIdentity'

vi.mock('vue-i18n', async (original) => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/api/admin/outboundIdentity', async (original) => ({
  ...await original<typeof import('@/api/admin/outboundIdentity')>(),
  getOutboundIdentity: vi.fn(), updateOutboundIdentity: vi.fn()
}))
const userAgentOf = (preset: string) => preset === 'stepfun' ? 'step (linux 6.8.0-31-generic; x64)' : preset === 'minimax' ? 'MiniMaxAgent' : preset === 'minimax_apikey' ? 'Anthropic/JS 0.91.1' : preset === 'kimi' ? 'kimi-code-cli/2.1.1' : preset === 'zcode' ? 'ZCode/3.14.3' : `${preset}/1.2.3`
const versionOf = (preset: string) => preset === 'minimax' || preset === 'stepfun' ? '' : preset === 'minimax_apikey' ? '0.91.1' : preset === 'kimi' ? '2.1.1' : preset === 'zcode' ? '3.14.3' : '1.2.3'
// The Kimi Code device set is the runtime tier: the official client resolves it
// from its own host, so the settings page exposes an editable value per header
// and the backend declares which headers those are.
const kimiDeviceHeaders: IdentityDeclaration[] = [
  { name: 'X-Msh-Device-Name', class: 'runtime', editable: true, builtin: 'kimi-gateway', value: 'kimi-gateway' },
  { name: 'X-Msh-Device-Model', class: 'pinned', editable: false, builtin: 'Linux 6.8.0-31-generic x64', value: 'Linux 6.8.0-31-generic x64' },
  { name: 'X-Msh-Os-Version', class: 'pinned', editable: false, builtin: '6.8.0-31-generic', value: '6.8.0-31-generic' },
  { name: 'X-Msh-Device-Id', class: 'runtime', editable: true, builtin: '11111111-1111-4111-8111-111111111111', value: '11111111-1111-4111-8111-111111111111' }
]
const kimiHeaders = {
  'User-Agent': 'kimi-code-cli/2.1.1',
  'X-Msh-Platform': 'kimi_code_cli',
  'X-Msh-Version': '2.1.1',
  'X-Msh-Device-Name': 'kimi-gateway',
  'X-Msh-Device-Model': 'Linux 6.8.0-31-generic x64',
  'X-Msh-Os-Version': '6.8.0-31-generic',
  'X-Msh-Device-Id': '11111111-1111-4111-8111-111111111111'
}
const zcodeHeaders = {
  'User-Agent': 'ZCode/3.14.3',
  'X-ZCode-App-Version': '3.14.3',
  'HTTP-Referer': 'https://zcode.z.ai',
  'X-Title': 'Z Code@electron',
  'X-Release-Channel': 'production',
  'X-ZCode-Agent': 'glm',
  'X-Client-Language': 'en-US',
  'X-Client-Timezone': 'UTC',
  'X-Platform': 'linux-x64',
  'X-Os-Category': 'linux',
  'X-Os-Version': '6.8.0-31-generic'
}
const zcodeRuntimeNames = ['X-Client-Language', 'X-Client-Timezone']
const fixture = (): OutboundIdentityView => {
  const identities = identityPresets.map(preset => ({
    preset,
    user_agent: userAgentOf(preset),
    originator: preset,
    version: versionOf(preset),
    source: 'compiled_default',
    headers: preset === 'kimi' ? kimiHeaders : preset === 'zcode' ? zcodeHeaders : { 'User-Agent': userAgentOf(preset) }
  }))
  const declarations: PresetDeclarations[] = identityPresets.map(preset => ({
    preset,
    headers: [
      { name: 'User-Agent', class: 'derived', editable: false, builtin: userAgentOf(preset), value: userAgentOf(preset) },
      ...(preset === 'kimi'
        ? [
            { name: 'X-Msh-Platform', class: 'pinned' as const, editable: false, builtin: 'kimi_code_cli', value: 'kimi_code_cli' },
            { name: 'X-Msh-Version', class: 'derived' as const, editable: false, builtin: '2.1.1', value: '2.1.1' },
            ...kimiDeviceHeaders
          ]
        : []),
      ...(preset === 'zcode' ? Object.entries(zcodeHeaders).filter(([name]) => name !== 'User-Agent').map(([name, value]): IdentityDeclaration => ({
        name, value, builtin: value,
        class: zcodeRuntimeNames.includes(name) ? 'runtime' : name === 'X-ZCode-App-Version' ? 'derived' : 'pinned',
        editable: zcodeRuntimeNames.includes(name)
      })) : [])
    ]
  }))
  return { account_policies: identityPolicyFixture(), settings: { profiles: {}, defaults: {} }, presets: identities, effective: identities, declarations }
}
describe('OutboundIdentitySettings', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(getOutboundIdentity).mockResolvedValue(fixture())
    vi.mocked(updateOutboundIdentity).mockResolvedValue(fixture())
  })

  it('offers exactly eleven compatible mappings in a collapsed advanced section', async () => {
    const wrapper = mount(OutboundIdentitySettings)
    await flushPromises()
    const advanced = wrapper.get('[data-testid="outbound-identity-defaults"]')
    expect(advanced.element.tagName).toBe('DETAILS')
    expect(advanced.attributes('open')).toBeUndefined()
    expect(advanced.findAll('[data-identity-mapping]').map(row => row.attributes('data-identity-mapping')).sort()).toEqual([
      'openai:apikey', 'openai:upstream', 'anthropic:apikey', 'anthropic:upstream',
      'gemini:apikey', 'gemini:upstream', 'grok:apikey', 'grok:upstream',
      'antigravity:upstream', 'typesafe:apikey', 'opencode_go:apikey'
    ].sort())
    const select = advanced.get('[data-identity-mapping="opencode_go:apikey"] select')
    expect(select.findAll('option')[0].text()).toContain('automatic')
    await select.setValue('string:claude')
    await wrapper.vm.save()
    expect(vi.mocked(updateOutboundIdentity).mock.calls[0][0].defaults).toEqual({ 'opencode_go:apikey': 'claude' })
    await select.setValue('string:')
    await wrapper.vm.save()
    expect(vi.mocked(updateOutboundIdentity).mock.calls[1][0].defaults).toEqual({})
    wrapper.unmount()
  })

  it('shows the official Grok media identity alongside the sampler identity', async () => {
    const view = fixture()
    const grok = view.effective.find(item => item.preset === 'grok')!
    grok.user_agent = 'grok-shell/1.0.45 (linux; x86_64)'
    grok.headers = { 'User-Agent': grok.user_agent, 'x-grok-client-version': '1.0.45' }
    view.wire_profiles = [{ ...grok, protocol: 'grok_media', headers: { 'User-Agent': 'xai-grok-build/1.0.45', 'x-grok-client-version': '1.0.45' } }]
    vi.mocked(getOutboundIdentity).mockResolvedValue(view)
    const wrapper = mount(OutboundIdentitySettings)
    await flushPromises()
    const group = wrapper.get('[data-identity-group="grok"]')
    expect(group.text()).toContain('grok-shell/1.0.45 (linux; x86_64)')
    expect(group.get('[data-testid="outbound-identity-wire-headers"]').text()).toContain('xai-grok-build/1.0.45')
    expect(group.text()).toContain('admin.settings.outboundIdentity.grokMedia')
  })

  it('separates MiniMax auth defaults and displays control and SDK wire headers', async () => {
    const view = fixture()
    const zcode = view.effective.find(item => item.preset === 'zcode')!
    view.control_plane = [{ ...zcode, headers: { 'User-Agent': 'ZCode/3.14.3', 'X-Os-Version': '#1 SMP' } }]
    view.wire_profiles = [{ ...zcode, protocol: 'anthropic', headers: { ...zcode.headers, 'User-Agent': 'ZCode/3.14.3 ai/6.0.193 ai-sdk/provider-utils/4.0.27 runtime/node.js/22' } }]
    vi.mocked(getOutboundIdentity).mockResolvedValue(view)
    const wrapper = mount(OutboundIdentitySettings)
    await flushPromises()
    const group = wrapper.get('[data-identity-group="minimax"]')
    expect(wrapper.findAll('[data-identity-group="minimax"]')).toHaveLength(1)
    expect(group.get('[data-identity-preset="minimax"]').text()).toContain('OAuth')
    const byok = group.get('[data-identity-preset="minimax_apikey"]')
    expect(byok.text()).toContain('Anthropic/JS 0.91.1')
    expect(byok.find('input').exists()).toBe(false)
    const control = wrapper.get('[data-testid="outbound-identity-control-headers"]')
    expect(control.text()).toContain('#1 SMP')
    expect(control.text()).not.toContain('X-ZCode-Agent')
    expect(wrapper.get('[data-testid="outbound-identity-wire-headers"]').text()).toContain('ai/6.0.193')
    wrapper.unmount()
  })

  it('explains DeepSeek offset seconds and the distinct GLM OAuth kernel build value', async () => {
    const view = fixture()
    const deepseek = view.effective.find(item => item.preset === 'deepseek')!
    const zcode = view.effective.find(item => item.preset === 'zcode')!
    view.control_plane = [
      { ...deepseek, headers: { 'X-Client-Timezone-Offset': '-25200' } },
      { ...zcode, headers: { 'X-Os-Version': '#31-Ubuntu SMP PREEMPT_DYNAMIC Sat Apr 20 00:40:06 UTC 2024' } }
    ]
    vi.mocked(getOutboundIdentity).mockResolvedValue(view)
    const wrapper = mount(OutboundIdentitySettings)
    await flushPromises()
    expect(wrapper.get('[data-testid="deepseek-offset-hint"]').text()).toContain('deepseekOffsetHint')
    expect(wrapper.get('[data-testid="zcode-kernel-hint"]').text()).toContain('zcodeKernelHint')
    expect(wrapper.text()).toContain('-25200')
    expect(wrapper.text()).toContain('#31-Ubuntu SMP PREEMPT_DYNAMIC Sat Apr 20 00:40:06 UTC 2024')
    wrapper.unmount()
  })

  it('shows effective identities and saves only configured presets and type defaults', async () => {
    const wrapper = mount(OutboundIdentitySettings, { slots: { codex: '<div data-testid="codex-existing-controls">Codex controls</div>' } })
    await flushPromises()
    expect(wrapper.get('[data-testid="codex-existing-controls"]').text()).toBe('Codex controls')
    expect(wrapper.text()).toContain('sources.compiled_default')
    const claude = wrapper.get('[data-identity-preset="claude"]')
    await claude.findAll('input')[0].setValue('2.9.1')
    const advanced = wrapper.get('[data-testid="outbound-identity-defaults"]')
    await advanced.get('summary').trigger('click')
    const apiKeyDefault = advanced.get('[data-identity-mapping="openai:apikey"] select')
    await apiKeyDefault.setValue('string:grok')
    await flushPromises()
    expect((apiKeyDefault.element as HTMLSelectElement).value).toBe('string:grok')
    await wrapper.vm.save()
    expect(updateOutboundIdentity).toHaveBeenCalledWith({ profiles: { claude: { preset: 'claude', user_agent: '', version: '2.9.1' } }, defaults: { 'openai:apikey': 'grok' }, runtime: {} })
    wrapper.unmount()
  })

  it('shows all five OAuth/API Key identities and saves only editable ZCode runtime headers', async () => {
    const wrapper = mount(OutboundIdentitySettings)
    await flushPromises()
    expect(wrapper.findAll('[data-testid="outbound-identity-auth-scope"]')).toHaveLength(4)
    const card = wrapper.get('[data-identity-preset="zcode"]')
    expect(wrapper.get('[data-identity-group="zcode"]').text()).toContain('GLM · ZCode')
    const headers = card.get('[data-testid="outbound-identity-headers"]')
    for (const [name, value] of Object.entries(zcodeHeaders)) {
      expect(headers.text()).toContain(name)
      expect(headers.text()).toContain(value)
    }
    expect(card.find('input[aria-label="X-ZCode-App-Version"]').exists()).toBe(false)
    expect(card.find('input[aria-label="X-Title"]').exists()).toBe(false)
    card.findAllComponents(IdentityRuntimeField).find(field => field.props('name') === 'X-Client-Timezone')!.vm.$emit('update:modelValue', 'Asia/Shanghai')
    await card.findAll('input')[0].setValue('4.1.0')
    await wrapper.vm.save()
    expect(updateOutboundIdentity).toHaveBeenCalledWith({
      profiles: { zcode: { preset: 'zcode', user_agent: '', version: '4.1.0' } },
      defaults: {}, runtime: { zcode: { 'X-Client-Timezone': 'Asia/Shanghai' } }
    })
    wrapper.unmount()
  })

  it('preserves header-only API profiles in the editable global runtime fields', async () => {
    const view = fixture()
    view.settings.profiles.zcode = { preset: 'zcode', headers: { 'X-Client-Timezone': 'Asia/Shanghai' } }
    view.settings.runtime = { zcode: { 'X-Client-Timezone': 'UTC' } }
    vi.mocked(getOutboundIdentity).mockResolvedValue(view)
    const wrapper = mount(OutboundIdentitySettings)
    await flushPromises()
    const card = wrapper.get('[data-identity-preset="zcode"]')
    expect(card.get('button[aria-label="X-Client-Timezone"]').text()).toContain('Asia/Shanghai')
    expect(wrapper.vm.isDirty).toBe(false)
    card.findAllComponents(IdentityRuntimeField).find(field => field.props('name') === 'X-Client-Timezone')!.vm.$emit('update:modelValue', 'Europe/Amsterdam')
    await wrapper.vm.save()
    expect(updateOutboundIdentity).toHaveBeenCalledWith({ profiles: {}, defaults: {}, runtime: { zcode: { 'X-Client-Timezone': 'Europe/Amsterdam' } } })
    wrapper.unmount()
  })

  it('shows one read-only environment with no per-field OS overrides', async () => {
    const wrapper = mount(OutboundIdentitySettings)
    await flushPromises()
    for (const preset of ['kimi', 'zcode']) {
      const card = wrapper.get(`[data-identity-preset="${preset}"]`)
      const environment = card.get('[data-testid="identity-environment-summary"]')
      expect(environment.text()).toContain('Ubuntu 24.04')
      expect(environment.find('input').exists()).toBe(false)
      expect(environment.find('select').exists()).toBe(false)
      expect(card.find('input[aria-label="X-Msh-Os-Version"]').exists()).toBe(false)
      expect(card.find('input[aria-label="X-Platform"]').exists()).toBe(false)
    }
    expect(wrapper.vm.isDirty).toBe(false)
    wrapper.unmount()
  })

  it('saves MiniMax timezone-only profiles without inventing a header', async () => {
    const wrapper = mount(OutboundIdentitySettings)
    await flushPromises()
    const card = wrapper.get('[data-identity-preset="minimax"]')
    card.getComponent(IdentityRuntimeField).vm.$emit('update:modelValue', 'Europe/Amsterdam')
    await wrapper.vm.save()
    expect(vi.mocked(updateOutboundIdentity).mock.calls[0][0]).toEqual({
      profiles: { minimax: { preset: 'minimax', user_agent: '', version: '', timezone: 'Europe/Amsterdam' } }, defaults: {}, runtime: {}
    })
    wrapper.unmount()
  })

  it('selects DeepSeek locale and timezone and labels the official empty bundle', async () => {
    const view = fixture()
    view.control_plane = [{ ...view.effective.find(item => item.preset === 'deepseek')!, headers: { 'X-Client-Bundle-Id': '', 'X-Client-Locale': 'zh_CN' } }]
    vi.mocked(getOutboundIdentity).mockResolvedValue(view)
    const wrapper = mount(OutboundIdentitySettings)
    await flushPromises()
    const card = wrapper.get('[data-identity-preset="deepseek"]')
    expect(card.get('[data-testid="outbound-identity-control-headers"]').text()).toContain('emptyOfficialValue')
    const language = card.get('select[aria-label="language"]')
    expect(language.findAll('option').map(option => option.attributes('value'))).toEqual(['string:', 'string:zh-CN', 'string:en-US'])
    await language.setValue('string:en-US')
    card.findAllComponents(IdentityRuntimeField).find(field => field.props('name') === 'timezone')!.vm.$emit('update:modelValue', 'Asia/Shanghai')
    await wrapper.vm.save()
    expect(vi.mocked(updateOutboundIdentity).mock.calls[0][0].profiles.deepseek).toEqual({ preset: 'deepseek', user_agent: '', version: '', language: 'en-US', timezone: 'Asia/Shanghai' })
    wrapper.unmount()
  })

  it('saves both MiniMax modes from their shared group without merging identities', async () => {
    const wrapper = mount(OutboundIdentitySettings)
    await flushPromises()
    const group = wrapper.get('[data-identity-group="minimax"]')
    for (const preset of ['minimax', 'minimax_apikey']) {
      group.get(`[data-identity-preset="${preset}"]`).getComponent(IdentityRuntimeField).vm.$emit('update:modelValue', preset === 'minimax' ? 'UTC' : 'Asia/Shanghai')
    }
    await wrapper.vm.save()
    expect(vi.mocked(updateOutboundIdentity).mock.calls[0][0].profiles).toEqual({
      minimax: { preset: 'minimax', user_agent: '', version: '', timezone: 'UTC' },
      minimax_apikey: { preset: 'minimax_apikey', user_agent: '', version: '', timezone: 'Asia/Shanghai' }
    })
    wrapper.unmount()
  })

  it('does not overwrite persisted settings when loading fails', async () => {
    vi.mocked(getOutboundIdentity).mockRejectedValueOnce(new Error('unavailable'))
    const wrapper = mount(OutboundIdentitySettings)
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('loadFailed')
    await expect(wrapper.vm.save()).rejects.toThrow('loadFailed')
    expect(updateOutboundIdentity).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('tracks pending edits and skips an unchanged save', async () => {
    const wrapper = mount(OutboundIdentitySettings)
    await flushPromises()
    expect(wrapper.vm.isDirty).toBe(false)
    await wrapper.vm.save()
    expect(updateOutboundIdentity).not.toHaveBeenCalled()
    const input = wrapper.get('[data-identity-preset="claude"]').findAll('input')[0]
    await input.setValue('3.9.1')
    expect(wrapper.vm.isDirty).toBe(true)
    await input.setValue('')
    expect(wrapper.vm.isDirty).toBe(false)
    wrapper.unmount()
  })

  it('retains edits made during saving and effective-identity refresh', async () => {
    const wrapper = mount(OutboundIdentitySettings)
    await flushPromises()
    const input = wrapper.get('[data-identity-preset="claude"]').findAll('input')[0]
    await input.setValue('3.9.1')
    let complete!: (value: OutboundIdentityView) => void
    vi.mocked(updateOutboundIdentity).mockReturnValueOnce(new Promise(resolve => { complete = resolve }))
    const save = wrapper.vm.save()
    await input.setValue('3.9.2')
    expect(vi.mocked(updateOutboundIdentity).mock.calls[0][0].profiles.claude?.version).toBe('3.9.1')
    complete(fixture())
    await save
    await wrapper.vm.refresh()
    expect((input.element as HTMLInputElement).value).toBe('3.9.2')
    expect(wrapper.vm.isDirty).toBe(true)
    await wrapper.vm.save()
    expect(vi.mocked(updateOutboundIdentity).mock.calls[1][0].profiles.claude?.version).toBe('3.9.2')
    expect(wrapper.vm.isDirty).toBe(false)
    wrapper.unmount()
  })

  it('exposes the typesafe API-key type default mapping', async () => {
    const wrapper = mount(OutboundIdentitySettings)
    await flushPromises()
    const row = wrapper.findAll('label').find(label => label.text().includes('TypeSafe / Jev · API Key'))
    expect(row, 'the typesafe type-default row must be configurable').toBeDefined()
    await row!.find('select').setValue('string:claude')
    await wrapper.vm.save()
    expect(vi.mocked(updateOutboundIdentity).mock.calls[0][0].defaults).toEqual({ 'typesafe:apikey': 'claude' })
    wrapper.unmount()
  })

  it('exposes the DeepSeek profile without a redundant type mapping', async () => {
    const wrapper = mount(OutboundIdentitySettings)
    await flushPromises()
    const deepseekCard = wrapper.get('[data-identity-preset="deepseek"]')
    expect(wrapper.get('[data-identity-group="deepseek"]').text()).toContain('DeepSeek')
    expect(deepseekCard.text()).toContain('deepseek/1.2.3')
    expect(wrapper.find('[data-identity-mapping="deepseek:apikey"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('shows the wire request headers and the versionless MiniMax declaration', async () => {
    const wrapper = mount(OutboundIdentitySettings)
    await flushPromises()
    const minimaxCard = wrapper.get('[data-identity-preset="minimax"]')
    expect(minimaxCard.text()).toContain('MiniMax')
    // The saved global identity exposes the exact headers sent upstream.
    const headers = minimaxCard.get('[data-testid="outbound-identity-headers"]')
    expect(headers.text()).toContain('User-Agent')
    expect(headers.text()).toContain('MiniMaxAgent')
    // A family that publishes no version shows the explicit placeholder instead
    // of a blank row, and offers no version or UA override control.
    expect(minimaxCard.text()).toContain('versionNotDeclared')
    expect(minimaxCard.text()).toContain('versionlessHint')
    expect(minimaxCard.find('input').exists()).toBe(false)
    expect(wrapper.find('[data-identity-mapping="minimax:apikey"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('mirrors the backend versionless client-family enumeration', () => {
    // Keep this list in lockstep with versionlessOutboundUserAgents in
    // backend/internal/service/outbound_identity.go.
    expect(versionlessIdentityPresets).toEqual(['minimax', 'stepfun'])
    for (const preset of versionlessIdentityPresets) expect(identityPresets).toContain(preset)
  })

  it('shows one Step-Code identity for both auth types without invented runtime fields', async () => {
    const view = fixture()
    const step = view.effective.find(item => item.preset === 'stepfun')!
    view.wire_profiles = [{ ...step, protocol: 'chat_completions', headers: {
      'User-Agent': 'step (linux 6.8.0-31-generic; x64)',
      'X-Step-Client': 'stepcode', 'X-Stainless-Package-Version': '6.40.0',
      'X-Stainless-OS': 'Linux', 'X-Stainless-Arch': 'x64'
    } }]
    vi.mocked(getOutboundIdentity).mockResolvedValue(view)
    const wrapper = mount(OutboundIdentitySettings)
    await flushPromises()
    const group = wrapper.get('[data-identity-group="stepfun"]')
    expect(group.findAll('[data-identity-preset]')).toHaveLength(1)
    expect(group.text()).toContain('step (linux 6.8.0-31-generic; x64)')
    expect(group.text()).toContain('stepcode')
    expect(group.text()).toContain('6.40.0')
    expect(group.text()).toContain('versionlessHint')
    expect(group.findAll('input, select')).toHaveLength(0)
    expect(group.text()).not.toContain('X-Msh-Device-Name')
    wrapper.unmount()
  })

  it('exposes the pinned ZCode preset and its API-key type default mapping', async () => {
    const wrapper = mount(OutboundIdentitySettings)
    await flushPromises()
    const zcodeCard = wrapper.get('[data-identity-preset="zcode"]')
    expect(zcodeCard.text()).toContain('ZCode')
    // ZCode is a versioned family: it renders the product token with its client
    // version, keeps the version control, and never shows the versionless hint.
    const headers = zcodeCard.get('[data-testid="outbound-identity-headers"]')
    expect(headers.text()).toContain('User-Agent')
    expect(headers.text()).toContain('ZCode/3.14.3')
    expect(headers.text()).not.toContain('Originator')
    expect(zcodeCard.text()).not.toContain('versionlessHint')
    expect(zcodeCard.find('input').exists()).toBe(true)
    expect(wrapper.find('[data-identity-mapping="zhipu:apikey"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('exposes the pinned Kimi Code preset and lets an operator manage its runtime declarations', async () => {
    const wrapper = mount(OutboundIdentitySettings)
    await flushPromises()
    const kimiCard = wrapper.get('[data-identity-preset="kimi"]')
    expect(wrapper.get('[data-identity-group="kimi"]').text()).toContain('Kimi Code')
    // The complete official declaration block reaches the saved identity view.
    const headers = kimiCard.get('[data-testid="outbound-identity-headers"]')
    expect(headers.text()).toContain('X-Msh-Platform')
    expect(headers.text()).toContain('kimi_code_cli')
    // Only the runtime device declarations are editable; the pinned family token
    // and the derived version companion stay read-only.
    const runtime = kimiCard.get('[data-testid="outbound-identity-runtime-headers"]')
    const inputs = runtime.findAll('input')
    expect(inputs).toHaveLength(2)
    expect(inputs.map(input => input.attributes('aria-label'))).toEqual(['X-Msh-Device-Name', 'X-Msh-Device-Id'])
    expect(inputs[0].attributes('placeholder')).toBe('kimi-gateway')
    expect(kimiCard.findAll('input').length).toBeGreaterThan(inputs.length)

    await inputs[0].setValue('kimi-gateway-2')
    await wrapper.vm.save()
    expect(vi.mocked(updateOutboundIdentity).mock.calls[0][0].runtime).toEqual({ kimi: { 'X-Msh-Device-Name': 'kimi-gateway-2' } })
    wrapper.unmount()
  })

  it('persists runtime declarations and keeps them out of the profile map', async () => {
    const saved = fixture()
    saved.settings.runtime = { kimi: { 'X-Msh-Device-Name': 'kimi-gateway', 'X-Msh-Device-Id': '22222222-2222-4222-8222-222222222222' } }
    vi.mocked(getOutboundIdentity).mockResolvedValueOnce(saved)
    vi.mocked(updateOutboundIdentity).mockImplementationOnce(async settings => ({ ...saved, settings }))
    const wrapper = mount(OutboundIdentitySettings)
    await flushPromises()
    // Loading persisted runtime values is not an unsaved edit on its own.
    expect(wrapper.vm.isDirty).toBe(false)
    const kimiCard = wrapper.get('[data-identity-preset="kimi"]')
    const deviceID = kimiCard.get('[data-testid="outbound-identity-runtime-headers"]').get('input[aria-label="X-Msh-Device-Id"]')
    expect((deviceID.element as HTMLInputElement).value).toBe('22222222-2222-4222-8222-222222222222')
    await deviceID.setValue('')
    await wrapper.vm.save()
    expect(vi.mocked(updateOutboundIdentity).mock.calls[0][0].runtime).toEqual({ kimi: { 'X-Msh-Device-Name': 'kimi-gateway' } })
    expect(vi.mocked(updateOutboundIdentity).mock.calls[0][0].profiles).toEqual({})
    wrapper.unmount()
  })

  it('preserves an already persisted versionless profile through an unrelated save', async () => {
    const saved = fixture()
    saved.settings.profiles = { minimax: { preset: 'minimax' } }
    vi.mocked(getOutboundIdentity).mockResolvedValueOnce(saved)
    vi.mocked(updateOutboundIdentity).mockImplementationOnce(async settings => ({ ...saved, settings }))
    const wrapper = mount(OutboundIdentitySettings)
    await flushPromises()
    // Loading a preserved profile is not an unsaved edit on its own.
    expect(wrapper.vm.isDirty).toBe(false)
    await wrapper.get('[data-identity-preset="claude"]').findAll('input')[0].setValue('3.9.1')
    await wrapper.vm.save()
    expect(vi.mocked(updateOutboundIdentity).mock.calls[0][0].profiles).toEqual({
      claude: { preset: 'claude', user_agent: '', version: '3.9.1' },
      minimax: { preset: 'minimax', user_agent: '', version: '' }
    })
    expect(wrapper.vm.isDirty).toBe(false)
    wrapper.unmount()
  })

  it('preserves advanced mappings when editing unrelated profile fields', async () => {
    const saved = fixture()
    saved.settings.defaults = { 'opencode_go:apikey': 'claude' }
    vi.mocked(getOutboundIdentity).mockResolvedValueOnce(saved)
    const wrapper = mount(OutboundIdentitySettings)
    await flushPromises()
    await wrapper.get('[data-identity-preset="claude"]').findAll('input')[0].setValue('3.9.1')
    await wrapper.vm.save()
    expect(vi.mocked(updateOutboundIdentity).mock.calls[0][0].defaults).toEqual({ 'opencode_go:apikey': 'claude' })
    wrapper.unmount()
  })
})
