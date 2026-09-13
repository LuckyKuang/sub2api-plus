import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import OutboundIdentitySettings from './OutboundIdentitySettings.vue'
import { getOutboundIdentity, updateOutboundIdentity, identityPresets, type OutboundIdentityView } from '@/api/admin/outboundIdentity'

vi.mock('vue-i18n', async (original) => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/api/admin/outboundIdentity', async (original) => ({
  ...await original<typeof import('@/api/admin/outboundIdentity')>(),
  getOutboundIdentity: vi.fn(), updateOutboundIdentity: vi.fn()
}))
const fixture = (): OutboundIdentityView => {
  const identities = identityPresets.map(preset => ({ preset, user_agent: `${preset}/1.2.3`, originator: preset, version: '1.2.3', source: 'compiled_default', headers: { 'User-Agent': `${preset}/1.2.3` } }))
  return { settings: { profiles: {}, defaults: {} }, presets: identities, effective: identities }
}
describe('OutboundIdentitySettings', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(getOutboundIdentity).mockResolvedValue(fixture())
    vi.mocked(updateOutboundIdentity).mockResolvedValue(fixture())
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
    expect(updateOutboundIdentity).toHaveBeenCalledWith({ profiles: { claude: { preset: 'claude', user_agent: '', version: '2.9.1' } }, defaults: { 'openai:apikey': 'grok' } })
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
