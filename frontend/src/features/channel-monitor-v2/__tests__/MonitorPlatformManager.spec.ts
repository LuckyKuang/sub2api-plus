import { defineComponent, h, ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { MonitorModelCatalog, MonitorModelCatalogItem, MonitorPlatformConfig } from '@/api/channelMonitorV2'
import MonitorPlatformManager from '../MonitorPlatformManager.vue'
import MonitorModelPicker from '../MonitorModelPicker.vue'
import Toggle from '@/components/common/Toggle.vue'

const mocks = vi.hoisted(() => ({ catalog: vi.fn() }))
vi.mock('@/api/channelMonitorV2', () => ({ getModelCatalog: mocks.catalog }))
vi.mock('vue-i18n', async importOriginal => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key }),
}))
const item = (model: string, sources: MonitorModelCatalogItem['sources'] = ['success'], platform = 'custom'): MonitorModelCatalogItem => ({ platform, model, sources, success_requests: 2, error_requests: 0 })
const response = (items: MonitorModelCatalogItem[]): MonitorModelCatalog => ({ config_version: 7, range: '30d', start: '', end: '', items })
const initial = (): MonitorPlatformConfig[] => [{ platform: 'custom', enabled: true, models: ['Alpha'], excluded_models: ['hidden'], model_display_mode: 'selected' }, { platform: 'other-provider', enabled: false, models: [], model_display_mode: 'all', excluded_models: [] }]
async function manager(platforms = initial()) {
  const value = ref(platforms)
  const wrapper = mount(defineComponent({ setup: () => () => h(MonitorPlatformManager, { modelValue: value.value, groupIds: [42], 'onUpdate:modelValue': (next: MonitorPlatformConfig[]) => { value.value = next } }) }), { global: { stubs: { teleport: true } } })
  await flushPromises()
  return { wrapper, value, view: wrapper.findComponent(MonitorPlatformManager) }
}

beforeEach(() => {
  vi.clearAllMocks()
  mocks.catalog.mockResolvedValue(response([item('Alpha'), item('alpha', ['error']), item('hidden'), item('seed-only', ['configured'])]))
})

describe('platform/model card interactions', () => {
  it('loads raw observed members scoped to platform/groups and excludes seeds from Other', async () => {
    const { wrapper, view } = await manager()
    expect(mocks.catalog).toHaveBeenCalledWith('custom', [42], expect.any(AbortSignal))
    await view.get('[data-category="other"]').trigger('click')
    expect(view.findAll('[data-model]').map(row => row.attributes('data-model'))).toEqual(['alpha'])
    await view.get('[data-category="excluded"]').trigger('click')
    expect(view.findAll('[data-model]').map(row => row.attributes('data-model'))).toEqual(['hidden'])
    wrapper.unmount()
  })

  it('moves the last named model into Other, bulk excludes observed members, restores them, and undoes the exact prior change', async () => {
    const { wrapper, view, value } = await manager()
    await view.get('[data-model="Alpha"] [data-action="other"]').trigger('click')
    expect(value.value[0]!.models).toEqual([])
    expect(value.value[0]!.model_display_mode).toBe('selected')
    await view.get('[data-category="other"]').trigger('click')
    expect(view.findAll('[data-model]').map(row => row.attributes('data-model'))).toEqual(['Alpha', 'alpha'])
    await view.get('[data-testid="select-visible-models"]').setValue(true)
    await view.get('[data-batch="exclude"]').trigger('click')
    expect(value.value[0]!.excluded_models).toEqual(['Alpha', 'alpha', 'hidden'])
    expect(view.findAll('[data-model]')).toHaveLength(0)
    await view.get('[data-category="excluded"]').trigger('click')
    await view.get('[data-testid="select-visible-models"]').setValue(true)
    await view.get('[data-batch="restore"]').trigger('click')
    expect(value.value[0]!.excluded_models).toEqual([])
    await view.get('[data-testid="undo-model-change"]').trigger('click')
    expect(value.value[0]!.excluded_models).toEqual(['Alpha', 'alpha', 'hidden'])
    expect(value.value[1]).toEqual(initial()[1])
    wrapper.unmount()
  })

  it('searches and selects current results without excluding nonmatching models', async () => {
    const { wrapper, view, value } = await manager([{ ...initial()[0]!, models: ['Alpha', 'Bravo', 'charlie'] }])
    await view.get('input[aria-label="channelMonitorV2.settings.searchModels"]').setValue('brav')
    await view.get('[data-testid="select-visible-models"]').setValue(true)
    await view.get('[data-batch="exclude"]').trigger('click')
    expect(value.value[0]!.models).toEqual(['Alpha', 'charlie'])
    expect(value.value[0]!.excluded_models).toEqual(['Bravo', 'hidden'])
    wrapper.unmount()
  })

  it('allows pausing and removing unknown saved platforms and undo restores their exact rules', async () => {
    const { wrapper, view, value } = await manager()
    view.findComponent(Toggle).vm.$emit('update:modelValue', false)
    await flushPromises()
    expect(value.value[0]!.enabled).toBe(false)
    await view.get('[data-testid="remove-platform"]').trigger('click')
    expect(value.value.map(platform => platform.platform)).toEqual(['other-provider'])
    await view.get('[data-testid="undo-model-change"]').trigger('click')
    expect(value.value[0]).toEqual({ ...initial()[0], enabled: false })
    wrapper.unmount()
  })

  it('tags zero-traffic seeds truthfully and ignores stale catalog responses after switching platforms', async () => {
    let resolveFirst!: (value: MonitorModelCatalog) => void
    mocks.catalog.mockReturnValueOnce(new Promise(resolve => { resolveFirst = resolve }))
    mocks.catalog.mockResolvedValueOnce(response([item('real-traffic', ['error'], 'other-provider'), item('configured-only', ['configured'], 'other-provider')]))
    const { wrapper, view } = await manager()
    await view.get('[data-platform="other-provider"]').trigger('click')
    await flushPromises()
    resolveFirst(response([item('wrong-platform', ['success'], 'other-provider')]))
    await flushPromises()
    expect(view.findAll('[data-model]').map(row => row.attributes('data-model'))).toEqual(['real-traffic'])
    await view.get('[data-mode="selected"]').trigger('click')
    await view.get('[data-testid="open-model-picker"]').trigger('click')
    expect(view.findComponent(MonitorModelPicker).props('candidates')).toEqual([{ model: 'configured-only', source: 'configured' }, { model: 'real-traffic', source: 'observed' }])
    wrapper.unmount()
  })

  it('shows a catalog failure and retries without making incomplete observations appear authoritative', async () => {
    mocks.catalog.mockRejectedValueOnce(new Error('failed'))
    const { wrapper, view, value } = await manager()
    expect(view.get('[role="alert"]').text()).toContain('channelMonitorV2.settings.catalogFailed')
    await view.get('[role="alert"] button').trigger('click'); await flushPromises()
    expect(view.find('[role="alert"]').exists()).toBe(false)
    expect(value.value).toEqual(initial())
    wrapper.unmount()
  })
})
