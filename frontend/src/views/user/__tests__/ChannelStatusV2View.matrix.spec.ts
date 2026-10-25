import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, shallowMount } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import type { MonitorCoverage, MonitorMatrixResponse, MonitorMatrixRow, MonitorSnapshot } from '@/api/channelMonitorV2'

const navigation = vi.hoisted(() => ({ query: { group_by: 'platform' }, replace: vi.fn() }))
vi.mock('vue-router', () => ({ useRoute: () => ({ query: navigation.query }), useRouter: () => ({ replace: navigation.replace }) }))
vi.mock('vue-i18n', async (original) => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key, te: () => true, locale: { value: 'en' } }) }))
vi.mock('@/composables/useUserView', () => ({ useUserView: () => ({ isAdmin: false }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn() }) }))
vi.mock('@/utils/featureFlags', () => ({ isChannelMonitorThroughputHidden: () => false, isChannelMonitorUserRankingHidden: () => false }))
vi.mock('@/api/channelMonitorV2', () => ({
  getDimensions: vi.fn(), getSnapshot: vi.fn(), getMatrix: vi.fn(), getModels: vi.fn(), getErrors: vi.fn(), getUsers: vi.fn(),
}))
import * as api from '@/api/channelMonitorV2'
import ChannelStatusV2View from '../ChannelStatusV2View.vue'

const coverage: MonitorCoverage = {
  requested_start: '2026-08-01T00:00:00Z', requested_end: '2026-08-01T00:18:00Z',
  coverage_start: '2026-08-01T00:00:00Z', data_through: '2026-08-01T00:18:00Z',
  computed_at: '2026-08-01T00:18:00Z', aggregation_lag_seconds: 0, coverage_complete: true, bucket_seconds: 60,
}
const metric = {
  error_rate: 0.1, cache_rate: 0.5, rpm: 1, tpm: 60,
  ttft: { p50_ms: 100, p95_ms: 300, avg_ms: 150 },
}
const platform = { platform: 'openai', metrics: metric, health: {}, buckets: [] } as unknown as MonitorMatrixRow
const group = { ...platform, group_id: 7, group_name: 'group-seven' }
const response = (groupBy: MonitorMatrixResponse['group_by'], items: MonitorMatrixRow[]) => ({ coverage, group_by: groupBy, items })
const MatrixStub = defineComponent({
  name: 'RelayPulseMatrix', props: ['rows', 'coverage', 'dimensionKey', 'viewState'], emits: ['update:viewState'],
  setup: () => () => h('div', { 'data-testid': 'matrix' }),
})
const AppLayoutStub = defineComponent({
  name: 'AppLayout', props: { workspace: Boolean }, template: '<main><slot /></main>',
})
function mountView() {
  return shallowMount(ChannelStatusV2View, { global: { stubs: {
    AppLayout: AppLayoutStub, RelayPulseMatrix: MatrixStub,
  } } })
}
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: unknown) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
enableAutoUnmount(afterEach)
beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(api.getDimensions).mockResolvedValue({ platforms: [], groups: [], models: [] })
  vi.mocked(api.getSnapshot).mockResolvedValue({ coverage, metrics: metric, health: {}, trend: [], config: { refresh_interval_seconds: 300 } } as MonitorSnapshot)
  vi.mocked(api.getMatrix).mockResolvedValue(response('platform', [platform]))
  vi.mocked(api.getModels).mockResolvedValue({ items: [] } as Awaited<ReturnType<typeof api.getModels>>)
})

describe('ChannelStatusV2 isolated activity layout', () => {
  // Acceptance: transparent pinned controls and scrollable records occupy
  // separate sibling boxes, including after detail-tab/view reconstruction.
  it('keeps the toolbar outside the activity viewport through details and chart switches', async () => {
    vi.mocked(api.getErrors).mockResolvedValue({ items: [] } as Awaited<ReturnType<typeof api.getErrors>>)
    vi.mocked(api.getUsers).mockResolvedValue({ items: [] } as Awaited<ReturnType<typeof api.getUsers>>)
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.getComponent(AppLayoutStub).props('workspace')).toBe(true)
    const toolbar = wrapper.get('.channel-monitor-toolbar')
    const activity = wrapper.get('[data-testid="channel-status-activity"]')
    expect(toolbar.element.parentElement).toBe(activity.element.parentElement)
    expect(activity.element.contains(toolbar.element)).toBe(false)
    expect(toolbar.classes()).toEqual(expect.arrayContaining(['workspace-fixed', 'workspace-shell']))
    expect(toolbar.classes()).not.toContain('sticky')
    expect(activity.classes()).toContain('workspace-scroll')

    const modelViewport = activity.get('.record-table-scroll')
    expect(modelViewport.classes()).toEqual(expect.arrayContaining(['monitor-tab-viewport', 'overflow-auto']))
    expect(modelViewport.element.parentElement?.classList.contains('overflow-auto')).toBe(false)
    const details = wrapper.findAll('[role="tab"]')
    await details.find(button => button.text() === 'channelMonitorV2.tabs.users')!.trigger('click')
    await flushPromises()
    expect(activity.get('.record-table-scroll').classes()).toContain('monitor-tab-viewport')
    await details.find(button => button.text() === 'channelMonitorV2.tabs.errors')!.trigger('click')
    await flushPromises()
    expect(activity.get('.monitor-tab-viewport').classes()).toContain('overflow-auto')
    expect(activity.find('.record-table-scroll').exists()).toBe(false)

    await wrapper.findAll('button').find(button => button.text() === 'channelMonitorV2.trendView.line')!.trigger('click')
    expect(activity.findComponent({ name: 'MonitorTrendChart' }).exists()).toBe(true)
    expect(wrapper.get('.channel-monitor-toolbar').element).toBe(toolbar.element)
    expect(wrapper.get('[data-testid="channel-status-activity"]').element).toBe(activity.element)
  })
})

describe('ChannelStatusV2 matrix payload ownership', () => {
  it('keeps the old payload visible under its own grouping while the selected grouping request is pending', async () => {
    const wrapper = mountView()
    await flushPromises()
    const matrix = wrapper.getComponent(MatrixStub)
    expect(matrix.props('rows')).toEqual([platform])
    const previousDimension = matrix.props('dimensionKey')
    const pending = deferred<MonitorMatrixResponse>()
    vi.mocked(api.getMatrix).mockReturnValueOnce(pending.promise)
    const grouping = wrapper.findAllComponents({ name: 'Select' }).find((control) => control.props('modelValue') === 'platform')!
    grouping.vm.$emit('update:modelValue', 'platform_group')
    await flushPromises()
    expect(api.getMatrix).toHaveBeenLastCalledWith(expect.anything(), 'platform_group', false, expect.any(AbortSignal))
    expect(matrix.props('rows')).toEqual([platform])
    expect(matrix.props('dimensionKey')).toBe(previousDimension)
    pending.resolve(response('platform_group', [platform, group]))
    await flushPromises()
    expect(matrix.props('rows')).toEqual([group])
    expect(matrix.props('dimensionKey')).not.toBe(previousDimension)
  })

  it('retains the previous matrix when the replacement grouping fails', async () => {
    const wrapper = mountView()
    await flushPromises()
    vi.mocked(api.getMatrix).mockRejectedValueOnce(new Error('offline'))
    const grouping = wrapper.findAllComponents({ name: 'Select' }).find((control) => control.props('modelValue') === 'platform')!
    grouping.vm.$emit('update:modelValue', 'platform_group_model')
    await flushPromises()
    expect(wrapper.getComponent(MatrixStub).props('rows')).toEqual([platform])
  })

  it('owns the matrix viewport state across pulse/line reconstruction and ordinary refresh', async () => {
    const wrapper = mountView()
    await flushPromises()
    const matrix = wrapper.getComponent(MatrixStub)
    const viewport = { zoom: { span: 0.4, offset: 0.2 }, scrollTop: 7200, scrollLeft: 40, domainKey: matrix.props('dimensionKey') }
    matrix.vm.$emit('update:viewState', viewport)
    await flushPromises()
    const buttons = () => wrapper.findAll('button')
    await buttons().find((button) => button.text() === 'channelMonitorV2.trendView.line')!.trigger('click')
    expect(wrapper.findComponent(MatrixStub).exists()).toBe(false)
    await buttons().find((button) => button.text() === 'channelMonitorV2.trendView.pulse')!.trigger('click')
    expect(wrapper.getComponent(MatrixStub).props('viewState')).toEqual(viewport)
    const previousDimension = wrapper.getComponent(MatrixStub).props('dimensionKey')
    await wrapper.get('button[title="common.refresh"]').trigger('click')
    await flushPromises()
    expect(wrapper.getComponent(MatrixStub).props('viewState')).toEqual(viewport)
    expect(wrapper.getComponent(MatrixStub).props('dimensionKey')).toBe(previousDimension)
  })
})
