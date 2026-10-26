import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import Select from '@/components/common/Select.vue'
import MonitorSettingsPanel from '../MonitorSettingsPanel.vue'
import MonitorPlatformManager from '../MonitorPlatformManager.vue'
import { CONCRETE_PLATFORM_OPTIONS } from '@/constants/platforms'
import type { MonitorConfig } from '@/api/channelMonitorV2'

const mocks = vi.hoisted(() => ({ getConfig: vi.fn(), getModelCatalog: vi.fn(), updateConfig: vi.fn(), groups: vi.fn(), showError: vi.fn(), showSuccess: vi.fn() }))
vi.mock('@/api/channelMonitorV2', () => ({ getConfig: mocks.getConfig, getModelCatalog: mocks.getModelCatalog, updateConfig: mocks.updateConfig, MONITOR_ERROR_CATEGORIES: ['authentication', 'network'] }))
vi.mock('@/api/admin', () => ({ adminAPI: { groups: { getAllIncludingInactive: mocks.groups } } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ cachedPublicSettings: { channel_monitor_enabled: true }, showError: mocks.showError, showSuccess: mocks.showSuccess }) }))
vi.mock('@/utils/featureFlags', () => ({ getChannelMonitorMode: () => 'v2', isChannelMonitorV2Mode: () => true }))
vi.mock('vue-i18n', async importOriginal => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key, te: () => false }),
}))

function config(): MonitorConfig {
  return {
    version: 2, enabled: true, refresh_interval_seconds: 60,
    platforms: [{ platform: 'openai', enabled: false, models: ['gpt-4'] }, { platform: 'custom-provider', enabled: true, models: ['custom-model'] }],
    group_ids: [42], ignored_error_categories: [],
    health_thresholds: { minimum_sample: 70, warning_error_rate: .1, critical_error_rate: .3, target_ttft_ms: 1500, warning_ttft_ms: 2000, critical_ttft_ms: 5000, warning_cache_rate: .7, critical_cache_rate: .4, error_weight: .5, ttft_weight: .3, cache_weight: .2 },
  }
}
async function panel(value = config()) {
  mocks.getConfig.mockResolvedValue(value)
  const wrapper = mount(MonitorSettingsPanel, { global: { stubs: { RouterLink: true, teleport: true } } })
  await flushPromises()
  return wrapper
}
type Panel = Awaited<ReturnType<typeof panel>>
function saveButton(wrapper: Panel) { return wrapper.findAll('button').find(item => item.text() === 'channelMonitorV2.settings.save')! }
async function add(wrapper: Panel, platform: string) {
  if (!wrapper.findComponent(Select).exists()) await wrapper.get('[data-testid="open-add-platform"]').trigger('click')
  wrapper.findComponent(Select).vm.$emit('update:modelValue', platform)
  await flushPromises()
  await wrapper.get('[data-testid="add-platform"]').trigger('click')
  await flushPromises()
}

beforeEach(() => {
  vi.clearAllMocks()
  mocks.groups.mockResolvedValue([])
  mocks.getModelCatalog.mockResolvedValue({ config_version: 2, range: '30d', start: '', end: '', items: [] })
  mocks.updateConfig.mockImplementation(async value => ({ ...JSON.parse(JSON.stringify(value)), version: value.version + 1 }))
})

describe('MonitorSettingsPanel real config persistence', () => {
  it('adds custom IDs through one popup and saves the whole versioned config without mutating the original', async () => {
    const original = config()
    const wrapper = await panel(original)
    expect(saveButton(wrapper).attributes('disabled')).toBeDefined()
    await add(wrapper, 'deepseek')
    await wrapper.get('[data-mode="selected"]').trigger('click')
    await wrapper.get('[data-testid="open-model-picker"]').trigger('click')
    await wrapper.findAll('button').find(item => item.text() === 'channelMonitorV2.settings.picker.bulk')!.trigger('click')
    await wrapper.get('textarea').setValue(' deepseek-chat, deepseek-chat，DeepSeek-Chat\ncustom-id ')
    await wrapper.get('[data-testid="picker-apply"]').trigger('click')
    await saveButton(wrapper).trigger('click')
    await flushPromises()
    expect(mocks.updateConfig).toHaveBeenCalledWith({
      ...original,
      platforms: [
        { ...original.platforms[0], model_display_mode: 'selected', excluded_models: [] },
        { ...original.platforms[1], model_display_mode: 'selected', excluded_models: [] },
        { platform: 'deepseek', enabled: true, models: ['DeepSeek-Chat', 'custom-id', 'deepseek-chat'], model_display_mode: 'selected', excluded_models: [] },
      ],
    })
    expect(original.platforms).toHaveLength(2)
    expect(saveButton(wrapper).attributes('disabled')).toBeDefined()
    wrapper.unmount()
    const saved = await mocks.updateConfig.mock.results[0].value
    const reloaded = await panel(saved)
    expect(reloaded.findComponent(MonitorPlatformManager).props('modelValue')[2]).toEqual(saved.platforms[2])
    reloaded.unmount()
  })

  it('preserves the edited draft on transient failure and retries using the original version', async () => {
    const wrapper = await panel()
    await add(wrapper, 'minimax')
    mocks.updateConfig.mockRejectedValueOnce({ status: 503, message: 'temporarily unavailable' })
    await saveButton(wrapper).trigger('click'); await flushPromises()
    expect(mocks.getConfig).toHaveBeenCalledTimes(1)
    expect(wrapper.findComponent(MonitorPlatformManager).props('modelValue').at(-1)!.platform).toBe('minimax')
    expect(saveButton(wrapper).attributes('disabled')).toBeUndefined()
    await saveButton(wrapper).trigger('click'); await flushPromises()
    expect(mocks.updateConfig.mock.calls[1]![0].version).toBe(2)
    expect(mocks.updateConfig.mock.calls[1]![0].platforms.at(-1).platform).toBe('minimax')
    wrapper.unmount()
  })

  it('preserves edits on 409 and requires explicit reload before using a newer version', async () => {
    const wrapper = await panel()
    await add(wrapper, 'kimi')
    mocks.updateConfig.mockRejectedValueOnce({ status: 409, message: 'version conflict' })
    await saveButton(wrapper).trigger('click'); await flushPromises()
    expect(mocks.getConfig).toHaveBeenCalledTimes(1)
    expect(wrapper.findComponent(MonitorPlatformManager).props('modelValue').at(-1)!.platform).toBe('kimi')
    expect(wrapper.get('[role="alert"]').text()).toContain('channelMonitorV2.settings.saveConflict')
    expect(saveButton(wrapper).attributes('disabled')).toBeDefined()
    mocks.getConfig.mockResolvedValue({ ...config(), version: 9 })
    await wrapper.get('[data-testid="reload-conflict"]').trigger('click'); await flushPromises()
    expect(wrapper.findComponent(MonitorPlatformManager).props('modelValue')).toHaveLength(2)
    expect(wrapper.find('[data-testid="reload-conflict"]').exists()).toBe(false)
    await add(wrapper, 'kimi')
    await saveButton(wrapper).trigger('click'); await flushPromises()
    expect(mocks.updateConfig.mock.calls[1]![0].version).toBe(9)
    wrapper.unmount()
  })

  it('collapses secondary settings and searches groups without losing hidden selections', async () => {
    mocks.groups.mockResolvedValue([{ id: 42, name: 'Original', platform: 'openai' }, { id: 7, name: 'Needle', platform: 'gemini' }])
    const wrapper = await panel()
    expect(wrapper.findAll('details')).toHaveLength(3)
    expect(wrapper.findAll('details').every(item => item.attributes('open') === undefined)).toBe(true)
    const groups = wrapper.findAll('details')[0]!
    await groups.get('input[type="search"]').setValue('Needle')
    expect(groups.findAll('input[type="checkbox"]')).toHaveLength(1)
    await groups.get('input[type="checkbox"]').setValue(true)
    await saveButton(wrapper).trigger('click'); await flushPromises()
    expect(mocks.updateConfig.mock.calls[0]![0].group_ids).toEqual([7, 42])
    expect(mocks.updateConfig.mock.calls[0]![0].ignored_error_categories).toEqual([])
    wrapper.unmount()
  })

  it('starts empty and hides add after every supported platform is configured', async () => {
    const wrapper = await panel({ ...config(), platforms: [] })
    await add(wrapper, 'kimi')
    expect(wrapper.findComponent(MonitorPlatformManager).props('modelValue')).toEqual([{ platform: 'kimi', enabled: true, models: [], model_display_mode: 'all', excluded_models: [] }])
    await wrapper.get('[data-testid="reset-draft"]').trigger('click')
    expect(wrapper.findComponent(MonitorPlatformManager).props('modelValue')).toEqual([])
    wrapper.unmount()
    const full = await panel({ ...config(), platforms: CONCRETE_PLATFORM_OPTIONS.map(({ value }) => ({ platform: value, enabled: true, models: [] })) })
    expect(full.find('[data-testid="open-add-platform"]').exists()).toBe(false)
    full.unmount()
  })
})
