import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import OutboundIdentitySettings from './OutboundIdentitySettings.vue'
import { getOutboundIdentity, updateOutboundIdentity, identityPresets, versionlessIdentityPresets, type IdentityDeclaration, type OutboundIdentityView, type PresetDeclarations } from '@/api/admin/outboundIdentity'

vi.mock('vue-i18n', async (original) => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/api/admin/outboundIdentity', async (original) => ({
  ...await original<typeof import('@/api/admin/outboundIdentity')>(),
  getOutboundIdentity: vi.fn(), updateOutboundIdentity: vi.fn()
}))
const userAgentOf = (preset: string) => preset === 'minimax' ? 'MiniMaxAgent' : preset === 'minimax_apikey' ? 'Anthropic/JS 0.91.1' : preset === 'kimi' ? 'kimi-code-cli/2.1.1' : preset === 'zcode' ? 'ZCode/3.14.3' : `${preset}/1.2.3`
const versionOf = (preset: string) => preset === 'minimax' ? '' : preset === 'minimax_apikey' ? '0.91.1' : preset === 'kimi' ? '2.1.1' : preset === 'zcode' ? '3.14.3' : '1.2.3'
// The Kimi Code device set is the runtime tier: the official client resolves it
// from its own host, so the settings page exposes an editable value per header
// and the backend declares which headers those are.
const kimiDeviceHeaders: IdentityDeclaration[] = [
  { name: 'X-Msh-Device-Name', class: 'runtime', editable: true, builtin: 'kimi-gateway', value: 'kimi-gateway' },
  { name: 'X-Msh-Device-Model', class: 'runtime', editable: true, builtin: 'Linux 6.14.0 x64', value: 'Linux 6.14.0 x64' },
  { name: 'X-Msh-Os-Version', class: 'runtime', editable: true, builtin: '6.14.0', value: '6.14.0' },
  { name: 'X-Msh-Device-Id', class: 'runtime', editable: true, builtin: '11111111-1111-4111-8111-111111111111', value: '11111111-1111-4111-8111-111111111111' }
]
const kimiHeaders = {
  'User-Agent': 'kimi-code-cli/2.1.1',
  'X-Msh-Platform': 'kimi_code_cli',
  'X-Msh-Version': '2.1.1',
  'X-Msh-Device-Name': 'kimi-gateway',
  'X-Msh-Device-Model': 'Linux 6.14.0 x64',
  'X-Msh-Os-Version': '6.14.0',
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
  'X-Platform': 'linux-arm64',
  'X-Os-Category': 'linux',
  'X-Os-Version': '6.8.0'
}
const zcodeRuntimeNames = ['X-Client-Language', 'X-Client-Timezone', 'X-Platform', 'X-Os-Category', 'X-Os-Version']
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
  return { settings: { profiles: {}, defaults: {} }, presets: identities, effective: identities, declarations }
}
describe('OutboundIdentitySettings', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(getOutboundIdentity).mockResolvedValue(fixture())
    vi.mocked(updateOutboundIdentity).mockResolvedValue(fixture())
  })

  it('separates MiniMax auth defaults and displays control and SDK wire headers', async () => {
    const view = fixture()
    const zcode = view.effective.find(item => item.preset === 'zcode')!
    view.control_plane = [{ ...zcode, headers: { 'User-Agent': 'ZCode/3.14.3', 'X-Os-Version': '#1 SMP' } }]
    view.wire_profiles = [{ ...zcode, protocol: 'anthropic', headers: { ...zcode.headers, 'User-Agent': 'ZCode/3.14.3 ai/6.0.193 ai-sdk/provider-utils/4.0.27 runtime/node.js/22' } }]
    vi.mocked(getOutboundIdentity).mockResolvedValue(view)
    const wrapper = mount(OutboundIdentitySettings)
    await flushPromises()
    const cards = wrapper.findAll('section')
    expect(cards[identityPresets.indexOf('minimax')].text()).toContain('MiniMax Code · OAuth')
    const byok = cards[identityPresets.indexOf('minimax_apikey')]
    expect(byok.text()).toContain('Anthropic/JS 0.91.1')
    expect(byok.find('input').exists()).toBe(false)
    const control = wrapper.get('[data-testid="outbound-identity-control-headers"]')
    expect(control.text()).toContain('#1 SMP')
    expect(control.text()).not.toContain('X-ZCode-Agent')
    expect(wrapper.get('[data-testid="outbound-identity-wire-headers"]').text()).toContain('ai/6.0.193')
    wrapper.unmount()
  })

  it('shows effective identities and saves only configured presets and type defaults', async () => {
    const wrapper = mount(OutboundIdentitySettings, { slots: { codex: '<div data-testid="codex-existing-controls">Codex controls</div>' } })
    await flushPromises()
    expect(wrapper.get('[data-testid="codex-existing-controls"]').text()).toBe('Codex controls')
    expect(wrapper.text()).toContain('sources.compiled_default')
    const claude = wrapper.findAll('section')[1]
    await claude.findAll('input')[0].setValue('2.9.1')
    const defaults = wrapper.findAll('select')
    await defaults[0].setValue('grok')
    await wrapper.vm.save()
    expect(updateOutboundIdentity).toHaveBeenCalledWith({ profiles: { claude: { preset: 'claude', user_agent: '', version: '2.9.1' } }, defaults: { 'openai:apikey': 'grok' }, runtime: {} })
    wrapper.unmount()
  })

  it('shows all four OAuth/API Key identities and saves only editable ZCode runtime headers', async () => {
    const wrapper = mount(OutboundIdentitySettings)
    await flushPromises()
    expect(wrapper.findAll('[data-testid="outbound-identity-auth-scope"]')).toHaveLength(3)
    const card = wrapper.findAll('section')[identityPresets.indexOf('zcode')]
    expect(card.text()).toContain('GLM · ZCode')
    const headers = card.get('[data-testid="outbound-identity-headers"]')
    for (const [name, value] of Object.entries(zcodeHeaders)) {
      expect(headers.text()).toContain(name)
      expect(headers.text()).toContain(value)
    }
    expect(card.find('input[aria-label="X-ZCode-App-Version"]').exists()).toBe(false)
    expect(card.find('input[aria-label="X-Title"]').exists()).toBe(false)
    await card.get('input[aria-label="X-Client-Timezone"]').setValue('Asia/Shanghai')
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
    const card = wrapper.findAll('section')[identityPresets.indexOf('zcode')]
    expect((card.get('input[aria-label="X-Client-Timezone"]').element as HTMLInputElement).value).toBe('Asia/Shanghai')
    expect(wrapper.vm.isDirty).toBe(false)
    await card.get('input[aria-label="X-Client-Timezone"]').setValue('Europe/Amsterdam')
    await wrapper.vm.save()
    expect(updateOutboundIdentity).toHaveBeenCalledWith({ profiles: {}, defaults: {}, runtime: { zcode: { 'X-Client-Timezone': 'Europe/Amsterdam' } } })
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
    const input = wrapper.findAll('section')[1].findAll('input')[0]
    await input.setValue('3.9.1')
    expect(wrapper.vm.isDirty).toBe(true)
    await input.setValue('')
    expect(wrapper.vm.isDirty).toBe(false)
    wrapper.unmount()
  })

  it('retains edits made during saving and effective-identity refresh', async () => {
    const wrapper = mount(OutboundIdentitySettings)
    await flushPromises()
    const input = wrapper.findAll('section')[1].findAll('input')[0]
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
    await row!.find('select').setValue('claude')
    await wrapper.vm.save()
    expect(vi.mocked(updateOutboundIdentity).mock.calls[0][0].defaults).toEqual({ 'typesafe:apikey': 'claude' })
    wrapper.unmount()
  })

  it('exposes the pinned DeepSeek preset and its API-key type default mapping', async () => {
    const wrapper = mount(OutboundIdentitySettings)
    await flushPromises()
    const deepseekCard = wrapper.findAll('section')[identityPresets.indexOf('deepseek')]
    expect(deepseekCard.text()).toContain('DeepSeek')
    expect(deepseekCard.text()).toContain('deepseek/1.2.3')
    const row = wrapper.findAll('label').find(label => label.text().includes('deepseek · API Key'))
    expect(row, 'the deepseek type-default row must be configurable').toBeDefined()
    await row!.find('select').setValue('deepseek')
    await wrapper.vm.save()
    expect(vi.mocked(updateOutboundIdentity).mock.calls[0][0].defaults).toEqual({ 'deepseek:apikey': 'deepseek' })
    wrapper.unmount()
  })

  it('shows the wire request headers and the versionless MiniMax declaration', async () => {
    const wrapper = mount(OutboundIdentitySettings)
    await flushPromises()
    const minimaxCard = wrapper.findAll('section')[identityPresets.indexOf('minimax')]
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
    const row = wrapper.findAll('label').find(label => label.text().includes('minimax · API Key'))
    expect(row, 'the minimax type-default row must be configurable').toBeDefined()
    await row!.find('select').setValue('minimax')
    await wrapper.vm.save()
    expect(vi.mocked(updateOutboundIdentity).mock.calls[0][0].defaults).toEqual({ 'minimax:apikey': 'minimax' })
    wrapper.unmount()
  })

  it('mirrors the backend versionless client-family enumeration', () => {
    // Keep this list in lockstep with versionlessOutboundUserAgents in
    // backend/internal/service/outbound_identity.go.
    expect(versionlessIdentityPresets).toEqual(['minimax'])
    for (const preset of versionlessIdentityPresets) expect(identityPresets).toContain(preset)
  })

  it('exposes the pinned ZCode preset and its API-key type default mapping', async () => {
    const wrapper = mount(OutboundIdentitySettings)
    await flushPromises()
    const zcodeCard = wrapper.findAll('section')[identityPresets.indexOf('zcode')]
    expect(zcodeCard.text()).toContain('ZCode')
    // ZCode is a versioned family: it renders the product token with its client
    // version, keeps the version control, and never shows the versionless hint.
    const headers = zcodeCard.get('[data-testid="outbound-identity-headers"]')
    expect(headers.text()).toContain('User-Agent')
    expect(headers.text()).toContain('ZCode/3.14.3')
    expect(headers.text()).not.toContain('Originator')
    expect(zcodeCard.text()).not.toContain('versionlessHint')
    expect(zcodeCard.find('input').exists()).toBe(true)
    const row = wrapper.findAll('label').find(label => label.text().includes('zhipu · API Key'))
    expect(row, 'the zhipu type-default row must be configurable').toBeDefined()
    await row!.find('select').setValue('zcode')
    await wrapper.vm.save()
    expect(vi.mocked(updateOutboundIdentity).mock.calls[0][0].defaults).toEqual({ 'zhipu:apikey': 'zcode' })
    wrapper.unmount()
  })

  it('exposes the pinned Kimi Code preset and lets an operator manage its runtime declarations', async () => {
    const wrapper = mount(OutboundIdentitySettings)
    await flushPromises()
    const kimiCard = wrapper.findAll('section')[identityPresets.indexOf('kimi')]
    expect(kimiCard.text()).toContain('Kimi Code')
    // The complete official declaration block reaches the saved identity view.
    const headers = kimiCard.get('[data-testid="outbound-identity-headers"]')
    expect(headers.text()).toContain('X-Msh-Platform')
    expect(headers.text()).toContain('kimi_code_cli')
    // Only the runtime device declarations are editable; the pinned family token
    // and the derived version companion stay read-only.
    const runtime = kimiCard.get('[data-testid="outbound-identity-runtime-headers"]')
    const inputs = runtime.findAll('input')
    expect(inputs).toHaveLength(4)
    expect(inputs.map(input => input.attributes('aria-label'))).toEqual(['X-Msh-Device-Name', 'X-Msh-Device-Model', 'X-Msh-Os-Version', 'X-Msh-Device-Id'])
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
    const kimiCard = wrapper.findAll('section')[identityPresets.indexOf('kimi')]
    const deviceID = kimiCard.get('[data-testid="outbound-identity-runtime-headers"]').findAll('input')[3]
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
    await wrapper.findAll('section')[1].findAll('input')[0].setValue('3.9.1')
    await wrapper.vm.save()
    expect(vi.mocked(updateOutboundIdentity).mock.calls[0][0].profiles).toEqual({
      claude: { preset: 'claude', user_agent: '', version: '3.9.1' },
      minimax: { preset: 'minimax', user_agent: '', version: '' }
    })
    expect(wrapper.vm.isDirty).toBe(false)
    wrapper.unmount()
  })

  it('preserves valid mappings that are not shown as editable rows', async () => {
    const saved = fixture()
    saved.settings.defaults = { 'anthropic:oauth': 'claude' }
    vi.mocked(getOutboundIdentity).mockResolvedValueOnce(saved)
    const wrapper = mount(OutboundIdentitySettings)
    await flushPromises()
    await wrapper.findAll('section')[1].findAll('input')[0].setValue('3.9.1')
    await wrapper.vm.save()
    expect(vi.mocked(updateOutboundIdentity).mock.calls[0][0].defaults).toEqual({ 'anthropic:oauth': 'claude' })
    wrapper.unmount()
  })
})
